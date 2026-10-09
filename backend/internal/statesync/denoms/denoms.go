// Package denoms is the denoms set of the state sync: it rewrites
// explorer.denoms from the local node's bank, tokenfactory and tradebin
// queries. Supply and metadata always come from the node; the explorer never
// computes them from events.
//
// An IBC denom is resolved through the node's denom trace (path and base
// denom), the IBC channel of the path's first hop (its counterparty chain)
// and the chain registry cache (the asset's symbol, name, exponent and logo:
// they win over the bank metadata, which for an IBC voucher is the
// placeholder ibc-go writes on first receipt, exponent 0 and the base
// denom upper-cased as symbol). A multi-hop denom is followed through the
// first hop chain's registry asset and its traces to its home chain; when
// that fails, the first hop's chain and the raw base denom stay. A chain or
// an asset missing from the cache is asked of the chain_registry set.
package denoms

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tokenfactorytypes "github.com/bze-alphateam/bze/x/tokenfactory/types"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainregistry"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultInterval is the safety-net period of the set.
const DefaultInterval = time.Hour

// PageLimit is the page size of the list queries.
const PageLimit = 1000

// Kinds of denoms, as the kind column stores them.
const (
	KindNative  = "native"
	KindFactory = "factory"
	KindIBC     = "ibc"
	KindLP      = "lp"
	KindUnknown = "unknown"
)

// Kind classifies a base denom by its shape: the native denom, a
// tokenfactory denom, an IBC voucher or a tradebin liquidity-pool share
// (ulp/<hash> since chain v8.2.0, ulp_<base>_<quote> before).
func Kind(denom string) string {
	switch {
	case denom == chain.BondDenom:
		return KindNative
	case strings.HasPrefix(denom, "factory/"):
		return KindFactory
	case strings.HasPrefix(denom, "ibc/"):
		return KindIBC
	case strings.HasPrefix(denom, "ulp/"), strings.HasPrefix(denom, "ulp_"):
		return KindLP
	default:
		return KindUnknown
	}
}

// seenPrefix starts the keys of denoms the indexer saw move in a block. The
// set resyncs such a denom only when it does not hold it yet, so a new IBC
// denom appears within a block without every ubze transfer costing a
// resync.
const seenPrefix = "seen:"

// SeenKey is the denoms-set key of a denom seen in a block's coins.
func SeenKey(denom string) string {
	if denom == "" {
		return ""
	}
	return seenPrefix + denom
}

// Bank is the part of the bank query client the set uses;
// banktypes.QueryClient satisfies it.
type Bank interface {
	TotalSupply(ctx context.Context, in *banktypes.QueryTotalSupplyRequest, opts ...grpc.CallOption) (*banktypes.QueryTotalSupplyResponse, error)
	SupplyOf(ctx context.Context, in *banktypes.QuerySupplyOfRequest, opts ...grpc.CallOption) (*banktypes.QuerySupplyOfResponse, error)
	DenomsMetadata(ctx context.Context, in *banktypes.QueryDenomsMetadataRequest, opts ...grpc.CallOption) (*banktypes.QueryDenomsMetadataResponse, error)
	DenomMetadataByQueryString(ctx context.Context, in *banktypes.QueryDenomMetadataByQueryStringRequest, opts ...grpc.CallOption) (*banktypes.QueryDenomMetadataByQueryStringResponse, error)
}

// TokenFactory is the part of the tokenfactory query client the set uses;
// tokenfactorytypes.QueryClient satisfies it.
type TokenFactory interface {
	DenomAuthority(ctx context.Context, in *tokenfactorytypes.QueryDenomAuthorityRequest, opts ...grpc.CallOption) (*tokenfactorytypes.QueryDenomAuthorityResponse, error)
}

// Tradebin is the part of the tradebin query client the set uses;
// tradebintypes.QueryClient satisfies it.
type Tradebin interface {
	AllMarkets(ctx context.Context, in *tradebintypes.QueryAllMarketsRequest, opts ...grpc.CallOption) (*tradebintypes.QueryAllMarketsResponse, error)
	HaltedDenoms(ctx context.Context, in *tradebintypes.QueryHaltedDenomsRequest, opts ...grpc.CallOption) (*tradebintypes.QueryHaltedDenomsResponse, error)
}

// Transfer is the part of the IBC transfer query client the set uses;
// ibctransfertypes.QueryClient satisfies it.
type Transfer interface {
	DenomTrace(ctx context.Context, in *ibctransfertypes.QueryDenomTraceRequest, opts ...grpc.CallOption) (*ibctransfertypes.QueryDenomTraceResponse, error)
}

// RegistryAsset is a cached chain-registry asset.
type RegistryAsset struct {
	Symbol   string
	Name     string
	Exponent *int
	LogoURL  string
	Traces   json.RawMessage
}

// Lookup reads what other sets keep: the IBC channels and the chain
// registry cache.
type Lookup interface {
	// ChannelChains maps the transfer channels to their counterparty chain
	// ids; a channel whose chain is not known yet is absent.
	ChannelChains(ctx context.Context) (map[string]string, error)
	// Asset returns the registry asset base of chainID; nil when the cache
	// has none.
	Asset(ctx context.Context, chainID, base string) (*RegistryAsset, error)
	// ChainID returns the chain id of the registry directory name; "" when
	// the cache has none.
	ChainID(ctx context.Context, registryName string) (string, error)
}

// Denom is one explorer.denoms row as the node describes it; first sight
// (created_*) and updated_at are the store's, and the columns of the
// holders and prices are left alone. Empty strings are stored as NULL.
type Denom struct {
	Denom       string
	Symbol      string
	Name        string
	Exponent    int
	Description string
	Kind        string
	// OriginChainID, IBCBaseDenom and IBCPath are an IBC denom's.
	OriginChainID string
	IBCBaseDenom  string
	IBCPath       string
	// Creator is the address in a factory denom.
	Creator string
	// Admin is the factory denom's admin; empty once renounced.
	Admin   string
	LogoURL string
	// Supply is the bank total supply in base units, "0" when the bank
	// holds none.
	Supply  string
	Halted  bool
	Markets []string
	// Metadata is the bank metadata as the node returns it; nil without.
	Metadata json.RawMessage
}

// Snapshot is one write of the set.
type Snapshot struct {
	// Denoms are upserted.
	Denoms []Denom
	// Full zeroes the supply, markets and halt of every stored denom absent
	// from Denoms: the bank holds none of it any more.
	Full bool
}

// Store persists the set.
type Store interface {
	// Save writes a snapshot in one transaction.
	Save(ctx context.Context, s Snapshot) error
}

// Deps of the set.
type Deps struct {
	Bank         Bank
	TokenFactory TokenFactory
	Tradebin     Tradebin
	Store        Store
	// Transfer traces IBC denoms; nil leaves them unresolved.
	Transfer Transfer
	// Lookup resolves the origin of traced IBC denoms; nil stops at the
	// trace.
	Lookup Lookup
	// Misses is told the chains whose registry cache an IBC denom missed;
	// nil drops them.
	Misses statesync.Publisher
}

// Set is the denoms set.
type Set struct {
	deps     Deps
	interval time.Duration
	mu       sync.Mutex

	// known are the stored denoms, for Canonical.
	knownMu sync.RWMutex
	known   map[string]bool
}

// New returns the set; interval zero is DefaultInterval.
func New(deps Deps, interval time.Duration) *Set {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Set{deps: deps, interval: interval}
}

var (
	_ statesync.Set           = (*Set)(nil)
	_ statesync.Canonicaliser = (*Set)(nil)
)

// Name is statesync.Denoms.
func (s *Set) Name() string { return statesync.Denoms }

// Interval is the safety-net period.
func (s *Set) Interval() time.Duration { return s.interval }

// Canonical turns the key of a denom seen in a block (SeenKey) into the
// denom when the set does not hold it yet, and into "" (no resync) when it
// does; other keys are returned as they are. Before the first full resync
// the set knows nothing yet and keeps the key: the syncer folds the queue
// again after the start resync.
func (s *Set) Canonical(key string) string {
	denom, seen := strings.CutPrefix(key, seenPrefix)
	if !seen {
		return key
	}
	s.knownMu.RLock()
	defer s.knownMu.RUnlock()
	if s.known == nil {
		return key
	}
	if s.known[denom] {
		return ""
	}
	return denom
}

// learn records stored denoms, replacing the whole set when full. A single
// resync before the first full one teaches nothing: the set would take every
// other denom for unknown.
func (s *Set) learn(full bool, denoms ...string) {
	s.knownMu.Lock()
	defer s.knownMu.Unlock()
	if full {
		s.known = map[string]bool{}
	}
	if s.known == nil {
		return
	}
	for _, d := range denoms {
		s.known[d] = true
	}
}

// FullResync reads every denom the bank knows (by supply or by metadata),
// with the factory admins, the markets and the halted denoms, and rewrites
// the table.
func (s *Set) FullResync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	supply := map[string]string{}
	var next []byte
	for {
		resp, err := s.deps.Bank.TotalSupply(ctx, &banktypes.QueryTotalSupplyRequest{
			Pagination: &query.PageRequest{Key: next, Limit: PageLimit},
		})
		if err != nil {
			return fmt.Errorf("bank TotalSupply: %w", err)
		}
		for _, c := range resp.Supply {
			supply[c.Denom] = c.Amount.String()
		}
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			break
		}
		next = resp.Pagination.NextKey
	}

	metadata := map[string]*banktypes.Metadata{}
	next = nil
	for {
		resp, err := s.deps.Bank.DenomsMetadata(ctx, &banktypes.QueryDenomsMetadataRequest{
			Pagination: &query.PageRequest{Key: next, Limit: PageLimit},
		})
		if err != nil {
			return fmt.Errorf("bank DenomsMetadata: %w", err)
		}
		for i := range resp.Metadatas {
			metadata[resp.Metadatas[i].Base] = &resp.Metadatas[i]
		}
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			break
		}
		next = resp.Pagination.NextKey
	}

	markets, err := s.markets(ctx)
	if err != nil {
		return err
	}
	halted, err := s.halted(ctx)
	if err != nil {
		return err
	}

	origins, err := s.origins(ctx)
	if err != nil {
		return err
	}

	all := slices.Sorted(maps.Keys(supply))
	for d := range metadata {
		if _, ok := supply[d]; !ok {
			all = append(all, d)
		}
	}
	slices.Sort(all)
	snap := Snapshot{Full: true}
	for _, d := range all {
		row, err := s.row(ctx, d, metadata[d], supply[d], markets, halted, origins)
		if err != nil {
			return err
		}
		snap.Denoms = append(snap.Denoms, row)
	}
	if err := s.deps.Store.Save(ctx, snap); err != nil {
		return err
	}
	s.learn(true, all...)
	origins.publish(s.deps.Misses)
	return nil
}

// ResyncOne rewrites the denom key.
func (s *Set) ResyncOne(ctx context.Context, key string) error {
	if key == statesync.All {
		return s.FullResync(ctx)
	}
	if denom, seen := strings.CutPrefix(key, seenPrefix); seen {
		key = denom
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var md *banktypes.Metadata
	resp, err := s.deps.Bank.DenomMetadataByQueryString(ctx, &banktypes.QueryDenomMetadataByQueryStringRequest{Denom: key})
	switch {
	case status.Code(err) == codes.NotFound:
	case err != nil:
		return fmt.Errorf("bank DenomMetadata %s: %w", key, err)
	default:
		md = &resp.Metadata
	}
	sup, err := s.deps.Bank.SupplyOf(ctx, &banktypes.QuerySupplyOfRequest{Denom: key})
	if err != nil {
		return fmt.Errorf("bank SupplyOf %s: %w", key, err)
	}
	markets, err := s.markets(ctx)
	if err != nil {
		return err
	}
	halted, err := s.halted(ctx)
	if err != nil {
		return err
	}
	origins, err := s.origins(ctx)
	if err != nil {
		return err
	}
	row, err := s.row(ctx, key, md, sup.Amount.Amount.String(), markets, halted, origins)
	if err != nil {
		return err
	}
	if err := s.deps.Store.Save(ctx, Snapshot{Denoms: []Denom{row}}); err != nil {
		return err
	}
	s.learn(false, key)
	origins.publish(s.deps.Misses)
	return nil
}

// markets returns the tradebin market ids (base/quote) by denom, sorted.
func (s *Set) markets(ctx context.Context) (map[string][]string, error) {
	out := map[string][]string{}
	var next []byte
	for {
		resp, err := s.deps.Tradebin.AllMarkets(ctx, &tradebintypes.QueryAllMarketsRequest{
			Pagination: &query.PageRequest{Key: next, Limit: PageLimit},
		})
		if err != nil {
			return nil, fmt.Errorf("tradebin AllMarkets: %w", err)
		}
		for _, m := range resp.Market {
			id := tradebintypes.CreateMarketId(m.Base, m.Quote)
			out[m.Base] = append(out[m.Base], id)
			out[m.Quote] = append(out[m.Quote], id)
		}
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			break
		}
		next = resp.Pagination.NextKey
	}
	for d := range out {
		slices.Sort(out[d])
		out[d] = slices.Compact(out[d])
	}
	return out, nil
}

// halted returns the denoms halted on the DEX. A node before chain v8.2.0
// has no halt and answers Unimplemented: nothing is halted.
func (s *Set) halted(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	var next []byte
	for {
		resp, err := s.deps.Tradebin.HaltedDenoms(ctx, &tradebintypes.QueryHaltedDenomsRequest{
			Pagination: &query.PageRequest{Key: next, Limit: PageLimit},
		})
		if status.Code(err) == codes.Unimplemented {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("tradebin HaltedDenoms: %w", err)
		}
		for _, d := range resp.Denoms {
			out[d] = true
		}
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			return out, nil
		}
		next = resp.Pagination.NextKey
	}
}

// row maps one denom, querying the admin of a factory denom and the trace
// of an IBC denom.
func (s *Set) row(ctx context.Context, denom string, md *banktypes.Metadata, supply string,
	markets map[string][]string, halted map[string]bool, origins *origins) (Denom, error) {
	row := Denom{
		Denom:   denom,
		Kind:    Kind(denom),
		Supply:  supply,
		Halted:  halted[denom],
		Markets: markets[denom],
	}
	if row.Supply == "" {
		row.Supply = "0"
	}
	if row.Markets == nil {
		row.Markets = []string{}
	}
	if md != nil {
		applyMetadata(&row, md)
		raw, err := json.Marshal(md)
		if err != nil {
			return Denom{}, fmt.Errorf("denom %s: metadata: %w", denom, err)
		}
		row.Metadata = raw
	} else if denom == chain.BondDenom {
		row.Symbol, row.Name, row.Exponent = chain.BondDenomSymbol, chain.BondDenomName, chain.BondDenomExponent
	}
	if row.Kind == KindFactory {
		if parts := strings.SplitN(denom, "/", 3); len(parts) == 3 {
			row.Creator = parts[1]
		}
		resp, err := s.deps.TokenFactory.DenomAuthority(ctx, &tokenfactorytypes.QueryDenomAuthorityRequest{Denom: denom})
		switch {
		case status.Code(err) == codes.NotFound:
		case err != nil:
			return Denom{}, fmt.Errorf("tokenfactory DenomAuthority %s: %w", denom, err)
		case resp.DenomAuthority != nil:
			row.Admin = resp.DenomAuthority.Admin
		}
	}
	if row.Kind == KindIBC && s.deps.Transfer != nil {
		if err := s.ibc(ctx, &row, origins); err != nil {
			return Denom{}, err
		}
	}
	return row, nil
}

// origins is what one run resolves IBC denoms with: the channels' chains,
// and the registry keys the cache missed.
type origins struct {
	lookup Lookup
	chains map[string]string
	misses statesync.Dirty
}

// origins reads the channels' chains for one run; nil without a Lookup.
func (s *Set) origins(ctx context.Context) (*origins, error) {
	if s.deps.Lookup == nil {
		return nil, nil
	}
	chains, err := s.deps.Lookup.ChannelChains(ctx)
	if err != nil {
		return nil, err
	}
	return &origins{lookup: s.deps.Lookup, chains: chains}, nil
}

// miss asks the chain_registry set for key (a chain id or a
// RegistryNameKey).
func (o *origins) miss(key string) {
	o.misses.Mark(statesync.ChainRegistry, key)
}

func (o *origins) publish(p statesync.Publisher) {
	if o != nil && p != nil && !o.misses.Empty() {
		p.Publish(o.misses)
	}
}

// ibc fills an IBC denom's trace, origin and display fields from the
// registry asset.
func (s *Set) ibc(ctx context.Context, row *Denom, o *origins) error {
	hash := strings.TrimPrefix(row.Denom, "ibc/")
	resp, err := s.deps.Transfer.DenomTrace(ctx, &ibctransfertypes.QueryDenomTraceRequest{Hash: hash})
	switch {
	case status.Code(err) == codes.NotFound:
		return nil
	case err != nil:
		return fmt.Errorf("transfer DenomTrace %s: %w", row.Denom, err)
	case resp.DenomTrace == nil:
		return nil
	}
	trace := resp.DenomTrace
	row.IBCPath, row.IBCBaseDenom = trace.Path, trace.BaseDenom
	if o == nil {
		return nil
	}
	hops := channels(trace.Path)
	if len(hops) == 0 {
		return nil
	}
	first := o.chains[hops[0]]
	if first == "" {
		return nil // the channel's chain is not known until the channels sync fills it
	}
	row.OriginChainID = first
	asset, err := s.originAsset(ctx, row, trace, first, o)
	if err != nil || asset == nil {
		return err
	}
	if asset.Symbol != "" {
		row.Symbol = asset.Symbol
	}
	if asset.Name != "" {
		row.Name = asset.Name
	}
	if asset.LogoURL != "" {
		row.LogoURL = asset.LogoURL
	}
	if asset.Exponent != nil {
		row.Exponent = *asset.Exponent
	}
	return nil
}

// originAsset returns the registry asset of a traced denom whose first hop
// is the chain first: the asset itself for one hop; for several, the first
// hop chain's asset for the rest of the path leads, through its traces, to
// the home chain, which then becomes the origin. nil when the cache has
// none (and the missing chain is asked for).
func (s *Set) originAsset(ctx context.Context, row *Denom, trace *ibctransfertypes.DenomTrace, first string, o *origins) (*RegistryAsset, error) {
	if len(channels(trace.Path)) == 1 {
		asset, err := o.lookup.Asset(ctx, first, trace.BaseDenom)
		if asset == nil && err == nil {
			o.miss(first)
		}
		return asset, err
	}
	_, rest, _ := strings.Cut(trace.Path, "/")
	_, rest, _ = strings.Cut(rest, "/")
	via, err := o.lookup.Asset(ctx, first, IBCDenom(rest+"/"+trace.BaseDenom))
	if err != nil {
		return nil, err
	}
	if via == nil {
		o.miss(first)
		return nil, nil
	}
	name, base, ok := chainregistry.Home(via.Traces)
	if !ok {
		return nil, nil
	}
	home, err := o.lookup.ChainID(ctx, name)
	if err != nil {
		return nil, err
	}
	if home == "" {
		o.miss(statesync.RegistryNameKey(name))
		return nil, nil
	}
	asset, err := o.lookup.Asset(ctx, home, base)
	if err != nil {
		return nil, err
	}
	if asset == nil {
		o.miss(home)
		return nil, nil
	}
	row.OriginChainID, row.IBCBaseDenom = home, base
	return asset, nil
}

// channels returns the channel ids of an IBC path (port/channel pairs:
// transfer/channel-0/transfer/channel-169), the first hop first; nil for a
// path that is not pairs.
func channels(path string) []string {
	parts := strings.Split(path, "/")
	if path == "" || len(parts)%2 != 0 {
		return nil
	}
	out := make([]string, 0, len(parts)/2)
	for i := 1; i < len(parts); i += 2 {
		out = append(out, parts[i])
	}
	return out
}

// IBCDenom is the voucher denom of a full IBC path with its base denom
// (transfer/channel-3/uusdc): ibc/ and the upper-case hex SHA-256 of it.
func IBCDenom(fullPath string) string {
	sum := sha256.Sum256([]byte(fullPath))
	return "ibc/" + strings.ToUpper(hex.EncodeToString(sum[:]))
}

// applyMetadata fills the display fields from bank metadata: the exponent is
// the display unit's (0 when the display unit is not listed).
func applyMetadata(row *Denom, md *banktypes.Metadata) {
	row.Symbol = md.Symbol
	row.Name = md.Name
	row.Description = md.Description
	row.LogoURL = md.URI
	for _, u := range md.DenomUnits {
		if u != nil && u.Denom == md.Display {
			row.Exponent = int(u.Exponent)
		}
	}
}
