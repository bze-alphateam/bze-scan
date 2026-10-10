// Package params reads the live parameters of every module the explorer's
// parameters page shows, as proto JSON per module, from the local node's
// gRPC. The API serves them through a short in-process cache (Cached) and
// the params set of the state sync snapshots them to explain changes; the
// values shown are always the live ones.
package params

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"

	burnertypes "github.com/bze-alphateam/bze/x/burner/types"
	cointrunktypes "github.com/bze-alphateam/bze/x/cointrunk/types"
	rewardstypes "github.com/bze-alphateam/bze/x/rewards/types"
	tokenfactorytypes "github.com/bze-alphateam/bze/x/tokenfactory/types"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	txfeecollectortypes "github.com/bze-alphateam/bze/x/txfeecollector/types"
)

// Modules are the modules whose parameters are read, in the order of the
// parameters page.
var Modules = []string{
	"txfeecollector", "staking", "mint", "distribution", "slashing", "gov",
	"tradebin", "tokenfactory", "rewards", "burner", "cointrunk",
}

// Reader reads the live parameters by module. A module that fails is left
// out of the map and its error is joined into the returned one, so a
// caller can use what answered.
type Reader interface {
	Params(ctx context.Context) (map[string]json.RawMessage, error)
}

// JSON marshals a proto message to proto JSON; *chain.Codec satisfies it.
type JSON interface {
	ProtoJSON(msg gogoproto.Message) ([]byte, error)
}

// GRPC reads the parameters over a gRPC connection to the node.
type GRPC struct {
	conn grpc.ClientConnInterface
	json JSON
}

// NewGRPC returns a reader over conn, rendering with json.
func NewGRPC(conn grpc.ClientConnInterface, json JSON) *GRPC {
	return &GRPC{conn: conn, json: json}
}

var _ Reader = (*GRPC)(nil)

// read returns one module's params message.
func (g *GRPC) read(ctx context.Context, module string) (gogoproto.Message, error) {
	switch module {
	case "staking":
		r, err := stakingtypes.NewQueryClient(g.conn).Params(ctx, &stakingtypes.QueryParamsRequest{})
		if err != nil {
			return nil, err
		}
		return &r.Params, nil
	case "mint":
		r, err := minttypes.NewQueryClient(g.conn).Params(ctx, &minttypes.QueryParamsRequest{})
		if err != nil {
			return nil, err
		}
		return &r.Params, nil
	case "distribution":
		r, err := distrtypes.NewQueryClient(g.conn).Params(ctx, &distrtypes.QueryParamsRequest{})
		if err != nil {
			return nil, err
		}
		return &r.Params, nil
	case "slashing":
		r, err := slashingtypes.NewQueryClient(g.conn).Params(ctx, &slashingtypes.QueryParamsRequest{})
		if err != nil {
			return nil, err
		}
		return &r.Params, nil
	case "gov":
		r, err := govv1.NewQueryClient(g.conn).Params(ctx, &govv1.QueryParamsRequest{})
		if err != nil {
			return nil, err
		}
		if r.Params == nil {
			return nil, errors.New("no params in the answer")
		}
		return r.Params, nil
	case "tradebin":
		r, err := tradebintypes.NewQueryClient(g.conn).Params(ctx, &tradebintypes.QueryParamsRequest{})
		if err != nil {
			return nil, err
		}
		return &r.Params, nil
	case "tokenfactory":
		r, err := tokenfactorytypes.NewQueryClient(g.conn).Params(ctx, &tokenfactorytypes.QueryParamsRequest{})
		if err != nil {
			return nil, err
		}
		return &r.Params, nil
	case "rewards":
		r, err := rewardstypes.NewQueryClient(g.conn).Params(ctx, &rewardstypes.QueryParamsRequest{})
		if err != nil {
			return nil, err
		}
		return &r.Params, nil
	case "burner":
		r, err := burnertypes.NewQueryClient(g.conn).Params(ctx, &burnertypes.QueryParamsRequest{})
		if err != nil {
			return nil, err
		}
		return &r.Params, nil
	case "cointrunk":
		r, err := cointrunktypes.NewQueryClient(g.conn).Params(ctx, &cointrunktypes.QueryParamsRequest{})
		if err != nil {
			return nil, err
		}
		return &r.Params, nil
	case "txfeecollector":
		r, err := txfeecollectortypes.NewQueryClient(g.conn).Params(ctx, &txfeecollectortypes.QueryParamsRequest{})
		if err != nil {
			return nil, err
		}
		return &r.Params, nil
	}
	return nil, fmt.Errorf("unknown module %s", module)
}

// Params reads every module concurrently.
func (g *GRPC) Params(ctx context.Context) (map[string]json.RawMessage, error) {
	type result struct {
		module string
		raw    json.RawMessage
		err    error
	}
	results := make([]result, len(Modules))
	var wg sync.WaitGroup
	for i, m := range Modules {
		wg.Go(func() {
			results[i].module = m
			msg, err := g.read(ctx, m)
			if err == nil {
				results[i].raw, err = g.json.ProtoJSON(msg)
			}
			results[i].err = err
		})
	}
	wg.Wait()
	out := make(map[string]json.RawMessage, len(Modules))
	var errs []error
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Errorf("%s params: %w", r.module, r.err))
			continue
		}
		out[r.module] = r.raw
	}
	return out, errors.Join(errs...)
}

// DefaultTTL is how long Cached serves an answer.
const DefaultTTL = 30 * time.Second

// Cached serves a Reader's answers for a TTL; concurrent misses share one
// read. Only a complete answer is cached: a partial one is served but read
// again next time.
type Cached struct {
	from Reader
	ttl  time.Duration
	now  func() time.Time

	group singleflight.Group
	mu    sync.Mutex
	val   map[string]json.RawMessage
	at    time.Time
}

// NewCached caches from for ttl (zero is DefaultTTL); now nil is the wall
// clock.
func NewCached(from Reader, ttl time.Duration, now func() time.Time) *Cached {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if now == nil {
		now = time.Now
	}
	return &Cached{from: from, ttl: ttl, now: now}
}

var _ Reader = (*Cached)(nil)

// Params returns the cached answer while it is fresh, else reads.
func (c *Cached) Params(ctx context.Context) (map[string]json.RawMessage, error) {
	c.mu.Lock()
	if c.val != nil && c.now().Sub(c.at) < c.ttl {
		v := c.val
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()
	type answer struct {
		val map[string]json.RawMessage
		err error
	}
	res, _, _ := c.group.Do("params", func() (any, error) {
		v, err := c.from.Params(context.WithoutCancel(ctx))
		if err == nil {
			c.mu.Lock()
			c.val, c.at = v, c.now()
			c.mu.Unlock()
		}
		return answer{val: v, err: err}, nil
	})
	a, _ := res.(answer) // the closure returns only answers
	return a.val, a.err
}
