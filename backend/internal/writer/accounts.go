package writer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// mergeDeltas merges the per-key deltas of a flush, combining the deltas of
// one key with merge, and returns them in key order. Every writer of a
// counter row applies its deltas in that order, so two flushes touching the
// same rows lock them in the same order; a deadlock can still come from
// other statements, which withDeadlockRetry covers.
func mergeDeltas[K cmp.Ordered, D any](deltas []D, key func(D) K, merge func(a, b D) D) []D {
	byKey := make(map[K]D, len(deltas))
	for _, d := range deltas {
		k := key(d)
		if cur, ok := byKey[k]; ok {
			d = merge(cur, d)
		}
		byKey[k] = d
	}
	keys := make([]K, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]D, len(keys))
	for i, k := range keys {
		out[i] = byKey[k]
	}
	return out
}

// deadlockDetected is PostgreSQL's SQLSTATE for a deadlock.
const deadlockDetected = "40P01"

// withDeadlockRetry runs write and runs it once more when it failed on a
// deadlock: PostgreSQL rolled the transaction back, and the second attempt
// usually finds the other transaction committed.
func withDeadlockRetry(ctx context.Context, write func(context.Context) error) error {
	err := write(ctx)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == deadlockDetected {
		return write(ctx)
	}
	return err
}

// accountRow is the delta of one explorer.accounts row in a flush: the
// first and last height the address signed at, and the number of its
// transactions the flush inserted.
type accountRow struct {
	Address     string    `json:"address"`
	FirstHeight int64     `json:"first_height"`
	FirstTime   time.Time `json:"first_time"`
	LastHeight  int64     `json:"last_height"`
	TxCount     int64     `json:"tx_count"`
}

// accountDeltas returns the accounts deltas of the signers of every
// transaction of ents, merged per address in address order. First and last
// sight move for every transaction (LEAST and GREATEST are idempotent);
// tx_count counts only the transactions in inserted, so writing a height
// again never counts it twice.
func accountDeltas(ents *transform.Entities, inserted map[TxKey]bool) []accountRow {
	var rows []accountRow
	for _, t := range ents.Transactions {
		var count int64
		if inserted[TxKey{Height: t.Height, TxIndex: t.TxIndex}] {
			count = 1
		}
		for _, s := range slices.Compact(slices.Sorted(slices.Values(t.Signers))) {
			if s == "" {
				continue
			}
			rows = append(rows, accountRow{Address: s, FirstHeight: t.Height, FirstTime: t.Time,
				LastHeight: t.Height, TxCount: count})
		}
	}
	return mergeDeltas(rows, func(r accountRow) string { return r.Address }, func(a, b accountRow) accountRow {
		if b.FirstHeight < a.FirstHeight {
			a.FirstHeight, a.FirstTime = b.FirstHeight, b.FirstTime
		}
		a.LastHeight = max(a.LastHeight, b.LastHeight)
		a.TxCount += b.TxCount
		return a
	})
}

// accountsSQL applies account deltas in address order: first sight with
// LEAST, last sight with GREATEST, tx_count incremented. A row that would
// not change is not rewritten.
const accountsSQL = `INSERT INTO explorer.accounts AS a (address, first_seen_height, first_seen_time, last_seen_height, tx_count)
		SELECT r.address, r.first_height, r.first_time, r.last_height, r.tx_count
		  FROM jsonb_to_recordset($1::jsonb)
		       AS r(address text, first_height bigint, first_time timestamptz, last_height bigint, tx_count bigint)
		 ORDER BY r.address
		ON CONFLICT (address) DO UPDATE SET
			first_seen_height = LEAST(a.first_seen_height, EXCLUDED.first_seen_height),
			first_seen_time = CASE WHEN EXCLUDED.first_seen_height < a.first_seen_height
			                       THEN EXCLUDED.first_seen_time ELSE a.first_seen_time END,
			last_seen_height = GREATEST(a.last_seen_height, EXCLUDED.last_seen_height),
			tx_count = a.tx_count + EXCLUDED.tx_count
		WHERE EXCLUDED.first_seen_height < a.first_seen_height
		   OR EXCLUDED.last_seen_height > a.last_seen_height
		   OR EXCLUDED.tx_count <> 0`

// accountStatements returns the accounts writes of a flush whose inserted
// transactions are known, in chunks of ChunkRows addresses.
func accountStatements(top int64, ents *transform.Entities, inserted map[TxKey]bool) ([]statement, error) {
	payloads, err := jsonChunks(top, "accounts", accountDeltas(ents, inserted))
	if err != nil {
		return nil, err
	}
	stmts := make([]statement, len(payloads))
	for i, p := range payloads {
		stmts[i] = statement{table: "accounts", sql: accountsSQL, args: []any{p}}
	}
	return stmts, nil
}

// execAll runs statements in order inside tx.
func execAll(ctx context.Context, tx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}, top int64, stmts []statement) error {
	for _, st := range stmts {
		if _, err := tx.Exec(ctx, st.sql, st.args...); err != nil {
			return fmt.Errorf("write %d: %s: %w", top, st.table, err)
		}
	}
	return nil
}
