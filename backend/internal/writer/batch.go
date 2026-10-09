package writer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// index_failures.source of the backfill pipeline and of the reindex command.
const (
	FailureSourceBackfill = "backfill"
	FailureSourceReindex  = "reindex"
)

// TxKey and MsgKey are the natural keys of a transactions and a messages
// row.
type (
	TxKey struct {
		Height  int64
		TxIndex int
	}
	MsgKey struct {
		Height            int64
		TxIndex, MsgIndex int
	}
)

// Inserted holds the keys of the rows a flush actually inserted, as opposed
// to rows that were already there (kept in ModeInsert, overwritten in
// ModeUpdate). Counters and totals move for these rows only, so writing a
// height twice never counts it twice.
type Inserted struct {
	Blocks       map[int64]bool
	Transactions map[TxKey]bool
	Messages     map[MsgKey]bool
}

func newInserted() Inserted {
	return Inserted{Blocks: map[int64]bool{}, Transactions: map[TxKey]bool{}, Messages: map[MsgKey]bool{}}
}

// Flush is what a PostFlush hook sees: every entity of the flush, the mode
// it was written in and the rows it inserted.
type Flush struct {
	Entities *transform.Entities
	Mode     Mode
	Inserted Inserted
}

// PostFlush runs inside a flush's transaction after its rows are written.
// An error rolls the whole flush back.
type PostFlush func(ctx context.Context, tx pgx.Tx, f *Flush) error

// BatchWriter writes the flushes of the backfill pipeline (the backfill, the
// catch-up and the reindex): the entities of several heights in one
// transaction, sent as one pgx batch of multi-row statements. It
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

// blockRow is the JSON shape of the blocks bulk write.
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
	Signers             []string        `json:"signers"`
}

// blockTimesSQL sets block_time_ms of the rows in [$1, $2] whose previous
// block is indexed, where it is missing or (after a reindex changed a time)
// wrong. The backfill walks downward, so a block's predecessor usually
// arrives in a later flush: $2 is one above the flush's highest height, which
// completes the block written by the previous flush (and the live floor's
// block on the first one).
const blockTimesSQL = `UPDATE explorer.blocks b
		SET block_time_ms = (EXTRACT(EPOCH FROM (b.time - p.time)) * 1000)::integer
		FROM explorer.blocks p
		WHERE b.height BETWEEN $1 AND $2 AND p.height = b.height - 1
		  AND b.block_time_ms IS DISTINCT FROM (EXTRACT(EPOCH FROM (b.time - p.time)) * 1000)::integer`

// resolveFailuresSQL marks the open failures of the heights a reindex
// wrote as resolved.
const resolveFailuresSQL = `UPDATE explorer.index_failures SET resolved_at = now()
		WHERE height = ANY($1::bigint[]) AND resolved_at IS NULL`

// Write writes the entities of every height in batch in one transaction: the
// transactions, messages, validator_events, transfers, block_events, token_events and
// blocks rows, block_time_ms where the previous
// block is known, the accounts of the signers (counters moved for the
// transactions inserted, deltas merged per address and applied in address
// order), then the PostFlush hooks. Partitions are topped up first when a
// height enters the last one. A deadlock retries the flush once.
//
// In ModeInsert existing rows are kept, so writing a height again changes
// nothing. In ModeUpdate existing rows that differ are overwritten, rows the
// height no longer produces are left alone, and the open index_failures of
// the heights are resolved.
func (w *BatchWriter) Write(ctx context.Context, batch []*transform.Entities, mode Mode) error {
	if mode != ModeInsert && mode != ModeUpdate {
		return fmt.Errorf("write: unsupported %s", mode)
	}
	var all transform.Entities
	for _, e := range batch {
		if e == nil {
			continue
		}
		all.Blocks = append(all.Blocks, e.Blocks...)
		all.Transactions = append(all.Transactions, e.Transactions...)
		all.Messages = append(all.Messages, e.Messages...)
		all.ValidatorEvents = append(all.ValidatorEvents, e.ValidatorEvents...)
		all.Transfers = append(all.Transfers, e.Transfers...)
		all.BlockEvents = append(all.BlockEvents, e.BlockEvents...)
		all.TokenEvents = append(all.TokenEvents, e.TokenEvents...)
		all.Proposals = append(all.Proposals, e.Proposals...)
		all.ProposalDeposits = append(all.ProposalDeposits, e.ProposalDeposits...)
		all.ProposalVotes = append(all.ProposalVotes, e.ProposalVotes...)
		all.ProposalStatuses = append(all.ProposalStatuses, e.ProposalStatuses...)
	}
	if len(all.Blocks) == 0 {
		return nil
	}
	lo, hi := all.Blocks[0].Height, all.Blocks[0].Height
	for _, b := range all.Blocks[1:] {
		lo, hi = min(lo, b.Height), max(hi, b.Height)
	}

	stmts, err := rowStatements(hi, &all, mode)
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
			SignaturesCount: b.SignaturesCount, Signers: nonNil(b.Signers),
		})
	}
	blockStmts, err := chunked(hi, "blocks", blocksTable, mode, blocks, scanBlockKeys)
	if err != nil {
		return err
	}
	stmts = append(stmts, blockStmts...)
	stmts = append(stmts, statement{table: "block times", sql: blockTimesSQL, args: []any{lo, hi + 1}})
	if mode == ModeUpdate {
		heights := make([]int64, len(all.Blocks))
		for i, b := range all.Blocks {
			heights[i] = b.Height
		}
		stmts = append(stmts, statement{table: "failures", sql: resolveFailuresSQL, args: []any{heights}})
	}

	topUp, err := w.parts.needsTopUp(ctx, hi)
	if err != nil {
		return err
	}
	if err := withDeadlockRetry(ctx, func(ctx context.Context) error {
		return w.write(ctx, &all, mode, stmts, lo, hi, topUp)
	}); err != nil {
		return err
	}
	if topUp {
		w.parts.toppedUp(hi)
	}
	return nil
}

// write is one attempt of Write.
func (w *BatchWriter) write(ctx context.Context, all *transform.Entities, mode Mode, stmts []statement,
	lo, hi int64, topUp bool) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("flush %d..%d: begin: %w", lo, hi, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	flushed := &Flush{Entities: all, Mode: mode, Inserted: newInserted()}
	b := &pgx.Batch{}
	if topUp {
		b.Queue(topUpSQL, hi, hi+PartitionTopUp)
	}
	for _, st := range stmts {
		q := b.Queue(st.sql, st.args...)
		if st.inserted != nil {
			q.Query(func(rows pgx.Rows) error {
				if err := st.inserted(rows, &flushed.Inserted); err != nil {
					return fmt.Errorf("%s: %w", st.table, err)
				}
				return nil
			})
		}
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("flush %d..%d: %w", lo, hi, err)
	}
	// The counters need the keys the batch inserted.
	accounts, err := accountStatements(hi, all, flushed.Inserted.Transactions)
	if err != nil {
		return err
	}
	if err := execAll(ctx, tx, hi, accounts); err != nil {
		return err
	}
	for _, hook := range w.hooks {
		if err := hook(ctx, tx, flushed); err != nil {
			return fmt.Errorf("flush %d..%d: post-flush: %w", lo, hi, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("flush %d..%d: commit: %w", lo, hi, err)
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
