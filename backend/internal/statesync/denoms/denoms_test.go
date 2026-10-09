package denoms_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tokenfactorytypes "github.com/bze-alphateam/bze/x/tokenfactory/types"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/denoms"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// Recorded mainnet denoms (chain v8.1.1, no halt and no branding queries).
const (
	vdl         = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uvdl"
	vdlCreator  = "bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk"
	mars        = "factory/bze15pqjgk4la0mfphwddce00d05n3th3u66n3ptcv/2MARS"
	marsAdmin   = "bze15pqjgk4la0mfphwddce00d05n3th3u66n3ptcv"
	usdc        = "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4"
	ctlNoSupply = "factory/bze1972aqfzdg29ugjln74edx0xvcg4ehvysjptk77/1000000000"
	vdlLP       = "ulp_factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uvdl_ubze"
	recorded    = 29 // 28 with supply, plus one with metadata only
)

func TestKind(t *testing.T) {
	for denom, want := range map[string]string{
		"ubze":                 denoms.KindNative,
		vdl:                    denoms.KindFactory,
		usdc:                   denoms.KindIBC,
		vdlLP:                  denoms.KindLP,
		"ulp/0A1B2C":           denoms.KindLP,
		"uatom":                denoms.KindUnknown,
		"factory":              denoms.KindUnknown,
		"lp_factory/x/uvdl_ub": denoms.KindUnknown, // the display unit, never a balance
	} {
		assert.Equal(t, want, denoms.Kind(denom), denom)
	}
}

// fakeNode answers the bank, tokenfactory and tradebin queries from the
// recorded fixtures, counting the calls.
type fakeNode struct {
	t *testing.T

	mu    sync.Mutex
	calls map[string]int
	err   error
	// halted answers HaltedDenoms (chain v8.2.0) instead of Unimplemented.
	halted []string
	// pages splits TotalSupply in two pages.
	pages bool
}

func newFakeNode(t *testing.T) *fakeNode { return &fakeNode{t: t, calls: map[string]int{}} }

func (n *fakeNode) count(m string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls[m]++
	return n.err
}

func (n *fakeNode) callsOf(m string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls[m]
}

func (n *fakeNode) TotalSupply(_ context.Context, in *banktypes.QueryTotalSupplyRequest, _ ...grpc.CallOption) (*banktypes.QueryTotalSupplyResponse, error) {
	if err := n.count("TotalSupply"); err != nil {
		return nil, err
	}
	var resp banktypes.QueryTotalSupplyResponse
	fakenode.LoadGRPCFixture(n.t, "bank", "TotalSupply", "", &resp)
	resp.Pagination.NextKey = nil
	if n.pages {
		half := len(resp.Supply) / 2
		if string(in.Pagination.Key) == "page2" {
			resp.Supply = resp.Supply[half:]
		} else {
			resp.Supply = resp.Supply[:half]
			resp.Pagination.NextKey = []byte("page2")
		}
	}
	return &resp, nil
}

func (n *fakeNode) SupplyOf(_ context.Context, in *banktypes.QuerySupplyOfRequest, _ ...grpc.CallOption) (*banktypes.QuerySupplyOfResponse, error) {
	if err := n.count("SupplyOf"); err != nil {
		return nil, err
	}
	var resp banktypes.QuerySupplyOfResponse
	fakenode.LoadGRPCFixture(n.t, "bank", "SupplyOf", url.PathEscape(in.Denom), &resp)
	return &resp, nil
}

func (n *fakeNode) DenomsMetadata(context.Context, *banktypes.QueryDenomsMetadataRequest, ...grpc.CallOption) (*banktypes.QueryDenomsMetadataResponse, error) {
	if err := n.count("DenomsMetadata"); err != nil {
		return nil, err
	}
	var resp banktypes.QueryDenomsMetadataResponse
	fakenode.LoadGRPCFixture(n.t, "bank", "DenomsMetadata", "", &resp)
	return &resp, nil
}

func (n *fakeNode) DenomMetadataByQueryString(_ context.Context, in *banktypes.QueryDenomMetadataByQueryStringRequest, _ ...grpc.CallOption) (*banktypes.QueryDenomMetadataByQueryStringResponse, error) {
	if err := n.count("DenomMetadata"); err != nil {
		return nil, err
	}
	key := url.PathEscape(in.Denom)
	if strings.Contains(string(fakenode.ReadGRPCFixture(n.t, "bank", "DenomMetadataByQueryString", key)), `"code": 5`) {
		return nil, status.Error(codes.NotFound, "client metadata for denom "+in.Denom)
	}
	var resp banktypes.QueryDenomMetadataByQueryStringResponse
	fakenode.LoadGRPCFixture(n.t, "bank", "DenomMetadataByQueryString", key, &resp)
	return &resp, nil
}

func (n *fakeNode) DenomAuthority(_ context.Context, in *tokenfactorytypes.QueryDenomAuthorityRequest, _ ...grpc.CallOption) (*tokenfactorytypes.QueryDenomAuthorityResponse, error) {
	if err := n.count("DenomAuthority"); err != nil {
		return nil, err
	}
	var resp tokenfactorytypes.QueryDenomAuthorityResponse
	fakenode.LoadGRPCFixture(n.t, "tokenfactory", "DenomAuthority", url.PathEscape(in.Denom), &resp)
	return &resp, nil
}

func (n *fakeNode) AllMarkets(context.Context, *tradebintypes.QueryAllMarketsRequest, ...grpc.CallOption) (*tradebintypes.QueryAllMarketsResponse, error) {
	if err := n.count("AllMarkets"); err != nil {
		return nil, err
	}
	var resp tradebintypes.QueryAllMarketsResponse
	fakenode.LoadGRPCFixture(n.t, "tradebin", "AllMarkets", "", &resp)
	return &resp, nil
}

func (n *fakeNode) HaltedDenoms(context.Context, *tradebintypes.QueryHaltedDenomsRequest, ...grpc.CallOption) (*tradebintypes.QueryHaltedDenomsResponse, error) {
	if err := n.count("HaltedDenoms"); err != nil {
		return nil, err
	}
	if n.halted == nil {
		return nil, status.Error(codes.Unimplemented, "unknown method HaltedDenoms")
	}
	return &tradebintypes.QueryHaltedDenomsResponse{Denoms: n.halted}, nil
}

// fakeStore keeps the snapshots.
type fakeStore struct {
	mu    sync.Mutex
	snaps []denoms.Snapshot
	err   error
}

func (s *fakeStore) Save(_ context.Context, snap denoms.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.snaps = append(s.snaps, snap)
	return nil
}

func (s *fakeStore) last(t *testing.T) denoms.Snapshot {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(t, s.snaps)
	return s.snaps[len(s.snaps)-1]
}

func newSet(t *testing.T) (*denoms.Set, *fakeNode, *fakeStore) {
	n, st := newFakeNode(t), &fakeStore{}
	return denoms.New(denoms.Deps{Bank: n, TokenFactory: n, Tradebin: n, Store: st}, 0), n, st
}

func byDenom(snap denoms.Snapshot) map[string]denoms.Denom {
	out := map[string]denoms.Denom{}
	for _, d := range snap.Denoms {
		out[d.Denom] = d
	}
	return out
}

func TestFullResyncMapsTheRecordedDenoms(t *testing.T) {
	set, n, st := newSet(t)
	n.pages = true
	assert.Equal(t, statesync.Denoms, set.Name())
	assert.Equal(t, denoms.DefaultInterval, set.Interval())

	require.NoError(t, set.FullResync(context.Background()))
	snap := st.last(t)
	assert.True(t, snap.Full)
	require.Len(t, snap.Denoms, recorded, "every denom with a supply or metadata, both pages")
	rows := byDenom(snap)

	bze := rows["ubze"]
	assert.Equal(t, denoms.KindNative, bze.Kind)
	assert.Equal(t, "BZE", bze.Symbol, "the native denom has no bank metadata: the chain's constants")
	assert.Equal(t, 6, bze.Exponent)
	assert.Nil(t, bze.Metadata)
	assert.NotEqual(t, "0", bze.Supply)

	v := rows[vdl]
	assert.Equal(t, denoms.KindFactory, v.Kind)
	assert.Equal(t, "VDL", v.Symbol)
	assert.Equal(t, 6, v.Exponent, "the display unit's exponent")
	assert.Equal(t, vdlCreator, v.Creator)
	assert.Empty(t, v.Admin, "renounced")
	assert.Equal(t, "20983185271838", v.Supply)
	assert.Contains(t, v.Markets, vdl+"/ubze")
	assert.False(t, v.Halted)
	assert.Equal(t, "VDL", metadataField(t, v.Metadata, "display"), "the raw metadata is kept")

	assert.Equal(t, marsAdmin, rows[mars].Admin)

	u := rows[usdc]
	assert.Equal(t, denoms.KindIBC, u.Kind)
	assert.Equal(t, 0, u.Exponent, "the display unit is not listed: 0 until the registry story")
	assert.Empty(t, u.Creator)

	assert.Equal(t, denoms.KindLP, rows[vdlLP].Kind)
	assert.Empty(t, rows[vdlLP].Symbol)
	assert.Equal(t, 6, rows[vdlLP].Exponent)

	assert.Equal(t, "0", rows[ctlNoSupply].Supply, "metadata without supply")
	for _, d := range snap.Denoms {
		assert.NotNil(t, d.Markets, d.Denom)
	}

	assert.Equal(t, 2, n.callsOf("TotalSupply"))
	assert.Equal(t, 6, n.callsOf("DenomAuthority"), "one per factory denom")
	assert.Equal(t, 1, n.callsOf("HaltedDenoms"), "Unimplemented on v8.1.1: nothing halted")
}

// metadataField reads one field of the stored metadata JSON.
func metadataField(t *testing.T, raw []byte, key string) any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m[key]
}

func TestHaltedDenomsOnAV820Node(t *testing.T) {
	set, n, st := newSet(t)
	n.halted = []string{usdc}
	require.NoError(t, set.FullResync(context.Background()))
	rows := byDenom(st.last(t))
	assert.True(t, rows[usdc].Halted)
	assert.False(t, rows[vdl].Halted)
}

func TestResyncOne(t *testing.T) {
	set, n, st := newSet(t)
	require.NoError(t, set.ResyncOne(context.Background(), vdl))
	snap := st.last(t)
	assert.False(t, snap.Full)
	require.Len(t, snap.Denoms, 1)
	d := snap.Denoms[0]
	assert.Equal(t, "VDL", d.Symbol)
	assert.Equal(t, 6, d.Exponent)
	assert.Equal(t, "20983185271838", d.Supply)
	assert.Equal(t, 1, n.callsOf("DenomAuthority"))
	assert.Zero(t, n.callsOf("TotalSupply"))

	require.NoError(t, set.ResyncOne(context.Background(), "ubze"))
	d = st.last(t).Denoms[0]
	assert.Equal(t, "BZE", d.Symbol, "metadata NotFound: the native constants")
	assert.Nil(t, d.Metadata)
	assert.Equal(t, 1, n.callsOf("DenomAuthority"), "not a factory denom")

	require.NoError(t, set.ResyncOne(context.Background(), statesync.All))
	assert.True(t, st.last(t).Full)
}

func TestSeenDenomsAreResyncedOnlyWhenUnknown(t *testing.T) {
	set, _, _ := newSet(t)
	assert.Equal(t, denoms.SeenKey(usdc), set.Canonical(denoms.SeenKey(usdc)), "nothing known before the first full resync")
	assert.Equal(t, vdl, set.Canonical(vdl), "a dirty denom is always resynced")
	assert.Empty(t, denoms.SeenKey(""))

	require.NoError(t, set.ResyncOne(context.Background(), denoms.SeenKey(usdc)))
	assert.Equal(t, denoms.SeenKey(usdc), set.Canonical(denoms.SeenKey(usdc)), "a single resync teaches nothing yet")

	require.NoError(t, set.FullResync(context.Background()))
	assert.Empty(t, set.Canonical(denoms.SeenKey(usdc)), "known: no resync")
	assert.Equal(t, "ibc/NEW", set.Canonical(denoms.SeenKey("ibc/NEW")), "unknown: resynced")
	assert.Equal(t, vdl, set.Canonical(vdl))
}

func TestANewDenomBecomesKnownAfterItsResync(t *testing.T) {
	// The bank lists one denom at the full resync; a new one appears later.
	n := newFakeNode(t)
	set := denoms.New(denoms.Deps{Bank: supplyOnly{n, sdk.NewCoin(vdl, sdkmath.NewInt(5))}, TokenFactory: n, Tradebin: n, Store: &fakeStore{}}, 0)
	require.NoError(t, set.FullResync(context.Background()))
	assert.Equal(t, usdc, set.Canonical(denoms.SeenKey(usdc)))

	require.NoError(t, set.ResyncOne(context.Background(), usdc))
	assert.Empty(t, set.Canonical(denoms.SeenKey(usdc)))
}

// supplyOnly answers TotalSupply with one coin and lists no metadata.
type supplyOnly struct {
	*fakeNode
	coin sdk.Coin
}

func (s supplyOnly) TotalSupply(context.Context, *banktypes.QueryTotalSupplyRequest, ...grpc.CallOption) (*banktypes.QueryTotalSupplyResponse, error) {
	return &banktypes.QueryTotalSupplyResponse{Supply: sdk.NewCoins(s.coin)}, nil
}

func (s supplyOnly) DenomsMetadata(context.Context, *banktypes.QueryDenomsMetadataRequest, ...grpc.CallOption) (*banktypes.QueryDenomsMetadataResponse, error) {
	return &banktypes.QueryDenomsMetadataResponse{}, nil
}

func TestFailuresKeepTheRows(t *testing.T) {
	set, n, st := newSet(t)
	n.err = errors.New("node down")
	require.ErrorContains(t, set.FullResync(context.Background()), "node down")
	require.ErrorContains(t, set.ResyncOne(context.Background(), vdl), "node down")
	assert.Empty(t, st.snaps)

	n.err = nil
	st.err = errors.New("db down")
	require.ErrorContains(t, set.FullResync(context.Background()), "db down")
	assert.Equal(t, denoms.SeenKey(usdc), set.Canonical(denoms.SeenKey(usdc)), "a failed save teaches nothing")
}
