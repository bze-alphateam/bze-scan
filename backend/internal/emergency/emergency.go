// Package emergency reads the chain's emergency switches live from the local
// node's gRPC (x/guardian, x/circuit, tradebin's halted denoms and the
// scheduled upgrade plan) and names the stored messages and events that pull
// or release one. Nothing here is stored. Every read is pinned to one height
// (the node's latest, from /status) so the lists describe the same block,
// and the answer is cached for a few seconds: concurrent callers share one
// set of node calls.
package emergency

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	circuittypes "cosmossdk.io/x/circuit/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	guardiantypes "github.com/bze-alphateam/bze/x/guardian/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// DefaultTTL is how long a state is served from the cache.
const DefaultTTL = 15 * time.Second

// EmergencyPlanPrefix starts the name of the upgrade plan x/guardian
// schedules for an immediate halt: emergency-halt-<height>.
const EmergencyPlanPrefix = "emergency-halt-"

// Guardian is the part of the guardian query client the reader uses;
// guardiantypes.QueryClient satisfies it.
type Guardian interface {
	Params(ctx context.Context, in *guardiantypes.QueryParamsRequest, opts ...grpc.CallOption) (*guardiantypes.QueryParamsResponse, error)
	BlockedDenoms(ctx context.Context, in *guardiantypes.QueryBlockedDenomsRequest, opts ...grpc.CallOption) (*guardiantypes.QueryBlockedDenomsResponse, error)
	AutomationPaused(ctx context.Context, in *guardiantypes.QueryAutomationPausedRequest, opts ...grpc.CallOption) (*guardiantypes.QueryAutomationPausedResponse, error)
	ClosedChannels(ctx context.Context, in *guardiantypes.QueryClosedChannelsRequest, opts ...grpc.CallOption) (*guardiantypes.QueryClosedChannelsResponse, error)
}

// Circuit is the part of the circuit query client the reader uses;
// circuittypes.QueryClient satisfies it.
type Circuit interface {
	DisabledList(ctx context.Context, in *circuittypes.QueryDisabledListRequest, opts ...grpc.CallOption) (*circuittypes.DisabledListResponse, error)
}

// Halted reads tradebin's halted denoms; *params.GRPC satisfies it (the
// read the parameters page uses).
type Halted interface {
	HaltedDenoms(ctx context.Context) ([]string, error)
}

// Upgrade is the part of the upgrade query client the reader uses;
// upgradetypes.QueryClient satisfies it.
type Upgrade interface {
	CurrentPlan(ctx context.Context, in *upgradetypes.QueryCurrentPlanRequest, opts ...grpc.CallOption) (*upgradetypes.QueryCurrentPlanResponse, error)
}

// StatusNode is the node's CometBFT RPC; *node.Client satisfies it.
type StatusNode interface {
	Status(ctx context.Context) (*node.Status, []byte, error)
}

// Deps are the reader's clients.
type Deps struct {
	Node     StatusNode
	Guardian Guardian
	Circuit  Circuit
	Halted   Halted
	Upgrade  Upgrade
}

// Config tunes the reader; the zero value is production.
type Config struct {
	// TTL is how long a state is cached; zero is DefaultTTL.
	TTL time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Plan is the scheduled upgrade plan; Emergency is true for the plan of an
// emergency halt.
type Plan struct {
	Name      string
	Height    int64
	Info      string
	Emergency bool
}

// State is every emergency switch at Height. Supported is false on a node
// from before chain v8.3.0 (no guardian module): everything else is then
// empty.
type State struct {
	Supported        bool
	Height           int64
	Guardian         string
	AutomationPaused bool
	ClosedChannels   []string
	BlockedDenoms    []string
	DisabledMessages []string
	HaltedDenoms     []string
	Plan             *Plan
}

// Active is true when any emergency measure is in force: a non-empty list,
// paused automation or a scheduled emergency halt. A governance upgrade plan
// is no emergency.
func (s State) Active() bool {
	return len(s.ClosedChannels) > 0 || len(s.BlockedDenoms) > 0 || len(s.DisabledMessages) > 0 ||
		len(s.HaltedDenoms) > 0 || s.AutomationPaused || (s.Plan != nil && s.Plan.Emergency)
}

// Reader reads the state live, through a short in-process cache.
type Reader struct {
	deps Deps
	ttl  time.Duration
	now  func() time.Time

	group singleflight.Group

	mu      sync.Mutex
	value   *State
	expires time.Time
}

// New returns a reader over deps.
func New(cfg Config, deps Deps) *Reader {
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultTTL
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Reader{deps: deps, ttl: cfg.TTL, now: cfg.Now}
}

// State returns the cached state while it is fresh, else reads it; concurrent
// misses share one read. A failed read is not cached. The read outlives a
// caller that gives up (the gRPC client bounds it), so one cancelled request
// does not fail the others.
func (r *Reader) State(ctx context.Context) (*State, error) {
	if s := r.fresh(); s != nil {
		return s, nil
	}
	ch := r.group.DoChan("state", func() (any, error) {
		s, err := r.read(context.WithoutCancel(ctx))
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.value, r.expires = s, r.now().Add(r.ttl)
		r.mu.Unlock()
		return s, nil
	})
	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		s, _ := res.Val.(*State) // the load returns a *State
		return s, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *Reader) fresh() *State {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.value != nil && r.now().Before(r.expires) {
		return r.value
	}
	return nil
}

// read asks the node for every switch at its latest height. The guardian
// params answer Unimplemented on a node from before v8.3.0: the state is
// then unsupported and empty. Any other failure fails the whole read.
func (r *Reader) read(ctx context.Context) (*State, error) {
	st, _, err := r.deps.Node.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("node status: %w", err)
	}
	s := &State{Supported: true, Height: st.LatestBlockHeight,
		ClosedChannels: []string{}, BlockedDenoms: []string{}, DisabledMessages: []string{}, HaltedDenoms: []string{}}
	ctx = metadata.AppendToOutgoingContext(ctx, grpctypes.GRPCBlockHeightHeader, strconv.FormatInt(s.Height, 10))

	params, err := r.deps.Guardian.Params(ctx, &guardiantypes.QueryParamsRequest{})
	if status.Code(err) == codes.Unimplemented {
		return &State{Height: s.Height, ClosedChannels: []string{}, BlockedDenoms: []string{},
			DisabledMessages: []string{}, HaltedDenoms: []string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("guardian params: %w", err)
	}
	s.Guardian = params.Params.Guardian

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		resp, err := r.deps.Guardian.AutomationPaused(gctx, &guardiantypes.QueryAutomationPausedRequest{})
		if err != nil {
			return fmt.Errorf("guardian automation paused: %w", err)
		}
		s.AutomationPaused = resp.Paused
		return nil
	})
	g.Go(func() error {
		resp, err := r.deps.Guardian.ClosedChannels(gctx, &guardiantypes.QueryClosedChannelsRequest{})
		if err != nil {
			return fmt.Errorf("guardian closed channels: %w", err)
		}
		s.ClosedChannels = nonNil(resp.ChannelIds)
		return nil
	})
	g.Go(func() error {
		resp, err := r.deps.Guardian.BlockedDenoms(gctx, &guardiantypes.QueryBlockedDenomsRequest{})
		if err != nil {
			return fmt.Errorf("guardian blocked denoms: %w", err)
		}
		s.BlockedDenoms = nonNil(resp.Denoms)
		return nil
	})
	g.Go(func() error {
		resp, err := r.deps.Circuit.DisabledList(gctx, &circuittypes.QueryDisabledListRequest{})
		if err != nil {
			return fmt.Errorf("circuit disabled list: %w", err)
		}
		s.DisabledMessages = nonNil(resp.DisabledList)
		return nil
	})
	g.Go(func() error {
		denoms, err := r.deps.Halted.HaltedDenoms(gctx)
		if err != nil {
			return fmt.Errorf("halted denoms: %w", err)
		}
		s.HaltedDenoms = nonNil(denoms)
		return nil
	})
	g.Go(func() error {
		resp, err := r.deps.Upgrade.CurrentPlan(gctx, &upgradetypes.QueryCurrentPlanRequest{})
		if err != nil {
			return fmt.Errorf("upgrade current plan: %w", err)
		}
		if resp.Plan != nil {
			s.Plan = &Plan{
				Name: resp.Plan.Name, Height: resp.Plan.Height, Info: resp.Plan.Info,
				Emergency: strings.HasPrefix(resp.Plan.Name, EmergencyPlanPrefix),
			}
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return s, nil
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
