package tickers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
	"github.com/bze-alphateam/bze-scan/backend/internal/orderbook"
	"github.com/bze-alphateam/bze-scan/backend/internal/tickers"
	"github.com/bze-alphateam/bze-scan/backend/internal/tradehistory"
)

var now = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

// fakes is one market (ubze/uusdc, traded at 0.05 then 0.06) and one pool,
// with switches to fail the candles.
type fakes struct {
	candlesErr error
}

func (fakes) Markets(context.Context) ([]orderbook.Market, error) {
	return []orderbook.Market{{ID: "ubze/uusdc", Base: "ubze", Quote: "uusdc"}}, nil
}

func (fakes) Book(context.Context, string) (*orderbook.Book, error) { return &orderbook.Book{}, nil }

func (f fakes) Candles(_ context.Context, market string, minutes int, from time.Time, _ int) ([]candles.Candle, error) {
	if f.candlesErr != nil {
		return nil, f.candlesErr
	}
	a, _ := candles.FillTrade(1, now.Add(-2*time.Hour), "0.05", "1000000")
	b, _ := candles.FillTrade(2, now.Add(-time.Hour), "0.06", "1000000")
	var out []candles.Candle
	for _, c := range candles.Build(market, []candles.Trade{a, b}) {
		if c.Minutes == minutes && !c.Start.Before(from) {
			out = append([]candles.Candle{c}, out...)
		}
	}
	return out, nil
}

func (fakes) Trades(_ context.Context, f tradehistory.Filter, _ int, _ ...string) ([]tradehistory.Trade, error) {
	if f.MarketID != "ubze/uusdc" {
		return []tradehistory.Trade{}, nil
	}
	return []tradehistory.Trade{{TradeID: 2, Price: "0.06"}}, nil
}

func (fakes) Heights(context.Context, int64, int64) (int64, int64, bool, error) {
	return 1, 2, true, nil
}

func (fakes) Pools(context.Context) ([]tickers.Pool, error) {
	return []tickers.Pool{{ID: "ubze_uusdc", Base: "ubze", Quote: "uusdc", LPDenom: "ulp_ubze_uusdc",
		ReserveBase: sdkmath.NewInt(1), ReserveQuote: sdkmath.NewInt(1)}}, nil
}

func (fakes) Supply(context.Context, string) (sdkmath.Int, error) { return sdkmath.NewInt(1), nil }

type exps map[string]int

func (x exps) Exponents(context.Context, []string) (map[string]int, error) { return x, nil }

func service(f fakes) *tickers.Service {
	lg, _ := logtest.NewNullLogger()
	return tickers.New(tickers.Config{NativeDenom: "ubze", USDCDenom: "uusdc"}, tickers.Deps{
		Books: f, Candles: f, Trades: f, Heights: f, Pools: f, Exponents: exps{"ubze": 6, "uusdc": 6},
		Log: lg, Clock: &clock{now: now},
	})
}

// Ticker builds one market for the prices job, without its liquidity; an
// unknown market is nil, and a part that fails is an error (where Tickers
// serves the rest).
func TestTickerOfOneMarket(t *testing.T) {
	ctx := context.Background()
	tk, err := service(fakes{}).Ticker(ctx, "ubze/uusdc")
	require.NoError(t, err)
	require.NotNil(t, tk)
	assert.Equal(t, "1/20", tk.OpenPrice.String())
	assert.Equal(t, "3/50", tk.LastPrice.String())
	assert.Equal(t, float32(20), tk.Change)

	pool, err := service(fakes{}).Ticker(ctx, "ubze_uusdc")
	require.NoError(t, err)
	assert.True(t, pool.Pool)
	assert.Nil(t, pool.LiquidityUSD, "no liquidity for the prices job")

	none, err := service(fakes{}).Ticker(ctx, "ubze/unone")
	require.NoError(t, err)
	assert.Nil(t, none)

	failing := fakes{candlesErr: errors.New("db down")}
	_, err = service(failing).Ticker(ctx, "ubze/uusdc")
	require.ErrorContains(t, err, "db down")
	list, err := service(failing).Tickers(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "3/50", list[0].LastPrice.String(), "the rest of the ticker still answers")
}

// tradebinNode pages the pools two by two and fails on demand.
type tradebinNode struct {
	pools []tradebintypes.LiquidityPool
	calls int
	fail  bool
	// started and release, when set, hold each read until released.
	started, release chan struct{}
}

func (n *tradebinNode) AllLiquidityPools(ctx context.Context, in *tradebintypes.QueryAllLiquidityPoolsRequest,
	_ ...grpc.CallOption) (*tradebintypes.QueryAllLiquidityPoolsResponse, error) {
	n.calls++
	if n.release != nil {
		n.started <- struct{}{}
		select {
		case <-n.release:
		case <-ctx.Done():
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if n.fail {
		return nil, errors.New("node down")
	}
	start := 0
	if len(in.GetPagination().GetKey()) > 0 {
		start = int(in.GetPagination().GetKey()[0])
	}
	end := min(start+2, len(n.pools))
	res := &tradebintypes.QueryAllLiquidityPoolsResponse{List: n.pools[start:end], Pagination: &query.PageResponse{}}
	if end < len(n.pools) {
		res.Pagination.NextKey = []byte{byte(end)}
	}
	return res, nil
}

type bankNode struct {
	calls int
	fail  bool
}

func (b *bankNode) SupplyOf(_ context.Context, in *banktypes.QuerySupplyOfRequest, _ ...grpc.CallOption) (*banktypes.QuerySupplyOfResponse, error) {
	b.calls++
	if b.fail {
		return nil, errors.New("node down")
	}
	return &banktypes.QuerySupplyOfResponse{Amount: sdk.NewCoin(in.GetDenom(), sdkmath.NewInt(int64(100*b.calls)))}, nil
}

func lp(id string) tradebintypes.LiquidityPool {
	return tradebintypes.LiquidityPool{Id: id, Base: "a", Quote: "b", LpDenom: "ulp_" + id,
		ReserveBase: sdkmath.NewInt(1), ReserveQuote: sdkmath.NewInt(2)}
}

// NodePools reads every page, keeps a read 10 s and serves the last good
// one while the node fails.
func TestNodePools(t *testing.T) {
	ctx := context.Background()
	tb := &tradebinNode{pools: []tradebintypes.LiquidityPool{lp("a_b"), lp("a_c"), lp("a_d")}}
	bank := &bankNode{}
	c := &clock{now: now}
	n := tickers.NewNodePools(tb, bank, 0, c)

	pools, err := n.Pools(ctx)
	require.NoError(t, err)
	require.Len(t, pools, 3)
	assert.Equal(t, tickers.Pool{ID: "a_d", Base: "a", Quote: "b", LPDenom: "ulp_a_d", ReserveBase: sdkmath.NewInt(1),
		ReserveQuote: sdkmath.NewInt(2)}, pools[2])
	assert.Equal(t, 2, tb.calls, "two pages")
	_, _ = n.Pools(ctx)
	assert.Equal(t, 2, tb.calls, "kept")

	s, err := n.Supply(ctx, "ulp_a_b")
	require.NoError(t, err)
	assert.Equal(t, "100", s.String())
	s, _ = n.Supply(ctx, "ulp_a_b")
	assert.Equal(t, "100", s.String(), "kept")

	c.now = c.now.Add(tickers.DefaultPoolsTTL)
	tb.fail, bank.fail = true, true
	pools, err = n.Pools(ctx)
	require.NoError(t, err)
	assert.Len(t, pools, 3, "the last good read")
	s, err = n.Supply(ctx, "ulp_a_b")
	require.NoError(t, err)
	assert.Equal(t, "100", s.String(), "the last good read")

	_, err = tickers.NewNodePools(tb, bank, 0, c).Pools(ctx)
	require.Error(t, err, "nothing read yet")
	_, err = tickers.NewNodePools(tb, bank, 0, c).Supply(ctx, "ulp_a_b")
	require.Error(t, err)
}

// The pools read is shared by every caller of the flight: the first caller
// hanging up fails none of them. Regression: the read ran on the first
// caller's context, so a cancelled first caller on a cold start failed
// every caller with nothing to fall back on.
func TestNodePoolsReadOutlivesTheFirstCaller(t *testing.T) {
	tb := &tradebinNode{pools: []tradebintypes.LiquidityPool{lp("a_b")},
		started: make(chan struct{}, 1), release: make(chan struct{})}
	n := tickers.NewNodePools(tb, &bankNode{}, 0, &clock{now: now})

	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := n.Pools(ctx)
		first <- err
	}()
	<-tb.started
	cancel()
	second := make(chan []tickers.Pool, 1)
	go func() {
		pools, _ := n.Pools(context.Background())
		second <- pools
	}()
	close(tb.release)
	require.NoError(t, <-first)
	assert.Len(t, <-second, 1)
	assert.Equal(t, 1, tb.calls, "one read for both callers")
}

// priceFakes adds two tokens to fakes: ulmn trades on ulmn/uusdc at 0.5 and
// has a BZE pool at 40 BZE, udog has only a BZE pool at 2 BZE. BZE is $1.
type priceFakes struct{ fakes }

func (priceFakes) Trades(_ context.Context, f tradehistory.Filter, _ int, _ ...string) ([]tradehistory.Trade, error) {
	if f.MarketID != "ulmn/uusdc" {
		return []tradehistory.Trade{}, nil
	}
	return []tradehistory.Trade{{TradeID: 3, Price: "0.5"}}, nil
}

func (priceFakes) Pools(context.Context) ([]tickers.Pool, error) {
	return []tickers.Pool{
		{ID: "ubze_uusdc", Base: "ubze", Quote: "uusdc", ReserveBase: sdkmath.NewInt(1), ReserveQuote: sdkmath.NewInt(1)},
		{ID: "ubze_ulmn", Base: "ubze", Quote: "ulmn", ReserveBase: sdkmath.NewInt(40), ReserveQuote: sdkmath.NewInt(1)},
		{ID: "ubze_udog", Base: "ubze", Quote: "udog", ReserveBase: sdkmath.NewInt(2), ReserveQuote: sdkmath.NewInt(1)},
	}, nil
}

// A token's USD price is its USDC market's price whenever it has one, never
// averaged with the BZE route; the BZE route prices only a token without a
// USDC price. Regression: the two routes were averaged, so a token with a
// stale or thin BZE pool got a USD price far from its USDC market.
func TestUSDPrefersTheUSDCMarket(t *testing.T) {
	ctx := context.Background()
	lg, _ := logtest.NewNullLogger()
	f := priceFakes{}
	s := tickers.New(tickers.Config{NativeDenom: "ubze", USDCDenom: "uusdc"}, tickers.Deps{
		Books: f, Candles: f, Trades: f, Heights: f, Pools: f,
		Exponents: exps{"ubze": 6, "uusdc": 6, "ulmn": 6, "udog": 6}, Log: lg, Clock: &clock{now: now},
	})
	assert.Equal(t, "1", s.USD(ctx, "ubze").RatString())
	assert.Equal(t, "1/2", s.USD(ctx, "ulmn").RatString(), "the USDC market, not (0.5 + 40) / 2")
	assert.Equal(t, "2", s.USD(ctx, "udog").RatString(), "no USDC price: the BZE route")
	assert.Equal(t, "0", s.USD(ctx, "unone").RatString())
}
