package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ActivityKey is the keyset position of an account_activity row of one
// address: tx_index -1 is a block-level row.
type ActivityKey struct {
	Height  int64
	TxIndex int64
}

// Activity is an account_activity row with its transaction's hash and fee
// and its counterparty's label or moniker.
type Activity struct {
	Height   int64
	TxIndex  int64
	Time     time.Time
	TxHash   *string
	Kind     string
	Category string
	IsSigner bool
	Success  bool
	// Counterparty with its label and the moniker of a validator operator,
	// each nil when unknown.
	Counterparty        *string
	CounterpartyLabel   *Label
	CounterpartyMoniker *string
	Deltas              json.RawMessage
	MsgTypes            []string
	Details             json.RawMessage
	// Fee is the transaction's fee when the address signed or paid it, nil
	// otherwise and for block-level rows.
	Fee json.RawMessage
}

// Activity returns up to limit account_activity rows of address, newest
// first (height, then tx_index, descending), only of category when it is not
// nil, after before when it is not nil. The category index serves the
// filtered list, the primary key the unfiltered one.
func (r *Explorer) Activity(ctx context.Context, address string, category *string, before *ActivityKey,
	limit int) ([]Activity, error) {
	return r.activity(ctx, address, category, before, 0, limit)
}

// ActivityPage returns up to limit account_activity rows of address, newest
// first, only of category when it is not nil, skipping the newest offset
// rows. Offsets stay within one address, so they are bounded by its own
// activity count.
func (r *Explorer) ActivityPage(ctx context.Context, address string, category *string,
	offset int64, limit int) ([]Activity, error) {
	return r.activity(ctx, address, category, nil, offset, limit)
}

// ActivityTotal returns the number of account_activity rows of address, only
// of category when it is not nil: the unfiltered count is the accounts row's
// activity_count (0 when the address has no row), a category is counted on
// its index.
func (r *Explorer) ActivityTotal(ctx context.Context, address string, category *string) (int64, error) {
	var n int64
	var err error
	if category == nil {
		err = r.db.QueryRow(ctx, `SELECT activity_count FROM explorer.accounts WHERE address = $1`, address).Scan(&n)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
	} else {
		err = r.db.QueryRow(ctx, `SELECT count(*) FROM explorer.account_activity
			WHERE address = $1 AND category = $2`, address, *category).Scan(&n)
	}
	if err != nil {
		return 0, fmt.Errorf("activity total %s: %w", address, err)
	}
	return n, nil
}

func (r *Explorer) activity(ctx context.Context, address string, category *string, before *ActivityKey,
	offset int64, limit int) ([]Activity, error) {
	var beforeHeight, beforeTxIndex *int64
	if before != nil {
		beforeHeight, beforeTxIndex = &before.Height, &before.TxIndex
	}
	// The page is cut from account_activity alone, then joined: joined
	// first, every row an OFFSET skips would look up its transaction, label
	// and validator too.
	rows, err := r.db.Query(ctx, `SELECT a.height, a.tx_index, a.time, t.hash, a.kind, a.category,
			a.is_signer, a.success, a.counterparty, l.name, l.kind, v.moniker, a.deltas, a.msg_types, a.details,
			CASE WHEN a.is_signer OR t.fee_payer = a.address THEN t.fee END
		FROM (SELECT * FROM explorer.account_activity
			WHERE address = $1
			  AND ($2::text IS NULL OR category = $2)
			  AND ($3::bigint IS NULL OR (height, tx_index) < ($3::bigint, $4::integer))
			ORDER BY height DESC, tx_index DESC
			LIMIT $5 OFFSET $6) a
		LEFT JOIN explorer.transactions t ON t.height = a.height AND t.tx_index = a.tx_index
		LEFT JOIN explorer.labels l ON l.address = a.counterparty
		LEFT JOIN explorer.validators v ON v.operator_address = a.counterparty
		ORDER BY a.height DESC, a.tx_index DESC`, address, category, beforeHeight, beforeTxIndex, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("activity %s: %w", address, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Activity, error) {
		var a Activity
		var labelName, labelKind *string
		err := row.Scan(&a.Height, &a.TxIndex, &a.Time, &a.TxHash, &a.Kind, &a.Category, &a.IsSigner, &a.Success,
			&a.Counterparty, &labelName, &labelKind, &a.CounterpartyMoniker, &a.Deltas, &a.MsgTypes, &a.Details, &a.Fee)
		a.CounterpartyLabel = label(a.Counterparty, labelName, labelKind)
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("activity %s: %w", address, err)
	}
	return out, nil
}
