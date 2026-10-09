package chainstate_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/bze-alphateam/bze-scan/backend/internal/chainstate"
)

const (
	addr = "bze19fgph876c3rqxrn6xk5ch6wd73r3g05w690uls"
	val1 = "bzevaloper19fgph876c3rqxrn6xk5ch6wd73r3g05wzpsht0"
	val2 = "bzevaloper1prm55vzlp5u6excqdunwlm4tw254cq943m6e6m"
)

// node answers the four queries from canned data and counts the calls.
type node struct {
	balances []sdk.Coins // one page each
	err      error
	// block, when set, holds every balances call until it is closed.
	block chan struct{}

	calls atomic.Int32
	pages []*query.PageRequest
	mu    sync.Mutex
}

func (n *node) AllBalances(_ context.Context, in *banktypes.QueryAllBalancesRequest, _ ...grpc.CallOption) (*banktypes.QueryAllBalancesResponse, error) {
	n.calls.Add(1)
	if n.block != nil {
		<-n.block
	}
	if n.err != nil {
		return nil, n.err
	}
	n.mu.Lock()
	page := len(n.pages)
	n.pages = append(n.pages, in.Pagination)
	n.mu.Unlock()
	resp := &banktypes.QueryAllBalancesResponse{Balances: n.balances[page], Pagination: &query.PageResponse{}}
	if page+1 < len(n.balances) {
		resp.Pagination.NextKey = []byte{byte(page + 1)}
	}
	return resp, nil
}

func (n *node) DelegatorDelegations(context.Context, *stakingtypes.QueryDelegatorDelegationsRequest, ...grpc.CallOption) (*stakingtypes.QueryDelegatorDelegationsResponse, error) {
	if n.err != nil {
		return nil, n.err
	}
	return &stakingtypes.QueryDelegatorDelegationsResponse{DelegationResponses: stakingtypes.DelegationResponses{
		{Delegation: stakingtypes.Delegation{DelegatorAddress: addr, ValidatorAddress: val1}, Balance: sdk.NewInt64Coin("ubze", 123)},
	}}, nil
}

func (n *node) DelegatorUnbondingDelegations(context.Context, *stakingtypes.QueryDelegatorUnbondingDelegationsRequest, ...grpc.CallOption) (*stakingtypes.QueryDelegatorUnbondingDelegationsResponse, error) {
	if n.err != nil {
		return nil, n.err
	}
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	return &stakingtypes.QueryDelegatorUnbondingDelegationsResponse{UnbondingResponses: stakingtypes.UnbondingDelegations{
		{DelegatorAddress: addr, ValidatorAddress: val2, Entries: []stakingtypes.UnbondingDelegationEntry{
			{CompletionTime: at, Balance: sdkmath.NewInt(5)},
			{CompletionTime: at.Add(time.Hour), Balance: sdkmath.NewInt(7)},
		}},
	}}, nil
}

func (n *node) DelegationTotalRewards(context.Context, *distrtypes.QueryDelegationTotalRewardsRequest, ...grpc.CallOption) (*distrtypes.QueryDelegationTotalRewardsResponse, error) {
	if n.err != nil {
		return nil, n.err
	}
	return &distrtypes.QueryDelegationTotalRewardsResponse{
		Rewards: []distrtypes.DelegationDelegatorReward{{ValidatorAddress: val1, Reward: sdk.DecCoins{
			sdk.NewDecCoinFromDec("ubze", sdkmath.LegacyMustNewDecFromStr("19010032.856273391050450376")),
			sdk.NewDecCoinFromDec("uvdl", sdkmath.LegacyMustNewDecFromStr("0.5")),
		}}},
		Total: sdk.DecCoins{sdk.NewDecCoinFromDec("ubze", sdkmath.LegacyMustNewDecFromStr("19010032.856273391050450376"))},
	}, nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newReader(n *node, c *clock) *chainstate.Reader {
	return chainstate.New(chainstate.Config{Now: c.now}, chainstate.Deps{Bank: n, Staking: n, Distribution: n})
}

func TestAccountReadsEveryQuery(t *testing.T) {
	n := &node{balances: []sdk.Coins{{sdk.NewInt64Coin("ubze", 30063263)}}}
	acc, err := newReader(n, &clock{time.Unix(0, 0)}).Account(context.Background(), addr)
	require.NoError(t, err)

	assert.Equal(t, []chainstate.Coin{{Denom: "ubze", Amount: "30063263"}}, acc.Balances)
	assert.Equal(t, []chainstate.Delegation{{Validator: val1, Amount: chainstate.Coin{Denom: "ubze", Amount: "123"}}}, acc.Delegations)
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, []chainstate.Unbonding{
		{Validator: val2, Amount: chainstate.Coin{Denom: "ubze", Amount: "5"}, CompletionTime: at},
		{Validator: val2, Amount: chainstate.Coin{Denom: "ubze", Amount: "7"}, CompletionTime: at.Add(time.Hour)},
	}, acc.Unbonding, "one item per entry")
	assert.Equal(t, chainstate.Rewards{
		ByValidator: []chainstate.Reward{{Validator: val1, Coins: []chainstate.Coin{{Denom: "ubze", Amount: "19010032"}}}},
		Total:       []chainstate.Coin{{Denom: "ubze", Amount: "19010032"}},
	}, acc.Rewards, "truncated to base units; a coin under one unit is left out")
}

func TestAnyFailureFailsTheAccount(t *testing.T) {
	n := &node{err: errors.New("connection refused")}
	_, err := newReader(n, &clock{}).Account(context.Background(), addr)
	assert.ErrorContains(t, err, "connection refused")
}

func TestBalancesFollowTheNextKey(t *testing.T) {
	n := &node{balances: []sdk.Coins{{sdk.NewInt64Coin("uaaa", 1)}, {sdk.NewInt64Coin("ubbb", 2)}}}
	got, err := newReader(n, &clock{}).Balances(context.Background(), addr)
	require.NoError(t, err)
	assert.Equal(t, []chainstate.Coin{{Denom: "uaaa", Amount: "1"}, {Denom: "ubbb", Amount: "2"}}, got)
	require.Len(t, n.pages, 2)
	assert.Nil(t, n.pages[0].Key)
	assert.Equal(t, []byte{1}, n.pages[1].Key)
	assert.Equal(t, uint64(chainstate.PageLimit), n.pages[1].Limit)
}

func TestAnswersAreCachedForTheTTL(t *testing.T) {
	n := &node{balances: []sdk.Coins{{sdk.NewInt64Coin("ubze", 1)}}}
	c := &clock{time.Unix(100, 0)}
	r := newReader(n, c)
	ctx := context.Background()

	for range 3 {
		_, err := r.Balances(ctx, addr)
		require.NoError(t, err)
		n.pages = nil // one page per call
	}
	assert.EqualValues(t, 1, n.calls.Load(), "served from the cache")

	_, err := r.Balances(ctx, "bze1other")
	require.NoError(t, err)
	assert.EqualValues(t, 2, n.calls.Load(), "keyed by address")
	n.pages = nil

	c.t = c.t.Add(chainstate.DefaultTTL)
	_, err = r.Balances(ctx, addr)
	require.NoError(t, err)
	assert.EqualValues(t, 3, n.calls.Load(), "expired after the TTL")
}

func TestErrorsAreNotCached(t *testing.T) {
	n := &node{balances: []sdk.Coins{{sdk.NewInt64Coin("ubze", 1)}}, err: errors.New("down")}
	r := newReader(n, &clock{})
	_, err := r.Balances(context.Background(), addr)
	require.Error(t, err)

	n.err = nil
	got, err := r.Balances(context.Background(), addr)
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestConcurrentMissesShareOneCall(t *testing.T) {
	n := &node{balances: []sdk.Coins{{sdk.NewInt64Coin("ubze", 1)}}, block: make(chan struct{})}
	r := newReader(n, &clock{})

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Balances(context.Background(), addr)
			assert.NoError(t, err)
		}()
	}
	require.Eventually(t, func() bool { return n.calls.Load() == 1 }, time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond) // let the others join the call
	close(n.block)
	wg.Wait()
	assert.EqualValues(t, 1, n.calls.Load())
}

func TestACallerThatGivesUpDoesNotFailTheOthers(t *testing.T) {
	n := &node{balances: []sdk.Coins{{sdk.NewInt64Coin("ubze", 1)}}, block: make(chan struct{})}
	r := newReader(n, &clock{})

	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := r.Balances(ctx, addr)
		first <- err
	}()
	require.Eventually(t, func() bool { return n.calls.Load() == 1 }, time.Second, time.Millisecond)
	cancel()
	assert.ErrorIs(t, <-first, context.Canceled)

	second := make(chan error, 1)
	go func() {
		_, err := r.Balances(context.Background(), addr)
		second <- err
	}()
	close(n.block)
	assert.NoError(t, <-second)
	assert.EqualValues(t, 1, n.calls.Load(), "the second caller joined the call still in flight")
}
