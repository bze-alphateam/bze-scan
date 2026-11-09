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
// nil, only those moving denom (a delta in it, the fee included) when it is
// not nil, only the incoming transfers when incoming is set (incomingClause),
// after before when it is not nil. The category index serves the category
// list, the primary key the unfiltered one, the deltas index the denom list,
// the incoming index the incoming list.
func (r *Explorer) Activity(ctx context.Context, address string, category, denom *string, incoming bool,
	before *ActivityKey, limit int) ([]Activity, error) {
	return r.activity(ctx, address, category, denom, incoming, before, 0, limit)
}

// ActivityPage returns up to limit account_activity rows of address, newest
// first, only of category and denom when they are not nil and only the
// incoming transfers when incoming is set, skipping the
// newest offset rows. Offsets stay within one address, so they are bounded by
// its own activity count.
func (r *Explorer) ActivityPage(ctx context.Context, address string, category, denom *string, incoming bool,
	offset int64, limit int) ([]Activity, error) {
	return r.activity(ctx, address, category, denom, incoming, nil, offset, limit)
}

// ActivityTotal returns the number of account_activity rows of address, only
// of category when it is not nil: the unfiltered count is the accounts row's
// activity_count (0 when the address has no row), a category, a denom or the
// incoming filter is counted on its index.
func (r *Explorer) ActivityTotal(ctx context.Context, address string, category, denom *string, incoming bool) (int64, error) {
	var n int64
	var err error
	if category == nil && denom == nil && !incoming {
		err = r.db.QueryRow(ctx, `SELECT activity_count FROM explorer.accounts WHERE address = $1`, address).Scan(&n)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
	} else {
		filter, args := activityFilter(denom, incoming, 3)
		err = r.db.QueryRow(ctx, `SELECT count(*) FROM explorer.account_activity
			WHERE address = $1 AND ($2::text IS NULL OR category = $2)`+filter,
			append([]any{address, category}, args...)...).Scan(&n)
	}
	if err != nil {
		return 0, fmt.Errorf("activity total %s: %w", address, err)
	}
	return n, nil
}

// denomClause is the filter of the rows with a delta in denom, whose
// parameter is number n; empty without a denom. It is added to the text only
// when a denom is given: behind an `$n IS NULL OR` the planner could not rely
// on the deltas index in a generic plan.
func denomClause(denom *string, n int) string {
	if denom == nil {
		return ""
	}
	return fmt.Sprintf(" AND deltas @> $%d::jsonb", n)
}

// denomArgs is the parameter of denomClause: the one-coin array the deltas
// have to contain.
func denomArgs(denom *string) []any {
	if denom == nil {
		return nil
	}
	b, _ := json.Marshal([]map[string]string{{"denom": *denom}})
	return []any{string(b)}
}

// Message types an incoming transfer can arrive through.
const (
	msgTypeSend      = "/cosmos.bank.v1beta1.MsgSend"
	msgTypeMultiSend = "/cosmos.bank.v1beta1.MsgMultiSend"
	msgTypeExec      = "/cosmos.authz.v1beta1.MsgExec"
	msgTypeRecv      = "/ibc.core.channel.v1.MsgRecvPacket"
)

// incomingClause keeps the rows of a successful transaction in which the
// address's balance rose in some denom and a message paid it: a bank send or
// multi-send to it, one of those run through authz, or a cross-chain receipt
// for it. The first three conditions are the predicate of
// idx_account_activity_incoming, word for word, and are literals so that a
// generic plan can use that index; the messages are read only for the rows it
// leaves. Stored message events keep fungible_token_packet, so the receiver of
// a packet is read from there (the packet data in the body is encoded).
const incomingClause = ` AND success
			  AND deltas @? '$[*] ? (!(@.amount starts with "-"))'
			  AND msg_types && ARRAY['` + msgTypeSend + `', '` + msgTypeMultiSend + `', '` + msgTypeExec + `', '` + msgTypeRecv + `']::text[]
			  AND EXISTS (SELECT 1 FROM explorer.messages m
			    WHERE m.height = account_activity.height AND m.tx_index = account_activity.tx_index
			      AND ((m.type_url = '` + msgTypeSend + `' AND m.body->>'to_address' = account_activity.address)
			        OR (m.type_url = '` + msgTypeMultiSend + `' AND m.body->'outputs' @> jsonb_build_array(jsonb_build_object('address', account_activity.address)))
			        OR (m.type_url = '` + msgTypeRecv + `' AND m.events @> jsonb_build_array(jsonb_build_object(
			              'type', 'fungible_token_packet', 'attrs', jsonb_build_object('receiver', account_activity.address))))
			        OR (m.type_url = '` + msgTypeExec + `' AND (
			              m.body->'msgs' @> jsonb_build_array(jsonb_build_object('@type', '` + msgTypeSend + `', 'to_address', account_activity.address))
			           OR m.body->'msgs' @> jsonb_build_array(jsonb_build_object('@type', '` + msgTypeMultiSend + `',
			                'outputs', jsonb_build_array(jsonb_build_object('address', account_activity.address))))))))`

// activityFilter is the optional part of the WHERE of an activity query, whose
// first parameter is number n, with its arguments. With both a denom and the
// incoming filter the balance has to have risen in that denom, not only in
// some other one.
func activityFilter(denom *string, incoming bool, n int) (string, []any) {
	clause, args := denomClause(denom, n), denomArgs(denom)
	if !incoming {
		return clause, args
	}
	clause += incomingClause
	if denom != nil {
		clause += fmt.Sprintf(` AND jsonb_path_exists(deltas, '$[*] ? (@.denom == $d && !(@.amount starts with "-"))',
			jsonb_build_object('d', $%d::text))`, n+len(args))
		args = append(args, *denom)
	}
	return clause, args
}

func (r *Explorer) activity(ctx context.Context, address string, category, denom *string, incoming bool,
	before *ActivityKey, offset int64, limit int) ([]Activity, error) {
	var beforeHeight, beforeTxIndex *int64
	if before != nil {
		beforeHeight, beforeTxIndex = &before.Height, &before.TxIndex
	}
	filter, filterArgs := activityFilter(denom, incoming, 7)
	// The page is cut from account_activity alone, then joined: joined
	// first, every row an OFFSET skips would look up its transaction, label
	// and validator too.
	rows, err := r.db.Query(ctx, `SELECT a.height, a.tx_index, a.time, t.hash, a.kind, a.category,
			a.is_signer, a.success, a.counterparty, l.name, l.kind, v.moniker, a.deltas, a.msg_types, a.details,
			CASE WHEN a.is_signer OR t.fee_payer = a.address THEN t.fee END
		FROM (SELECT * FROM explorer.account_activity
			WHERE address = $1
			  AND ($2::text IS NULL OR category = $2)
			  AND ($3::bigint IS NULL OR (height, tx_index) < ($3::bigint, $4::integer))`+filter+`
			ORDER BY height DESC, tx_index DESC
			LIMIT $5 OFFSET $6) a
		LEFT JOIN explorer.transactions t ON t.height = a.height AND t.tx_index = a.tx_index
		LEFT JOIN explorer.labels l ON l.address = a.counterparty
		LEFT JOIN explorer.validators v ON v.operator_address = a.counterparty
		ORDER BY a.height DESC, a.tx_index DESC`,
		append([]any{address, category, beforeHeight, beforeTxIndex, limit, offset}, filterArgs...)...)
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
