// Package denoms is the denoms set of the state sync: it rewrites
// explorer.denoms from the local node's bank, tokenfactory and tradebin
// queries. Supply and metadata always come from the node; the explorer never
// computes them from events.
package denoms

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tokenfactorytypes "github.com/bze-alphateam/bze/x/tokenfactory/types"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
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

// Denom is one explorer.denoms row as the node describes it; first sight
// (created_*) and updated_at are the store's, and the columns of the
// holders, prices and IBC origin are left alone. Empty strings are stored as
// NULL.
type Denom struct {
	Denom       string
	Symbol      string
	Name        string
	Exponent    int
	Description string
	Kind        string
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

	all := slices.Sorted(maps.Keys(supply))
	for d := range metadata {
		if _, ok := supply[d]; !ok {
			all = append(all, d)
		}
	}
	slices.Sort(all)
	snap := Snapshot{Full: true}
	for _, d := range all {
		row, err := s.row(ctx, d, metadata[d], supply[d], markets, halted)
		if err != nil {
			return err
		}
		snap.Denoms = append(snap.Denoms, row)
	}
	if err := s.deps.Store.Save(ctx, snap); err != nil {
		return err
	}
	s.learn(true, all...)
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
	row, err := s.row(ctx, key, md, sup.Amount.Amount.String(), markets, halted)
	if err != nil {
		return err
	}
	if err := s.deps.Store.Save(ctx, Snapshot{Denoms: []Denom{row}}); err != nil {
		return err
	}
	s.learn(false, key)
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

// row maps one denom, querying the admin of a factory denom.
func (s *Set) row(ctx context.Context, denom string, md *banktypes.Metadata, supply string,
	markets map[string][]string, halted map[string]bool) (Denom, error) {
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
	return row, nil
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
