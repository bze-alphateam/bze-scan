package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/labstack/echo/v5"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/server"
	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
	"github.com/bze-alphateam/bze-scan/backend/internal/marketmerge"
	"github.com/bze-alphateam/bze-scan/backend/internal/orderbook"
	"github.com/bze-alphateam/bze-scan/backend/internal/tickers"
	"github.com/bze-alphateam/bze-scan/backend/internal/tradehistory"
)

// tkNow is the tickers' clock: the window starts at 2026-10-27T12:00:00Z.
var tkNow = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

// tkTrade is a fill or swap of the fakes, in chain units; with 6-decimal
// denoms the chain price is the display price.
type tkTrade struct {
	id     int64
	at     time.Time
	price  string
	amount int64
}

// tkEnv is the fakes behind one tickers service.
type tkEnv struct {
	mu         sync.Mutex
	clock      time.Time
	markets    []orderbook.Market
	marketsErr error
	books      map[string]*orderbook.Book
	bookErr    map[string]error
	trades     map[string][]tkTrade
	pools      []tickers.Pool
	poolsErr   error
	supplies   map[string]sdkmath.Int
	gecko      map[string]string
	tradesErr  error
	exps       swapExponents

	marketCalls, geckoCalls int
}

func newTkEnv() *tkEnv {
	return &tkEnv{clock: tkNow, books: map[string]*orderbook.Book{}, bookErr: map[string]error{},
		trades: map[string][]tkTrade{}, supplies: map[string]sdkmath.Int{},
		exps: swapExponents{"ubze": 6, "uusdc": 6, "uvdl": 6, "ucorey": 6, "unew": 6, "uold": 6, "ubig": 18}}
}

func (e *tkEnv) Now() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.clock
}

func (e *tkEnv) advance(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.clock = e.clock.Add(d)
}

func (e *tkEnv) Markets(ctx context.Context) ([]orderbook.Market, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.marketCalls++
	return e.markets, e.marketsErr
}

func (e *tkEnv) Book(ctx context.Context, id string) (*orderbook.Book, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := e.bookErr[id]; err != nil {
		return nil, err
	}
	if b := e.books[id]; b != nil {
		return b, nil
	}
	return &orderbook.Book{}, nil
}

// Candles builds the candles of the market's trades, as the writers do.
func (e *tkEnv) Candles(ctx context.Context, market string, minutes int, from time.Time, limit int) ([]candles.Candle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var trades []candles.Trade
	for _, t := range e.trades[market] {
		tr, err := candles.FillTrade(t.id, t.at, t.price, fmt.Sprint(t.amount))
		if err != nil {
			return nil, err
		}
		trades = append(trades, tr)
	}
	var out []candles.Candle
	built := candles.Build(market, trades)
	for i := len(built) - 1; i >= 0; i-- {
		c := built[i]
		if c.Minutes == minutes && !c.Start.Before(from) && len(out) < limit {
			out = append(out, c)
		}
	}
	return out, nil
}

// Heights makes a block of every millisecond: its height is its unix
// milliseconds.
func (e *tkEnv) Heights(ctx context.Context, fromMs, toMs int64) (lo, hi int64, ok bool, err error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, false, err
	}
	return fromMs, toMs, true, nil
}

// Trades answers the newest trade of the market, in the heights' window
// when one is set.
func (e *tkEnv) Trades(ctx context.Context, f tradehistory.Filter, limit int, _ ...string) ([]tradehistory.Trade, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.tradesErr != nil {
		return nil, e.tradesErr
	}
	var best *tkTrade
	for _, t := range e.trades[f.MarketID] {
		ms := t.at.UnixMilli()
		if f.FromHeight > 0 && (ms < f.FromHeight || ms > f.ToHeight) {
			continue
		}
		if best == nil || t.id > best.id {
			best = &t
		}
	}
	if best == nil || limit < 1 {
		return []tradehistory.Trade{}, nil
	}
	return []tradehistory.Trade{{TradeID: best.id, TimeMs: best.at.UnixMilli(), Price: best.price}}, nil
}

func (e *tkEnv) Pools(ctx context.Context) ([]tickers.Pool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return e.pools, e.poolsErr
}

func (e *tkEnv) Supply(ctx context.Context, denom string) (sdkmath.Int, error) {
	if err := ctx.Err(); err != nil {
		return sdkmath.Int{}, err
	}
	s, ok := e.supplies[denom]
	if !ok {
		return sdkmath.Int{}, errors.New("no supply")
	}
	return s, nil
}

func (e *tkEnv) USD(ctx context.Context) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.geckoCalls++
	if e.gecko == nil {
		return nil, errors.New("coingecko down")
	}
	return e.gecko, nil
}

// pool adds a pool with reserves in base units and an LP supply of one
// whole LP token, so its liquidity is its value.
func (e *tkEnv) pool(base, quote string, reserveBase, reserveQuote int64) string {
	id := base + "_" + quote
	e.pools = append(e.pools, tickers.Pool{ID: id, Base: base, Quote: quote, LPDenom: tickers.LPPrefix + id,
		ReserveBase: sdkmath.NewInt(reserveBase), ReserveQuote: sdkmath.NewInt(reserveQuote)})
	e.supplies[tickers.LPPrefix+id] = sdkmath.NewInt(1_000_000_000_000)
	return id
}

func (e *tkEnv) market(base, quote string) string {
	id := base + "/" + quote
	e.markets = append(e.markets, orderbook.Market{ID: id, Base: base, Quote: quote})
	return id
}

func (e *tkEnv) service() *tickers.Service {
	lg, _ := logtest.NewNullLogger()
	return tickers.New(tickers.Config{NativeDenom: "ubze", USDCDenom: "uusdc"}, tickers.Deps{
		Books: e, Candles: e, Trades: e, Heights: e, Pools: e, Exponents: e.exps, CoinGecko: e, Log: lg, Clock: e,
	})
}

func (e *tkEnv) server(merges marketmerge.Merges) *echo.Echo {
	lg, _ := logtest.NewNullLogger()
	return server.New(server.Deps{CompatTickers: e.service(), CoinGeckoMerges: merges, Log: lg})
}

func tickersBody(t *testing.T, e *echo.Echo, query string) string {
	t.Helper()
	rec := corsRequest(e, http.MethodGet, "/aggregator/api/dex/tickers"+query, "https://dex.getbze.com")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	return rec.Body.String()
}

const tkZero = `"last_price":0,"base_volume":0,"quote_volume":0,"bid":0,"ask":0,"high":0,"low":0,"open_price":0,"change":0}`

// The default format: every market and pool by market id (the aggregator's
// order is random), the book's best levels, the candles starting in the
// last 24 hours (the bucket starting exactly 24 hours ago included), the
// newest trade of the window. A market whose book fails keeps the rest of
// its ticker.
func TestCompatTickersDefaultFormat(t *testing.T) {
	e := newTkEnv()
	vdl := e.market("uvdl", "ubze")
	bze := e.market("ubze", "uusdc")
	pool := e.pool("ubze", "uusdc", 0, 0)
	e.books[bze] = &orderbook.Book{Bids: []orderbook.Level{{Price: "0.049"}, {Price: "0.048"}}, Asks: []orderbook.Level{{Price: "0.051"}}}
	e.bookErr[vdl] = errors.New("node down")
	since := tkNow.Add(-24 * time.Hour)
	e.trades[bze] = []tkTrade{
		{1, since.Add(-time.Second), "0.04", 1_000_000},
		{2, since, "0.045", 2_000_000},
		{3, tkNow.Add(-time.Hour), "0.05", 1_000_000},
		{4, tkNow.Add(-30 * time.Minute), "0.052", 1_000_000},
	}
	e.trades[vdl] = []tkTrade{{5, tkNow.Add(-25 * time.Hour), "2", 1_000_000}}

	assert.Equal(t, `[{"base":"ubze","quote":"uusdc","market_id":"`+bze+`","last_price":0.052,"base_volume":4,`+
		`"quote_volume":0.192,"bid":0.049,"ask":0.051,"high":0.052,"low":0.045,"open_price":0.045,"change":15.56},`+
		`{"base":"ubze","quote":"uusdc","market_id":"`+pool+`",`+tkZero+`,`+
		`{"base":"uvdl","quote":"ubze","market_id":"`+vdl+`",`+tkZero+`]`+"\n",
		tickersBody(t, e.server(nil), ""))
}

// The last price is the newest trade after now − 24 h, as the aggregator's
// executed_at > now − 24 h: one exactly 24 hours old is not.
func TestCompatTickersLastPriceWindow(t *testing.T) {
	e := newTkEnv()
	m := e.market("ubze", "uusdc")
	since := tkNow.Add(-24 * time.Hour)
	e.trades[m] = []tkTrade{{1, since.Add(-5 * time.Minute), "1", 1_000_000}, {2, since, "2", 1_000_000}}
	assert.Contains(t, tickersBody(t, e.server(nil), ""), `"last_price":0,`, "no trade after now − 24 h")

	e.trades[m] = append(e.trades[m], tkTrade{3, since.Add(time.Millisecond), "3", 1_000_000})
	assert.Contains(t, tickersBody(t, e.server(nil), ""), `"last_price":3,`)
}

// change: (last − open) / open × 100 to two decimals, 0 with a zero open.
func TestCompatTickersChange(t *testing.T) {
	for name, tc := range map[string]struct {
		trades []tkTrade
		change string
	}{
		"rounded":     {[]tkTrade{{1, tkNow.Add(-time.Hour), "3", 1}, {2, tkNow.Add(-time.Minute), "3.1", 1}}, "3.33"},
		"down":        {[]tkTrade{{1, tkNow.Add(-time.Hour), "3", 1}, {2, tkNow.Add(-time.Minute), "2", 1}}, "-33.33"},
		"half up":     {[]tkTrade{{1, tkNow.Add(-time.Hour), "8", 1}, {2, tkNow.Add(-time.Minute), "8.0004", 1}}, "0.01"},
		"zero open":   {[]tkTrade{{1, tkNow.Add(-time.Hour), "0", 1}, {2, tkNow.Add(-time.Minute), "1", 1}}, "0"},
		"no movement": {[]tkTrade{{1, tkNow.Add(-time.Hour), "5", 1}}, "0"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newTkEnv()
			m := e.market("ubze", "uusdc")
			e.trades[m] = tc.trades
			assert.Contains(t, tickersBody(t, e.server(nil), ""), `"change":`+tc.change+`}`)
		})
	}
}

// A trade id handed over twice (the chain's doubled v8.0.x events) counts
// once in the volumes.
func TestCompatTickersCountATradeOnce(t *testing.T) {
	e := newTkEnv()
	m := e.market("ubze", "uusdc")
	tr := tkTrade{1, tkNow.Add(-time.Hour), "2", 1_000_000}
	e.trades[m] = []tkTrade{tr, tr}
	assert.Contains(t, tickersBody(t, e.server(nil), ""), `"base_volume":1,"quote_volume":2,`)
}

// BZE-133: a quote with 12 decimals more than the base, where the
// aggregator's factor is 0, keeps its price.
func TestCompatTickersBigExponentGap(t *testing.T) {
	e := newTkEnv()
	m := e.market("uusdc", "ubig")
	e.trades[m] = []tkTrade{{1, tkNow.Add(-time.Hour), "1000000000000", 1_000_000}}
	assert.Contains(t, tickersBody(t, e.server(nil), ""),
		`"base_volume":1,"quote_volume":1,"bid":0,"ask":0,"high":1,"low":1,"open_price":1,`)
}

// The CoinGecko format: in market id order, a pool's ids prefixed lp_, its
// liquidity in USD (USDC at $1, BZE through the BZE/USDC pool).
func TestCompatTickersCoinGeckoFormat(t *testing.T) {
	e := newTkEnv()
	m := e.market("ubze", "uusdc")
	pool := e.pool("ubze", "uusdc", 1_000_000_000, 50_000_000)
	e.books[m] = &orderbook.Book{Bids: []orderbook.Level{{Price: "0.049"}}, Asks: []orderbook.Level{{Price: "0.051"}}}
	e.trades[m] = []tkTrade{{1, tkNow.Add(-time.Hour), "0.05", 2_000_000}}
	e.trades[pool] = []tkTrade{{2, tkNow.Add(-time.Minute), "0.04", 1_000_000}}

	assert.Equal(t, `[{"ticker_id":"ubze_uusdc","base_currency":"ubze","target_currency":"uusdc","pool_id":"ubze/uusdc",`+
		`"last_price":0.05,"base_volume":2,"target_volume":0.1,"bid":0.049,"ask":0.051,"high":0.05,"low":0.05},`+
		`{"ticker_id":"lp_ubze_uusdc","base_currency":"ubze","target_currency":"uusdc","pool_id":"lp_ubze_uusdc",`+
		`"last_price":0.04,"base_volume":1,"target_volume":0.04,"bid":0,"ask":0,"high":0.04,"low":0.04,"liquidity_in_usd":100}]`+"\n",
		tickersBody(t, e.server(nil), "?format=coingecko"))
	// Any other format is the default one.
	assert.Contains(t, tickersBody(t, e.server(nil), "?format=tv"), `"market_id":"ubze/uusdc"`)
}

// liquidityOf is the liquidity_in_usd of the pool id's CoinGecko ticker, ""
// when it has none.
func liquidityOf(t *testing.T, e *tkEnv, id string) string {
	t.Helper()
	body := tickersBody(t, e.server(nil), "?format=coingecko")
	var list []struct {
		PoolID    string   `json:"pool_id"`
		Liquidity *float64 `json:"liquidity_in_usd"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &list))
	for _, c := range list {
		if c.PoolID == "lp_"+id {
			if c.Liquidity == nil {
				return ""
			}
			return fmt.Sprint(*c.Liquidity)
		}
	}
	t.Fatalf("pool %s not listed", id)
	return ""
}

// The USD prices behind the liquidity, step by step: BZE from its USDC
// pool, else its last order book price of the last 24 hours, else its last
// trade ever, else CoinGecko's bzedge price (which the aggregator never
// finds); another token from its USDC route, its BZE route, or the average
// of both.
func TestCompatTickersUSDPrices(t *testing.T) {
	// VDL/BZE: 1000 BZE and 500 VDL, so a VDL is 2 BZE.
	vdlPool := func(e *tkEnv) string { return e.pool("ubze", "uvdl", 1_000_000_000, 500_000_000) }
	for name, tc := range map[string]struct {
		setup func(e *tkEnv)
		want  string
	}{
		"BZE from its USDC pool": {func(e *tkEnv) {
			e.pool("ubze", "uusdc", 1_000_000_000, 50_000_000)
		}, "100"},
		"BZE from the last 24 hours of its market": {func(e *tkEnv) {
			e.pool("ubze", "uusdc", 0, 0)
			e.trades["ubze/uusdc"] = []tkTrade{{1, tkNow.Add(-time.Hour), "0.04", 1}}
		}, "80"},
		"BZE from its last trade ever": {func(e *tkEnv) {
			e.trades["ubze/uusdc"] = []tkTrade{{1, tkNow.Add(-30 * 24 * time.Hour), "0.03", 1}}
		}, "60"},
		"BZE from CoinGecko": {func(e *tkEnv) {
			e.gecko = map[string]string{"bzedge": "0.02", "ubze": "9"}
		}, "40"},
		"no BZE price": {func(*tkEnv) {}, ""},
		"VDL from both routes, averaged": {func(e *tkEnv) {
			e.pool("ubze", "uusdc", 1_000_000_000, 50_000_000)
			// 30 USDC and 100 VDL: 0.3 USD a VDL, 0.1 through BZE.
			e.pool("uusdc", "uvdl", 30_000_000, 100_000_000)
		}, "150"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newTkEnv()
			tc.setup(e)
			assert.Equal(t, tc.want, liquidityOf(t, e, vdlPool(e)))
		})
	}

	// COREY from its USDC route only (no BZE pool or market): 10 COREY for
	// 5 USDC.
	e := newTkEnv()
	assert.Equal(t, "10", liquidityOf(t, e, e.pool("ucorey", "uusdc", 10_000_000, 5_000_000)))
	// A pool without an LP supply has no liquidity.
	delete(e.supplies, tickers.LPPrefix+"ucorey_uusdc")
	assert.Empty(t, liquidityOf(t, e, "ucorey_uusdc"))
}

// The USD prices are kept 60 s, the tickers 5 s.
func TestCompatTickersCaches(t *testing.T) {
	e := newTkEnv()
	e.market("ubze", "uusdc")
	e.pool("ubze", "uvdl", 1_000_000_000, 500_000_000)
	e.gecko = map[string]string{"bzedge": "0.02"}
	srv := e.server(nil)

	first := tickersBody(t, srv, "?format=coingecko")
	e.trades["ubze/uusdc"] = []tkTrade{{1, tkNow.Add(-time.Minute), "7", 1}}
	assert.Equal(t, first, tickersBody(t, srv, "?format=coingecko"), "served from the cache")
	tickersBody(t, srv, "")
	assert.Equal(t, 1, e.marketCalls, "one build for both formats")
	assert.Equal(t, 1, e.geckoCalls)

	e.advance(tickers.DefaultTTL)
	assert.Contains(t, tickersBody(t, srv, ""), `"last_price":7,`, "rebuilt after 5 s")
	assert.Equal(t, 2, e.marketCalls)
	assert.Contains(t, tickersBody(t, srv, "?format=coingecko"), `"liquidity_in_usd":40}`, "BZE's USD price still the kept one")

	e.advance(tickers.DefaultPriceTTL)
	assert.Contains(t, tickersBody(t, srv, "?format=coingecko"), `"liquidity_in_usd":14000}`,
		"BZE at 7 USDC from its last trade once the price expired")
}

// COINGECKO_MERGED_MARKETS: the new market is not listed; its volumes are
// added, high and low span both, its last price and its bid/ask pair win
// when it has them. The default format keeps both.
func TestCompatTickersMergedMarkets(t *testing.T) {
	e := newTkEnv()
	oldM := e.market("ubze", "uold")
	newM := e.market("ubze", "unew")
	e.trades[oldM] = []tkTrade{{1, tkNow.Add(-2 * time.Hour), "1", 1_000_000}, {2, tkNow.Add(-time.Hour), "2", 1_000_000}}
	e.books[oldM] = &orderbook.Book{Bids: []orderbook.Level{{Price: "1.9"}}, Asks: []orderbook.Level{{Price: "2.6"}}}
	merges := marketmerge.Merges{newM: oldM}

	// The new market without trades or orders: the old one's figures stay.
	assert.Equal(t, `[{"ticker_id":"ubze_uold","base_currency":"ubze","target_currency":"uold","pool_id":"ubze/uold",`+
		`"last_price":2,"base_volume":2,"target_volume":3,"bid":1.9,"ask":2.6,"high":2,"low":1}]`+"\n",
		tickersBody(t, e.server(merges), "?format=coingecko"))

	e.trades[newM] = []tkTrade{{3, tkNow.Add(-90 * time.Minute), "3", 1_000_000}, {4, tkNow.Add(-10 * time.Minute), "0.5", 2_000_000}}
	e.books[newM] = &orderbook.Book{Bids: []orderbook.Level{{Price: "2.4"}}}
	assert.Equal(t, `[{"ticker_id":"ubze_uold","base_currency":"ubze","target_currency":"uold","pool_id":"ubze/uold",`+
		`"last_price":0.5,"base_volume":5,"target_volume":7,"bid":2.4,"ask":0,"high":3,"low":0.5}]`+"\n",
		tickersBody(t, e.server(merges), "?format=coingecko"))

	body := tickersBody(t, e.server(merges), "")
	assert.Contains(t, body, `"market_id":"ubze/unew"`)
	assert.Contains(t, body, `"market_id":"ubze/uold"`)

	// Folded into a market that is not listed: listed itself.
	assert.Contains(t, tickersBody(t, e.server(marketmerge.Merges{newM: "ubze/ugone"}), "?format=coingecko"),
		`"ticker_id":"ubze_unew"`)
}

// No market is [], never null; the market list failing serves the last
// one read; nothing ever read is the aggregator's 500.
func TestCompatTickersEmptyAndFailing(t *testing.T) {
	e := newTkEnv()
	srv := e.server(nil)
	assert.Equal(t, "[]\n", tickersBody(t, srv, ""))
	assert.Equal(t, "[]\n", tickersBody(t, srv, "?format=coingecko"))

	e = newTkEnv()
	e.market("ubze", "uusdc")
	svc := e.service()
	lg, _ := logtest.NewNullLogger()
	srv = server.New(server.Deps{CompatTickers: svc, Log: lg})
	tickersBody(t, srv, "")
	e.marketsErr, e.poolsErr = errors.New("node down"), errors.New("node down")
	e.advance(tickers.DefaultTTL)
	assert.Contains(t, tickersBody(t, srv, ""), `"market_id":"ubze/uusdc"`, "the last market list")

	e = newTkEnv()
	e.marketsErr, e.poolsErr = errors.New("node down"), errors.New("node down")
	rec := serve(t, e.server(nil), http.MethodGet, "/aggregator/api/dex/tickers")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"message":"Unknown error! Please report this issue to the administrators at alphateam@getbze.com"}`, rec.Body.String())
}

// A build started by a request that hangs up (a closed tab, a client
// timeout) still reads everything: it is cached and shared, so it must not
// end with that request. Regression: the build ran on the request's
// context and cached all-zero tickers for 5 s.
func TestCompatTickersBuildOutlivesTheRequest(t *testing.T) {
	e := newTkEnv()
	m := e.market("ubze", "uusdc")
	e.pool("ubze", "uusdc", 1_000_000_000, 50_000_000)
	e.books[m] = &orderbook.Book{Asks: []orderbook.Level{{Price: "0.051"}}}
	e.trades[m] = []tkTrade{{1, tkNow.Add(-time.Hour), "0.05", 2_000_000}}
	srv := e.server(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/aggregator/api/dex/tickers?format=coingecko", nil).WithContext(ctx)
	srv.ServeHTTP(httptest.NewRecorder(), req)

	want := `"last_price":0.05,"base_volume":2,"target_volume":0.1,"bid":0,"ask":0.051,"high":0.05,"low":0.05}`
	body := tickersBody(t, srv, "?format=coingecko")
	assert.Contains(t, body, want, "the cached build has the real figures")
	assert.Contains(t, body, `"liquidity_in_usd":100}`)
	assert.Equal(t, 1, e.marketCalls, "the next request is served from that build")
}

// A USD price computed after a failed read (a cancelled request, a store
// down) is answered but not kept, so the next read prices it. Regression:
// the zero was kept 60 s and liquidity_in_usd disappeared for a minute.
func TestCompatTickersUSDPriceAfterAFailedReadIsNotKept(t *testing.T) {
	e := newTkEnv()
	e.trades["ubze/uusdc"] = []tkTrade{{1, tkNow.Add(-time.Hour), "0.04", 1}}
	svc := e.service()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Equal(t, "0", svc.USD(ctx, "ubze").RatString(), "nothing read")
	assert.Equal(t, "1/25", svc.USD(context.Background(), "ubze").RatString())

	e = newTkEnv()
	e.trades["ubze/uusdc"] = []tkTrade{{1, tkNow.Add(-time.Hour), "0.04", 1}}
	e.tradesErr = errors.New("db down")
	svc = e.service()
	assert.Equal(t, "0", svc.USD(context.Background(), "ubze").RatString(), "the store down")
	e.tradesErr = nil
	assert.Equal(t, "1/25", svc.USD(context.Background(), "ubze").RatString())

	// A price that read everything is kept, zero included.
	e = newTkEnv()
	e.gecko = map[string]string{}
	svc = e.service()
	assert.Equal(t, "0", svc.USD(context.Background(), "ubze").RatString())
	e.trades["ubze/uusdc"] = []tkTrade{{1, tkNow.Add(-time.Hour), "0.04", 1}}
	assert.Equal(t, "0", svc.USD(context.Background(), "ubze").RatString(), "kept for 60 s")
}
