package writer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// FailureSourceBackfill is index_failures.source for the backfill pipeline.
const FailureSourceBackfill = "backfill"

// PostFlush runs inside a flush's transaction after its rows are inserted,
// with every entity of the flush. An error rolls the whole flush back.
type PostFlush func(ctx context.Context, tx pgx.Tx, flushed *transform.Entities) error

// BatchWriter writes the backfill pipeline's flushes: the entities of several
// heights in one transaction, sent as one pgx batch of multi-row inserts. It
// never touches the live floor or the live cursor, except through
// AdvanceCursor. Give it a pool of its own, never the live writer's.
type BatchWriter struct {
	db    DB
	parts *partitions
	hooks []PostFlush
}

// NewBatchWriter returns a batch writer over db that runs hooks, in order,
// at the end of every flush.
func NewBatchWriter(db DB, hooks ...PostFlush) *BatchWriter {
	return &BatchWriter{db: db, parts: newPartitions(db), hooks: hooks}
}

// blockRow is the JSON shape of the blocks bulk insert.
type blockRow struct {
	Height              int64           `json:"height"`
	Time                time.Time       `json:"time"`
	TxCount             int             `json:"tx_count"`
	TxFailedCount       int             `json:"tx_failed_count"`
	Hash                string          `json:"hash"`
	ProposerConsAddress *string         `json:"proposer_cons_address"`
	SizeBytes           int             `json:"size_bytes"`
	Minted              *string         `json:"minted"`
	Inflation           *string         `json:"inflation"`
	FeesDistributed     json.RawMessage `json:"fees_distributed"`
	SignaturesCount     int             `json:"signatures_count"`
	SignaturesPowerPct  *string         `json:"signatures_power_pct"`
}

const insertBlocksSQL = `INSERT INTO explorer.blocks (
			height, time, tx_count, tx_failed_count, hash, proposer_cons_address, size_bytes,
			minted, inflation, fees_distributed, signatures_count, signatures_power_pct)
		SELECT height, time, tx_count, tx_failed_count, hash, proposer_cons_address, size_bytes,
			minted, inflation, NULLIF(fees_distributed, 'null'::jsonb), signatures_count, signatures_power_pct
		  FROM jsonb_to_recordset($1::jsonb) AS r(
			height bigint, time timestamptz, tx_count integer, tx_failed_count integer, hash text,
			proposer_cons_address text, size_bytes integer, minted numeric, inflation numeric,
			fees_distributed jsonb, signatures_count integer, signatures_power_pct numeric)
		ON CONFLICT (height) DO NOTHING`

// blockTimesSQL fills block_time_ms of the rows in [$1, $2] whose previous
// block is now indexed. The backfill walks downward, so a block's
// predecessor usually arrives in a later flush: $2 is one above the flush's
// highest height, which completes the block written by the previous flush
// (and the live floor's block on the first one).
const blockTimesSQL = `UPDATE explorer.blocks b
		SET block_time_ms = (EXTRACT(EPOCH FROM (b.time - p.time)) * 1000)::integer
		FROM explorer.blocks p
		WHERE b.height BETWEEN $1 AND $2 AND b.block_time_ms IS NULL AND p.height = b.height - 1`

// Write writes the entities of every height in batch in one transaction: the
// transactions, messages and blocks rows, block_time_ms where the previous
// block is known, then the PostFlush hooks. Partitions are topped up first
// when a height enters the last one. Writing a height again changes nothing.
func (w *BatchWriter) Write(ctx context.Context, batch []*transform.Entities) error {
	var all transform.Entities
	for _, e := range batch {
		if e == nil {
			continue
		}
		all.Blocks = append(all.Blocks, e.Blocks...)
		all.Transactions = append(all.Transactions, e.Transactions...)
		all.Messages = append(all.Messages, e.Messages...)
	}
	if len(all.Blocks) == 0 {
		return nil
	}
	lo, hi := all.Blocks[0].Height, all.Blocks[0].Height
	for _, b := range all.Blocks[1:] {
		lo, hi = min(lo, b.Height), max(hi, b.Height)
	}

	stmts, err := rowStatements(hi, &all)
	if err != nil {
		return err
	}
	blocks := make([]blockRow, 0, len(all.Blocks))
	for i := range all.Blocks {
		b := &all.Blocks[i]
		fees, err := b.FeesDistributedJSON()
		if err != nil {
			return fmt.Errorf("write %d: fees_distributed: %w", b.Height, err)
		}
		blocks = append(blocks, blockRow{
			Height: b.Height, Time: b.Time, TxCount: b.TxCount, TxFailedCount: b.TxFailedCount, Hash: b.Hash,
			ProposerConsAddress: nullIfEmpty(b.ProposerConsAddress), SizeBytes: b.SizeBytes,
			Minted: b.Minted, Inflation: b.Inflation, FeesDistributed: fees,
			SignaturesCount: b.SignaturesCount, SignaturesPowerPct: b.SignaturesPowerPct,
		})
	}
	blockStmts, err := chunked(hi, "blocks", insertBlocksSQL, blocks)
	if err != nil {
		return err
	}
	stmts = append(stmts, blockStmts...)
	stmts = append(stmts, statement{table: "block times", sql: blockTimesSQL, args: []any{lo, hi + 1}})

	topUp, err := w.parts.needsTopUp(ctx, hi)
	if err != nil {
		return err
	}
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("flush %d..%d: begin: %w", lo, hi, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	b := &pgx.Batch{}
	if topUp {
		b.Queue(topUpSQL, hi, hi+PartitionTopUp)
	}
	for _, st := range stmts {
		b.Queue(st.sql, st.args...)
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("flush %d..%d: %w", lo, hi, err)
	}
	for _, hook := range w.hooks {
		if err := hook(ctx, tx, &all); err != nil {
			return fmt.Errorf("flush %d..%d: post-flush: %w", lo, hi, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("flush %d..%d: commit: %w", lo, hi, err)
	}
	if topUp {
		w.parts.toppedUp(hi)
	}
	return nil
}

// RecordFailure records a height the pipeline gave up on in index_failures
// under source, replacing an earlier record of the same height and source.
func (w *BatchWriter) RecordFailure(ctx context.Context, height int64, source string, attempts int, cause error) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("record failure %d: begin: %w", height, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := upsertFailure(ctx, tx, height, source, attempts, cause); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("record failure %d: commit: %w", height, err)
	}
	return nil
}

// AdvanceCursor moves indexer_state.last_indexed_height up to height (never
// down): the catch-up job hands the live indexer back a cursor past the
// heights it repaired.
func (w *BatchWriter) AdvanceCursor(ctx context.Context, height int64) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("advance cursor to %d: begin: %w", height, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := advanceCursor(ctx, tx, height); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("advance cursor to %d: commit: %w", height, err)
	}
	return nil
}
