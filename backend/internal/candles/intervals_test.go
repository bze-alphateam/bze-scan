package candles_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
	"github.com/bze-alphateam/bze-scan/backend/internal/orderbook"
)

// fakeReader serves stored candles and first trades from memory.
type fakeReader struct {
	candles []candles.Candle
	firsts  map[string]candles.First
	// created are the markets' creation times, asked with which denom.
	created map[string]time.Time
	denoms  []string
	asked   []time.Time
	fail    error
}

func (r *fakeReader) Candles(_ context.Context, market string, minutes int, from time.Time, limit int) ([]candles.Candle, error) {
	r.asked = append(r.asked, from)
	var out []candles.Candle
	for _, c := range slices.Backward(r.candles) {
		if c.Market == market && c.Minutes == minutes && !c.Start.Before(from) && len(out) < limit {
			out = append(out, c)
		}
	}
	return out, r.fail
}

func (r *fakeReader) First(_ context.Context, market string) (candles.First, bool, error) {
	f, ok := r.firsts[market]
	return f, ok, r.fail
}

func (r *fakeReader) Created(_ context.Context, market, base string) (time.Time, error) {
	r.denoms = append(r.denoms, base)
	return r.created[market], r.fail
}

type fakeMarkets map[string]orderbook.Market

func (m fakeMarkets) Market(_ context.Context, id string) (orderbook.Market, error) {
	if mk, ok := m[id]; ok {
		return mk, nil
	}
	return orderbook.Market{}, orderbook.ErrUnknownMarket
}

type fakeExponents map[string]int

func (e fakeExponents) Exponents(_ context.Context, denoms []string) (map[string]int, error) {
	out := map[string]int{}
	for _, d := range denoms {
		if exp, ok := e[d]; ok {
			out[d] = exp
		}
	}
	return out, nil
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

const market = "ubze/uvdl"

// service has the market ubze/uvdl (both denoms 6 decimals) and the pool
// ubze_utoken (base 6, quote 9), now being 10:07:30.
func service(r *fakeReader) *candles.Service {
	return candles.NewService(candles.ServiceDeps{
		Reader:    r,
		Markets:   fakeMarkets{market: {ID: market, Base: "ubze", Quote: "uvdl"}, "ubze/unew": {ID: "ubze/unew", Base: "ubze", Quote: "unew"}},
		Exponents: fakeExponents{"ubze": 6, "uvdl": 6, "utoken": 9},
		Clock:     fixedClock(at(10, 7, 30)),
	})
}

func built(m string, trades ...candles.Trade) []candles.Candle { return candles.Build(m, trades) }

// The volume-weighted average, display units.
func TestIntervalsAverageIsTheVWAP(t *testing.T) {
	r := &fakeReader{firsts: map[string]candles.First{market: {Time: at(10, 0, 0)}},
		candles: built(market, trade(1, at(10, 1, 0), "1", 10_000_000), trade(2, at(10, 2, 0), "5", 10_000_000),
			trade(3, at(10, 3, 0), "2", 20_000_000))}
	ivs, err := service(r).Intervals(context.Background(), market, 5, 2)
	require.NoError(t, err)
	require.Len(t, ivs, 2, "the stored candle and the current bucket's zero one: fewer than limit")
	iv := ivs[0]
	assert.Equal(t, candles.Interval{Start: at(10, 0, 0), End: at(10, 5, 0), Low: "1", Open: "1", Average: "2.5", High: "5",
		Close: "2", BaseVolume: "40", QuoteVolume: "100"}, iv)
	assert.Equal(t, candles.Interval{Start: at(10, 5, 0), End: at(10, 10, 0), Low: "0", Open: "0", Average: "0", High: "0",
		Close: "0", BaseVolume: "0", QuoteVolume: "0"}, ivs[1])
}

// While high equals low the average is the trade price, never a division.
func TestIntervalsAverageWhenHighEqualsLow(t *testing.T) {
	r := &fakeReader{firsts: map[string]candles.First{market: {Time: at(10, 0, 0)}},
		candles: built(market, trade(1, at(10, 1, 0), "0.3", 10), trade(2, at(10, 2, 0), "0.3", 3))}
	ivs, err := service(r).Intervals(context.Background(), market, 5, 2)
	require.NoError(t, err)
	require.Len(t, ivs, 2)
	assert.Equal(t, "0.3", ivs[0].Average)
	assert.Equal(t, "0.000013", ivs[0].BaseVolume)
	assert.Equal(t, "0.000003", ivs[0].QuoteVolume, "3 and 0 quote base units: truncated")
}

// Gap filling: from the current bucket down to the window's start, a zero
// candle for every bucket without one, limit + 1 of them when the window
// starts inside a bucket.
func TestIntervalsFillTheGapsDownToTheWindow(t *testing.T) {
	r := &fakeReader{firsts: map[string]candles.First{market: {Time: at(8, 0, 0)}},
		candles: built(market, trade(1, at(9, 51, 0), "2", 1))}
	ivs, err := service(r).Intervals(context.Background(), market, 5, 4)
	require.NoError(t, err)
	// now 10:07:30 − 4 × 5 min = 09:47:30: buckets 09:50 … 10:05, the 09:50
	// one stored.
	assert.Equal(t, []time.Time{at(9, 47, 30)}, r.asked)
	var starts []time.Time
	for _, iv := range ivs {
		starts = append(starts, iv.Start)
	}
	assert.Equal(t, []time.Time{at(9, 50, 0), at(9, 55, 0), at(10, 0, 0), at(10, 5, 0)}, starts)
	assert.Equal(t, "2", ivs[0].Close)
	assert.Equal(t, "0", ivs[1].Close)

	// A window starting on a bucket start holds limit + 1 buckets.
	r.asked = nil
	s := candles.NewService(candles.ServiceDeps{Reader: r, Markets: fakeMarkets{market: {Base: "ubze", Quote: "uvdl"}},
		Exponents: fakeExponents{"ubze": 6, "uvdl": 6}, Clock: fixedClock(at(10, 5, 0))})
	ivs, err = s.Intervals(context.Background(), market, 5, 3)
	require.NoError(t, err)
	assert.Len(t, ivs, 4, "limit + 1")
	assert.Equal(t, at(9, 50, 0), ivs[0].Start)
}

// Without a stored creation the floor is the start of the first trade's
// bucket: no zero candle below it.
func TestIntervalsStopAtTheFloor(t *testing.T) {
	r := &fakeReader{firsts: map[string]candles.First{market: {Time: at(9, 56, 0)}},
		candles: built(market, trade(1, at(9, 56, 0), "2", 1))}
	ivs, err := service(r).Intervals(context.Background(), market, 5, 500)
	require.NoError(t, err)
	assert.Equal(t, []time.Time{at(9, 55, 0)}, r.asked)
	assert.Equal(t, []string{"ubze"}, r.denoms, "the creation is looked up by the market's base")
	require.Len(t, ivs, 3, "09:55, 10:00, 10:05")
	assert.Equal(t, at(9, 55, 0), ivs[0].Start)
}

// The bucket holding the first trade is always answered, wherever in it the
// trade falls (the floor used to be the trade minus a minute, which dropped
// a pool's first candle); a pool's denoms come from its swaps.
func TestIntervalsKeepTheFirstTradesBucket(t *testing.T) {
	pool := "ubze_utoken"
	r := &fakeReader{firsts: map[string]candles.First{pool: {Time: at(9, 58, 20), Base: "ubze", Quote: "utoken"}},
		candles: built(pool, trade(1, at(9, 58, 20), "1000", 1), trade(2, at(10, 1, 0), "2000", 1))}
	ivs, err := service(r).Intervals(context.Background(), pool, 5, 500)
	require.NoError(t, err)
	assert.Equal(t, []time.Time{at(9, 55, 0)}, r.asked)
	require.Len(t, ivs, 3, "09:55, 10:00 and 10:05")
	assert.Equal(t, at(9, 55, 0), ivs[0].Start)
	// Allowed difference: the quote has three decimals more than the base,
	// where the aggregator's prices are 0 (BZE-133).
	assert.Equal(t, "1", ivs[0].Close)
	assert.Equal(t, "2", ivs[1].Close)
}

// A market created before its first trade's bucket starts at its creation,
// as the aggregator dates a pool it syncs live: zero candles from there.
func TestIntervalsFloorAtTheCreation(t *testing.T) {
	pool := "ubze_utoken"
	r := &fakeReader{firsts: map[string]candles.First{pool: {Time: at(9, 58, 20), Base: "ubze", Quote: "utoken"}},
		created: map[string]time.Time{pool: at(9, 42, 10)},
		candles: built(pool, trade(1, at(9, 58, 20), "1000", 1))}
	ivs, err := service(r).Intervals(context.Background(), pool, 5, 500)
	require.NoError(t, err)
	assert.Equal(t, []time.Time{at(9, 42, 10)}, r.asked)
	var starts []time.Time
	for _, iv := range ivs {
		starts = append(starts, iv.Start)
	}
	assert.Equal(t, []time.Time{at(9, 45, 0), at(9, 50, 0), at(9, 55, 0), at(10, 0, 0), at(10, 5, 0)}, starts)
	assert.Equal(t, "0", ivs[0].Close)
	assert.Equal(t, "1", ivs[2].Close)

	// A creation inside the first trade's bucket does not move the floor
	// above the bucket's start.
	r = &fakeReader{firsts: map[string]candles.First{pool: {Time: at(9, 58, 20), Base: "ubze", Quote: "utoken"}},
		created: map[string]time.Time{pool: at(9, 57, 0)},
		candles: built(pool, trade(1, at(9, 58, 20), "1000", 1))}
	ivs, err = service(r).Intervals(context.Background(), pool, 5, 500)
	require.NoError(t, err)
	assert.Equal(t, []time.Time{at(9, 55, 0)}, r.asked)
	assert.Equal(t, at(9, 55, 0), ivs[0].Start)
}

// When the store holds limit candles from the window's start they are the
// answer, ascending, no zero candle added.
func TestIntervalsWithAFullWindow(t *testing.T) {
	r := &fakeReader{firsts: map[string]candles.First{market: {Time: at(9, 0, 0)}},
		candles: built(market, trade(1, at(9, 31, 0), "1", 1), trade(2, at(9, 46, 0), "2", 1), trade(3, at(10, 1, 0), "3", 1))}
	ivs, err := service(r).Intervals(context.Background(), market, 15, 2)
	require.NoError(t, err)
	require.Len(t, ivs, 2)
	assert.Equal(t, at(9, 45, 0), ivs[0].Start)
	assert.Equal(t, at(10, 0, 0), ivs[1].Start)
}

// Daily candles have no maximum: the window reaches the floor.
func TestIntervalsDailyWithoutLimit(t *testing.T) {
	first := at(10, 0, 0).Add(-3 * candles.Day)
	r := &fakeReader{firsts: map[string]candles.First{market: {Time: first}},
		candles: built(market, trade(1, first, "1", 1))}
	ivs, err := service(r).Intervals(context.Background(), market, 1440, 100_000)
	require.NoError(t, err)
	assert.Len(t, ivs, 4, "the first trade's day and the three after it")
	assert.Equal(t, candles.BucketStart(first, 1440), r.asked[0])
}

func TestIntervalsOfMarketsWithoutCandles(t *testing.T) {
	r := &fakeReader{firsts: map[string]candles.First{}}
	s := service(r)
	// A market of the chain without a trade: none.
	ivs, err := s.Intervals(context.Background(), "ubze/unew", 5, 10)
	require.NoError(t, err)
	assert.Empty(t, ivs)
	// Neither a market nor a pool with a swap.
	_, err = s.Intervals(context.Background(), "ubze_unknown", 5, 10)
	assert.ErrorIs(t, err, candles.ErrUnknownMarket)
	// A denom without a known exponent.
	r.firsts["upool_x"] = candles.First{Time: at(10, 0, 0), Base: "upool", Quote: "x"}
	_, err = s.Intervals(context.Background(), "upool_x", 5, 10)
	assert.Error(t, err)
	// The store failing.
	r.fail = errors.New("database is down")
	_, err = s.Intervals(context.Background(), market, 5, 10)
	assert.ErrorIs(t, err, r.fail)
}

// The floor is cached: the first trade is read once.
func TestIntervalsCacheTheFloor(t *testing.T) {
	r := &fakeReader{firsts: map[string]candles.First{market: {Time: at(10, 0, 0)}}}
	s := service(r)
	_, err := s.Intervals(context.Background(), market, 5, 10)
	require.NoError(t, err)
	r.firsts[market] = candles.First{Time: at(1, 0, 0)}
	_, err = s.Intervals(context.Background(), market, 5, 10)
	require.NoError(t, err)
	assert.Equal(t, at(10, 0, 0), r.asked[1], "the cached floor")
}
