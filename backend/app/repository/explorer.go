// Package repository is the read side of the explorer tables: raw
// parameterised SQL over pgx, no business logic. Lists are keyset queries
// that carry a height predicate whenever a cursor is given, so PostgreSQL
// prunes the partitions above it. A missing row is ErrNotFound.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNotFound is returned when the block, transaction or row asked for is not
// in the explorer tables.
var ErrNotFound = errors.New("not found")

// DB is what the repository needs from a connection pool; *pgxpool.Pool
// implements it.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Explorer reads the explorer schema.
type Explorer struct {
	db DB
}

// NewExplorer returns a repository over db.
func NewExplorer(db DB) *Explorer {
	return &Explorer{db: db}
}

// BlockSummary is a row of the block list.
type BlockSummary struct {
	Height              int64
	Time                time.Time
	Hash                string
	TxCount             int64
	TxFailedCount       int64
	ProposerConsAddress *string
	BlockTimeMs         *int64
	SizeBytes           *int64
}

// Block is every column of a blocks row plus the block's transactions.
// Numeric columns come back as their decimal text.
type Block struct {
	BlockSummary
	Minted             *string
	Inflation          *string
	FeesDistributed    json.RawMessage
	SignaturesCount    *int64
	SignaturesPowerPct *string
	Transactions       []BlockTx
}

// BlockTx is a transaction as the block page lists it.
type BlockTx struct {
	TxIndex  int64
	Hash     string
	Success  bool
	MsgTypes []string
	Fee      json.RawMessage
	Signer   *string // first signer
}

// TxSummary is a row of the transaction list.
type TxSummary struct {
	Height   int64
	TxIndex  int64
	Hash     string
	Time     time.Time
	Success  bool
	MsgCount int64
	MsgTypes []string
	Fee      json.RawMessage
	Signer   *string // first signer
}

// Tx is every column of a transactions row plus its messages.
type Tx struct {
	TxSummary
	Code      int64
	Codespace *string
	ErrorLog  *string
	GasWanted *int64
	GasUsed   *int64
	FeePayer  *string
	Signers   []string
	Memo      *string
	Messages  []Message
}

// Message is a messages row.
type Message struct {
	MsgIndex int64
	TypeURL  string
	Sender   *string
	Module   *string
	Body     json.RawMessage // NULL when the message could not be decoded
	Events   json.RawMessage
}

// TxKey is the position of a transaction, the key of the transaction list.
type TxKey struct {
	Height  int64
	TxIndex int64
}

const blockSummaryCols = `height, time, hash, tx_count, tx_failed_count, proposer_cons_address, block_time_ms, size_bytes`

// Blocks lists up to limit blocks by height descending, below before when it
// is set.
func (r *Explorer) Blocks(ctx context.Context, before *int64, limit int) ([]BlockSummary, error) {
	q := `SELECT ` + blockSummaryCols + ` FROM explorer.blocks ORDER BY height DESC LIMIT $1`
	args := []any{limit}
	if before != nil {
		q = `SELECT ` + blockSummaryCols + ` FROM explorer.blocks WHERE height < $1 ORDER BY height DESC LIMIT $2`
		args = []any{*before, limit}
	}
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list blocks: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (BlockSummary, error) {
		var b BlockSummary
		err := row.Scan(&b.Height, &b.Time, &b.Hash, &b.TxCount, &b.TxFailedCount,
			&b.ProposerConsAddress, &b.BlockTimeMs, &b.SizeBytes)
		return b, err
	})
}

// Block returns the block at height with its transactions in index order.
func (r *Explorer) Block(ctx context.Context, height int64) (*Block, error) {
	var b Block
	err := r.db.QueryRow(ctx, `SELECT `+blockSummaryCols+`, minted::text, inflation::text, fees_distributed,
		signatures_count, signatures_power_pct::text
		FROM explorer.blocks WHERE height = $1`, height).Scan(
		&b.Height, &b.Time, &b.Hash, &b.TxCount, &b.TxFailedCount, &b.ProposerConsAddress, &b.BlockTimeMs,
		&b.SizeBytes, &b.Minted, &b.Inflation, &b.FeesDistributed, &b.SignaturesCount, &b.SignaturesPowerPct)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("block %d: %w", height, err)
	}

	rows, err := r.db.Query(ctx, `SELECT tx_index, hash, success, msg_types, fee, signers[1]
		FROM explorer.transactions WHERE height = $1 ORDER BY tx_index`, height)
	if err != nil {
		return nil, fmt.Errorf("block %d transactions: %w", height, err)
	}
	b.Transactions, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (BlockTx, error) {
		var t BlockTx
		err := row.Scan(&t.TxIndex, &t.Hash, &t.Success, &t.MsgTypes, &t.Fee, &t.Signer)
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("block %d transactions: %w", height, err)
	}
	return &b, nil
}

// Txs lists up to limit transactions by height and index descending, below
// before when it is set, only the successful (or only the failed) ones when
// success is set.
func (r *Explorer) Txs(ctx context.Context, before *TxKey, success *bool, limit int) ([]TxSummary, error) {
	q := `SELECT height, tx_index, hash, time, success, msg_count, msg_types, fee, signers[1]
		FROM explorer.transactions WHERE true`
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if before != nil {
		// The plain height predicate is what prunes the partitions; the row
		// comparison alone does not.
		h, i := arg(before.Height), arg(before.TxIndex)
		q += ` AND height <= ` + h + ` AND (height, tx_index) < (` + h + `, ` + i + `)`
	}
	if success != nil {
		q += ` AND success = ` + arg(*success)
	}
	q += ` ORDER BY height DESC, tx_index DESC LIMIT ` + arg(limit)

	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (TxSummary, error) {
		var t TxSummary
		err := row.Scan(&t.Height, &t.TxIndex, &t.Hash, &t.Time, &t.Success, &t.MsgCount, &t.MsgTypes, &t.Fee, &t.Signer)
		return t, err
	})
}

// Tx returns the transaction with hash (upper-case hex) and its messages in
// order.
func (r *Explorer) Tx(ctx context.Context, hash string) (*Tx, error) {
	var t Tx
	err := r.db.QueryRow(ctx, `SELECT height, tx_index, hash, time, success, msg_count, msg_types, fee, signers[1],
		code, codespace, error_log, gas_wanted, gas_used, fee_payer, signers, memo
		FROM explorer.transactions WHERE hash = $1 ORDER BY height LIMIT 1`, hash).Scan(
		&t.Height, &t.TxIndex, &t.Hash, &t.Time, &t.Success, &t.MsgCount, &t.MsgTypes, &t.Fee, &t.Signer,
		&t.Code, &t.Codespace, &t.ErrorLog, &t.GasWanted, &t.GasUsed, &t.FeePayer, &t.Signers, &t.Memo)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("transaction %s: %w", hash, err)
	}

	rows, err := r.db.Query(ctx, `SELECT msg_index, type_url, sender, module, body, events
		FROM explorer.messages WHERE height = $1 AND tx_index = $2 ORDER BY msg_index`, t.Height, t.TxIndex)
	if err != nil {
		return nil, fmt.Errorf("transaction %s messages: %w", hash, err)
	}
	t.Messages, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Message, error) {
		var m Message
		err := row.Scan(&m.MsgIndex, &m.TypeURL, &m.Sender, &m.Module, &m.Body, &m.Events)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("transaction %s messages: %w", hash, err)
	}
	return &t, nil
}

// BlockExists reports whether the block at height is indexed.
func (r *Explorer) BlockExists(ctx context.Context, height int64) (bool, error) {
	return r.exists(ctx, `SELECT EXISTS (SELECT 1 FROM explorer.blocks WHERE height = $1)`, height)
}

// TxExists reports whether the transaction with hash is indexed.
func (r *Explorer) TxExists(ctx context.Context, hash string) (bool, error) {
	return r.exists(ctx, `SELECT EXISTS (SELECT 1 FROM explorer.transactions WHERE hash = $1)`, hash)
}

// AccountIndexed reports whether address has a row in accounts.
func (r *Explorer) AccountIndexed(ctx context.Context, address string) (bool, error) {
	return r.exists(ctx, `SELECT EXISTS (SELECT 1 FROM explorer.accounts WHERE address = $1)`, address)
}

// ValidatorMoniker returns the moniker of the validator with operator
// address, or ErrNotFound.
func (r *Explorer) ValidatorMoniker(ctx context.Context, operator string) (string, error) {
	var moniker string
	err := r.db.QueryRow(ctx, `SELECT moniker FROM explorer.validators WHERE operator_address = $1`, operator).Scan(&moniker)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("validator %s: %w", operator, err)
	}
	return moniker, nil
}

func (r *Explorer) exists(ctx context.Context, q string, arg any) (bool, error) {
	var ok bool
	if err := r.db.QueryRow(ctx, q, arg).Scan(&ok); err != nil {
		return false, fmt.Errorf("exists: %w", err)
	}
	return ok, nil
}
