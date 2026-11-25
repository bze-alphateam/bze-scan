// Package poolprice reads a liquidity pool's price average from the node and
// judges how far the pool's price has drifted from it, the way the chain
// does when it decides to charge a fee in BZE or to hold a module swap.
package poolprice

import (
	"context"
	"fmt"
	"sync"
	"time"

	sdkmath "cosmossdk.io/math"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DefaultTTL is how long Cached serves a read of one pool's average.
const DefaultTTL = 10 * time.Second

// Timeout bounds one read of an average, which the callers of the flight
// share.
const Timeout = 10 * time.Second

// Average is the node's price average of a pool: AvgPrice in chain units
// (quote base units per base base unit), as of block LastHeight.
type Average struct {
	AvgPrice   sdkmath.LegacyDec
	LastHeight int64
}

// Node is the part of the tradebin query client Cached reads;
// tradebintypes.QueryClient satisfies it.
type Node interface {
	PoolPriceAverage(ctx context.Context, in *tradebintypes.QueryPoolPriceAverageRequest,
		opts ...grpc.CallOption) (*tradebintypes.QueryPoolPriceAverageResponse, error)
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

// Cached reads pool price averages from the node, each at most every TTL;
// the last good read is served while the node fails. A pool the node does
// not know (NotFound), or a node from before chain v8.3.0 (Unimplemented),
// answers nil: no average. Safe for concurrent use.
type Cached struct {
	node  Node
	ttl   time.Duration
	clock Clock
	group singleflight.Group

	mu   sync.Mutex
	held map[string]held
}

type held struct {
	avg *Average
	at  time.Time
}

// NewCached returns a reader over node; ttl zero is DefaultTTL, a nil clock
// the wall clock.
func NewCached(node Node, ttl time.Duration, clock Clock) *Cached {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if clock == nil {
		clock = wallClock{}
	}
	return &Cached{node: node, ttl: ttl, clock: clock, held: map[string]held{}}
}

// Average returns the average of the pool, nil when the node has none.
func (c *Cached) Average(ctx context.Context, poolID string) (*Average, error) {
	c.mu.Lock()
	h, ok := c.held[poolID]
	c.mu.Unlock()
	if ok && c.clock.Now().Sub(h.at) < c.ttl {
		return h.avg, nil
	}
	v, err, _ := c.group.Do(poolID, func() (any, error) {
		// Shared by every caller of the flight: not cancelled with the first.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), Timeout)
		defer cancel()
		res, err := c.node.PoolPriceAverage(ctx, &tradebintypes.QueryPoolPriceAverageRequest{PoolId: poolID})
		var avg *Average
		switch status.Code(err) {
		case codes.OK:
			avg = &Average{AvgPrice: res.AvgPrice, LastHeight: res.LastHeight}
		case codes.NotFound, codes.Unimplemented:
			// no average: cached as such
		default:
			return nil, fmt.Errorf("pool price average of %s: %w", poolID, err)
		}
		c.mu.Lock()
		c.held[poolID] = held{avg: avg, at: c.clock.Now()}
		c.mu.Unlock()
		return avg, nil
	})
	if err != nil {
		if ok {
			return h.avg, nil
		}
		return nil, err
	}
	avg, _ := v.(*Average)
	return avg, nil
}
