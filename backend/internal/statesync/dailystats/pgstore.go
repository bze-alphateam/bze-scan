package dailystats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// Stats are one day's numbers, as daily_stats stores them and the stats
// endpoint serves them.
type Stats struct {
	Day             time.Time
	Blocks          int64
	Txs             int64
	TxsFailed       int64
	ActiveAddresses int64
	NewAddresses    int64
	Transfers       int64
	// Fees sums the transactions' fees per denom, sorted by denom.
	Fees []chain.Coin
	// AvgBlockTimeMs is nil when no block of the day has a block time.
	AvgBlockTimeMs *int64
}

// aggregateSQL computes the numbers of the blocks with a time in [$1, $2).
// The blocks are found on their time; every other table is read on the
// height range they span, which belongs to the day alone since heights and
// times grow together. One row, blocks = 0 when no block is indexed.
const aggregateSQL = `WITH b AS (
		SELECT count(*) AS blocks, min(height) AS lo, max(height) AS hi,
		       coalesce(sum(tx_count), 0) AS txs, coalesce(sum(tx_failed_count), 0) AS txs_failed,
		       round(avg(block_time_ms))::bigint AS avg_block_time_ms
		  FROM explorer.blocks WHERE time >= $1 AND time < $2)
	SELECT b.blocks, b.txs, b.txs_failed,
	       (SELECT count(DISTINCT a.address) FROM explorer.account_activity a WHERE a.height BETWEEN b.lo AND b.hi),
	       (SELECT count(*) FROM explorer.accounts ac WHERE ac.first_seen_time >= $1 AND ac.first_seen_time < $2),
	       (SELECT count(*) FROM explorer.transfers t WHERE t.height BETWEEN b.lo AND b.hi),
	       (SELECT coalesce(jsonb_agg(jsonb_build_object('denom', f.denom, 'amount', f.amount::text) ORDER BY f.denom), '[]')
	          FROM (SELECT c->>'denom' AS denom, sum((c->>'amount')::numeric) AS amount
	                  FROM explorer.transactions t CROSS JOIN LATERAL jsonb_array_elements(t.fee) c
	                 WHERE t.height BETWEEN b.lo AND b.hi GROUP BY 1) f),
	       b.avg_block_time_ms
	  FROM b`

// recomputeSQL upserts the day $3 from the aggregate of [$1, $2); a day
// without blocks writes nothing.
const recomputeSQL = `INSERT INTO explorer.daily_stats
		(day, blocks, txs, txs_failed, active_addresses, new_addresses, transfers, fees, avg_block_time_ms)
	SELECT $3::date, a.* FROM (` + aggregateSQL + `) a WHERE a.blocks > 0
	ON CONFLICT (day) DO UPDATE SET
	  blocks = EXCLUDED.blocks, txs = EXCLUDED.txs, txs_failed = EXCLUDED.txs_failed,
	  active_addresses = EXCLUDED.active_addresses, new_addresses = EXCLUDED.new_addresses,
	  transfers = EXCLUDED.transfers, fees = EXCLUDED.fees, avg_block_time_ms = EXCLUDED.avg_block_time_ms`

// Querier runs one query of one row; a pgx pool, connection or transaction
// satisfies it.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Aggregate computes the numbers of the blocks with a time in [from, to)
// with the query the nightly run stores: the live "today" of the stats
// endpoint. Blocks is 0 when no block of the window is indexed.
func Aggregate(ctx context.Context, q Querier, from, to time.Time) (Stats, error) {
	s := Stats{Day: Day(from)}
	var fees []byte
	err := q.QueryRow(ctx, aggregateSQL, from.UTC(), to.UTC()).Scan(&s.Blocks, &s.Txs, &s.TxsFailed,
		&s.ActiveAddresses, &s.NewAddresses, &s.Transfers, &fees, &s.AvgBlockTimeMs)
	if err != nil {
		return Stats{}, fmt.Errorf("daily stats aggregate: %w", err)
	}
	if err := json.Unmarshal(fees, &s.Fees); err != nil {
		return Stats{}, fmt.Errorf("daily stats aggregate fees: %w", err)
	}
	return s, nil
}

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Querier
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// PGStore reads the indexed tables and writes explorer.daily_stats.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// OldestDay reads the day of the lowest indexed block.
func (s *PGStore) OldestDay(ctx context.Context) (time.Time, bool, error) {
	var t time.Time
	err := s.db.QueryRow(ctx, `SELECT time FROM explorer.blocks ORDER BY height LIMIT 1`).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("oldest block: %w", err)
	}
	return Day(t), true, nil
}

// StoredDays reads the days in [from, to] that have a row.
func (s *PGStore) StoredDays(ctx context.Context, from, to time.Time) ([]time.Time, error) {
	rows, err := s.db.Query(ctx, `SELECT day FROM explorer.daily_stats WHERE day BETWEEN $1::date AND $2::date ORDER BY day`,
		from.UTC(), to.UTC())
	if err != nil {
		return nil, fmt.Errorf("stored days: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[time.Time])
	if err != nil {
		return nil, fmt.Errorf("stored days: %w", err)
	}
	return out, nil
}

// LowestDispatched reads the main backfill job's checkpoint.
func (s *PGStore) LowestDispatched(ctx context.Context) (int64, bool, error) {
	var h int64
	err := s.db.QueryRow(ctx, `SELECT lowest_dispatched FROM explorer.backfill_checkpoints WHERE job = 'main'`).Scan(&h)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("backfill checkpoint: %w", err)
	}
	return h, true, nil
}

// HeightDays reads the days of the lowest indexed block at or above lo and
// the highest at or below hi.
func (s *PGStore) HeightDays(ctx context.Context, lo, hi int64) (first, last time.Time, ok bool, err error) {
	var f, l *time.Time
	err = s.db.QueryRow(ctx, `SELECT
		(SELECT time FROM explorer.blocks WHERE height BETWEEN $1 AND $2 ORDER BY height LIMIT 1),
		(SELECT time FROM explorer.blocks WHERE height BETWEEN $1 AND $2 ORDER BY height DESC LIMIT 1)`,
		lo, hi).Scan(&f, &l)
	if err != nil {
		return time.Time{}, time.Time{}, false, fmt.Errorf("backfilled days: %w", err)
	}
	if f == nil || l == nil {
		return time.Time{}, time.Time{}, false, nil
	}
	return Day(*f), Day(*l), true, nil
}

// Recompute upserts the day's row from the indexed tables.
func (s *PGStore) Recompute(ctx context.Context, day time.Time) (bool, error) {
	from := Day(day)
	tag, err := s.db.Exec(ctx, recomputeSQL, from, from.AddDate(0, 0, 1), from)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// Cursor reads the set's stored cursor.
func (s *PGStore) Cursor(ctx context.Context) (json.RawMessage, error) {
	var raw []byte
	err := s.db.QueryRow(ctx, `SELECT cursor FROM explorer.sync_jobs WHERE job = $1`, statesync.DailyStats).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("daily stats cursor: %w", err)
	}
	return raw, nil
}
