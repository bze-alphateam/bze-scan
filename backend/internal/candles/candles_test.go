package candles_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
	"github.com/bze-alphateam/bze-scan/backend/internal/dexunits"
)

// at is a UTC time on 2026-10-28.
func at(hh, mm, ss int) time.Time { return time.Date(2026, 10, 28, hh, mm, ss, 0, time.UTC) }

func dec(s string) sdkmath.LegacyDec { return sdkmath.LegacyMustNewDecFromStr(s) }

// trade is a fill-like trade: base amount, price, quote = base × price.
func trade(id int64, t time.Time, price string, base int64) candles.Trade {
	p := dec(price)
	b := sdkmath.NewInt(base)
	return candles.Trade{ID: id, Time: t, Price: p, Base: b, Quote: sdkmath.LegacyNewDecFromInt(b).Mul(p).TruncateInt()}
}

// of returns the candle of minutes starting at start, failing when absent.
func of(t *testing.T, cs []candles.Candle, minutes int, start time.Time) candles.Candle {
	t.Helper()
	for _, c := range cs {
		if c.Minutes == minutes && c.Start.Equal(start) {
			return c
		}
	}
	require.Failf(t, "no candle", "%d minutes at %s", minutes, start)
	return candles.Candle{}
}

func TestBucketStartAlignsEveryLengthToTheEpochInUTC(t *testing.T) {
	before := at(23, 58, 30)
	after := before.Add(2 * time.Minute) // 2026-10-29 00:00:30
	midnight := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	for minutes, want := range map[int]time.Time{
		5: at(23, 55, 0), 15: at(23, 45, 0), 60: at(23, 0, 0), 240: at(20, 0, 0), 1440: at(0, 0, 0),
	} {
		assert.Equal(t, want, candles.BucketStart(before, minutes), minutes)
		assert.Equal(t, midnight, candles.BucketStart(after, minutes), minutes)
	}
	// The zone of the input never moves a boundary.
	local := before.In(time.FixedZone("plus-five-45", 5*3600+45*60))
	assert.Equal(t, at(0, 0, 0), candles.BucketStart(local, 1440))
	assert.Equal(t, time.UTC, candles.BucketStart(local, 1440).Location())
	// A bucket is half open: its end opens the next one.
	assert.Equal(t, at(23, 55, 0), candles.BucketStart(at(23, 59, 59), 5))
}

func TestBuildSplitsTradesAcrossADayBoundaryAtEveryLength(t *testing.T) {
	cs := candles.Build("ubze/uvdl", []candles.Trade{
		trade(1, at(23, 58, 30), "2", 10),
		trade(2, at(23, 58, 30).Add(2*time.Minute), "3", 10),
	})
	require.Len(t, cs, 10, "two candles per length")
	for _, minutes := range candles.Lengths {
		c := of(t, cs, minutes, candles.BucketStart(at(23, 58, 30), minutes))
		assert.Equal(t, "ubze/uvdl", c.Market)
		assert.Equal(t, int64(1), c.FirstTradeID)
		assert.Equal(t, dec("2"), c.Close)
		c = of(t, cs, minutes, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC))
		assert.Equal(t, dec("3"), c.Open)
	}
	// Ordered by length, then start.
	assert.Equal(t, 5, cs[0].Minutes)
	assert.True(t, cs[0].Start.Before(cs[1].Start))
	assert.Equal(t, 1440, cs[9].Minutes)
}

// Trades of one block share a time: open and close follow the trade id, in
// whatever order the trades come.
func TestBuildOpensAndClosesByTradeIDOnTies(t *testing.T) {
	same := at(10, 1, 0)
	cs := candles.Build("m", []candles.Trade{
		trade(25000000000003, same, "3", 1),
		trade(25000000000001, same, "1", 1),
		trade(25000000000002, same, "7", 1),
	})
	c := of(t, cs, 5, at(10, 0, 0))
	assert.Equal(t, dec("1"), c.Open)
	assert.Equal(t, dec("3"), c.Close)
	assert.Equal(t, dec("7"), c.High)
	assert.Equal(t, dec("1"), c.Low)
	assert.Equal(t, int64(25000000000001), c.FirstTradeID)
	assert.Equal(t, int64(25000000000003), c.LastTradeID)
	assert.Equal(t, sdkmath.NewInt(3), c.BaseVolume)
	assert.Equal(t, sdkmath.NewInt(11), c.QuoteVolume)
}

func TestFillAndSwapQuoteAmounts(t *testing.T) {
	// A fill's quote is amount × price truncated to quote base units.
	fill, err := candles.FillTrade(1, at(1, 0, 0), "0.333000000000000000", "10")
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(3), fill.Quote)
	assert.Equal(t, sdkmath.NewInt(10), fill.Base)
	assert.Equal(t, dec("0.333"), fill.Price)

	// A swap's amounts are exact; its price quote / base rounded to 18
	// decimals.
	swap, err := candles.SwapTrade(2, at(1, 0, 0), "3", "1")
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(1), swap.Quote)
	assert.Equal(t, sdkmath.NewInt(3), swap.Base)
	assert.Equal(t, "0.333333333333333333", swap.Price.String())

	cs := candles.Build("m", []candles.Trade{fill, swap})
	c := of(t, cs, 5, at(1, 0, 0))
	assert.Equal(t, sdkmath.NewInt(13), c.BaseVolume)
	assert.Equal(t, sdkmath.NewInt(4), c.QuoteVolume)

	_, err = candles.SwapTrade(3, at(1, 0, 0), "0", "5")
	assert.Error(t, err, "no base amount, no price")
	_, err = candles.FillTrade(4, at(1, 0, 0), "x", "5")
	assert.Error(t, err)
	_, err = candles.FillTrade(4, at(1, 0, 0), "1", "5.5")
	assert.Error(t, err)
}

// Allowed difference: no 50,000-trade cap. The aggregator's rebuild loads
// at most 50,000 trades from the start of the day; every trade counts here.
func TestBuildHasNoTradeCap(t *testing.T) {
	trades := make([]candles.Trade, 60_000)
	for i := range trades {
		trades[i] = trade(int64(i+1), at(0, 0, 0).Add(time.Duration(i)*time.Second), "1", 1)
	}
	c := of(t, candles.Build("m", trades), 1440, at(0, 0, 0))
	assert.Equal(t, sdkmath.NewInt(60_000), c.BaseVolume)
	assert.Equal(t, int64(60_000), c.LastTradeID)
}

// Allowed difference: doubled events. Chain v8.0.x emitted some events twice;
// Scan stores them once, and a trade id handed over twice counts once.
func TestBuildCountsATradeOnce(t *testing.T) {
	tr := trade(20300028000001, at(5, 0, 0), "2", 10)
	c := of(t, candles.Build("m", []candles.Trade{tr, tr}), 5, at(5, 0, 0))
	assert.Equal(t, sdkmath.NewInt(10), c.BaseVolume)
	assert.Equal(t, sdkmath.NewInt(20), c.QuoteVolume)
}

// BZE-136 (won't fix) copied: a candle whose trades so far were priced 0
// has no average yet, so the next trade replaces the volumes and the zero
// low; the open stays at the zero.
func TestBuildFoldsZeroPricesAsTheAggregator(t *testing.T) {
	c := of(t, candles.Build("m", []candles.Trade{
		{ID: 1, Time: at(2, 0, 0), Price: dec("0"), Base: sdkmath.NewInt(10), Quote: sdkmath.ZeroInt()},
		trade(2, at(2, 1, 0), "2", 5),
	}), 5, at(2, 0, 0))
	assert.Equal(t, sdkmath.NewInt(5), c.BaseVolume, "the zero-priced trade's volume is gone")
	assert.Equal(t, sdkmath.NewInt(10), c.QuoteVolume)
	assert.Equal(t, dec("2"), c.Low)
	assert.Equal(t, dec("0"), c.Open)

	// A zero after a priced trade is the low, and the volumes add up.
	c = of(t, candles.Build("m", []candles.Trade{
		trade(1, at(2, 0, 0), "2", 5),
		{ID: 2, Time: at(2, 1, 0), Price: dec("0"), Base: sdkmath.NewInt(10), Quote: sdkmath.ZeroInt()},
	}), 5, at(2, 0, 0))
	assert.Equal(t, sdkmath.NewInt(15), c.BaseVolume)
	assert.Equal(t, dec("0"), c.Low)
}

func TestBuildWithoutTrades(t *testing.T) {
	assert.Empty(t, candles.Build("m", nil))
}

// memStore is an in-memory Store: trades per market, candles keyed by
// market/minutes/start.
type memStore struct {
	trades  map[string][]candles.Trade
	candles map[string]candles.Candle
	locks   [][]string
	pending bool
	touched []candles.Touch
	reads   int
	fail    error
}

func newMemStore() *memStore {
	return &memStore{trades: map[string][]candles.Trade{}, candles: map[string]candles.Candle{}}
}

func key(market string, minutes int, start time.Time) string {
	return fmt.Sprintf("%s|%d|%d", market, minutes, start.Unix())
}

func (s *memStore) Lock(_ context.Context, markets []string) error {
	s.locks = append(s.locks, markets)
	return s.fail
}

func (s *memStore) Trades(_ context.Context, market string, from, to time.Time) ([]candles.Trade, error) {
	s.reads++
	var out []candles.Trade
	for _, t := range s.trades[market] {
		if !t.Time.Before(from) && t.Time.Before(to) {
			out = append(out, t)
		}
	}
	return out, s.fail
}

func (s *memStore) Replace(_ context.Context, market string, from, to time.Time, cs []candles.Candle) error {
	for k, c := range s.candles {
		if c.Market == market && !c.Start.Before(from) && c.Start.Before(to) {
			delete(s.candles, k)
		}
	}
	for _, c := range cs {
		s.candles[key(market, c.Minutes, c.Start)] = c
	}
	return s.fail
}

func (s *memStore) Span(_ context.Context, market string) (first, last time.Time, ok bool, err error) {
	for _, t := range s.trades[market] {
		if !ok || t.Time.Before(first) {
			first = t.Time
		}
		if !ok || t.Time.After(last) {
			last = t.Time
		}
		ok = true
	}
	return first, last, ok, s.fail
}

func (s *memStore) Pending(context.Context) (bool, error) { return s.pending, s.fail }

func (s *memStore) Touched(context.Context, int64, int64) ([]candles.Touch, error) {
	return s.touched, s.fail
}

// stored returns the candles of market as a set of comparable strings.
func (s *memStore) stored(market string) map[string]string {
	out := map[string]string{}
	for k, c := range s.candles {
		if c.Market == market {
			out[k] = fmt.Sprintf("%s %s %s %s %s %s %d %d", c.Open, c.High, c.Low, c.Close, c.BaseVolume, c.QuoteVolume,
				c.FirstTradeID, c.LastTradeID)
		}
	}
	return out
}

// Allowed difference: a fill's quote volume is amount × chain price
// truncated, exact. For a base one or two decimals short of the quote the
// aggregator multiplies display amount × its display price, which it has
// rounded to 18 decimals (dexunits.TradePrice), so its quote can be off.
func TestFillQuoteIsExactWhereTheAggregatorRoundsThePrice(t *testing.T) {
	// A 6/7-decimal market: chain price 1.5e-17, 10^18 base units.
	fill, err := candles.FillTrade(1, at(1, 0, 0), "0.000000000000000015", "1000000000000000000")
	require.NoError(t, err)
	assert.Equal(t, sdkmath.NewInt(15), fill.Quote, "quote base units, exact")

	// The aggregator: display price 1.5e-18 rounds (half to even) to 2e-18,
	// display amount 10^12, quote 2e-6 at 7 decimals = 20 base units.
	display, err := dexunits.TradePrice("0.000000000000000015", 6, 7)
	require.NoError(t, err)
	assert.Equal(t, "0.000000000000000002", display)
	aggregator := sdkmath.LegacyMustNewDecFromStr("1000000000000").Mul(sdkmath.LegacyMustNewDecFromStr(display)).
		MulInt64(10_000_000).TruncateInt()
	assert.Equal(t, sdkmath.NewInt(20), aggregator)
}
