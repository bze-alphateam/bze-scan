package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// BlockTxIndex is the tx_index of a block's own transfers rows.
const BlockTxIndex = -1

// Transfer is a transfers row with the denom's display data and the labels
// of both ends, each nil when the explorer does not know it.
type Transfer struct {
	Seq            int64
	MsgIndex       *int64
	Kind           string
	Sender         *string
	Recipient      *string
	Denom          string
	Amount         string
	Symbol         *string
	Exponent       *int
	SenderLabel    *Label
	RecipientLabel *Label
}

// BlockEvent is a block_events row.
type BlockEvent struct {
	Seq   int64
	Type  string
	Attrs json.RawMessage
}

// Transfers returns the transfers rows of the transaction at (height,
// txIndex), or of the block itself for BlockTxIndex, in seq order.
func (r *Explorer) Transfers(ctx context.Context, height, txIndex int64) ([]Transfer, error) {
	rows, err := r.db.Query(ctx, `SELECT t.seq, t.msg_index, t.kind, t.sender, t.recipient, t.denom, t.amount::text,
			d.symbol, d.exponent, ls.name, ls.kind, lr.name, lr.kind
		FROM explorer.transfers t
		LEFT JOIN explorer.denoms d ON d.denom = t.denom
		LEFT JOIN explorer.labels ls ON ls.address = t.sender
		LEFT JOIN explorer.labels lr ON lr.address = t.recipient
		WHERE t.height = $1 AND t.tx_index = $2 ORDER BY t.seq`, height, txIndex)
	if err != nil {
		return nil, fmt.Errorf("transfers %d/%d: %w", height, txIndex, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Transfer, error) {
		var t Transfer
		var senderName, senderKind, recipientName, recipientKind *string
		err := row.Scan(&t.Seq, &t.MsgIndex, &t.Kind, &t.Sender, &t.Recipient, &t.Denom, &t.Amount,
			&t.Symbol, &t.Exponent, &senderName, &senderKind, &recipientName, &recipientKind)
		t.SenderLabel = label(t.Sender, senderName, senderKind)
		t.RecipientLabel = label(t.Recipient, recipientName, recipientKind)
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("transfers %d/%d: %w", height, txIndex, err)
	}
	return out, nil
}

// BlockEvents returns up to limit block_events rows of height in seq order,
// after seq after (from the first when negative).
func (r *Explorer) BlockEvents(ctx context.Context, height, after int64, limit int) ([]BlockEvent, error) {
	rows, err := r.db.Query(ctx, `SELECT seq, type, attrs FROM explorer.block_events
		WHERE height = $1 AND seq > $2 ORDER BY seq LIMIT $3`, height, after, limit)
	if err != nil {
		return nil, fmt.Errorf("block events %d: %w", height, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (BlockEvent, error) {
		var e BlockEvent
		err := row.Scan(&e.Seq, &e.Type, &e.Attrs)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("block events %d: %w", height, err)
	}
	return out, nil
}

func label(address, name, kind *string) *Label {
	if address == nil || name == nil || kind == nil {
		return nil
	}
	return &Label{Address: *address, Name: *name, Kind: *kind}
}
