// Package candles keeps explorer.candles, the OHLC candles of every order
// book market and liquidity pool, and serves them in display units for the
// aggregator-compatible GET /aggregator/api/dex/intervals.
//
// A candle is a bucket of Minutes (5, 15, 60, 240 or 1440) aligned to the
// Unix epoch, so in UTC, holding the trades whose time falls in it: the order
// book fills of explorer.order_fills (keyed by market id, base/quote) and the
// swaps of explorer.swaps (keyed by pool id, base_quote). Candles are stored
// in chain units (chain prices, base-unit amounts) and converted on read, so
// a corrected exponent never needs a rebuild.
//
// A candle is never incremented: every write recomputes the buckets it
// touches from their trades (Build), so replays and out-of-order flushes give
// the same rows. The writers recompute a market's whole UTC day, which holds
// every bucket of every length, through the Upkeep PostFlush hook;
// internal/trades does the same for the fills it numbers late; Rebuild
// recomputes a market's whole history, for `bze-scan candles rebuild` and the
// post-migration step "candles".
package candles

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	sdkmath "cosmossdk.io/math"
)

// Lengths are the candle lengths in minutes, the aggregator's.
var Lengths = []int{5, 15, 60, 240, 1440}

// Day is the longest length: every shorter bucket lies inside one day.
const Day = 24 * time.Hour

// Trade is one fill or swap in chain units.
type Trade struct {
	// ID is the trade id (transform.TradeIDsPerBlock): it orders the
	// trades, ties of one block included.
	ID   int64
	Time time.Time
	// Price is quote base units per base base unit.
	Price       sdkmath.LegacyDec
	Base, Quote sdkmath.Int
}

// FillTrade is an order book fill as a trade: its quote amount is amount ×
// price truncated to quote base units, as the aggregator computes it.
func FillTrade(id int64, at time.Time, price, amount string) (Trade, error) {
	p, err := sdkmath.LegacyNewDecFromStr(price)
	if err != nil {
		return Trade{}, fmt.Errorf("fill %d: price %q: %w", id, price, err)
	}
	a, ok := sdkmath.NewIntFromString(amount)
	if !ok {
		return Trade{}, fmt.Errorf("fill %d: amount %q is not an integer", id, amount)
	}
	return Trade{ID: id, Time: at, Price: p, Base: a, Quote: sdkmath.LegacyNewDecFromInt(a).Mul(p).TruncateInt()}, nil
}

// SwapTrade is a pool swap as a trade: the exact amounts of both sides, and
// the price quote / base as an 18-decimal LegacyDec, as the aggregator
// computes it. A swap without a base amount has no price.
func SwapTrade(id int64, at time.Time, base, quote string) (t Trade, err error) {
	b, okB := sdkmath.NewIntFromString(base)
	q, okQ := sdkmath.NewIntFromString(quote)
	if !okB || !okQ || b.IsZero() {
		return Trade{}, fmt.Errorf("swap %d: amounts %q / %q do not make a price", id, quote, base)
	}
	// LegacyDec panics past its 256 bits; such an amount has no price.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("swap %d: price of %s / %s: %v", id, quote, base, r)
		}
	}()
	return Trade{ID: id, Time: at, Price: sdkmath.LegacyNewDecFromInt(q).Quo(sdkmath.LegacyNewDecFromInt(b)),
		Base: b, Quote: q}, nil
}

// Candle is one bucket of one market in chain units.
type Candle struct {
	Market  string
	Minutes int
	Start   time.Time
	// Open and Close are the prices of the bucket's first and last trade by
	// trade id.
	Open, High, Low, Close  sdkmath.LegacyDec
	BaseVolume, QuoteVolume sdkmath.Int
	FirstTradeID            int64
	LastTradeID             int64
}

// BucketStart is the start of the bucket of minutes that holds t: t's Unix
// second rounded down to a multiple of the length, in UTC.
func BucketStart(t time.Time, minutes int) time.Time {
	secs := int64(minutes) * 60
	s := t.Unix()
	return time.Unix(s-((s%secs)+secs)%secs, 0).UTC()
}

// Build computes the candles of every length of market's trades, ordered by
// length then start; a trade id given twice counts once. Each bucket folds its trades in trade id order the way
// the aggregator's Interval.AddOrder does, zero prices included (BZE-136,
// won't fix): while the candle has no non-zero average price (its trades so
// far were priced 0), the next trade replaces the volumes instead of adding
// to them, and a zero low or high is replaced by the next trade's price.
// Without a zero price (an order book price is never 0; a swap's only for a
// quote amount below 10^-18 of the base) every candle is the usual one.
func Build(market string, trades []Trade) []Candle {
	sorted := slices.SortedFunc(slices.Values(trades), func(a, b Trade) int { return cmp.Compare(a.ID, b.ID) })
	// A trade counts once, whatever hands it over twice.
	sorted = slices.CompactFunc(sorted, func(a, b Trade) bool { return a.ID == b.ID })
	var out []Candle
	for _, minutes := range Lengths {
		buckets := map[int64]*Candle{}
		avgs := map[int64]sdkmath.LegacyDec{}
		var starts []int64
		for _, t := range sorted {
			start := BucketStart(t.Time, minutes)
			key := start.Unix()
			c := buckets[key]
			if c == nil {
				c = &Candle{Market: market, Minutes: minutes, Start: start, Open: t.Price,
					High: sdkmath.LegacyZeroDec(), Low: sdkmath.LegacyZeroDec(),
					BaseVolume: sdkmath.ZeroInt(), QuoteVolume: sdkmath.ZeroInt(), FirstTradeID: t.ID}
				buckets[key], avgs[key] = c, sdkmath.LegacyZeroDec()
				starts = append(starts, key)
			}
			avgs[key] = fold(c, avgs[key], t)
		}
		slices.Sort(starts)
		for _, s := range starts {
			out = append(out, *buckets[s])
		}
	}
	return out
}

// fold adds t to c and returns the candle's new average price, which only
// matters as the aggregator's "first trade" flag (zero) and is recomputed
// in display units on read.
func fold(c *Candle, avg sdkmath.LegacyDec, t Trade) sdkmath.LegacyDec {
	c.Close, c.LastTradeID = t.Price, t.ID
	if c.Low.IsZero() || t.Price.LT(c.Low) {
		c.Low = t.Price
	}
	if c.High.IsZero() || t.Price.GT(c.High) {
		c.High = t.Price
	}
	if avg.IsZero() {
		c.BaseVolume, c.QuoteVolume = t.Base, t.Quote
		return t.Price
	}
	c.BaseVolume, c.QuoteVolume = c.BaseVolume.Add(t.Base), c.QuoteVolume.Add(t.Quote)
	if c.High.Equal(c.Low) || c.BaseVolume.IsZero() {
		return t.Price
	}
	return sdkmath.LegacyNewDecFromInt(c.QuoteVolume).Quo(sdkmath.LegacyNewDecFromInt(c.BaseVolume))
}
