package emergency_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	circuittypes "cosmossdk.io/x/circuit/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	guardiantypes "github.com/bze-alphateam/bze/x/guardian/types"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/emergency"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

type fakeNode struct {
	height int64
	err    error
	calls  atomic.Int32
}

func (f *fakeNode) Status(context.Context) (*node.Status, []byte, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, nil, f.err
	}
	return &node.Status{LatestBlockHeight: f.height}, nil, nil
}

type fakeGuardian struct {
	params   *guardiantypes.QueryParamsResponse
	paramErr error
	blocked  []string
	paused   bool
	channels []string
	chanErr  error
}

func (f *fakeGuardian) Params(context.Context, *guardiantypes.QueryParamsRequest, ...grpc.CallOption) (*guardiantypes.QueryParamsResponse, error) {
	return f.params, f.paramErr
}

func (f *fakeGuardian) BlockedDenoms(context.Context, *guardiantypes.QueryBlockedDenomsRequest, ...grpc.CallOption) (*guardiantypes.QueryBlockedDenomsResponse, error) {
	return &guardiantypes.QueryBlockedDenomsResponse{Denoms: f.blocked}, nil
}

func (f *fakeGuardian) AutomationPaused(context.Context, *guardiantypes.QueryAutomationPausedRequest, ...grpc.CallOption) (*guardiantypes.QueryAutomationPausedResponse, error) {
	return &guardiantypes.QueryAutomationPausedResponse{Paused: f.paused}, nil
}

func (f *fakeGuardian) ClosedChannels(context.Context, *guardiantypes.QueryClosedChannelsRequest, ...grpc.CallOption) (*guardiantypes.QueryClosedChannelsResponse, error) {
	return &guardiantypes.QueryClosedChannelsResponse{ChannelIds: f.channels}, f.chanErr
}

type fakeCircuit struct{ disabled []string }

func (f fakeCircuit) DisabledList(context.Context, *circuittypes.QueryDisabledListRequest, ...grpc.CallOption) (*circuittypes.DisabledListResponse, error) {
	return &circuittypes.DisabledListResponse{DisabledList: f.disabled}, nil
}

type fakeTradebin struct{ halted []string }

func (f fakeTradebin) HaltedDenoms(context.Context, *tradebintypes.QueryHaltedDenomsRequest, ...grpc.CallOption) (*tradebintypes.QueryHaltedDenomsResponse, error) {
	return &tradebintypes.QueryHaltedDenomsResponse{Denoms: f.halted}, nil
}

type fakeUpgrade struct{ plan *upgradetypes.Plan }

func (f fakeUpgrade) CurrentPlan(context.Context, *upgradetypes.QueryCurrentPlanRequest, ...grpc.CallOption) (*upgradetypes.QueryCurrentPlanResponse, error) {
	return &upgradetypes.QueryCurrentPlanResponse{Plan: f.plan}, nil
}

func newGuardian() *fakeGuardian {
	return &fakeGuardian{params: &guardiantypes.QueryParamsResponse{Params: guardiantypes.Params{Guardian: "bze1guardian"}}}
}

func newReader(n *fakeNode, g *fakeGuardian, c fakeCircuit, tb fakeTradebin, up fakeUpgrade) *emergency.Reader {
	return emergency.New(emergency.Config{}, emergency.Deps{Node: n, Guardian: g, Circuit: c, Tradebin: tb, Upgrade: up})
}

func TestEveryQueryMapsIntoTheState(t *testing.T) {
	g := newGuardian()
	g.blocked, g.paused, g.channels = []string{"ibc/AAA"}, true, []string{"channel-3"}
	r := newReader(&fakeNode{height: 4405200}, g, fakeCircuit{disabled: []string{"/bze.tradebin.MsgCreateOrder"}},
		fakeTradebin{halted: []string{"factory/bze1x/utok"}},
		fakeUpgrade{plan: &upgradetypes.Plan{Name: "emergency-halt-4405300", Height: 4405300, Info: "bug"}})

	s, err := r.State(context.Background())

	require.NoError(t, err)
	assert.Equal(t, &emergency.State{
		Supported: true, Height: 4405200, Guardian: "bze1guardian", AutomationPaused: true,
		ClosedChannels: []string{"channel-3"}, BlockedDenoms: []string{"ibc/AAA"},
		DisabledMessages: []string{"/bze.tradebin.MsgCreateOrder"}, HaltedDenoms: []string{"factory/bze1x/utok"},
		Plan: &emergency.Plan{Name: "emergency-halt-4405300", Height: 4405300, Info: "bug", Emergency: true},
	}, s)
}

func TestActiveTruthTable(t *testing.T) {
	plan := func(name string) *emergency.Plan {
		return &emergency.Plan{Name: name, Emergency: name == "emergency-halt-9"}
	}
	for name, tc := range map[string]struct {
		state  emergency.State
		active bool
	}{
		"nothing":            {emergency.State{Supported: true}, false},
		"paused automation":  {emergency.State{AutomationPaused: true}, true},
		"closed channel":     {emergency.State{ClosedChannels: []string{"channel-1"}}, true},
		"blocked denom":      {emergency.State{BlockedDenoms: []string{"a"}}, true},
		"disabled message":   {emergency.State{DisabledMessages: []string{"/x"}}, true},
		"halted denom":       {emergency.State{HaltedDenoms: []string{"a"}}, true},
		"emergency plan":     {emergency.State{Plan: plan("emergency-halt-9")}, true},
		"governance upgrade": {emergency.State{Plan: plan("v8.4.0")}, false},
	} {
		assert.Equal(t, tc.active, tc.state.Active(), name)
	}
}

func TestANodeBeforeV830IsUnsupportedAndEmpty(t *testing.T) {
	g := newGuardian()
	g.paramErr = status.Error(codes.Unimplemented, "unknown service bze.guardian.v1.Query")
	r := newReader(&fakeNode{height: 7}, g, fakeCircuit{}, fakeTradebin{}, fakeUpgrade{})

	s, err := r.State(context.Background())

	require.NoError(t, err)
	assert.False(t, s.Supported)
	assert.False(t, s.Active())
	assert.Equal(t, int64(7), s.Height)
	assert.NotNil(t, s.BlockedDenoms)
	assert.Empty(t, s.ClosedChannels)
}

func TestAFailingQueryFailsTheWholeRead(t *testing.T) {
	boom := errors.New("node down")
	g := newGuardian()
	g.chanErr = boom
	_, err := newReader(&fakeNode{height: 1}, g, fakeCircuit{}, fakeTradebin{}, fakeUpgrade{}).State(context.Background())
	assert.ErrorIs(t, err, boom)

	g = newGuardian()
	g.paramErr = boom
	_, err = newReader(&fakeNode{height: 1}, g, fakeCircuit{}, fakeTradebin{}, fakeUpgrade{}).State(context.Background())
	assert.ErrorIs(t, err, boom)

	_, err = newReader(&fakeNode{err: boom}, newGuardian(), fakeCircuit{}, fakeTradebin{}, fakeUpgrade{}).State(context.Background())
	assert.ErrorIs(t, err, boom)
}

func TestConcurrentCallersShareOneReadAndTheCacheExpires(t *testing.T) {
	n := &fakeNode{height: 5}
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	r := emergency.New(emergency.Config{TTL: 15 * time.Second, Now: clock}, emergency.Deps{
		Node: n, Guardian: newGuardian(), Circuit: fakeCircuit{}, Tradebin: fakeTradebin{}, Upgrade: fakeUpgrade{},
	})

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			_, err := r.State(context.Background())
			assert.NoError(t, err)
		})
	}
	wg.Wait()
	_, err := r.State(context.Background())
	require.NoError(t, err)
	assert.LessOrEqual(t, n.calls.Load(), int32(2)) // singleflight may let a late caller start a second read before the first stores
	before := n.calls.Load()

	_, err = r.State(context.Background())
	require.NoError(t, err)
	assert.Equal(t, before, n.calls.Load(), "fresh answer served from the cache")

	mu.Lock()
	now = now.Add(16 * time.Second)
	mu.Unlock()
	_, err = r.State(context.Background())
	require.NoError(t, err)
	assert.Equal(t, before+1, n.calls.Load(), "expired answer read again")
}

func TestActionNames(t *testing.T) {
	a, ok := emergency.MessageAction("/bze.tradebin.MsgUnhaltDenoms")
	assert.True(t, ok)
	assert.Equal(t, emergency.UnhaltTrading, a)
	a, ok = emergency.EventAction("bze.guardian.v1.ChannelReopenedEvent")
	assert.True(t, ok)
	assert.Equal(t, emergency.ReopenChannel, a)
	_, ok = emergency.MessageAction("/cosmos.bank.v1beta1.MsgSend")
	assert.False(t, ok)
	assert.Contains(t, emergency.MessageTypes(), "/cosmos.circuit.v1.MsgResetCircuitBreaker")
	assert.Len(t, emergency.EventTypes(), 10)
}
