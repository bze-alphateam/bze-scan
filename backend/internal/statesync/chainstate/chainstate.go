// Package chainstate is the chain_state set of the state sync: every minute
// it reads the chain-wide numbers no event carries (the staking pool, the
// supply, inflation, the community pool, the parameters the home page and
// the validators header use, the local node's status) and overwrites one
// explorer.chain_state row per key, with the height the node answered at.
// Every gRPC read of a run is pinned to the height /status reported, so the
// numbers of one run agree with each other. A key that fails keeps its
// previous row; the others are written.
package chainstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultInterval is the minute refresh.
const DefaultInterval = time.Minute

// Keys of the chain_state rows.
const (
	KeyStakingPool     = "staking_pool"
	KeySupply          = "supply"
	KeyMint            = "mint"
	KeyCommunityPool   = "community_pool"
	KeyChainParams     = "chain_params"
	KeyValidatorCounts = "validator_counts"
	KeyNodeStatus      = "node_status"
)

// PriceKey is the key of a denom's price row (price_ubze).
func PriceKey(denom string) string { return "price_" + denom }

// Node is the local node's RPC; *node.Client satisfies it.
type Node interface {
	Status(ctx context.Context) (*node.Status, []byte, error)
}

// Staking is the part of the staking query client the set uses;
// stakingtypes.QueryClient satisfies it.
type Staking interface {
	Pool(ctx context.Context, in *stakingtypes.QueryPoolRequest, opts ...grpc.CallOption) (*stakingtypes.QueryPoolResponse, error)
	Params(ctx context.Context, in *stakingtypes.QueryParamsRequest, opts ...grpc.CallOption) (*stakingtypes.QueryParamsResponse, error)
}

// Bank is the part of the bank query client the set uses;
// banktypes.QueryClient satisfies it.
type Bank interface {
	SupplyOf(ctx context.Context, in *banktypes.QuerySupplyOfRequest, opts ...grpc.CallOption) (*banktypes.QuerySupplyOfResponse, error)
}

// Mint is the part of the mint query client the set uses;
// minttypes.QueryClient satisfies it.
type Mint interface {
	Inflation(ctx context.Context, in *minttypes.QueryInflationRequest, opts ...grpc.CallOption) (*minttypes.QueryInflationResponse, error)
	AnnualProvisions(ctx context.Context, in *minttypes.QueryAnnualProvisionsRequest, opts ...grpc.CallOption) (*minttypes.QueryAnnualProvisionsResponse, error)
	Params(ctx context.Context, in *minttypes.QueryParamsRequest, opts ...grpc.CallOption) (*minttypes.QueryParamsResponse, error)
}

// Distribution is the part of the distribution query client the set uses;
// distrtypes.QueryClient satisfies it.
type Distribution interface {
	CommunityPool(ctx context.Context, in *distrtypes.QueryCommunityPoolRequest, opts ...grpc.CallOption) (*distrtypes.QueryCommunityPoolResponse, error)
	Params(ctx context.Context, in *distrtypes.QueryParamsRequest, opts ...grpc.CallOption) (*distrtypes.QueryParamsResponse, error)
}

// JSON marshals a proto message to proto JSON; *chain.Codec satisfies it.
type JSON interface {
	ProtoJSON(msg gogoproto.Message) ([]byte, error)
}

// Row is one chain_state row.
type Row struct {
	Key    string
	Value  json.RawMessage
	Height int64
}

// ValidatorCounts is the validator_counts row: validators in the active
// set, jailed ones and every known one.
type ValidatorCounts struct {
	Bonded int64 `json:"bonded"`
	Jailed int64 `json:"jailed"`
	Total  int64 `json:"total"`
}

// Price is the price row of a denom; both null until the prices job ran.
type Price struct {
	PriceUSD          *string `json:"price_usd"`
	PriceChange24hPct *string `json:"price_change_24h_pct"`
}

// Store persists the set and reads what the explorer tables already hold.
type Store interface {
	ValidatorCounts(ctx context.Context) (ValidatorCounts, error)
	Price(ctx context.Context, denom string) (Price, error)
	// Save overwrites the rows, in one transaction.
	Save(ctx context.Context, rows []Row) error
}

// Deps of the set.
type Deps struct {
	Node         Node
	Staking      Staking
	Bank         Bank
	Mint         Mint
	Distribution Distribution
	JSON         JSON
	Store        Store
	// Denom is the native denom: its supply and its price are read.
	Denom string
}

// Set is the chain_state set.
type Set struct {
	deps     Deps
	interval time.Duration
}

// New returns the set; interval zero is DefaultInterval.
func New(deps Deps, interval time.Duration) *Set {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Set{deps: deps, interval: interval}
}

var _ statesync.Set = (*Set)(nil)

// Name is statesync.ChainState.
func (s *Set) Name() string { return statesync.ChainState }

// Interval is the minute refresh.
func (s *Set) Interval() time.Duration { return s.interval }

// ResyncOne reads everything: the set has no entries of its own.
func (s *Set) ResyncOne(ctx context.Context, _ string) error { return s.FullResync(ctx) }

// FullResync reads every key at the node's latest height and writes the
// ones that answered; it returns the failures joined.
func (s *Set) FullResync(ctx context.Context) error {
	st, _, err := s.deps.Node.Status(ctx)
	if err != nil {
		return fmt.Errorf("node status: %w", err)
	}
	h := st.LatestBlockHeight
	at := metadata.AppendToOutgoingContext(ctx, grpctypes.GRPCBlockHeightHeader, strconv.FormatInt(h, 10))

	readers := []struct {
		key  string
		read func(context.Context) (any, error)
	}{
		{KeyNodeStatus, func(context.Context) (any, error) { return nodeStatus(st), nil }},
		{KeyStakingPool, s.stakingPool},
		{KeySupply, s.supply},
		{KeyMint, s.mint},
		{KeyCommunityPool, s.communityPool},
		{KeyChainParams, s.chainParams},
		{KeyValidatorCounts, func(ctx context.Context) (any, error) { return s.deps.Store.ValidatorCounts(ctx) }},
		{PriceKey(s.deps.Denom), func(ctx context.Context) (any, error) { return s.deps.Store.Price(ctx, s.deps.Denom) }},
	}
	var rows []Row
	var errs []error
	for _, r := range readers {
		v, err := r.read(at)
		if err == nil {
			var raw []byte
			if raw, err = json.Marshal(v); err == nil {
				rows = append(rows, Row{Key: r.key, Value: raw, Height: h})
				continue
			}
		}
		errs = append(errs, fmt.Errorf("%s: %w", r.key, err))
	}
	if len(rows) > 0 {
		if err := s.deps.Store.Save(ctx, rows); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// NodeStatus is the node_status row.
type NodeStatus struct {
	Moniker      string    `json:"moniker"`
	Network      string    `json:"network"`
	LatestHeight int64     `json:"latest_height"`
	LatestTime   time.Time `json:"latest_time"`
	CatchingUp   bool      `json:"catching_up"`
}

func nodeStatus(st *node.Status) NodeStatus {
	return NodeStatus{
		Moniker: st.Moniker, Network: st.Network, LatestHeight: st.LatestBlockHeight,
		LatestTime: st.LatestBlockTime.UTC(), CatchingUp: st.CatchingUp,
	}
}

// StakingPool is the staking_pool row, base units.
type StakingPool struct {
	BondedTokens    string `json:"bonded_tokens"`
	NotBondedTokens string `json:"not_bonded_tokens"`
}

func (s *Set) stakingPool(ctx context.Context) (any, error) {
	res, err := s.deps.Staking.Pool(ctx, &stakingtypes.QueryPoolRequest{})
	if err != nil {
		return nil, err
	}
	return StakingPool{BondedTokens: res.Pool.BondedTokens.String(), NotBondedTokens: res.Pool.NotBondedTokens.String()}, nil
}

// Coin is an amount in base units; a community pool amount has decimals.
type Coin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

func (s *Set) supply(ctx context.Context) (any, error) {
	res, err := s.deps.Bank.SupplyOf(ctx, &banktypes.QuerySupplyOfRequest{Denom: s.deps.Denom})
	if err != nil {
		return nil, err
	}
	return Coin{Denom: s.deps.Denom, Amount: res.Amount.Amount.String()}, nil
}

// MintState is the mint row: the current inflation and annual provisions
// (decimals) and the mint params as proto JSON.
type MintState struct {
	Inflation        string          `json:"inflation"`
	AnnualProvisions string          `json:"annual_provisions"`
	Params           json.RawMessage `json:"params"`
}

func (s *Set) mint(ctx context.Context) (any, error) {
	infl, err := s.deps.Mint.Inflation(ctx, &minttypes.QueryInflationRequest{})
	if err != nil {
		return nil, err
	}
	prov, err := s.deps.Mint.AnnualProvisions(ctx, &minttypes.QueryAnnualProvisionsRequest{})
	if err != nil {
		return nil, err
	}
	params, err := s.deps.Mint.Params(ctx, &minttypes.QueryParamsRequest{})
	if err != nil {
		return nil, err
	}
	raw, err := s.deps.JSON.ProtoJSON(&params.Params)
	if err != nil {
		return nil, err
	}
	return MintState{Inflation: infl.Inflation.String(), AnnualProvisions: prov.AnnualProvisions.String(), Params: raw}, nil
}

func (s *Set) communityPool(ctx context.Context) (any, error) {
	res, err := s.deps.Distribution.CommunityPool(ctx, &distrtypes.QueryCommunityPoolRequest{})
	if err != nil {
		return nil, err
	}
	return decCoins(res.Pool), nil
}

func decCoins(cs sdk.DecCoins) []Coin {
	out := make([]Coin, 0, len(cs))
	for _, c := range cs {
		out = append(out, Coin{Denom: c.Denom, Amount: c.Amount.String()})
	}
	return out
}

// ChainParams is the chain_params row: the parameters the stats read,
// from the staking and distribution params.
type ChainParams struct {
	MaxValidators uint32 `json:"max_validators"`
	// UnbondingTimeS is the unbonding period in seconds.
	UnbondingTimeS int64  `json:"unbonding_time_s"`
	CommunityTax   string `json:"community_tax"`
}

func (s *Set) chainParams(ctx context.Context) (any, error) {
	st, err := s.deps.Staking.Params(ctx, &stakingtypes.QueryParamsRequest{})
	if err != nil {
		return nil, err
	}
	d, err := s.deps.Distribution.Params(ctx, &distrtypes.QueryParamsRequest{})
	if err != nil {
		return nil, err
	}
	return ChainParams{
		MaxValidators:  st.Params.MaxValidators,
		UnbondingTimeS: int64(st.Params.UnbondingTime / time.Second),
		CommunityTax:   d.Params.CommunityTax.String(),
	}, nil
}
