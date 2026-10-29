package candles_test

import (
	"context"
	"errors"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

func upkeepOver(s *memStore) *candles.Upkeep {
	return candles.NewUpkeep(func(candles.Tx) candles.Store { return s })
}

// flush is a writer flush with the fills and swaps of the trades given.
func flush(fills []transform.OrderFill, swaps []transform.Swap) *writer.Flush {
	return &writer.Flush{Entities: &transform.Entities{OrderFills: fills, Swaps: swaps}}
}

// The writers' hook recomputes the touched days from the stored trades:
// trades arriving out of order (a backfill walking down, a replay) give the
// same candles as all of them at once.
func TestPostFlushRecomputesFromTheTradesWhateverTheOrder(t *testing.T) {
	ctx := context.Background()
	early, late := trade(1, at(9, 0, 10), "2", 10), trade(2, at(9, 3, 0), "4", 5)
	want := candles.Build("ubze/uvdl", []candles.Trade{early, late})

	s := newMemStore()
	u := upkeepOver(s)
	// The later trade's block first, then the earlier one's.
	s.trades["ubze/uvdl"] = []candles.Trade{late}
	require.NoError(t, u.PostFlush(ctx, nil, flush([]transform.OrderFill{{MarketID: "ubze/uvdl", Time: late.Time}}, nil)))
	s.trades["ubze/uvdl"] = append(s.trades["ubze/uvdl"], early)
	require.NoError(t, u.PostFlush(ctx, nil, flush([]transform.OrderFill{{MarketID: "ubze/uvdl", Time: early.Time}}, nil)))
	// Replayed: nothing changes.
	require.NoError(t, u.PostFlush(ctx, nil, flush([]transform.OrderFill{{MarketID: "ubze/uvdl", Time: early.Time}}, nil)))

	all := newMemStore()
	for _, c := range want {
		all.candles[key(c.Market, c.Minutes, c.Start)] = c
	}
	assert.Equal(t, all.stored("ubze/uvdl"), s.stored("ubze/uvdl"))
	c := of(t, want, 5, at(9, 0, 0))
	assert.Equal(t, dec("2"), c.Open)
	assert.Equal(t, dec("4"), c.Close)
}

// A pool is keyed by its pool id; markets are locked sorted, each day once.
func TestPostFlushLocksAndRecomputesEveryTouchedDayOnce(t *testing.T) {
	s := newMemStore()
	pool := "ubze_uvdl"
	s.trades[pool] = []candles.Trade{trade(1, at(1, 0, 0), "1", 1)}
	s.trades["z/a"] = []candles.Trade{trade(2, at(1, 0, 0), "1", 1), trade(3, at(1, 0, 0).Add(candles.Day), "1", 1)}
	err := upkeepOver(s).PostFlush(context.Background(), nil, flush(
		[]transform.OrderFill{{MarketID: "z/a", Time: at(1, 0, 0)}, {MarketID: "z/a", Time: at(3, 0, 0)},
			{MarketID: "z/a", Time: at(1, 0, 0).Add(candles.Day)}},
		[]transform.Swap{{PoolID: pool, Time: at(1, 0, 0)}}))
	require.NoError(t, err)
	assert.Equal(t, [][]string{{pool, "z/a"}}, s.locks)
	assert.Equal(t, 3, s.reads, "the pool's day and the market's two")
	assert.Len(t, s.stored(pool), 5)
	assert.Len(t, s.stored("z/a"), 10)
}

func TestPostFlushWithoutTradesDoesNothing(t *testing.T) {
	s := newMemStore()
	u := upkeepOver(s)
	require.NoError(t, u.PostFlush(context.Background(), nil, flush(nil, nil)))
	require.NoError(t, u.PostFlush(context.Background(), nil, nil))
	assert.Empty(t, s.locks)
}

// A bucket whose trades are gone (a reindex changed the height) loses its
// candle.
func TestRecomputeDropsCandlesWithoutTrades(t *testing.T) {
	s := newMemStore()
	s.trades["m"] = []candles.Trade{trade(1, at(1, 0, 0), "1", 1), trade(2, at(2, 0, 0), "1", 1)}
	require.NoError(t, candles.Recompute(context.Background(), s, []candles.Touch{{Market: "m", Time: at(1, 0, 0)}}))
	assert.Len(t, s.stored("m"), 8, "two 5/15/60 buckets, one 240 and one day")
	s.trades["m"] = s.trades["m"][:1]
	require.NoError(t, candles.Recompute(context.Background(), s, []candles.Touch{{Market: "m", Time: at(2, 0, 0)}}))
	assert.Len(t, s.stored("m"), 5)
}

func TestRecomputeFails(t *testing.T) {
	s := newMemStore()
	s.fail = errors.New("database is down")
	err := candles.Recompute(context.Background(), s, []candles.Touch{{Market: "m", Time: at(1, 0, 0)}})
	assert.ErrorIs(t, err, s.fail)
}

// Trades numbered late by internal/trades count from then on; while the
// whole history has still to be built, that build covers them.
func TestAfterNumbering(t *testing.T) {
	s := newMemStore()
	s.trades["m"] = []candles.Trade{trade(1, at(1, 0, 0), "1", 1)}
	s.touched = []candles.Touch{{Market: "m", Time: at(1, 0, 0)}}
	u := upkeepOver(s)

	s.pending = true
	require.NoError(t, u.AfterNumbering(context.Background(), nil, 1, 2))
	assert.Empty(t, s.stored("m"))

	s.pending = false
	require.NoError(t, u.AfterNumbering(context.Background(), nil, 1, 2))
	assert.Len(t, s.stored("m"), 5)
}

// Rebuild rewrites a market's whole history: the candles of days that lost
// their trades go, those far apart in time are built too.
func TestRebuild(t *testing.T) {
	s := newMemStore()
	stale := candles.Candle{Market: "m", Minutes: 5, Start: at(0, 0, 0).Add(-400 * candles.Day), Open: dec("1"),
		High: dec("1"), Low: dec("1"), Close: dec("1"), BaseVolume: sdkmath.OneInt(), QuoteVolume: sdkmath.OneInt()}
	s.candles[key("m", stale.Minutes, stale.Start)] = stale
	far := at(1, 0, 0).Add(100 * candles.Day)
	s.trades["m"] = []candles.Trade{trade(1, at(1, 0, 0), "1", 1), trade(2, far, "2", 1)}

	n, err := candles.Rebuild(context.Background(), within(s), "m")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Len(t, s.stored("m"), 10)
	assert.NotContains(t, s.stored("m"), key("m", 5, stale.Start))
	// The span, then the first trade's 30 days and the far one's chunk.
	assert.Equal(t, [][]string{{"m"}, {"m"}, {"m"}, {"m"}, {"m"}}, s.locks[:5])
	assert.Equal(t, 4, s.reads, "four 30-day chunks to cover 101 days")

	// A market without trades keeps no candle.
	s.trades["m"] = nil
	_, err = candles.Rebuild(context.Background(), within(s), "m")
	require.NoError(t, err)
	assert.Empty(t, s.stored("m"))
}

// within runs fn over s, as one transaction would.
func within(s *memStore) candles.Within {
	return func(_ context.Context, fn func(candles.Store) error) error { return fn(s) }
}

// A failing chunk stops the rebuild with its error.
func TestRebuildFails(t *testing.T) {
	s := newMemStore()
	s.trades["m"] = []candles.Trade{trade(1, at(1, 0, 0), "1", 1)}
	s.fail = errors.New("database is down")
	_, err := candles.Rebuild(context.Background(), within(s), "m")
	assert.ErrorIs(t, err, s.fail)
}
