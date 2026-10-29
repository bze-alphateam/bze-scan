// Package trades numbers the trades of blocks already stored: the swaps rows
// and the order_fills trade ids, from the stored SwapEvents (messages.events,
// block_events) and fills, through transform.NumberTrades and
// writer.WriteTrades, the transformer's and the writers' own code.
//
// It covers the blocks no transformer of this release numbered: everything
// stored before migration 000023, and the blocks the previous release's
// indexer writes while both releases run (a deploy's overlap). Its cursor is
// migration 000023's explorer.pending_rewrites row 'swaps': resume_from is
// the lowest height not checked yet. CatchUp walks from there to the highest
// stored block, one transaction per range of at most a partition that also
// moves the cursor, so an interrupted run resumes where it stopped. Rows
// already there are kept: running it again changes nothing.
//
// A height the previous release stores below the cursor after CatchUp
// passed it (a hole its backfill fills late) is not numbered; its fills
// are, by the check of fills without a trade id that every run starts with.
package trades

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
	"github.com/bze-alphateam/bze-scan/backend/migrations"
)

// RangeSize is the most heights one transaction numbers: a partition.
const RangeSize = migrations.PartitionSize

// DB is what CatchUp needs; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Tx is the part of a transaction one range uses; pgx.Tx satisfies it.
type Tx interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Result counts what a CatchUp wrote.
type Result struct {
	// Ranges are the transactions run, Heights the heights with trades seen.
	Ranges, Heights int
}

// CatchUp numbers the stored trades from the cursor up to the highest stored
// block, and first the heights of fills without a trade id below it. Without
// the cursor row (a schema before 000023's) it does nothing.
func CatchUp(ctx context.Context, db DB) (Result, error) {
	var res Result
	var from int64
	err := db.QueryRow(ctx, `SELECT resume_from FROM explorer.pending_rewrites WHERE name = 'swaps'`).Scan(&from)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("trades cursor: %w", err)
	}
	var top *int64
	if err := db.QueryRow(ctx, `SELECT max(height) FROM explorer.blocks`).Scan(&top); err != nil {
		return res, fmt.Errorf("highest block: %w", err)
	}

	// Fills below the cursor that no one numbered: one range per height.
	rows, err := db.Query(ctx, `SELECT DISTINCT height FROM explorer.order_fills
		WHERE trade_id IS NULL AND height < $1 ORDER BY height`, from)
	if err != nil {
		return res, fmt.Errorf("fills without a trade id: %w", err)
	}
	missed, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return res, fmt.Errorf("fills without a trade id: %w", err)
	}
	for _, h := range missed {
		if err := inTx(ctx, db, h, h+1, false, &res); err != nil {
			return res, err
		}
	}

	if top == nil || *top < from {
		return res, nil
	}
	for lo := from; lo <= *top; {
		hi := min(migrations.PartitionLower(lo)+RangeSize, *top+1)
		if err := inTx(ctx, db, lo, hi, true, &res); err != nil {
			return res, err
		}
		lo = hi
	}
	return res, nil
}

// inTx numbers [lo, hi) in a transaction of its own, moving the cursor to hi
// when advance is set.
func inTx(ctx context.Context, db DB, lo, hi int64, advance bool, res *Result) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("trades %d..%d: begin: %w", lo, hi-1, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	n, err := Range(ctx, tx, lo, hi)
	if err != nil {
		return err
	}
	if advance {
		if _, err := tx.Exec(ctx, `UPDATE explorer.pending_rewrites SET resume_from = $1
			WHERE name = 'swaps' AND resume_from < $1`, hi); err != nil {
			return fmt.Errorf("trades %d..%d: cursor: %w", lo, hi-1, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("trades %d..%d: commit: %w", lo, hi-1, err)
	}
	res.Ranges++
	res.Heights += n
	return nil
}

// height is one block's stored trades.
type height struct {
	txSwaps, blockSwaps []transform.Swap
	fills               []transform.OrderFill
}

// Range numbers the stored trades of [lo, hi) in tx, recomputes their
// candles (candles.Upkeep.AfterNumbering) and returns how many heights have
// trades.
func Range(ctx context.Context, tx Tx, lo, hi int64) (int, error) {
	fail := func(what string, err error) (int, error) {
		return 0, fmt.Errorf("trades %d..%d: %s: %w", lo, hi-1, what, err)
	}
	byHeight := map[int64]*height{}
	at := func(h int64) *height {
		if byHeight[h] == nil {
			byHeight[h] = &height{}
		}
		return byHeight[h]
	}

	// The transactions' swaps, in numbering order.
	rows, err := tx.Query(ctx, `SELECT m.height, m.tx_index, m.msg_index, coalesce(t.hash, ''), b.time, e.ev->'attrs'
		  FROM explorer.messages m
		  CROSS JOIN LATERAL jsonb_array_elements(m.events) WITH ORDINALITY AS e(ev, k)
		  JOIN explorer.blocks b ON b.height = m.height
		  LEFT JOIN explorer.transactions t ON t.height = m.height AND t.tx_index = m.tx_index
		 WHERE m.height >= $1 AND m.height < $2
		   AND m.events @? '$[*] ? (@.type == "bze.tradebin.SwapEvent")'
		   AND e.ev->>'type' = 'bze.tradebin.SwapEvent'
		 ORDER BY m.height, m.tx_index, m.msg_index, e.k`, lo, hi)
	if err != nil {
		return fail("transaction swaps", err)
	}
	var (
		h               int64
		txIndex, msgIdx int
		hash            string
		blockTime       time.Time
		raw             []byte
	)
	_, err = pgx.ForEachRow(rows, []any{&h, &txIndex, &msgIdx, &hash, &blockTime, &raw}, func() error {
		s, err := swapOf(h, blockTime, raw)
		if err != nil {
			return fmt.Errorf("height %d tx %d msg %d: %w", h, txIndex, msgIdx, err)
		}
		ti, mi := txIndex, msgIdx
		s.TxHash, s.TxIndex, s.MsgIndex = hash, &ti, &mi
		at(h).txSwaps = append(at(h).txSwaps, s)
		return nil
	})
	if err != nil {
		return fail("transaction swaps", err)
	}

	// The block-level swaps.
	rows, err = tx.Query(ctx, `SELECT e.height, e.seq, b.time, e.attrs
		  FROM explorer.block_events e JOIN explorer.blocks b ON b.height = e.height
		 WHERE e.height >= $1 AND e.height < $2 AND e.type = 'bze.tradebin.SwapEvent'
		 ORDER BY e.height, e.seq`, lo, hi)
	if err != nil {
		return fail("block swaps", err)
	}
	var seq int
	_, err = pgx.ForEachRow(rows, []any{&h, &seq, &blockTime, &raw}, func() error {
		s, err := swapOf(h, blockTime, raw)
		if err != nil {
			return fmt.Errorf("height %d block event %d: %w", h, seq, err)
		}
		sq := seq
		s.BlockSeq = &sq
		at(h).blockSwaps = append(at(h).blockSwaps, s)
		return nil
	})
	if err != nil {
		return fail("block swaps", err)
	}

	// The fills.
	rows, err = tx.Query(ctx, `SELECT height, seq FROM explorer.order_fills
		 WHERE height >= $1 AND height < $2 ORDER BY height, seq`, lo, hi)
	if err != nil {
		return fail("fills", err)
	}
	_, err = pgx.ForEachRow(rows, []any{&h, &seq}, func() error {
		at(h).fills = append(at(h).fills, transform.OrderFill{Height: h, Seq: seq})
		return nil
	})
	if err != nil {
		return fail("fills", err)
	}

	var swaps []transform.Swap
	var fills []transform.OrderFill
	for h, t := range byHeight {
		numbered, err := transform.NumberTrades(h, t.txSwaps, t.blockSwaps, t.fills)
		if err != nil {
			return fail(fmt.Sprintf("height %d", h), err)
		}
		swaps = append(swaps, numbered...)
		fills = append(fills, t.fills...)
	}
	// Key order, as the writers go, so concurrent writers wait instead of
	// deadlocking.
	slices.SortFunc(swaps, func(a, b transform.Swap) int { return cmp.Compare(a.TradeID, b.TradeID) })
	slices.SortFunc(fills, func(a, b transform.OrderFill) int { return cmp.Compare(a.TradeID, b.TradeID) })
	if err := writer.WriteTrades(ctx, tx, hi-1, swaps, fills); err != nil {
		return 0, err
	}
	// The candles of the fills numbered here count them only now.
	if err := candles.Default.AfterNumbering(ctx, tx, lo, hi); err != nil {
		return 0, fmt.Errorf("trades %d..%d: %w", lo, hi-1, err)
	}
	return len(byHeight), nil
}

// swapOf reads a stored SwapEvent's attributes.
func swapOf(h int64, blockTime time.Time, raw []byte) (transform.Swap, error) {
	var attrs map[string]any
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return transform.Swap{}, err
	}
	return transform.StoredSwap(h, blockTime, attrs)
}

// MigrationStep is the post-migration step "swaps": CatchUp at every
// `migrate up`, so a release starts with every stored trade numbered.
func MigrationStep() migrations.Step {
	return migrations.Step{Name: "swaps", Run: func(ctx context.Context, db migrations.DB) error {
		_, err := CatchUp(ctx, db)
		return err
	}}
}

// Interval is how often Run catches up.
const Interval = time.Minute

// Run catches up at start and every interval (Interval when zero) until ctx
// ends; a failed pass is logged and retried at the next tick.
func Run(ctx context.Context, db DB, interval time.Duration, lg log.FieldLogger) error {
	if interval <= 0 {
		interval = Interval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		res, err := CatchUp(ctx, db)
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil:
			lg.WithError(err).Error("trades: catch-up failed")
		case res.Heights > 0:
			lg.WithFields(log.Fields{"ranges": res.Ranges, "heights": res.Heights}).Debug("trades: numbered")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}
