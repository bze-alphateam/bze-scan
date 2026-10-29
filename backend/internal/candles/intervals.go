package candles

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	sdkmath "cosmossdk.io/math"

	"github.com/bze-alphateam/bze-scan/backend/internal/dexunits"
	"github.com/bze-alphateam/bze-scan/backend/internal/orderbook"
)

// ErrUnknownMarket is a market id that is neither a market of the chain nor
// a pool with a stored swap.
var ErrUnknownMarket = errors.New("market not found")

// First is a market's first trade.
type First struct {
	Time time.Time
	// Base and Quote are the pool's denoms; empty for an order book market,
	// whose denoms come from Markets.
	Base, Quote string
}

// Reader reads the stored candles; PGReader implements it.
type Reader interface {
	// Candles returns market's candles of minutes starting at from or
	// later, the newest limit, newest first.
	Candles(ctx context.Context, market string, minutes int, from time.Time, limit int) ([]Candle, error)
	// First returns market's first numbered trade, and false when it has
	// none.
	First(ctx context.Context, market string) (First, bool, error)
}

// Markets finds the chain's order book markets; orderbook.Service
// implements it.
type Markets interface {
	// Market returns the market id's denoms, or orderbook.ErrUnknownMarket.
	Market(ctx context.Context, id string) (orderbook.Market, error)
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Interval is a candle in display units, the aggregator's strings.
type Interval struct {
	Start, End                     time.Time
	Low, Open, Average, High       string
	Close, BaseVolume, QuoteVolume string
}

// FloorTTL is how long a market's floor is cached.
const FloorTTL = 10 * time.Minute

// ServiceDeps are a Service's dependencies; Clock defaults to the system's.
type ServiceDeps struct {
	Reader    Reader
	Markets   Markets
	Exponents dexunits.Exponents
	Clock     Clock
}

// Service serves the candles as the aggregator's intervals.
type Service struct {
	deps ServiceDeps

	mu     sync.Mutex
	floors map[string]floor
}

// floor is a market's cached first trade, with its denoms.
type floor struct {
	first       time.Time
	base, quote string
	expires     time.Time
}

// NewService returns a service over deps.
func NewService(deps ServiceDeps) *Service {
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}
	return &Service{deps: deps, floors: map[string]floor{}}
}

// Intervals answers market's candles of minutes as the aggregator does.
// The floor is the market's first trade minus a minute; the window starts
// at the later of it and now − limit × minutes. When the store holds limit
// candles from there they are the answer; otherwise the buckets from the
// current one down to the window's start are answered, a zero candle for
// each bucket without one (so limit + 1 of them at most). Ascending by
// start. A market of the chain without a trade has no candle, an unknown
// one is ErrUnknownMarket.
func (s *Service) Intervals(ctx context.Context, market string, minutes, limit int) ([]Interval, error) {
	f, traded, err := s.floor(ctx, market)
	if err != nil || !traded {
		return nil, err
	}
	length := time.Duration(minutes) * time.Minute
	now := s.deps.Clock.Now()
	// The aggregator's expression, overflow included (a daily limit has no
	// maximum).
	startAt := now.Add(-time.Duration(limit) * length)
	if startAt.Before(f.first) {
		startAt = f.first
	}
	stored, err := s.deps.Reader.Candles(ctx, market, minutes, startAt, limit)
	if err != nil {
		return nil, err
	}
	exps, err := s.deps.Exponents.Exponents(ctx, []string{f.base, f.quote})
	if err != nil {
		return nil, err
	}
	baseExp, okB := exps[f.base]
	quoteExp, okQ := exps[f.quote]
	if !okB || !okQ {
		return nil, fmt.Errorf("market %s: exponents of %s and %s not both known", market, f.base, f.quote)
	}
	byStart := make(map[int64]Interval, len(stored))
	for _, c := range stored {
		iv, err := display(c, baseExp, quoteExp)
		if err != nil {
			return nil, fmt.Errorf("market %s: candle %d/%s: %w", market, minutes, c.Start.Format(time.RFC3339), err)
		}
		byStart[c.Start.Unix()] = iv
	}

	var out []Interval
	if len(byStart) == limit {
		for _, iv := range byStart {
			out = append(out, iv)
		}
	} else {
		for start := BucketStart(now, minutes); !start.Before(startAt); start = BucketStart(start.Add(-length), minutes) {
			iv, ok := byStart[start.Unix()]
			if !ok {
				iv = Interval{Start: start, End: start.Add(length), Low: "0", Open: "0", Average: "0", High: "0",
					Close: "0", BaseVolume: "0", QuoteVolume: "0"}
			}
			out = append(out, iv)
		}
	}
	slices.SortFunc(out, func(a, b Interval) int { return a.Start.Compare(b.Start) })
	return out, nil
}

// floor returns market's cached floor; false for a market of the chain
// without a trade.
func (s *Service) floor(ctx context.Context, market string) (floor, bool, error) {
	now := s.deps.Clock.Now()
	s.mu.Lock()
	f, ok := s.floors[market]
	s.mu.Unlock()
	if ok && now.Before(f.expires) {
		return f, true, nil
	}
	first, traded, err := s.deps.Reader.First(ctx, market)
	if err != nil {
		return floor{}, false, err
	}
	// A pool's denoms come with its first swap, a market's from the chain.
	if !traded || first.Base == "" {
		m, err := s.deps.Markets.Market(ctx, market)
		if errors.Is(err, orderbook.ErrUnknownMarket) {
			return floor{}, false, fmt.Errorf("%w: %s", ErrUnknownMarket, market)
		}
		if err != nil || !traded {
			return floor{}, false, err
		}
		first.Base, first.Quote = m.Base, m.Quote
	}
	f = floor{first: first.Time.Add(-time.Minute), base: first.Base, quote: first.Quote, expires: now.Add(FloorTTL)}
	s.mu.Lock()
	s.floors[market] = f
	s.mu.Unlock()
	return f, true, nil
}

// display converts a stored candle to the aggregator's strings: prices per
// dexunits.TradePrice, volumes exact. The average is the close when high
// and low are equal, else quote volume / base volume as an 18-decimal
// LegacyDec of the display volumes, the aggregator's VWAP.
func display(c Candle, baseExp, quoteExp int) (iv Interval, err error) {
	iv = Interval{Start: c.Start, End: c.Start.Add(time.Duration(c.Minutes) * time.Minute)}
	for _, p := range []struct {
		to *string
		v  sdkmath.LegacyDec
	}{{&iv.Low, c.Low}, {&iv.Open, c.Open}, {&iv.High, c.High}, {&iv.Close, c.Close}} {
		if *p.to, err = dexunits.TradePrice(p.v.String(), baseExp, quoteExp); err != nil {
			return Interval{}, err
		}
	}
	if iv.BaseVolume, err = dexunits.Amount(c.BaseVolume.String(), baseExp); err != nil {
		return Interval{}, err
	}
	if iv.QuoteVolume, err = dexunits.Amount(c.QuoteVolume.String(), quoteExp); err != nil {
		return Interval{}, err
	}
	if iv.High == iv.Low || c.BaseVolume.IsZero() {
		iv.Average = iv.Close
		return iv, nil
	}
	// LegacyDec takes at most 18 decimals and 256 bits.
	defer func() {
		if r := recover(); r != nil {
			iv, err = Interval{}, fmt.Errorf("average of %s / %s: %v", iv.QuoteVolume, iv.BaseVolume, r)
		}
	}()
	quote := sdkmath.LegacyNewDecFromIntWithPrec(c.QuoteVolume, int64(quoteExp))
	base := sdkmath.LegacyNewDecFromIntWithPrec(c.BaseVolume, int64(baseExp))
	iv.Average = dexunits.TrimTrailingZeros(quote.Quo(base).String())
	return iv, nil
}
