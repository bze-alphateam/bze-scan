package candles

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// Store is the candles' storage, inside one transaction; PGStore implements
// it.
type Store interface {
	// Lock takes the candle lock of every market of markets (sorted) until
	// the transaction ends: two recomputations of one market never
	// interleave their reads and writes.
	Lock(ctx context.Context, markets []string) error
	// Trades returns market's numbered trades with a time in [from, to),
	// in any order.
	Trades(ctx context.Context, market string, from, to time.Time) ([]Trade, error)
	// Replace makes candles the stored candles of market that start in
	// [from, to): those are written, every other one in the window goes.
	Replace(ctx context.Context, market string, from, to time.Time, candles []Candle) error
	// Span returns the time of market's first and last numbered trade, and
	// false when it has none.
	Span(ctx context.Context, market string) (first, last time.Time, ok bool, err error)
	// Pending reports whether the post-migration build of the whole history
	// has still to list the markets (MigrationStep).
	Pending(ctx context.Context) (bool, error)
	// Touched returns the markets and times of the trades at the heights
	// [lo, hi).
	Touched(ctx context.Context, lo, hi int64) ([]Touch, error)
}

// Tx is the part of a transaction a PGStore uses; pgx.Tx satisfies it. An
// alias of the interface literal, the same as trades.Tx, so Upkeep is a
// trades.Hook.
type Tx = interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Touch is a trade of Market at Time: its day is recomputed.
type Touch struct {
	Market string
	Time   time.Time
}

// Recompute recomputes, in s, every candle of the UTC days the touches fall
// in, for their markets: one day's trades at a time, under the markets'
// locks.
func Recompute(ctx context.Context, s Store, touches []Touch) error {
	days := map[string]map[int64]bool{}
	for _, t := range touches {
		if days[t.Market] == nil {
			days[t.Market] = map[int64]bool{}
		}
		days[t.Market][BucketStart(t.Time, 1440).Unix()] = true
	}
	if len(days) == 0 {
		return nil
	}
	markets := slices.Sorted(maps.Keys(days))
	if err := s.Lock(ctx, markets); err != nil {
		return err
	}
	for _, m := range markets {
		for _, d := range slices.Sorted(maps.Keys(days[m])) {
			from := time.Unix(d, 0).UTC()
			if err := recomputeWindow(ctx, s, m, from, from.Add(Day)); err != nil {
				return err
			}
		}
	}
	return nil
}

// recomputeWindow rewrites market's candles of [from, to), a whole number
// of days, from its trades.
func recomputeWindow(ctx context.Context, s Store, market string, from, to time.Time) error {
	trades, err := s.Trades(ctx, market, from, to)
	if err != nil {
		return err
	}
	return s.Replace(ctx, market, from, to, Build(market, trades))
}

// RebuildChunk is how much of a market's history Rebuild reads at once,
// in a transaction of its own.
const RebuildChunk = 30 * Day

// Within runs fn over a store in a transaction of its own, committed when fn
// succeeds.
type Within func(ctx context.Context, fn func(s Store) error) error

// Rebuild recomputes every candle of market from its trades: first, in one
// transaction, the candles outside the days of its first and last trade go
// (all of them when it has none); then the days between are rewritten a
// RebuildChunk at a time, each chunk in its own transaction under the
// market's lock, so the writers wait for one chunk at most. Chunks are whole
// days and independent: a trade written meanwhile is the writers' to
// recompute, before or after the chunk. It returns the number of trades
// read.
func Rebuild(ctx context.Context, within Within, market string) (int, error) {
	var lo, hi time.Time
	err := within(ctx, func(s Store) error {
		if err := s.Lock(ctx, []string{market}); err != nil {
			return err
		}
		first, last, ok, err := s.Span(ctx, market)
		if err != nil {
			return err
		}
		lo, hi = BucketStart(first, 1440), BucketStart(last, 1440).Add(Day)
		if !ok {
			lo, hi = time.Unix(0, 0).UTC(), time.Unix(0, 0).UTC()
		}
		if err := s.Replace(ctx, market, time.Unix(0, 0).UTC(), lo, nil); err != nil {
			return err
		}
		return s.Replace(ctx, market, hi, farFuture, nil)
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for from := lo; from.Before(hi); from = from.Add(RebuildChunk) {
		to := from.Add(RebuildChunk)
		if to.After(hi) {
			to = hi
		}
		err := within(ctx, func(s Store) error {
			if err := s.Lock(ctx, []string{market}); err != nil {
				return err
			}
			trades, err := s.Trades(ctx, market, from, to)
			if err != nil {
				return err
			}
			n += len(trades)
			return s.Replace(ctx, market, from, to, Build(market, trades))
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// farFuture bounds the windows that reach the end of time.
var farFuture = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// Upkeep keeps the candles of the trades the writers write.
type Upkeep struct {
	open func(tx Tx) Store
}

// NewUpkeep returns an upkeep whose store over a transaction is open(tx).
func NewUpkeep(open func(tx Tx) Store) *Upkeep {
	return &Upkeep{open: open}
}

// Default is the upkeep over PGStore; the composition root gives it to the
// writers (PostFlush) and to internal/trades (AfterNumbering).
var Default = NewUpkeep(func(tx Tx) Store { return NewPGStore(tx) })

// PostFlush is the writers' hook: it recomputes the days of the flush's
// fills and swaps, whether they were inserted or already there.
func (u *Upkeep) PostFlush(ctx context.Context, tx pgx.Tx, f *writer.Flush) error {
	if f == nil || f.Entities == nil {
		return nil
	}
	var touches []Touch
	for _, fl := range f.Entities.OrderFills {
		touches = append(touches, Touch{Market: fl.MarketID, Time: fl.Time})
	}
	for _, s := range f.Entities.Swaps {
		touches = append(touches, Touch{Market: s.PoolID, Time: s.Time})
	}
	if len(touches) == 0 {
		return nil
	}
	return Recompute(ctx, u.open(tx), touches)
}

// AfterNumbering recomputes, in tx, the days of the trades at the heights
// [lo, hi) that internal/trades has just numbered: a fill counts only once
// it has its trade id. While the post-migration build has the whole history
// still to list it does nothing: that build covers them.
func (u *Upkeep) AfterNumbering(ctx context.Context, tx Tx, lo, hi int64) error {
	s := u.open(tx)
	pending, err := s.Pending(ctx)
	if err != nil || pending {
		return err
	}
	touches, err := s.Touched(ctx, lo, hi)
	if err != nil {
		return err
	}
	return Recompute(ctx, s, touches)
}
