package repository

import (
	"context"
	"encoding/json"
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
	var beforeHeight, beforeTxIndex *int64
	if before != nil {
		beforeHeight, beforeTxIndex = &before.Height, &before.TxIndex
	}
	rows, err := r.db.Query(ctx, `SELECT a.height, a.tx_index, a.time, t.hash, a.kind, a.category,
			a.is_signer, a.success, a.counterparty, l.name, l.kind, v.moniker, a.deltas, a.msg_types, a.details,
			CASE WHEN a.is_signer OR t.fee_payer = a.address THEN t.fee END
		FROM explorer.account_activity a
		LEFT JOIN explorer.transactions t ON t.height = a.height AND t.tx_index = a.tx_index
		LEFT JOIN explorer.labels l ON l.address = a.counterparty
		LEFT JOIN explorer.validators v ON v.operator_address = a.counterparty
		WHERE a.address = $1
		  AND ($2::text IS NULL OR a.category = $2)
		  AND ($3::bigint IS NULL OR (a.height, a.tx_index) < ($3::bigint, $4::integer))
		ORDER BY a.height DESC, a.tx_index DESC
		LIMIT $5`, address, category, beforeHeight, beforeTxIndex, limit)
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
