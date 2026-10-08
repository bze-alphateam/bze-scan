// Package writer persists the transformer's entities. A write is one
// database transaction, so a height is either fully written or absent, and
// every statement is idempotent: writing the same height twice, or two
// processes overlapping during a blue/green release, changes nothing.
package writer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/migrations"
)

// indexer_state keys.
const (
	KeyLiveFloor         = "live_floor"
	KeyLastIndexedHeight = "last_indexed_height"
)

// FailureSourceLive is index_failures.source for the live indexer.
const FailureSourceLive = "live"

// PartitionTopUp is how far above a height the writer creates partitions
// when the height enters the last existing partition.
const PartitionTopUp int64 = 20_000_000

// DB is the part of a PostgreSQL connection pool the writers use;
// *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// LiveWriter writes the live indexer's blocks, one height per transaction.
// Give it a pool of its own: the live path must not wait on other writers.
type LiveWriter struct {
	db    DB
	parts *partitions
}

// NewLiveWriter returns a live writer over db.
func NewLiveWriter(db DB) *LiveWriter {
	return &LiveWriter{db: db, parts: newPartitions(db)}
}

// Cursor returns indexer_state.last_indexed_height, and false when the live
// indexer has never written.
func (w *LiveWriter) Cursor(ctx context.Context) (int64, bool, error) {
	return w.stateHeight(ctx, KeyLastIndexedHeight)
}

// LiveFloor returns indexer_state.live_floor, and false when it is not set.
func (w *LiveWriter) LiveFloor(ctx context.Context) (int64, bool, error) {
	return w.stateHeight(ctx, KeyLiveFloor)
}

func (w *LiveWriter) stateHeight(ctx context.Context, key string) (int64, bool, error) {
	var v string
	err := w.db.QueryRow(ctx, `SELECT value FROM explorer.indexer_state WHERE key = $1`, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read %s: %w", key, err)
	}
	h, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("read %s: %q is not a height", key, v)
	}
	return h, true, nil
}

// WriteBlock writes the entities of one or more heights in one transaction:
// the transactions and messages rows, the blocks rows (block_time_ms from the
// previous row when it exists), the live floor at the first write ever, and
// the cursor moved to the highest height. Partitions are topped up first when
// a height enters the last one. A blocks row therefore proves its height is
// complete.
func (w *LiveWriter) WriteBlock(ctx context.Context, ents *transform.Entities) error {
	if ents == nil || len(ents.Blocks) == 0 {
		return nil
	}
	top := ents.Blocks[0].Height
	for _, b := range ents.Blocks[1:] {
		top = max(top, b.Height)
	}

	topUp, err := w.parts.needsTopUp(ctx, top)
	if err != nil {
		return err
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("write %d: begin: %w", top, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if topUp {
		if _, err := tx.Exec(ctx, topUpSQL, top, top+PartitionTopUp); err != nil {
			return fmt.Errorf("write %d: top up partitions: %w", top, err)
		}
	}
	rows, err := rowStatements(top, ents, ModeInsert)
	if err != nil {
		return err
	}
	for _, st := range rows {
		if _, err := tx.Exec(ctx, st.sql, st.args...); err != nil {
			return fmt.Errorf("write %d: %s: %w", top, st.table, err)
		}
	}
	for i := range ents.Blocks {
		if err := insertBlock(ctx, tx, &ents.Blocks[i]); err != nil {
			return err
		}
	}
	first := ents.Blocks[0].Height
	for _, b := range ents.Blocks[1:] {
		first = min(first, b.Height)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO explorer.indexer_state (key, value, updated_at)
		VALUES ($1, $2, now()) ON CONFLICT (key) DO NOTHING`,
		KeyLiveFloor, strconv.FormatInt(first, 10)); err != nil {
		return fmt.Errorf("write %d: live floor: %w", top, err)
	}
	if err := advanceCursor(ctx, tx, top); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("write %d: commit: %w", top, err)
	}

	if topUp {
		w.parts.toppedUp(top)
	}
	return nil
}

// RecordFailure records a height the live indexer gave up on in
// index_failures (replacing an earlier record of the same height) and moves
// the cursor past it, in one transaction.
func (w *LiveWriter) RecordFailure(ctx context.Context, height int64, attempts int, cause error) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("record failure %d: begin: %w", height, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := upsertFailure(ctx, tx, height, FailureSourceLive, attempts, cause); err != nil {
		return err
	}
	if err := advanceCursor(ctx, tx, height); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("record failure %d: commit: %w", height, err)
	}
	return nil
}

func upsertFailure(ctx context.Context, tx pgx.Tx, height int64, source string, attempts int, cause error) error {
	if _, err := tx.Exec(ctx, `INSERT INTO explorer.index_failures (height, source, attempts, error, failed_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (height, source) DO UPDATE
		SET attempts = EXCLUDED.attempts, error = EXCLUDED.error, failed_at = now(), resolved_at = NULL`,
		height, source, attempts, cause.Error()); err != nil {
		return fmt.Errorf("record failure %d: %w", height, err)
	}
	return nil
}

func insertBlock(ctx context.Context, tx pgx.Tx, b *transform.Block) error {
	fees, err := b.FeesDistributedJSON()
	if err != nil {
		return fmt.Errorf("write %d: fees_distributed: %w", b.Height, err)
	}
	// block_time_ms: header time minus the previous block's, when that block
	// is already indexed. ON CONFLICT DO NOTHING keeps a rewrite a no-op.
	_, err = tx.Exec(ctx, `INSERT INTO explorer.blocks (
			height, time, tx_count, tx_failed_count, block_time_ms, hash,
			proposer_cons_address, size_bytes, minted, inflation, fees_distributed,
			signatures_count, signatures_power_pct)
		VALUES ($1, $2, $3, $4,
			(SELECT (EXTRACT(EPOCH FROM ($2::timestamptz - p.time)) * 1000)::integer
			   FROM explorer.blocks p WHERE p.height = $1::bigint - 1),
			$5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (height) DO NOTHING`,
		b.Height, b.Time, b.TxCount, b.TxFailedCount, b.Hash,
		nullIfEmpty(b.ProposerConsAddress), b.SizeBytes, b.Minted, b.Inflation, fees,
		b.SignaturesCount, b.SignaturesPowerPct)
	if err != nil {
		return fmt.Errorf("write %d: blocks: %w", b.Height, err)
	}
	return nil
}

// txRow and msgRow are the JSON shape the bulk inserts expand with
// jsonb_to_recordset: one parameter per table whatever the row count, and
// array columns that unnest could not take.
type txRow struct {
	Height    int64           `json:"height"`
	TxIndex   int             `json:"tx_index"`
	Hash      string          `json:"hash"`
	Time      time.Time       `json:"time"`
	Success   bool            `json:"success"`
	Code      uint32          `json:"code"`
	Codespace *string         `json:"codespace"`
	ErrorLog  *string         `json:"error_log"`
	GasWanted int64           `json:"gas_wanted"`
	GasUsed   int64           `json:"gas_used"`
	Fee       json.RawMessage `json:"fee"`
	FeePayer  *string         `json:"fee_payer"`
	Signers   []string        `json:"signers"`
	Memo      *string         `json:"memo"`
	MsgCount  int             `json:"msg_count"`
	MsgTypes  []string        `json:"msg_types"`
}

type msgRow struct {
	Height   int64           `json:"height"`
	TxIndex  int             `json:"tx_index"`
	MsgIndex int             `json:"msg_index"`
	TypeURL  string          `json:"type_url"`
	Sender   *string         `json:"sender"`
	Module   *string         `json:"module"`
	Events   json.RawMessage `json:"events"`
	Body     json.RawMessage `json:"body"`
}

// ChunkRows bounds the rows of one multi-row statement.
const ChunkRows = 1000

const topUpSQL = `SELECT explorer.ensure_partitions($1, $2)`

// statement is one bulk statement of up to ChunkRows rows. inserted, when
// set, reads the keys an INSERT returned into the flush's Inserted.
type statement struct {
	table    string
	sql      string
	args     []any
	inserted func(rows pgx.Rows, into *Inserted) error
}

// rowStatements returns the bulk writes of the transactions and messages of
// ents, in chunks of ChunkRows rows. top names the write in errors.
func rowStatements(top int64, ents *transform.Entities, mode Mode) ([]statement, error) {
	txRows := make([]txRow, 0, len(ents.Transactions))
	for _, t := range ents.Transactions {
		fee, err := json.Marshal(nonNil(t.Fee))
		if err != nil {
			return nil, fmt.Errorf("write %d: transactions: fee: %w", top, err)
		}
		txRows = append(txRows, txRow{
			Height: t.Height, TxIndex: t.TxIndex, Hash: t.Hash, Time: t.Time, Success: t.Success,
			Code: t.Code, Codespace: nullIfEmpty(t.Codespace), ErrorLog: nullIfEmpty(t.ErrorLog),
			GasWanted: t.GasWanted, GasUsed: t.GasUsed, Fee: fee, FeePayer: nullIfEmpty(t.FeePayer),
			Signers: nonNil(t.Signers), Memo: nullIfEmpty(t.Memo), MsgCount: t.MsgCount, MsgTypes: nonNil(t.MsgTypes),
		})
	}
	msgRows := make([]msgRow, 0, len(ents.Messages))
	for _, m := range ents.Messages {
		events, err := json.Marshal(nonNil(m.Events))
		if err != nil {
			return nil, fmt.Errorf("write %d: messages: events: %w", top, err)
		}
		msgRows = append(msgRows, msgRow{
			Height: m.Height, TxIndex: m.TxIndex, MsgIndex: m.MsgIndex, TypeURL: m.TypeURL,
			Sender: nullIfEmpty(m.Sender), Module: nullIfEmpty(m.Module), Events: events, Body: m.Body,
		})
	}
	txs, err := chunked(top, "transactions", transactionsTable, mode, txRows, scanTransactionKeys)
	if err != nil {
		return nil, err
	}
	msgs, err := chunked(top, "messages", messagesTable, mode, msgRows, scanMessageKeys)
	if err != nil {
		return nil, err
	}
	return append(txs, msgs...), nil
}

// chunked splits rows into statements of tbl with one JSON parameter each:
// per chunk, in ModeUpdate the update of the rows that differ, then the
// insert of the missing ones, whose returned keys scan reads.
func chunked[T any](top int64, label string, tbl table, mode Mode, rows []T,
	scan func(pgx.Rows, *Inserted) error) ([]statement, error) {
	var out []statement
	for lo := 0; lo < len(rows); lo += ChunkRows {
		payload, err := json.Marshal(rows[lo:min(lo+ChunkRows, len(rows))])
		if err != nil {
			return nil, fmt.Errorf("write %d: %s: %w", top, label, err)
		}
		if mode == ModeUpdate {
			out = append(out, statement{table: label + " update", sql: tbl.updateSQL(), args: []any{payload}})
		}
		out = append(out, statement{table: label, sql: tbl.insertSQL(), args: []any{payload}, inserted: scan})
	}
	return out, nil
}

func scanBlockKeys(rows pgx.Rows, into *Inserted) error {
	for rows.Next() {
		var h int64
		if err := rows.Scan(&h); err != nil {
			return err
		}
		into.Blocks[h] = true
	}
	return rows.Err()
}

func scanTransactionKeys(rows pgx.Rows, into *Inserted) error {
	for rows.Next() {
		var k TxKey
		if err := rows.Scan(&k.Height, &k.TxIndex); err != nil {
			return err
		}
		into.Transactions[k] = true
	}
	return rows.Err()
}

func scanMessageKeys(rows pgx.Rows, into *Inserted) error {
	for rows.Next() {
		var k MsgKey
		if err := rows.Scan(&k.Height, &k.TxIndex, &k.MsgIndex); err != nil {
			return err
		}
		into.Messages[k] = true
	}
	return rows.Err()
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func advanceCursor(ctx context.Context, tx pgx.Tx, height int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO explorer.indexer_state (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE
		SET value = GREATEST(explorer.indexer_state.value::bigint, EXCLUDED.value::bigint)::text,
		    updated_at = now()`,
		KeyLastIndexedHeight, strconv.FormatInt(height, 10))
	if err != nil {
		return fmt.Errorf("advance cursor to %d: %w", height, err)
	}
	return nil
}

// partitions tracks the highest existing blocks partition of one writer.
type partitions struct {
	db DB

	mu sync.Mutex
	// last is the lower bound of the highest existing blocks partition; -1
	// until read from the catalog.
	last int64
}

func newPartitions(db DB) *partitions {
	return &partitions{db: db, last: -1}
}

// needsTopUp reports whether height lies in (or above) the last existing
// blocks partition. The partition list is read from the catalog once and
// then tracked in memory.
func (p *partitions) needsTopUp(ctx context.Context, height int64) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last < 0 {
		var top *int64
		err := p.db.QueryRow(ctx, `SELECT max(substring(c.relname FROM '^blocks_p([0-9]+)$')::bigint)
			FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
			WHERE i.inhparent = 'explorer.blocks'::regclass`).Scan(&top)
		if err != nil {
			return false, fmt.Errorf("read partitions: %w", err)
		}
		if top == nil {
			p.last = 0
			return true, nil
		}
		p.last = *top * migrations.PartitionSize
	}
	return migrations.PartitionLower(height) >= p.last, nil
}

// toppedUp records a committed top-up above height.
func (p *partitions) toppedUp(height int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = max(p.last, migrations.PartitionLower(height+PartitionTopUp))
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
