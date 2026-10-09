// Package chainstate reads an account's current state live from the local
// node's gRPC: balances, delegations, unbonding delegations and pending
// rewards. None of it is stored; each answer is cached in memory for a few
// seconds, so N open tabs of one account cost one node call per query, and
// concurrent misses of one key share a single call.
package chainstate

import (
	"context"
	"fmt"
	"sync"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
)

// DefaultTTL is how long an answer is served from the cache.
const DefaultTTL = 5 * time.Second

// BondDenom is the chain's staking denom; unbonding entries carry no denom.
const BondDenom = "ubze"

// PageLimit is the page size of the paginated queries; MaxPages bounds how
// many pages one query reads.
const (
	PageLimit = 1000
	MaxPages  = 20
)

// Bank is the part of the bank query client the reader uses;
// banktypes.QueryClient satisfies it.
type Bank interface {
	AllBalances(ctx context.Context, in *banktypes.QueryAllBalancesRequest, opts ...grpc.CallOption) (*banktypes.QueryAllBalancesResponse, error)
}

// Staking is the part of the staking query client the reader uses;
// stakingtypes.QueryClient satisfies it.
type Staking interface {
	DelegatorDelegations(ctx context.Context, in *stakingtypes.QueryDelegatorDelegationsRequest, opts ...grpc.CallOption) (*stakingtypes.QueryDelegatorDelegationsResponse, error)
	DelegatorUnbondingDelegations(ctx context.Context, in *stakingtypes.QueryDelegatorUnbondingDelegationsRequest, opts ...grpc.CallOption) (*stakingtypes.QueryDelegatorUnbondingDelegationsResponse, error)
}

// Distribution is the part of the distribution query client the reader
// uses; distrtypes.QueryClient satisfies it.
type Distribution interface {
	DelegationTotalRewards(ctx context.Context, in *distrtypes.QueryDelegationTotalRewardsRequest, opts ...grpc.CallOption) (*distrtypes.QueryDelegationTotalRewardsResponse, error)
}

// Deps are the reader's query clients.
type Deps struct {
	Bank         Bank
	Staking      Staking
	Distribution Distribution
}

// Config tunes the reader; the zero value is production.
type Config struct {
	// TTL is how long an answer is cached; zero is DefaultTTL.
	TTL time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Coin is an amount of a denom in base units.
type Coin struct {
	Denom  string
	Amount string
}

// Delegation is the stake of an account with one validator.
type Delegation struct {
	Validator string
	Amount    Coin
}

// Unbonding is one entry of an unbonding delegation.
type Unbonding struct {
	Validator      string
	Amount         Coin
	CompletionTime time.Time
}

// Reward is the pending reward of an account from one validator.
type Reward struct {
	Validator string
	Coins     []Coin
}

// Rewards are an account's pending rewards and their total. Amounts are
// truncated to whole base units, which is what a withdrawal pays.
type Rewards struct {
	ByValidator []Reward
	Total       []Coin
}

// Account is everything the account page reads live.
type Account struct {
	Balances    []Coin
	Delegations []Delegation
	Unbonding   []Unbonding
	Rewards     Rewards
}

// Reader reads account state live, through a short in-process cache.
type Reader struct {
	deps  Deps
	cache *cache
}

// New returns a reader over deps.
func New(cfg Config, deps Deps) *Reader {
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultTTL
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Reader{deps: deps, cache: newCache(cfg.TTL, cfg.Now)}
}

// Account reads the four queries of address concurrently; any failure fails
// the whole read.
func (r *Reader) Account(ctx context.Context, address string) (*Account, error) {
	var acc Account
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		acc.Balances, err = r.Balances(gctx, address)
		return err
	})
	g.Go(func() (err error) {
		acc.Delegations, err = r.Delegations(gctx, address)
		return err
	})
	g.Go(func() (err error) {
		acc.Unbonding, err = r.Unbonding(gctx, address)
		return err
	})
	g.Go(func() (err error) {
		acc.Rewards, err = r.Rewards(gctx, address)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return &acc, nil
}

// Balances returns every balance of address (bank AllBalances).
func (r *Reader) Balances(ctx context.Context, address string) ([]Coin, error) {
	return cached(ctx, r.cache, "balances|"+address, func(ctx context.Context) ([]Coin, error) {
		var out []Coin
		err := paginate(ctx, "balances", func(ctx context.Context, page *query.PageRequest) (*query.PageResponse, error) {
			resp, err := r.deps.Bank.AllBalances(ctx, &banktypes.QueryAllBalancesRequest{Address: address, Pagination: page})
			if err != nil {
				return nil, err
			}
			out = append(out, coins(resp.Balances)...)
			return resp.Pagination, nil
		})
		return out, err
	})
}

// Delegations returns the delegations of address (staking
// DelegatorDelegations).
func (r *Reader) Delegations(ctx context.Context, address string) ([]Delegation, error) {
	return cached(ctx, r.cache, "delegations|"+address, func(ctx context.Context) ([]Delegation, error) {
		var out []Delegation
		err := paginate(ctx, "delegations", func(ctx context.Context, page *query.PageRequest) (*query.PageResponse, error) {
			resp, err := r.deps.Staking.DelegatorDelegations(ctx,
				&stakingtypes.QueryDelegatorDelegationsRequest{DelegatorAddr: address, Pagination: page})
			if err != nil {
				return nil, err
			}
			for _, d := range resp.DelegationResponses {
				out = append(out, Delegation{Validator: d.Delegation.ValidatorAddress, Amount: coin(d.Balance)})
			}
			return resp.Pagination, nil
		})
		return out, err
	})
}

// Unbonding returns the unbonding entries of address, one per entry
// (staking DelegatorUnbondingDelegations).
func (r *Reader) Unbonding(ctx context.Context, address string) ([]Unbonding, error) {
	return cached(ctx, r.cache, "unbonding|"+address, func(ctx context.Context) ([]Unbonding, error) {
		var out []Unbonding
		err := paginate(ctx, "unbonding", func(ctx context.Context, page *query.PageRequest) (*query.PageResponse, error) {
			resp, err := r.deps.Staking.DelegatorUnbondingDelegations(ctx,
				&stakingtypes.QueryDelegatorUnbondingDelegationsRequest{DelegatorAddr: address, Pagination: page})
			if err != nil {
				return nil, err
			}
			for _, u := range resp.UnbondingResponses {
				for _, e := range u.Entries {
					out = append(out, Unbonding{
						Validator:      u.ValidatorAddress,
						Amount:         Coin{Denom: BondDenom, Amount: e.Balance.String()},
						CompletionTime: e.CompletionTime.UTC(),
					})
				}
			}
			return resp.Pagination, nil
		})
		return out, err
	})
}

// Rewards returns the pending rewards of address (distribution
// DelegationTotalRewards).
func (r *Reader) Rewards(ctx context.Context, address string) (Rewards, error) {
	return cached(ctx, r.cache, "rewards|"+address, func(ctx context.Context) (Rewards, error) {
		resp, err := r.deps.Distribution.DelegationTotalRewards(ctx,
			&distrtypes.QueryDelegationTotalRewardsRequest{DelegatorAddress: address})
		if err != nil {
			return Rewards{}, fmt.Errorf("rewards: %w", err)
		}
		out := Rewards{Total: decCoins(resp.Total)}
		for _, rw := range resp.Rewards {
			out.ByValidator = append(out.ByValidator, Reward{Validator: rw.ValidatorAddress, Coins: decCoins(rw.Reward)})
		}
		return out, nil
	})
}

// paginate calls fetch page after page until the node returns no next key,
// at most MaxPages times.
func paginate(ctx context.Context, what string,
	fetch func(ctx context.Context, page *query.PageRequest) (*query.PageResponse, error)) error {
	var key []byte
	for range MaxPages {
		resp, err := fetch(ctx, &query.PageRequest{Key: key, Limit: PageLimit})
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		if resp == nil || len(resp.NextKey) == 0 {
			return nil
		}
		key = resp.NextKey
	}
	return fmt.Errorf("%s: more than %d pages", what, MaxPages)
}

func coin(c sdk.Coin) Coin {
	return Coin{Denom: c.Denom, Amount: c.Amount.String()}
}

func coins(cs sdk.Coins) []Coin {
	out := make([]Coin, 0, len(cs))
	for _, c := range cs {
		out = append(out, coin(c))
	}
	return out
}

// decCoins truncates decimal coins to whole base units, leaving out the ones
// that truncate to zero.
func decCoins(cs sdk.DecCoins) []Coin {
	out := []Coin{}
	for _, c := range cs {
		if amount := c.Amount.TruncateInt(); amount.IsPositive() {
			out = append(out, Coin{Denom: c.Denom, Amount: amount.String()})
		}
	}
	return out
}

// cache holds answers by key until they expire. Errors are never cached.
type cache struct {
	ttl time.Duration
	now func() time.Time

	group singleflight.Group

	mu        sync.Mutex
	entries   map[string]entry
	lastSweep time.Time
}

type entry struct {
	value   any
	expires time.Time
}

func newCache(ttl time.Duration, now func() time.Time) *cache {
	return &cache{ttl: ttl, now: now, entries: map[string]entry{}}
}

func (c *cache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || !c.now().Before(e.expires) {
		return nil, false
	}
	return e.value, true
}

// put stores value and, at most once per TTL, drops the expired entries.
func (c *cache) put(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if now.Sub(c.lastSweep) >= c.ttl {
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
		c.lastSweep = now
	}
	c.entries[key] = entry{value: value, expires: now.Add(c.ttl)}
}

// cached returns the cached answer of key, or loads it once for every
// concurrent caller. The load outlives a caller that gives up (the gRPC
// client bounds it), so one cancelled request does not fail the others.
func cached[T any](ctx context.Context, c *cache, key string, load func(context.Context) (T, error)) (T, error) {
	if v, ok := c.get(key); ok {
		if t, ok := v.(T); ok {
			return t, nil
		}
	}
	ch := c.group.DoChan(key, func() (any, error) {
		v, err := load(context.WithoutCancel(ctx))
		if err != nil {
			return nil, err
		}
		c.put(key, v)
		return v, nil
	})
	select {
	case res := <-ch:
		if res.Err != nil {
			var zero T
			return zero, res.Err
		}
		t, _ := res.Val.(T) // load returns a T
		return t, nil
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}
