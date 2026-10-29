package tickers

import (
	"context"
	"fmt"
	"sync"
	"time"

	sdkmath "cosmossdk.io/math"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"

	"github.com/bze-alphateam/bze-scan/backend/internal/orderbook"
)

// DefaultPoolsTTL is how long NodePools serves a read.
const DefaultPoolsTTL = 10 * time.Second

// PoolsTimeout bounds one read of every pool, which the callers of the
// flight share.
const PoolsTimeout = 30 * time.Second

// PoolNode is the part of the tradebin query client NodePools reads;
// tradebintypes.QueryClient satisfies it.
type PoolNode interface {
	AllLiquidityPools(ctx context.Context, in *tradebintypes.QueryAllLiquidityPoolsRequest,
		opts ...grpc.CallOption) (*tradebintypes.QueryAllLiquidityPoolsResponse, error)
}

// SupplyNode is the part of the bank query client NodePools reads;
// banktypes.QueryClient satisfies it.
type SupplyNode interface {
	SupplyOf(ctx context.Context, in *banktypes.QuerySupplyOfRequest, opts ...grpc.CallOption) (*banktypes.QuerySupplyOfResponse, error)
}

// NodePools implements Pools over the node: the pools and each supply are
// read at most every TTL, and the last good read is served while the node
// fails. Safe for concurrent use.
type NodePools struct {
	tradebin PoolNode
	bank     SupplyNode
	ttl      time.Duration
	clock    Clock
	group    singleflight.Group

	mu       sync.Mutex
	pools    []Pool
	poolsAt  time.Time
	supplies map[string]cachedSupply
}

type cachedSupply struct {
	amount sdkmath.Int
	at     time.Time
}

// NewNodePools returns pools over the node's tradebin and bank queries;
// ttl zero is DefaultPoolsTTL, a nil clock the wall clock.
func NewNodePools(tradebin PoolNode, bank SupplyNode, ttl time.Duration, clock Clock) *NodePools {
	if ttl <= 0 {
		ttl = DefaultPoolsTTL
	}
	if clock == nil {
		clock = realClock{}
	}
	return &NodePools{tradebin: tradebin, bank: bank, ttl: ttl, clock: clock, supplies: map[string]cachedSupply{}}
}

// Pools implements Pools: every pool, all pages.
func (n *NodePools) Pools(ctx context.Context) ([]Pool, error) {
	n.mu.Lock()
	held, at := n.pools, n.poolsAt
	n.mu.Unlock()
	if held != nil && n.clock.Now().Sub(at) < n.ttl {
		return held, nil
	}
	v, err, _ := n.group.Do("pools", func() (any, error) {
		// The read is shared by every caller of the flight, so it must not
		// end when the first one hangs up.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), PoolsTimeout)
		defer cancel()
		var (
			out []Pool
			key []byte
		)
		for {
			res, err := n.tradebin.AllLiquidityPools(ctx, &tradebintypes.QueryAllLiquidityPoolsRequest{
				Pagination: &query.PageRequest{Key: key, Limit: orderbook.PageLimit},
			})
			if err != nil {
				return nil, fmt.Errorf("all liquidity pools: %w", err)
			}
			for _, p := range res.GetList() {
				out = append(out, Pool{ID: p.GetId(), Base: p.GetBase(), Quote: p.GetQuote(), LPDenom: p.GetLpDenom(),
					ReserveBase: p.ReserveBase, ReserveQuote: p.ReserveQuote})
			}
			if key = res.GetPagination().GetNextKey(); len(key) == 0 {
				break
			}
		}
		if out == nil {
			out = []Pool{}
		}
		n.mu.Lock()
		n.pools, n.poolsAt = out, n.clock.Now()
		n.mu.Unlock()
		return out, nil
	})
	if err != nil {
		if held != nil {
			return held, nil
		}
		return nil, err
	}
	return v.([]Pool), nil
}

// Supply implements Pools.
func (n *NodePools) Supply(ctx context.Context, denom string) (sdkmath.Int, error) {
	n.mu.Lock()
	held, ok := n.supplies[denom]
	n.mu.Unlock()
	if ok && n.clock.Now().Sub(held.at) < n.ttl {
		return held.amount, nil
	}
	res, err := n.bank.SupplyOf(ctx, &banktypes.QuerySupplyOfRequest{Denom: denom})
	if err != nil {
		if ok {
			return held.amount, nil
		}
		return sdkmath.Int{}, fmt.Errorf("supply of %s: %w", denom, err)
	}
	amount := res.GetAmount().Amount
	if amount.IsNil() {
		amount = sdkmath.ZeroInt()
	}
	n.mu.Lock()
	n.supplies[denom] = cachedSupply{amount: amount, at: n.clock.Now()}
	n.mu.Unlock()
	return amount, nil
}
