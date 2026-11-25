package emergency_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	circuittypes "cosmossdk.io/x/circuit/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	guardiantypes "github.com/bze-alphateam/bze/x/guardian/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/emergency"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

type fakeNode struct {
	height int64
	err    error
	calls  atomic.Int32
	// gate, when not nil, holds every Status call until it is closed.
	gate chan struct{}
}

func (f *fakeNode) Status(context.Context) (*node.Status, []byte, error) {
	f.calls.Add(1)
	if f.gate != nil {
		<-f.gate
	}
	if f.err != nil {
		return nil, nil, f.err
	}
	return &node.Status{LatestBlockHeight: f.height}, nil, nil
}

// heights records the x-cosmos-block-height of every query, by query name.
type heights struct {
	mu   sync.Mutex
	seen map[string]string
}

func (h *heights) note(ctx context.Context, query string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.seen == nil {
		h.seen = map[string]string{}
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	h.seen[query] = strings.Join(md.Get("x-cosmos-block-height"), ",")
}

var pinned = &heights{}

type fakeGuardian struct {
	params   *guardiantypes.QueryParamsResponse
	paramErr error
	blocked  []string
	paused   bool
	channels []string
	chanErr  error
}

func (f *fakeGuardian) Params(ctx context.Context, _ *guardiantypes.QueryParamsRequest, _ ...grpc.CallOption) (*guardiantypes.QueryParamsResponse, error) {
	pinned.note(ctx, "guardian.Params")
	return f.params, f.paramErr
}

func (f *fakeGuardian) BlockedDenoms(ctx context.Context, _ *guardiantypes.QueryBlockedDenomsRequest, _ ...grpc.CallOption) (*guardiantypes.QueryBlockedDenomsResponse, error) {
	pinned.note(ctx, "guardian.BlockedDenoms")
	return &guardiantypes.QueryBlockedDenomsResponse{Denoms: f.blocked}, nil
}

func (f *fakeGuardian) AutomationPaused(ctx context.Context, _ *guardiantypes.QueryAutomationPausedRequest, _ ...grpc.CallOption) (*guardiantypes.QueryAutomationPausedResponse, error) {
	pinned.note(ctx, "guardian.AutomationPaused")
	return &guardiantypes.QueryAutomationPausedResponse{Paused: f.paused}, nil
}

func (f *fakeGuardian) ClosedChannels(ctx context.Context, _ *guardiantypes.QueryClosedChannelsRequest, _ ...grpc.CallOption) (*guardiantypes.QueryClosedChannelsResponse, error) {
	pinned.note(ctx, "guardian.ClosedChannels")
	return &guardiantypes.QueryClosedChannelsResponse{ChannelIds: f.channels}, f.chanErr
}

type fakeCircuit struct{ disabled []string }

func (f fakeCircuit) DisabledList(ctx context.Context, _ *circuittypes.QueryDisabledListRequest, _ ...grpc.CallOption) (*circuittypes.DisabledListResponse, error) {
	pinned.note(ctx, "circuit.DisabledList")
	return &circuittypes.DisabledListResponse{DisabledList: f.disabled}, nil
}

type fakeHalted struct{ halted []string }

func (f fakeHalted) HaltedDenoms(ctx context.Context) ([]string, error) {
	pinned.note(ctx, "halted")
	return f.halted, nil
}

type fakeUpgrade struct{ plan *upgradetypes.Plan }

func (f fakeUpgrade) CurrentPlan(ctx context.Context, _ *upgradetypes.QueryCurrentPlanRequest, _ ...grpc.CallOption) (*upgradetypes.QueryCurrentPlanResponse, error) {
	pinned.note(ctx, "upgrade.CurrentPlan")
	return &upgradetypes.QueryCurrentPlanResponse{Plan: f.plan}, nil
}

func newGuardian() *fakeGuardian {
	return &fakeGuardian{params: &guardiantypes.QueryParamsResponse{Params: guardiantypes.Params{Guardian: "bze1guardian"}}}
}

func newReader(n *fakeNode, g *fakeGuardian, c fakeCircuit, tb fakeHalted, up fakeUpgrade) *emergency.Reader {
	return emergency.New(emergency.Config{}, emergency.Deps{Node: n, Guardian: g, Circuit: c, Halted: tb, Upgrade: up})
}

func TestEveryQueryMapsIntoTheState(t *testing.T) {
	g := newGuardian()
	g.blocked, g.paused, g.channels = []string{"ibc/AAA"}, true, []string{"channel-3"}
	r := newReader(&fakeNode{height: 4405200}, g, fakeCircuit{disabled: []string{"/bze.tradebin.MsgCreateOrder"}},
		fakeHalted{halted: []string{"factory/bze1x/utok"}},
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
	r := newReader(&fakeNode{height: 7}, g, fakeCircuit{}, fakeHalted{}, fakeUpgrade{})

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
	_, err := newReader(&fakeNode{height: 1}, g, fakeCircuit{}, fakeHalted{}, fakeUpgrade{}).State(context.Background())
	assert.ErrorIs(t, err, boom)

	g = newGuardian()
	g.paramErr = boom
	_, err = newReader(&fakeNode{height: 1}, g, fakeCircuit{}, fakeHalted{}, fakeUpgrade{}).State(context.Background())
	assert.ErrorIs(t, err, boom)

	_, err = newReader(&fakeNode{err: boom}, newGuardian(), fakeCircuit{}, fakeHalted{}, fakeUpgrade{}).State(context.Background())
	assert.ErrorIs(t, err, boom)
}

func TestEveryQueryIsPinnedToTheStatusHeight(t *testing.T) {
	pinned.mu.Lock()
	pinned.seen = nil
	pinned.mu.Unlock()
	r := newReader(&fakeNode{height: 4405200}, newGuardian(), fakeCircuit{}, fakeHalted{}, fakeUpgrade{})

	_, err := r.State(context.Background())

	require.NoError(t, err)
	pinned.mu.Lock()
	defer pinned.mu.Unlock()
	assert.Equal(t, map[string]string{
		"guardian.Params": "4405200", "guardian.BlockedDenoms": "4405200", "guardian.AutomationPaused": "4405200",
		"guardian.ClosedChannels": "4405200", "circuit.DisabledList": "4405200", "halted": "4405200",
		"upgrade.CurrentPlan": "4405200",
	}, pinned.seen)
}

func TestConcurrentCallersShareExactlyOneRead(t *testing.T) {
	n := &fakeNode{height: 5, gate: make(chan struct{})}
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	r := emergency.New(emergency.Config{TTL: 15 * time.Second, Now: func() time.Time { return now }}, emergency.Deps{
		Node: n, Guardian: newGuardian(), Circuit: fakeCircuit{}, Halted: fakeHalted{}, Upgrade: fakeUpgrade{},
	})

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			_, err := r.State(context.Background())
			assert.NoError(t, err)
		})
	}
	require.Eventually(t, func() bool { return n.calls.Load() == 1 }, 5*time.Second, time.Millisecond)
	close(n.gate) // the one read held at the node answers for all of them
	wg.Wait()
	assert.Equal(t, int32(1), n.calls.Load())

	_, err := r.State(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(1), n.calls.Load(), "a fresh answer is served from the cache")
}

func TestTheCacheExpires(t *testing.T) {
	n := &fakeNode{height: 5}
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	r := emergency.New(emergency.Config{TTL: 15 * time.Second, Now: func() time.Time { return now }}, emergency.Deps{
		Node: n, Guardian: newGuardian(), Circuit: fakeCircuit{}, Halted: fakeHalted{}, Upgrade: fakeUpgrade{},
	})
	_, _ = r.State(context.Background())
	now = now.Add(14 * time.Second)
	_, _ = r.State(context.Background())
	assert.Equal(t, int32(1), n.calls.Load())
	now = now.Add(2 * time.Second)
	_, _ = r.State(context.Background())
	assert.Equal(t, int32(2), n.calls.Load())
}

func TestEveryMessageAndEventHasItsAction(t *testing.T) {
	messages := map[string]string{
		"/bze.guardian.v1.MsgTightenRateLimit":      emergency.TightenRateLimit,
		"/bze.guardian.v1.MsgBlockDenom":            emergency.BlockDenom,
		"/bze.guardian.v1.MsgUnblockDenom":          emergency.UnblockDenom,
		"/bze.guardian.v1.MsgDisableMessages":       emergency.DisableMessages,
		"/bze.guardian.v1.MsgHaltTradingDenoms":     emergency.HaltTrading,
		"/bze.guardian.v1.MsgHaltChain":             emergency.HaltChain,
		"/bze.guardian.v1.MsgPauseAutomation":       emergency.PauseAutomation,
		"/bze.guardian.v1.MsgResumeAutomation":      emergency.ResumeAutomation,
		"/bze.guardian.v1.MsgCloseChannel":          emergency.CloseChannel,
		"/bze.guardian.v1.MsgReopenChannel":         emergency.ReopenChannel,
		"/bze.guardian.v1.MsgUpdateParams":          emergency.SetGuardian,
		"/cosmos.circuit.v1.MsgResetCircuitBreaker": emergency.EnableMessages,
		"/bze.tradebin.MsgHaltDenoms":               emergency.HaltTrading,
		"/bze.tradebin.MsgUnhaltDenoms":             emergency.UnhaltTrading,
	}
	for url, want := range messages {
		got, ok := emergency.MessageAction(url)
		assert.True(t, ok, url)
		assert.Equal(t, want, got, url)
	}
	assert.ElementsMatch(t, keysOf(messages), emergency.MessageTypes())

	events := map[string]string{
		"bze.guardian.v1.TightenRateLimitEvent":    emergency.TightenRateLimit,
		"bze.guardian.v1.DenomBlockedEvent":        emergency.BlockDenom,
		"bze.guardian.v1.DenomUnblockedEvent":      emergency.UnblockDenom,
		"bze.guardian.v1.MessagesDisabledEvent":    emergency.DisableMessages,
		"bze.guardian.v1.TradingDenomsHaltedEvent": emergency.HaltTrading,
		"bze.guardian.v1.ChainHaltScheduledEvent":  emergency.HaltChain,
		"bze.guardian.v1.AutomationPausedEvent":    emergency.PauseAutomation,
		"bze.guardian.v1.AutomationResumedEvent":   emergency.ResumeAutomation,
		"bze.guardian.v1.ChannelClosedEvent":       emergency.CloseChannel,
		"bze.guardian.v1.ChannelReopenedEvent":     emergency.ReopenChannel,
	}
	for typ, want := range events {
		got, ok := emergency.EventAction(typ)
		assert.True(t, ok, typ)
		assert.Equal(t, want, got, typ)
	}
	assert.ElementsMatch(t, keysOf(events), emergency.EventTypes())

	_, ok := emergency.MessageAction("/cosmos.bank.v1beta1.MsgSend")
	assert.False(t, ok)
	_, ok = emergency.EventAction("bze.burner.v1.SomethingElse")
	assert.False(t, ok)
	// What only governance can run is read from passed proposals; each has an action.
	for _, url := range emergency.GovernanceMessageTypes() {
		_, ok := emergency.MessageAction(url)
		assert.True(t, ok, url)
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
