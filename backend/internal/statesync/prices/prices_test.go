package prices_test

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/prices"
	"github.com/bze-alphateam/bze-scan/backend/internal/tickers"
)

// The BZE/USDC.n liquidity pool, whose ticker prices USDC.n in ubze.
const pool = "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4_ubze"

type fakeSource struct {
	prices    map[string]string
	err       error
	ticker    *tickers.Ticker
	tickerErr error
	tickers   int
}

func (s *fakeSource) USD(context.Context) (map[string]string, error) { return s.prices, s.err }

func (s *fakeSource) Ticker(_ context.Context, market string) (*tickers.Ticker, error) {
	s.tickers++
	if s.ticker == nil || s.ticker.MarketID != market {
		return nil, s.tickerErr
	}
	return s.ticker, s.tickerErr
}

// fakeStore keeps the last write, and the change as the table keeps it.
type fakeStore struct {
	denoms    []string
	denomsErr error
	listed    int
	last      prices.Snapshot
	change    *string
	writes    int
}

func (s *fakeStore) Denoms(context.Context) ([]string, error) {
	s.listed++
	return s.denoms, s.denomsErr
}

func (s *fakeStore) Save(_ context.Context, snap prices.Snapshot) error {
	s.last = snap
	if !snap.KeepChange {
		s.change = snap.Change
	}
	s.writes++
	return nil
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

// rat is a decimal string as a number; it panics on anything else.
func rat(s string) *big.Rat {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		panic(s)
	}
	return r
}

func poolTicker(open, last string) *tickers.Ticker {
	return &tickers.Ticker{MarketID: pool, Base: "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4",
		Quote: "ubze", OpenPrice: rat(open), LastPrice: rat(last)}
}

func newSet(src *fakeSource, store *fakeStore, c *clock, lg logrus.FieldLogger) *prices.Set {
	return prices.New(prices.Deps{Prices: src, Tickers: src, Store: store, ChainID: "beezee-1", Denom: "ubze",
		ChangeMarket: pool, Log: lg, Clock: c}, 0)
}

func TestPricesAreRefreshedAndKeptWhenTheSourceFails(t *testing.T) {
	lg, hook := logtest.NewNullLogger()
	src := &fakeSource{prices: map[string]string{"bzedge": "0.000168060000"}}
	store := &fakeStore{}
	s := prices.New(prices.Deps{Prices: src, Tickers: src, Store: store, ChainID: "beezee-1", Log: lg}, 0)
	assert.Equal(t, statesync.Prices, s.Name())
	assert.Equal(t, prices.DefaultInterval, s.Interval())
	ctx := context.Background()

	require.NoError(t, s.FullResync(ctx))
	assert.Equal(t, map[string]string{"bzedge": "0.000168060000"}, store.last.Prices)
	assert.Equal(t, "beezee-1", store.last.ChainID)
	assert.True(t, store.last.KeepChange, "no change market: the change is left alone")
	assert.Zero(t, src.tickers)

	// CoinGecko fails for three minutes: nothing is written, one warning.
	src.err = errors.New("HTTP 502")
	for range 3 {
		err := s.FullResync(ctx)
		require.Error(t, err)
		assert.ErrorContains(t, err, "HTTP 502", "recorded in sync_jobs")
	}
	assert.Equal(t, 1, store.writes, "the previous prices are kept")
	warnings := 0
	for _, e := range hook.AllEntries() {
		if e.Level == logrus.WarnLevel {
			warnings++
		}
	}
	assert.Equal(t, 1, warnings, "logged once per change of state")

	// It answers again: one info line, the new prices written.
	src.err, src.prices = nil, map[string]string{"bzedge": "0.0002"}
	require.NoError(t, s.ResyncOne(ctx, "any"))
	assert.Equal(t, logrus.InfoLevel, hook.LastEntry().Level)
	assert.Equal(t, map[string]string{"bzedge": "0.0002"}, store.last.Prices)
	assert.Len(t, hook.AllEntries(), 2)
}

func TestTheChangeComesFromThePoolAndIsReadEveryFiveMinutes(t *testing.T) {
	lg, _ := logtest.NewNullLogger()
	// The pool's ticker on 2026-10-09: USDC.n rose from 5624.37 to 5747.14
	// ubze, so BZE fell 2.1362 % against USDC.
	src := &fakeSource{prices: map[string]string{"bzedge": "0.00017"}, ticker: poolTicker("5624.368717574341", "5747.1387947581325")}
	store := &fakeStore{}
	c := &clock{now: time.Unix(1000, 0)}
	s := newSet(src, store, c, lg)
	ctx := context.Background()

	require.NoError(t, s.FullResync(ctx))
	require.NotNil(t, store.change)
	assert.Equal(t, "-2.1362", *store.change)
	assert.Equal(t, "ubze", store.last.ChangeDenom)
	assert.Equal(t, 1, src.tickers)

	// Within five minutes the ticker is not read again.
	src.ticker = poolTicker("5747.1387947581325", "5624.368717574341")
	c.now = c.now.Add(prices.ChangeTTL - time.Second)
	require.NoError(t, s.FullResync(ctx))
	assert.Equal(t, 1, src.tickers)
	assert.Equal(t, "-2.1362", *store.change)

	c.now = c.now.Add(time.Second)
	require.NoError(t, s.FullResync(ctx))
	assert.Equal(t, 2, src.tickers)
	assert.Equal(t, "2.1828", *store.change)
}

// Without tickers the prices are still written and the
// change is left alone.
func TestWithoutTickersThePricesAreWrittenAndTheChangeLeftAlone(t *testing.T) {
	lg, _ := logtest.NewNullLogger()
	src := &fakeSource{prices: map[string]string{"bzedge": "0.00017"}, ticker: poolTicker("2", "1")}
	store := &fakeStore{}
	s := prices.New(prices.Deps{Prices: src, Store: store, ChainID: "beezee-1", Denom: "ubze",
		ChangeMarket: pool, Log: lg, Clock: &clock{now: time.Unix(1000, 0)}}, 0)
	require.NoError(t, s.FullResync(context.Background()))
	assert.Equal(t, map[string]string{"bzedge": "0.00017"}, store.last.Prices)
	assert.True(t, store.last.KeepChange)
	assert.Zero(t, src.tickers)
}

func TestAFailingTickerKeepsTheChangeAndStillWritesThePrices(t *testing.T) {
	lg, hook := logtest.NewNullLogger()
	src := &fakeSource{prices: map[string]string{"bzedge": "0.00017"}, ticker: poolTicker("2", "1")}
	store := &fakeStore{}
	c := &clock{now: time.Unix(1000, 0)}
	s := newSet(src, store, c, lg)
	ctx := context.Background()
	require.NoError(t, s.FullResync(ctx))
	assert.Equal(t, "100.0000", *store.change, "USDC halved in ubze: BZE doubled")

	src.tickerErr, src.prices = errors.New("HTTP 502"), map[string]string{"bzedge": "0.0003"}
	c.now = c.now.Add(prices.ChangeTTL)
	err := s.FullResync(ctx)
	require.ErrorContains(t, err, "HTTP 502")
	assert.Equal(t, map[string]string{"bzedge": "0.0003"}, store.last.Prices, "the prices are written")
	assert.Equal(t, "100.0000", *store.change, "the change read last is kept")
	require.Error(t, s.FullResync(ctx), "retried every minute while failing")
	assert.Equal(t, 1, countLevel(hook, logrus.WarnLevel))
}

func TestAMarketTheAggregatorDoesNotListClearsTheChange(t *testing.T) {
	lg, hook := logtest.NewNullLogger()
	src := &fakeSource{prices: map[string]string{}, ticker: poolTicker("1", "1")}
	store := &fakeStore{}
	c := &clock{now: time.Unix(1000, 0)}
	s := newSet(src, store, c, lg)
	require.NoError(t, s.FullResync(context.Background()))
	assert.Equal(t, "0.0000", *store.change)

	src.ticker = nil // the pool was removed (the USDC migration)
	c.now = c.now.Add(prices.ChangeTTL)
	require.NoError(t, s.FullResync(context.Background()))
	assert.False(t, store.last.KeepChange)
	assert.Nil(t, store.change)

	// A market that does not trade ubze is a configuration error.
	src.ticker = &tickers.Ticker{MarketID: pool, Base: "uatom", Quote: "uosmo", OpenPrice: rat("1"), LastPrice: rat("1")}
	c.now = c.now.Add(prices.ChangeTTL)
	require.ErrorContains(t, s.FullResync(context.Background()), "does not trade ubze")
	assert.Equal(t, 1, countLevel(hook, logrus.WarnLevel))
}

func countLevel(hook *logtest.Hook, level logrus.Level) int {
	n := 0
	for _, e := range hook.AllEntries() {
		if e.Level == level {
			n++
		}
	}
	return n
}

func TestAFailingTickerAfterARestartLeavesTheStoredChange(t *testing.T) {
	lg, _ := logtest.NewNullLogger()
	src := &fakeSource{prices: map[string]string{}, tickerErr: errors.New("HTTP 502")}
	store := &fakeStore{}
	require.Error(t, newSet(src, store, &clock{now: time.Unix(1000, 0)}, lg).FullResync(context.Background()))
	assert.True(t, store.last.KeepChange, "nothing read yet: the stored change stays")
	assert.Equal(t, 1, store.writes, "the prices are written")
}

func TestAFailureIsNotLoggedAgainByTheSyncer(t *testing.T) {
	lg, hook := logtest.NewNullLogger()
	s := prices.New(prices.Deps{Prices: &fakeSource{err: errors.New("down")}, Store: &fakeStore{}, Log: lg}, 0)
	sync := statesync.New(statesync.Config{Log: lg}, noJobs{}, s)
	require.Error(t, sync.RunOnce(context.Background()))
	require.Error(t, sync.RunOnce(context.Background()))
	assert.Len(t, hook.AllEntries(), 1, "the set's own warning only")
}

type noJobs struct{}

func (noJobs) RecordRun(context.Context, statesync.Run) error { return nil }

// The change of either side of a ticker: the pool's recorded figures
// (USDC.n +2.18 % in ubze is BZE -2.1362 % in USDC.n).
func TestChangePctOfEitherSide(t *testing.T) {
	tk := poolTicker("5624.368717574341", "5747.1387947581325")
	pct, ok := prices.ChangePct(tk, "ubze")
	require.True(t, ok)
	assert.Equal(t, "-2.1362", pct)
	pct, ok = prices.ChangePct(tk, tk.Base)
	require.True(t, ok)
	assert.Equal(t, "2.1828", pct, "the base side as the ticker's own change")
	pct, ok = prices.ChangePct(poolTicker("0.00018", "0.00018"), "ubze")
	require.True(t, ok)
	assert.Equal(t, "0.0000", pct)
}

func TestChangePctNeedsPricesAndTheDenom(t *testing.T) {
	for name, tk := range map[string]*tickers.Ticker{
		"no ticker":     nil,
		"no trade":      {Base: "ubze", Quote: "uusdc", OpenPrice: rat("0"), LastPrice: rat("1")},
		"no last price": {Base: "ubze", Quote: "uusdc", OpenPrice: rat("1"), LastPrice: rat("0")},
		"no prices":     {Base: "ubze", Quote: "uusdc"},
		"another pair":  {Base: "uatom", Quote: "uusdc", OpenPrice: rat("1"), LastPrice: rat("1")},
	} {
		_, ok := prices.ChangePct(tk, "ubze")
		assert.False(t, ok, name)
	}
	pct, ok := prices.ChangePct(&tickers.Ticker{Base: "ubze", Quote: "u", OpenPrice: rat("3"), LastPrice: rat("2")}, "ubze")
	require.True(t, ok)
	assert.Equal(t, "-33.3333", pct)
	pct, _ = prices.ChangePct(&tickers.Ticker{Base: "ubze", Quote: "u", OpenPrice: rat("200000"), LastPrice: rat("200001")}, "ubze")
	assert.Equal(t, "0.0005", pct, "0.0005 rounds away from zero")
}

// fakePricer answers the DEX price chain from fixed prices: a missing denom
// is zero (no DEX price), a nil one unanswered. It records the order of
// the calls.
type fakePricer struct {
	prices map[string]*big.Rat
	mu     sync.Mutex
	calls  []string
}

func (p *fakePricer) USD(_ context.Context, denom string) *big.Rat {
	p.mu.Lock()
	p.calls = append(p.calls, denom)
	p.mu.Unlock()
	r, ok := p.prices[denom]
	if !ok {
		return new(big.Rat)
	}
	return r
}

const honey = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney"

func TestTheDEXPriceChainPricesEveryDenomBeforeCoinGecko(t *testing.T) {
	lg, hook := logtest.NewNullLogger()
	src := &fakeSource{prices: map[string]string{"bzedge": "0.00017", "cosmos": "1.96"}}
	// ubze and HONEY trade on the DEX; ATOM has no route (zero, CoinGecko
	// prices it through its registry id); DUST is priced below the
	// column's twelve decimals; NOTHING has nothing.
	pricer := &fakePricer{prices: map[string]*big.Rat{
		"ubze": rat("0.0002"), honey: rat("0.000001234567891234"), "uatom": new(big.Rat), "udust": rat("0.0000000000001"),
	}}
	store := &fakeStore{denoms: []string{honey, "uatom", "ubze", "udust", "unothing"}}
	s := prices.New(prices.Deps{Prices: src, USDPricer: pricer, Store: store, ChainID: "beezee-1", Denom: "ubze",
		Log: lg, Clock: &clock{now: time.Unix(1000, 0)}}, 0)
	require.NoError(t, s.FullResync(context.Background()))

	assert.Equal(t, 1, store.listed)
	assert.Equal(t, map[string]string{"ubze": "0.000200000000", honey: "0.000001234568"}, store.last.DEX,
		"a DEX price wins, rounded to the column; a zero price and one below it fall through to CoinGecko")
	assert.Equal(t, map[string]string{"bzedge": "0.00017", "cosmos": "1.96"}, store.last.Prices, "CoinGecko's prices, for the rest")
	assert.Empty(t, store.last.Kept)
	assert.Equal(t, "beezee-1", store.last.ChainID)
	assert.Equal(t, "ubze", pricer.calls[0], "the native denom first: the others' BZE route reads its cached price")
	assert.Len(t, pricer.calls, 5)
	assert.Empty(t, hook.AllEntries())
}

// A denom the pricer cannot answer keeps its stored price: it is listed as
// kept, the run is recorded as failed and warned once, until it answers.
func TestAnUnansweredDenomIsKeptAndLoggedOnce(t *testing.T) {
	lg, hook := logtest.NewNullLogger()
	src := &fakeSource{prices: map[string]string{"bzedge": "0.00017"}}
	pricer := &fakePricer{prices: map[string]*big.Rat{"ubze": rat("0.0002"), honey: nil}}
	store := &fakeStore{denoms: []string{honey, "ubze"}}
	s := prices.New(prices.Deps{Prices: src, USDPricer: pricer, Store: store, ChainID: "beezee-1", Denom: "ubze", Log: lg}, 0)
	ctx := context.Background()
	for range 3 {
		err := s.FullResync(ctx)
		require.ErrorContains(t, err, "1 of 2 denoms unanswered ("+honey+")")
	}
	assert.Equal(t, 3, store.writes, "the other denoms are still written")
	assert.Equal(t, []string{honey}, store.last.Kept)
	assert.Equal(t, map[string]string{"ubze": "0.000200000000"}, store.last.DEX)
	assert.Equal(t, 1, countLevel(hook, logrus.WarnLevel), "logged once per change of state")

	pricer.prices[honey] = rat("0.001")
	require.NoError(t, s.FullResync(ctx))
	assert.Empty(t, store.last.Kept)
	assert.Equal(t, "0.001000000000", store.last.DEX[honey])
	assert.Equal(t, logrus.InfoLevel, hook.LastEntry().Level)
	assert.Len(t, hook.AllEntries(), 2)
}

// The denoms unreadable: nothing is written, one warning.
func TestAFailingDenomsListWritesNothing(t *testing.T) {
	lg, hook := logtest.NewNullLogger()
	src := &fakeSource{prices: map[string]string{"bzedge": "0.00017"}}
	store := &fakeStore{denomsErr: errors.New("connection refused")}
	s := prices.New(prices.Deps{Prices: src, USDPricer: &fakePricer{}, Store: store, ChainID: "beezee-1", Denom: "ubze", Log: lg}, 0)
	for range 2 {
		require.ErrorContains(t, s.FullResync(context.Background()), "connection refused")
	}
	assert.Zero(t, store.writes)
	assert.Equal(t, 1, countLevel(hook, logrus.WarnLevel))

	store.denomsErr = nil
	require.NoError(t, s.FullResync(context.Background()))
	assert.Equal(t, 1, store.writes)
}

// Without a pricer the denoms are not listed and CoinGecko prices alone.
func TestWithoutAPricerCoinGeckoPricesAlone(t *testing.T) {
	lg, _ := logtest.NewNullLogger()
	store := &fakeStore{denoms: []string{"ubze"}}
	s := prices.New(prices.Deps{Prices: &fakeSource{prices: map[string]string{"bzedge": "0.00017"}}, Store: store, Log: lg}, 0)
	require.NoError(t, s.FullResync(context.Background()))
	assert.Zero(t, store.listed)
	assert.Nil(t, store.last.DEX)
	assert.Equal(t, map[string]string{"bzedge": "0.00017"}, store.last.Prices)
}
