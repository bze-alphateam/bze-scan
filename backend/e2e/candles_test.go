//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
	"github.com/bze-alphateam/bze-scan/backend/internal/orderbook"
	"github.com/bze-alphateam/bze-scan/backend/internal/trades"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// indexCandles writes the trade heights of the swaps tests (recorded fills
// and swaps, and swapH's synthetic ones) with the candles' hook, through the
// live writer or the batch writer walking down.
func indexCandles(t *testing.T, e *syncEnv, backfill bool) {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	defer pool.Close()
	b, r, c := swapBlock(t)
	synthetic, err := newTransformer(t).Transform(transform.Input{Block: b, Results: r, Commit: c})
	require.NoError(t, err)
	if backfill {
		w := writer.NewBatchWriter(pool, writer.AttributeOrders, candles.Default.PostFlush)
		require.NoError(t, w.Write(ctx, []*transform.Entities{synthetic}, writer.ModeInsert))
		for _, h := range slices.Backward(swapHeights) {
			require.NoError(t, w.Write(ctx, []*transform.Entities{transformed(t, e, h)}, writer.ModeInsert))
		}
		return
	}
	live := writer.NewLiveWriter(pool, writer.AttributeOrders, candles.Default.PostFlush)
	for _, h := range swapHeights {
		require.NoError(t, live.WriteBlock(ctx, transformed(t, e, h)))
	}
	require.NoError(t, live.WriteBlock(ctx, synthetic))
}

func candleRows(t *testing.T, e *syncEnv) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', market_id, minutes, to_char(start_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI'),
			open, high, low, close, base_volume, quote_volume, first_trade_id, last_trade_id) FROM explorer.candles`)
}

func rebuilder(t *testing.T, e *syncEnv) *candles.Rebuilder {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return candles.NewRebuilder(pool, nil, nil)
}

// The live writer, the backfill and `candles rebuild` give the same rows.
func TestCandlesOfTheIndexerAndTheRebuildAgree(t *testing.T) {
	ctx := context.Background()
	var written [][]string
	for _, backfill := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "backfill"}[backfill], func(t *testing.T) {
			e := newSyncEnv(t)
			indexCandles(t, e, backfill)
			rows := candleRows(t, e)
			// Three order book markets and three pools, five lengths per
			// day traded: eight market days.
			markets := queryStrings(t, e.db, `SELECT DISTINCT market_id FROM explorer.candles`)
			require.Len(t, markets, 6, markets)
			require.Len(t, rows, 40)
			// The synthetic block: two fills of 10 at 2 on the honey market.
			assert.Contains(t, rows, swapMarket+" 1440 2027-01-15T00:00 2.000000000000000000 2.000000000000000000 "+
				"2.000000000000000000 2.000000000000000000 20 40 "+fmt.Sprint(swapH*1_000_000+3)+" "+fmt.Sprint(swapH*1_000_000+5))
			written = append(written, rows)

			_, err := e.db.Exec(`DELETE FROM explorer.candles`)
			require.NoError(t, err)
			n, tradesRead, err := rebuilder(t, e).All(ctx, "")
			require.NoError(t, err)
			assert.Equal(t, 6, n)
			assert.Equal(t, 11, tradesRead, "six fills and five swaps")
			assert.Equal(t, rows, candleRows(t, e), "the rebuild gives the indexer's rows")

			// One market, its stale rows replaced.
			_, err = e.db.Exec(`UPDATE explorer.candles SET close = 99 WHERE market_id = $1`, swapMarket)
			require.NoError(t, err)
			_, _, err = rebuilder(t, e).All(ctx, swapMarket)
			require.NoError(t, err)
			assert.Equal(t, rows, candleRows(t, e))
		})
	}
	require.Len(t, written, 2)
	assert.Equal(t, written[0], written[1], "both writers give the same rows")
}

// The post-migration step builds the whole history once, per market: an
// interrupted run resumes at the markets left; once done a later up does
// nothing.
func TestCandlesMigrationStepResumes(t *testing.T) {
	e := newSyncEnv(t)
	assert.Empty(t, queryStrings(t, e.db, `SELECT name FROM explorer.pending_rewrites WHERE name LIKE 'candles%'`),
		"a fresh database is built at once")
	indexCandles(t, e, false)
	rows := candleRows(t, e)
	markets := queryStrings(t, e.db, `SELECT DISTINCT market_id FROM explorer.candles`)

	// As 000024 leaves a database with trades: the row 'candles', no candle.
	_, err := e.db.Exec(`DELETE FROM explorer.candles; INSERT INTO explorer.pending_rewrites (name) VALUES ('candles')`)
	require.NoError(t, err)
	res := migrateUp(t, e.url)
	assert.Contains(t, res.Steps, "candles")
	assert.Equal(t, rows, candleRows(t, e))
	assert.Empty(t, queryStrings(t, e.db, `SELECT name FROM explorer.pending_rewrites WHERE name LIKE 'candles%'`))

	// Interrupted after listing the markets and building all but the first:
	// only that one is built at the next up.
	_, err = e.db.Exec(`DELETE FROM explorer.candles`)
	require.NoError(t, err)
	_, err = e.db.Exec(`INSERT INTO explorer.pending_rewrites (name) VALUES ($1)`, "candles:"+markets[0])
	require.NoError(t, err)
	migrateUp(t, e.url)
	assert.Equal(t, []string{markets[0]}, queryStrings(t, e.db, `SELECT DISTINCT market_id FROM explorer.candles`))
	assert.Empty(t, queryStrings(t, e.db, `SELECT name FROM explorer.pending_rewrites WHERE name LIKE 'candles%'`))

	// Done: nothing rebuilt.
	_, err = e.db.Exec(`DELETE FROM explorer.candles`)
	require.NoError(t, err)
	migrateUp(t, e.url)
	assert.Empty(t, candleRows(t, e))
}

// Fills numbered late by internal/trades (the previous release's, during a
// deploy) enter their candles when they get their trade id.
func TestCandlesOfTradesNumberedLate(t *testing.T) {
	e := newSyncEnv(t)
	indexCandles(t, e, false)
	rows := candleRows(t, e)
	for _, q := range []string{`UPDATE explorer.order_fills SET trade_id = NULL`, `DELETE FROM explorer.swaps`,
		`UPDATE explorer.pending_rewrites SET resume_from = 0 WHERE name = 'swaps'`} {
		_, err := e.db.Exec(q)
		require.NoError(t, err)
	}
	pool, err := pgxpool.New(context.Background(), e.url)
	require.NoError(t, err)
	defer pool.Close()
	// Without trade ids the fills are no trades yet: their buckets go.
	_, _, err = rebuilder(t, e).All(context.Background(), "")
	require.NoError(t, err)
	assert.Less(t, len(candleRows(t, e)), len(rows))

	_, err = trades.CatchUp(context.Background(), pool)
	require.NoError(t, err)
	assert.Equal(t, rows, candleRows(t, e))
}

type e2eMarkets map[string]orderbook.Market

func (m e2eMarkets) Market(_ context.Context, id string) (orderbook.Market, error) {
	if mk, ok := m[id]; ok {
		return mk, nil
	}
	return orderbook.Market{}, orderbook.ErrUnknownMarket
}

type e2eExponents map[string]int

func (x e2eExponents) Exponents(_ context.Context, denoms []string) (map[string]int, error) {
	return x, nil
}

type e2eClock time.Time

func (c e2eClock) Now() time.Time { return time.Time(c) }

// The intervals over the stored candles and trades: a pool's denoms come
// with its first swap, a market's from the chain.
func TestCandlesIntervalsOverTheStore(t *testing.T) {
	ctx := context.Background()
	e := newSyncEnv(t)
	indexCandles(t, e, false)
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	defer pool.Close()

	b, _, _ := swapBlock(t)
	svc := candles.NewService(candles.ServiceDeps{
		Reader:    candles.NewPGReader(pool),
		Markets:   e2eMarkets{swapMarket: {ID: swapMarket, Base: swapHoney, Quote: "ubze"}},
		Exponents: e2eExponents{swapAtom: 6, "ubze": 6, swapHoney: 6},
		Clock:     e2eClock(b.Time.Add(7 * time.Minute)),
	})
	// swapH's first hop: 50 atom in, 1200 ubze out; the window starts at
	// 07:52, so 07:55, 08:00 and 08:05.
	ivs, err := svc.Intervals(ctx, swapAtom+"_ubze", 5, 3)
	require.NoError(t, err)
	require.Len(t, ivs, 3)
	start := candles.BucketStart(b.Time, 5)
	assert.Equal(t, candles.Interval{Start: start, End: start.Add(5 * time.Minute), Low: "24", Open: "24", Average: "24",
		High: "24", Close: "24", BaseVolume: "0.00005", QuoteVolume: "0.0012"}, ivs[1])
	assert.Equal(t, "0", ivs[0].Close)
	assert.Equal(t, "0", ivs[2].Close)

	// swapH's two fills of 10 at 2.
	ivs, err = svc.Intervals(ctx, swapMarket, 5, 3)
	require.NoError(t, err)
	require.Len(t, ivs, 3)
	assert.Equal(t, candles.Interval{Start: start, End: start.Add(5 * time.Minute), Low: "2", Open: "2", Average: "2",
		High: "2", Close: "2", BaseVolume: "0.00002", QuoteVolume: "0.00004"}, ivs[1])

	_, err = svc.Intervals(ctx, "uatom/unknown", 5, 10)
	assert.ErrorIs(t, err, candles.ErrUnknownMarket)
}

// The candles' share of a live block, for a market with a busy day: the hook
// recomputes the whole UTC day of every market the block touches.
func TestCandlesPerBlockCost(t *testing.T) {
	ctx := context.Background()
	e := newSyncEnv(t)
	indexCandles(t, e, false)
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	defer pool.Close()

	const busy = 5_000
	day := time.Date(2027, 1, 16, 0, 0, 0, 0, time.UTC)
	_, err = e.db.Exec(`INSERT INTO explorer.order_fills (height, seq, order_id, market_id, side, price, amount, maker, taker, time, trade_id)
		SELECT 26100000 + g, 0, lpad(g::text, 24, '0'), $1, 'sell', 0.5 + g / 10000.0, 1000 + g, 'bze1m', 'bze1t',
		       $2::timestamptz + make_interval(secs => g * 17), (26100000 + g)::bigint * 1000000 + 1
		  FROM generate_series(1, $3) AS g`, swapMarket, day, busy)
	require.NoError(t, err)
	block := fillFlush(swapMarket, day.Add(23*time.Hour))

	const runs = 20
	var total time.Duration
	for range runs {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		start := time.Now()
		require.NoError(t, candles.Default.PostFlush(ctx, tx, block))
		total += time.Since(start)
		require.NoError(t, tx.Rollback(ctx))
	}
	t.Logf("candles per block, one market with %d trades that day: %s", busy, total/runs)

	start := time.Now()
	_, n, err := rebuilder(t, e).All(ctx, swapMarket)
	require.NoError(t, err)
	elapsed := time.Since(start)
	t.Logf("rebuild: %d trades in %s, %.0f trades/s", n, elapsed, float64(n)/elapsed.Seconds())
	assert.Equal(t, 288+96+24+6+1+5, len(queryStrings(t, e.db, `SELECT 1 FROM explorer.candles WHERE market_id = $1`, swapMarket)),
		"the busy day's every bucket, and the synthetic block's")
	assert.True(t, strings.HasPrefix(queryStrings(t, e.db, `SELECT base_volume::text FROM explorer.candles
		WHERE market_id = $1 AND minutes = 1440 AND start_at = $2`, swapMarket, day)[0], fmt.Sprint(busy*1000+busy*(busy+1)/2)))
}

// fillFlush is a writer flush with one fill of market at t.
func fillFlush(market string, t time.Time) *writer.Flush {
	return &writer.Flush{Entities: &transform.Entities{OrderFills: []transform.OrderFill{{MarketID: market, Time: t}}}}
}
