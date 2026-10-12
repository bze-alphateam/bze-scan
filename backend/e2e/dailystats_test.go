//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/dailystats"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// The recorded heights on either side of the 2026-10-09 UTC midnight.
const (
	hBeforeMidnight = int64(25003529) // 2026-10-08T23:59:57Z, no transaction
	hAfterMidnight  = int64(25003530) // 2026-10-09T00:00:02Z, one transaction
)

func dailyRows(t *testing.T, e *syncEnv) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', day, blocks, txs, txs_failed, active_addresses, new_addresses,
		transfers, fees::text, coalesce(avg_block_time_ms::text, '-')) FROM explorer.daily_stats`)
}

// The nightly job aggregates each closed UTC day on its own, a second run
// rewrites the same rows, and sync-state --from --to recomputes a range.
func TestDailyStatsOfTheIndexedDays(t *testing.T) {
	e := newSyncEnv(t)
	e.index(t, hBeforeMidnight, hAfterMidnight)

	// What each day's rows hold, counted table by table.
	perDay := func(query string) []string { return queryStrings(t, e.db, query) }
	require.Equal(t, []string{"2026-10-08 1", "2026-10-09 1"},
		perDay(`SELECT concat_ws(' ', d, count(*)) FROM (
			SELECT (time AT TIME ZONE 'UTC')::date AS d FROM explorer.blocks) b GROUP BY d`))

	code, out := e.syncState(t, "--job", "daily_stats")
	require.Equal(t, 0, code, out)
	want := dailyRows(t, e)
	require.Len(t, want, 2, "one row per closed day")
	// 10-08: one empty block whose predecessor is not indexed (no block
	// time). 10-09: a relayer's MsgUpdateClient paying 237,736 ubze, its one
	// transfer the fee to the fee collector (a module account, not an
	// active address); the relayer is first seen here; 5,756 ms after the
	// block before midnight.
	assert.Equal(t, []string{
		"2026-10-08 1 0 0 0 0 0 [] -",
		"2026-10-09 1 1 0 1 1 1 [{\"denom\": \"ubze\", \"amount\": \"237736\"}] 5756",
	}, want)
	assert.Equal(t, []string{"daily_stats"}, queryStrings(t, e.db,
		`SELECT job FROM explorer.sync_jobs WHERE job = 'daily_stats' AND last_error IS NULL AND cursor IS NULL`),
		"recorded; no backfill, so no cursor")

	code, out = e.syncState(t, "--job", "daily_stats")
	require.Equal(t, 0, code, out)
	assert.Equal(t, want, dailyRows(t, e), "a second run rewrites the same rows")

	// A reindex changed the rows behind a day: the range recomputes it.
	_, err := e.db.Exec(`UPDATE explorer.daily_stats SET blocks = 99, fees = '[]'`)
	require.NoError(t, err)
	code, out = e.syncState(t, "--job", "daily_stats", "--from", "2026-10-07", "--to", "2026-10-09")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "2 days of daily_stats recomputed", "a day without blocks gets no row")
	assert.Equal(t, want, dailyRows(t, e))
	assert.Equal(t, []string{"2026-10-08", "2026-10-09"}, perDay(`SELECT day::text FROM explorer.daily_stats`))

	code, out = e.syncState(t, "--job", "daily_stats", "--from", "2026-10-09", "--to", time.Now().UTC().Format(time.DateOnly))
	assert.Equal(t, 1, code, out)
}

// The backfill's position is the job's cursor: stored on the first run,
// moved when the backfill wrote lower heights, and the days in between are
// recomputed. (Until 2026-10-12 these days are also among the last three
// closed days; the window alone is pinned by the unit tests.)
func TestDailyStatsFollowTheBackfill(t *testing.T) {
	e := newSyncEnv(t)
	e.index(t, hAfterMidnight)
	_, err := e.db.Exec(`INSERT INTO explorer.backfill_checkpoints
		(job, ceiling_height, floor_height, lowest_dispatched, resume_height, status, started_at, updated_at)
		VALUES ('main', $1, 1, $1, $1, 'running', now(), now())`, hAfterMidnight)
	require.NoError(t, err)
	code, out := e.syncState(t, "--job", "daily_stats")
	require.Equal(t, 0, code, out)
	assert.Equal(t, []string{`{"lowest_dispatched": 25003530}`}, queryStrings(t, e.db,
		`SELECT cursor::text FROM explorer.sync_jobs WHERE job = 'daily_stats'`))
	assert.Equal(t, []string{"2026-10-09"}, queryStrings(t, e.db, `SELECT day::text FROM explorer.daily_stats`))

	// The backfill writes the height before midnight: 10-08 is new, the
	// stale 10-09 row (made wrong here) is recomputed as the day above.
	_, err = e.db.Exec(`UPDATE explorer.daily_stats SET blocks = 99`)
	require.NoError(t, err)
	e.index(t, hBeforeMidnight)
	_, err = e.db.Exec(`UPDATE explorer.backfill_checkpoints SET lowest_dispatched = $1`, hBeforeMidnight)
	require.NoError(t, err)
	code, out = e.syncState(t, "--job", "daily_stats")
	require.Equal(t, 0, code, out)
	assert.Equal(t, []string{"2026-10-08 1", "2026-10-09 1"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', day, blocks) FROM explorer.daily_stats`),
		"the window 10-08 plus the day above")
	assert.Equal(t, []string{`{"lowest_dispatched": 25003529}`}, queryStrings(t, e.db,
		`SELECT cursor::text FROM explorer.sync_jobs WHERE job = 'daily_stats'`))
}

// todayHeight is a synthetic height written at the current time.
const todayHeight = int64(30_000_000)

// The stats endpoint serves the stored days of the window, oldest first,
// and today computed live with the same query.
func TestStatsServeTheDailySeriesAndToday(t *testing.T) {
	e := newSyncEnv(t)
	today := dailystats.Day(time.Now())
	for _, ago := range []int{1, 2, 5, 40} {
		_, err := e.db.Exec(`INSERT INTO explorer.daily_stats VALUES ($1::date, 17000, 40, 2, 12, 3, 80,
			'[{"denom":"ubze","amount":"1000"}]', 5050)`, today.AddDate(0, 0, -ago))
		require.NoError(t, err)
	}

	// Today: two transactions of one signer, fees in two denoms.
	at := time.Now().UTC().Truncate(time.Second)
	ents := &transform.Entities{Blocks: []transform.Block{{Height: todayHeight, Time: at, TxCount: 2, TxFailedCount: 1, Hash: "TODAY"}}}
	for i, fee := range [][]chain.Coin{
		{{Denom: "ubze", Amount: "1500"}, {Denom: "ibc/AB", Amount: "7"}},
		{{Denom: "ubze", Amount: "2500"}},
	} {
		ents.Transactions = append(ents.Transactions, transform.Transaction{Height: todayHeight, TxIndex: i, Time: at,
			Hash: txHash(900 + i), Success: i == 0, Fee: fee, Signers: []string{dexOwner}})
		ents.Activity = append(ents.Activity, transform.Activity{Address: dexOwner, Height: todayHeight, TxIndex: i, Time: at,
			Category: "other", Kind: "other", IsSigner: true, Success: i == 0})
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, writer.NewLiveWriter(pool).WriteBlock(ctx, ents))

	base := e.serveAPI(t)
	stats := func(query string) dto.Stats {
		r := fetch(t, base+"/stats"+query)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		return into[dto.Stats](t, r)
	}
	days := func(s dto.Stats) []string {
		var out []string
		for _, d := range s.Daily {
			out = append(out, d.Day)
		}
		return out
	}
	day := func(ago int) string { return today.AddDate(0, 0, -ago).Format(time.DateOnly) }

	s := stats("")
	assert.Equal(t, []string{day(5), day(2), day(1)}, days(s), "the last 30 closed days, oldest first, gaps skipped")
	assert.Equal(t, dto.DayStats{Day: day(1), Blocks: 17000, Txs: 40, TxsFailed: 2, ActiveAddresses: 12, NewAddresses: 3,
		Transfers: 80, Fees: []dto.Coin{{Denom: "ubze", Amount: "1000"}}, AvgBlockTimeMs: ptr(int64(5050))}, s.Daily[2])
	require.NotNil(t, s.Today)
	assert.Equal(t, dto.DayStats{Day: today.Format(time.DateOnly), Blocks: 1, Txs: 2, TxsFailed: 1, ActiveAddresses: 1,
		NewAddresses: 1, Fees: []dto.Coin{{Denom: "ibc/AB", Amount: "7"}, {Denom: "ubze", Amount: "4000"}}}, *s.Today)

	assert.Equal(t, []string{day(40), day(5), day(2), day(1)}, days(stats("?days=365")))
	assert.Equal(t, []string{day(2), day(1)}, days(stats("?days=2")))
	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/stats?days=366").status)
}

func ptr[T any](v T) *T { return &v }
