package prices_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/aggregator"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/prices"
)

// The BZE/USDC.n liquidity pool, whose ticker prices USDC.n in ubze.
const pool = "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4_ubze"

type fakeSource struct {
	prices    map[string]string
	err       error
	ticker    *aggregator.Ticker
	tickerErr error
	tickers   int
}

func (s *fakeSource) Prices(context.Context) (map[string]string, error) { return s.prices, s.err }

func (s *fakeSource) Ticker(_ context.Context, market string) (*aggregator.Ticker, error) {
	s.tickers++
	if s.ticker == nil || s.ticker.MarketID != market {
		return nil, s.tickerErr
	}
	return s.ticker, s.tickerErr
}

// fakeStore keeps the last write, and the change as the table keeps it.
type fakeStore struct {
	last   prices.Snapshot
	change *string
	writes int
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

func poolTicker(open, last string) *aggregator.Ticker {
	return &aggregator.Ticker{MarketID: pool, Base: "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4",
		Quote: "ubze", OpenPrice: open, LastPrice: last}
}

func newSet(src *fakeSource, store *fakeStore, c *clock, lg logrus.FieldLogger) *prices.Set {
	return prices.New(prices.Deps{Source: src, Store: store, ChainID: "beezee-1", Denom: "ubze",
		ChangeMarket: pool, Log: lg, Clock: c}, 0)
}

func TestPricesAreRefreshedAndKeptWhenTheSourceFails(t *testing.T) {
	lg, hook := logtest.NewNullLogger()
	src := &fakeSource{prices: map[string]string{"bzedge": "0.000168060000"}}
	store := &fakeStore{}
	s := prices.New(prices.Deps{Source: src, Store: store, ChainID: "beezee-1", Log: lg}, 0)
	assert.Equal(t, statesync.Prices, s.Name())
	assert.Equal(t, prices.DefaultInterval, s.Interval())
	ctx := context.Background()

	require.NoError(t, s.FullResync(ctx))
	assert.Equal(t, map[string]string{"bzedge": "0.000168060000"}, store.last.Prices)
	assert.Equal(t, "beezee-1", store.last.ChainID)
	assert.True(t, store.last.KeepChange, "no change market: the change is left alone")
	assert.Zero(t, src.tickers)

	// The aggregator fails for three minutes: nothing is written, one warning.
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
	src.ticker = &aggregator.Ticker{MarketID: pool, Base: "uatom", Quote: "uosmo", OpenPrice: "1", LastPrice: "1"}
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
	s := prices.New(prices.Deps{Source: &fakeSource{err: errors.New("down")}, Store: &fakeStore{}, Log: lg}, 0)
	sync := statesync.New(statesync.Config{Log: lg}, noJobs{}, s)
	require.Error(t, sync.RunOnce(context.Background()))
	require.Error(t, sync.RunOnce(context.Background()))
	assert.Len(t, hook.AllEntries(), 1, "the set's own warning only")
}

type noJobs struct{}

func (noJobs) RecordRun(context.Context, statesync.Run) error { return nil }
