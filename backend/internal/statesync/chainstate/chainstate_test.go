package chainstate_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/chainstate"
)

// pinned collects the block heights the gRPC reads were pinned to.
type pinned map[string]bool

func (p pinned) note(ctx context.Context) {
	md, _ := metadata.FromOutgoingContext(ctx)
	for _, h := range md.Get("x-cosmos-block-height") {
		p[h] = true
	}
}

type fakeNode struct{ err error }

func (n fakeNode) Status(context.Context) (*node.Status, []byte, error) {
	if n.err != nil {
		return nil, nil, n.err
	}
	return &node.Status{Network: "beezee-1", Moniker: "local", LatestBlockHeight: 500,
		LatestBlockTime: time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC), CatchingUp: true}, nil, nil
}

type fakeChain struct {
	pins     pinned
	poolErr  error
	supplied string
}

func (f *fakeChain) Pool(ctx context.Context, _ *stakingtypes.QueryPoolRequest, _ ...grpc.CallOption) (*stakingtypes.QueryPoolResponse, error) {
	f.pins.note(ctx)
	if f.poolErr != nil {
		return nil, f.poolErr
	}
	return &stakingtypes.QueryPoolResponse{Pool: stakingtypes.Pool{
		BondedTokens: sdkmath.NewInt(600), NotBondedTokens: sdkmath.NewInt(40)}}, nil
}

func (f *fakeChain) Params(ctx context.Context, _ *stakingtypes.QueryParamsRequest, _ ...grpc.CallOption) (*stakingtypes.QueryParamsResponse, error) {
	f.pins.note(ctx)
	p := stakingtypes.DefaultParams()
	p.MaxValidators, p.UnbondingTime = 40, 21*24*time.Hour
	return &stakingtypes.QueryParamsResponse{Params: p}, nil
}

func (f *fakeChain) SupplyOf(ctx context.Context, in *banktypes.QuerySupplyOfRequest, _ ...grpc.CallOption) (*banktypes.QuerySupplyOfResponse, error) {
	f.pins.note(ctx)
	f.supplied = in.Denom
	return &banktypes.QuerySupplyOfResponse{Amount: sdk.NewInt64Coin(in.Denom, 1000)}, nil
}

type fakeMint struct{ pins pinned }

func (f fakeMint) Inflation(ctx context.Context, _ *minttypes.QueryInflationRequest, _ ...grpc.CallOption) (*minttypes.QueryInflationResponse, error) {
	f.pins.note(ctx)
	return &minttypes.QueryInflationResponse{Inflation: sdkmath.LegacyMustNewDecFromStr("0.05")}, nil
}

func (f fakeMint) AnnualProvisions(ctx context.Context, _ *minttypes.QueryAnnualProvisionsRequest, _ ...grpc.CallOption) (*minttypes.QueryAnnualProvisionsResponse, error) {
	f.pins.note(ctx)
	return &minttypes.QueryAnnualProvisionsResponse{AnnualProvisions: sdkmath.LegacyMustNewDecFromStr("50.5")}, nil
}

func (f fakeMint) Params(ctx context.Context, _ *minttypes.QueryParamsRequest, _ ...grpc.CallOption) (*minttypes.QueryParamsResponse, error) {
	f.pins.note(ctx)
	return &minttypes.QueryParamsResponse{Params: minttypes.Params{MintDenom: "ubze", BlocksPerYear: 5543269}}, nil
}

type fakeDistr struct{ pins pinned }

func (f fakeDistr) CommunityPool(ctx context.Context, _ *distrtypes.QueryCommunityPoolRequest, _ ...grpc.CallOption) (*distrtypes.QueryCommunityPoolResponse, error) {
	f.pins.note(ctx)
	return &distrtypes.QueryCommunityPoolResponse{Pool: sdk.NewDecCoins(sdk.NewDecCoinFromDec("ubze", sdkmath.LegacyMustNewDecFromStr("12.5")))}, nil
}

func (f fakeDistr) Params(ctx context.Context, _ *distrtypes.QueryParamsRequest, _ ...grpc.CallOption) (*distrtypes.QueryParamsResponse, error) {
	f.pins.note(ctx)
	return &distrtypes.QueryParamsResponse{Params: distrtypes.Params{CommunityTax: sdkmath.LegacyMustNewDecFromStr("0.02")}}, nil
}

// fakeJSON renders the mint params' denom only.
type fakeJSON struct{}

func (fakeJSON) ProtoJSON(msg gogoproto.Message) ([]byte, error) {
	p, ok := msg.(*minttypes.Params)
	if !ok {
		return nil, errors.New("unexpected message")
	}
	return json.Marshal(map[string]string{"mint_denom": p.MintDenom})
}

type fakeStore struct {
	rows     map[string]chainstate.Row
	priceArg string
}

func (s *fakeStore) ValidatorCounts(context.Context) (chainstate.ValidatorCounts, error) {
	return chainstate.ValidatorCounts{Bonded: 22, Jailed: 3, Total: 31}, nil
}

func (s *fakeStore) Price(_ context.Context, denom string) (chainstate.Price, error) {
	s.priceArg = denom
	return chainstate.Price{}, nil
}

func (s *fakeStore) Save(_ context.Context, rows []chainstate.Row) error {
	if s.rows == nil {
		s.rows = map[string]chainstate.Row{}
	}
	for _, r := range rows {
		s.rows[r.Key] = r
	}
	return nil
}

func newSet(chain *fakeChain, n chainstate.Node, store *fakeStore) *chainstate.Set {
	return chainstate.New(chainstate.Deps{
		Node: n, Staking: chain, Bank: chain, Mint: fakeMint{chain.pins}, Distribution: fakeDistr{chain.pins},
		JSON: fakeJSON{}, Store: store, Denom: "ubze",
	}, 0)
}

func TestEveryKeyIsWrittenAtTheStatusHeight(t *testing.T) {
	chain := &fakeChain{pins: pinned{}}
	store := &fakeStore{}
	s := newSet(chain, fakeNode{}, store)
	assert.Equal(t, statesync.ChainState, s.Name())
	assert.Equal(t, time.Minute, s.Interval())

	require.NoError(t, s.ResyncOne(context.Background(), "anything"))
	assert.Equal(t, pinned{"500": true}, chain.pins, "every gRPC read at the height /status reported")
	assert.Equal(t, "ubze", chain.supplied)
	assert.Equal(t, "ubze", store.priceArg)

	want := map[string]string{
		"node_status":      `{"moniker":"local","network":"beezee-1","latest_height":500,"latest_time":"2026-10-10T09:00:00Z","catching_up":true}`,
		"staking_pool":     `{"bonded_tokens":"600","not_bonded_tokens":"40"}`,
		"supply":           `{"denom":"ubze","amount":"1000"}`,
		"mint":             `{"inflation":"0.050000000000000000","annual_provisions":"50.500000000000000000","params":{"mint_denom":"ubze"}}`,
		"community_pool":   `[{"denom":"ubze","amount":"12.500000000000000000"}]`,
		"chain_params":     `{"max_validators":40,"unbonding_time_s":1814400,"community_tax":"0.020000000000000000"}`,
		"validator_counts": `{"bonded":22,"jailed":3,"total":31}`,
		"price_ubze":       `{"price_usd":null,"price_change_24h_pct":null}`,
	}
	require.Len(t, store.rows, len(want))
	for k, v := range want {
		assert.JSONEq(t, v, string(store.rows[k].Value), k)
		assert.Equal(t, int64(500), store.rows[k].Height, k)
	}
}

func TestAFailingKeyKeepsItsRowAndTheOthersAreWritten(t *testing.T) {
	chain := &fakeChain{pins: pinned{}, poolErr: errors.New("unavailable")}
	store := &fakeStore{rows: map[string]chainstate.Row{"staking_pool": {Key: "staking_pool", Value: json.RawMessage(`{"old":true}`), Height: 1}}}
	err := newSet(chain, fakeNode{}, store).FullResync(context.Background())
	require.ErrorContains(t, err, "staking_pool: unavailable")
	assert.JSONEq(t, `{"old":true}`, string(store.rows["staking_pool"].Value))
	assert.Contains(t, store.rows, "supply")
}

func TestANodeThatIsDownWritesNothing(t *testing.T) {
	store := &fakeStore{}
	err := newSet(&fakeChain{pins: pinned{}}, fakeNode{err: errors.New("refused")}, store).FullResync(context.Background())
	require.ErrorContains(t, err, "node status")
	assert.Empty(t, store.rows)
}
