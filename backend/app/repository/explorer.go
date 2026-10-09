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
	// Proposer is the validator with the proposer's consensus address, nil
	// when none is synced (before the first state sync, for example).
	Proposer *Proposer
}

// Proposer names the validator that proposed a block.
type Proposer struct {
	OperatorAddress string
	Moniker         string
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

// blockSummaryCols are the block list columns of explorer.blocks b, the
// proposer's from explorer.validators p last; blockSummaryFrom joins them.
const (
	blockSummaryCols = `b.height, b.time, b.hash, b.tx_count, b.tx_failed_count, b.proposer_cons_address,
		b.block_time_ms, b.size_bytes, p.operator_address, p.moniker`
	blockSummaryFrom = `explorer.blocks b LEFT JOIN LATERAL (
		SELECT v.operator_address, v.moniker FROM explorer.validators v
		 WHERE v.consensus_address = b.proposer_cons_address ORDER BY v.operator_address LIMIT 1) p ON true`
)

// blockSummaryDest is what blockSummaryCols scan into; finish sets the
// proposer once scanned.
func blockSummaryDest(b *BlockSummary) (dest []any, finish func()) {
	var op, moniker *string
	dest = []any{&b.Height, &b.Time, &b.Hash, &b.TxCount, &b.TxFailedCount, &b.ProposerConsAddress,
		&b.BlockTimeMs, &b.SizeBytes, &op, &moniker}
	finish = func() {
		if op != nil && moniker != nil {
			b.Proposer = &Proposer{OperatorAddress: *op, Moniker: *moniker}
		}
	}
	return dest, finish
}

func collectBlockSummaries(rows pgx.Rows) ([]BlockSummary, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (BlockSummary, error) {
		var b BlockSummary
		dest, finish := blockSummaryDest(&b)
		err := row.Scan(dest...)
		finish()
		return b, err
	})
}

// Blocks lists up to limit blocks by height descending, below before when it
// is set.
func (r *Explorer) Blocks(ctx context.Context, before *int64, limit int) ([]BlockSummary, error) {
	q := `SELECT ` + blockSummaryCols + ` FROM ` + blockSummaryFrom + ` ORDER BY b.height DESC LIMIT $1`
	args := []any{limit}
	if before != nil {
		q = `SELECT ` + blockSummaryCols + ` FROM ` + blockSummaryFrom + ` WHERE b.height < $1 ORDER BY b.height DESC LIMIT $2`
		args = []any{*before, limit}
	}
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list blocks: %w", err)
	}
	return collectBlockSummaries(rows)
}

// Block returns the block at height with its transactions in index order.
func (r *Explorer) Block(ctx context.Context, height int64) (*Block, error) {
	var b Block
	dest, finish := blockSummaryDest(&b.BlockSummary)
	err := r.db.QueryRow(ctx, `SELECT `+blockSummaryCols+`, b.minted::text, b.inflation::text, b.fees_distributed,
		b.signatures_count, b.signatures_power_pct::text
		FROM `+blockSummaryFrom+` WHERE b.height = $1`, height).Scan(
		append(dest, &b.Minted, &b.Inflation, &b.FeesDistributed, &b.SignaturesCount, &b.SignaturesPowerPct)...)
	finish()
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

// TxPosition returns the height of the transaction with hash (upper-case
// hex) and its index in the block, or ErrNotFound.
func (r *Explorer) TxPosition(ctx context.Context, hash string) (int64, int64, error) {
	var height, index int64
	err := r.db.QueryRow(ctx, `SELECT height, tx_index FROM explorer.transactions
		WHERE hash = $1 ORDER BY height LIMIT 1`, hash).Scan(&height, &index)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, ErrNotFound
	}
	if err != nil {
		return 0, 0, fmt.Errorf("transaction %s position: %w", hash, err)
	}
	return height, index, nil
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

// ValidatorSummary is a row of the validator list. Numeric columns come back
// as their decimal text.
type ValidatorSummary struct {
	// Position is the row's place in the list order, the cursor key.
	Position           int64
	Rank               *int64
	Moniker            string
	OperatorAddress    string
	Tokens             string
	VotingPowerPct     *string
	CommissionRate     string
	MissedBlocks       *int64
	SignedBlocksWindow *int64
	Jailed             bool
	Status             string
}

// Validators lists the validators of status (every status when nil) in
// rank order: the bonded ones by rank, then the others by tokens. after is
// the position of the last row of the previous page (0 for the first). The
// table holds tens of rows, so the position is the keyset.
func (r *Explorer) Validators(ctx context.Context, status *string, after int64, limit int) ([]ValidatorSummary, error) {
	rows, err := r.db.Query(ctx, `
		WITH v AS (
			SELECT row_number() OVER (ORDER BY rank NULLS LAST,
			         CASE status WHEN 'bonded' THEN 0 WHEN 'unbonding' THEN 1 ELSE 2 END,
			         tokens DESC, operator_address) AS pos,
			       rank, moniker, operator_address, tokens::text, voting_power_pct::text, commission_rate::text,
			       missed_blocks, signed_blocks_window, jailed, status
			  FROM explorer.validators
			 WHERE $1::text IS NULL OR status = $1)
		SELECT * FROM v WHERE pos > $2 ORDER BY pos LIMIT $3`, status, after, limit)
	if err != nil {
		return nil, fmt.Errorf("validators: %w", err)
	}
	defer rows.Close()
	var out []ValidatorSummary
	for rows.Next() {
		var v ValidatorSummary
		if err := rows.Scan(&v.Position, &v.Rank, &v.Moniker, &v.OperatorAddress, &v.Tokens, &v.VotingPowerPct,
			&v.CommissionRate, &v.MissedBlocks, &v.SignedBlocksWindow, &v.Jailed, &v.Status); err != nil {
			return nil, fmt.Errorf("validators: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ValidatorDetail is every column of a validators row. Numeric columns come
// back as their decimal text.
type ValidatorDetail struct {
	ValidatorSummary
	AccountAddress          string
	ConsensusAddress        *string
	ConsensusPubkey         *string
	Identity                *string
	Website                 *string
	SecurityContact         *string
	Details                 *string
	Tombstoned              bool
	JailedUntil             *time.Time
	DelegatorShares         string
	CommissionMaxRate       string
	CommissionMaxChangeRate string
	CommissionUpdateTime    *time.Time
	MinSelfDelegation       *string
	SelfDelegation          *string
	DelegatorCount          *int64
	FirstSeenHeight         *int64
	FirstSeenTime           *time.Time
	UpdatedAt               time.Time
}

// ValidatorEvent is a validator_events row.
type ValidatorEvent struct {
	Height  int64
	TxIndex int64
	Seq     int64
	Kind    string
	Details json.RawMessage
	Time    time.Time
	// TxHash is the hash of the transaction, nil for a block-level event.
	TxHash *string
}

// ValidatorVote is a governance vote of a validator's owner account.
type ValidatorVote struct {
	ProposalID int64
	Title      *string // nil until the proposal is synced
	Option     *string
	Options    json.RawMessage
	Height     int64
	Time       time.Time
}

// Validator returns the validator with operator address, or ErrNotFound.
func (r *Explorer) Validator(ctx context.Context, operator string) (*ValidatorDetail, error) {
	var v ValidatorDetail
	v.OperatorAddress = operator
	err := r.db.QueryRow(ctx, `SELECT rank, moniker, tokens::text, voting_power_pct::text, commission_rate::text,
			missed_blocks, signed_blocks_window, jailed, status,
			account_address, consensus_address, consensus_pubkey, identity, website, security_contact, details,
			tombstoned, jailed_until, delegator_shares::text, commission_max_rate::text,
			commission_max_change_rate::text, commission_update_time, min_self_delegation::text,
			self_delegation::text, delegator_count, first_seen_height, first_seen_time, updated_at
		FROM explorer.validators WHERE operator_address = $1`, operator).Scan(
		&v.Rank, &v.Moniker, &v.Tokens, &v.VotingPowerPct, &v.CommissionRate,
		&v.MissedBlocks, &v.SignedBlocksWindow, &v.Jailed, &v.Status,
		&v.AccountAddress, &v.ConsensusAddress, &v.ConsensusPubkey, &v.Identity, &v.Website, &v.SecurityContact,
		&v.Details, &v.Tombstoned, &v.JailedUntil, &v.DelegatorShares, &v.CommissionMaxRate,
		&v.CommissionMaxChangeRate, &v.CommissionUpdateTime, &v.MinSelfDelegation,
		&v.SelfDelegation, &v.DelegatorCount, &v.FirstSeenHeight, &v.FirstSeenTime, &v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("validator %s: %w", operator, err)
	}
	return &v, nil
}

// ValidatorBlocks lists up to limit blocks proposed by the validator with
// consensus address cons (upper-case hex) by height descending, below before
// when it is set.
func (r *Explorer) ValidatorBlocks(ctx context.Context, cons string, before *int64, limit int) ([]BlockSummary, error) {
	q := `SELECT ` + blockSummaryCols + ` FROM ` + blockSummaryFrom +
		` WHERE b.proposer_cons_address = $1 ORDER BY b.height DESC LIMIT $2`
	args := []any{cons, limit}
	if before != nil {
		q = `SELECT ` + blockSummaryCols + ` FROM ` + blockSummaryFrom +
			` WHERE b.proposer_cons_address = $1 AND b.height < $3 ORDER BY b.height DESC LIMIT $2`
		args = append(args, *before)
	}
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("validator %s blocks: %w", cons, err)
	}
	return collectBlockSummaries(rows)
}

// ValidatorEvents lists the last limit events of the validator with
// operator address, newest first.
func (r *Explorer) ValidatorEvents(ctx context.Context, operator string, limit int) ([]ValidatorEvent, error) {
	rows, err := r.db.Query(ctx, `SELECT e.height, e.tx_index, e.seq, e.kind, e.details, e.time, t.hash
		FROM explorer.validator_events e
		LEFT JOIN explorer.transactions t ON e.tx_index >= 0 AND t.height = e.height AND t.tx_index = e.tx_index
		WHERE e.operator_address = $1
		ORDER BY e.height DESC, e.tx_index DESC, e.seq DESC LIMIT $2`, operator, limit)
	if err != nil {
		return nil, fmt.Errorf("validator %s events: %w", operator, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ValidatorEvent, error) {
		var e ValidatorEvent
		err := row.Scan(&e.Height, &e.TxIndex, &e.Seq, &e.Kind, &e.Details, &e.Time, &e.TxHash)
		return e, err
	})
}

// VotesOf lists the last limit governance votes of account, newest proposal
// first.
func (r *Explorer) VotesOf(ctx context.Context, account string, limit int) ([]ValidatorVote, error) {
	rows, err := r.db.Query(ctx, `SELECT v.proposal_id, p.title, v.option, v.options, v.height, v.time
		FROM explorer.proposal_votes v LEFT JOIN explorer.proposals p ON p.id = v.proposal_id
		WHERE v.voter = $1 ORDER BY v.proposal_id DESC LIMIT $2`, account, limit)
	if err != nil {
		return nil, fmt.Errorf("votes of %s: %w", account, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ValidatorVote, error) {
		var v ValidatorVote
		err := row.Scan(&v.ProposalID, &v.Title, &v.Option, &v.Options, &v.Height, &v.Time)
		return v, err
	})
}
