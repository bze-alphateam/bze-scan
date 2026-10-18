package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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

// BlockTransfer is a transfers row of a block with its transaction's
// index and hash (nil for the block's own rows).
type BlockTransfer struct {
	Transfer
	TxIndex int64
	TxHash  *string
}

// TransferKey is the keyset position of a block's transfers: by
// transaction index (the block's own rows, BlockTxIndex, first), then seq.
type TransferKey struct {
	TxIndex int64
	Seq     int64
}

// TransferVolume is how many transfers rows moved a denom in a window and
// their base-unit sum as decimal text.
type TransferVolume struct {
	Count  int64
	Amount string
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

// BlockTransfers returns up to limit transfers rows of the block at height,
// the transactions' and the block's own, by transaction index (the block's
// own first) then seq, after the key after (from the first when nil).
func (r *Explorer) BlockTransfers(ctx context.Context, height int64, after *TransferKey, limit int) ([]BlockTransfer, error) {
	args := []any{height, limit}
	keyset := ""
	if after != nil {
		args = append(args, after.TxIndex, after.Seq)
		keyset = ` AND (x.tx_index, x.seq) > ($3, $4)`
	}
	rows, err := r.db.Query(ctx, `SELECT x.tx_index, tx.hash, x.seq, x.msg_index, x.kind, x.sender, x.recipient,
			x.denom, x.amount::text, d.symbol, d.exponent, ls.name, ls.kind, lr.name, lr.kind
		FROM explorer.transfers x
		LEFT JOIN explorer.transactions tx ON tx.height = $1 AND tx.tx_index = x.tx_index
		LEFT JOIN explorer.denoms d ON d.denom = x.denom
		LEFT JOIN explorer.labels ls ON ls.address = x.sender
		LEFT JOIN explorer.labels lr ON lr.address = x.recipient
		WHERE x.height = $1`+keyset+`
		ORDER BY x.tx_index, x.seq LIMIT $2`, args...)
	if err != nil {
		return nil, fmt.Errorf("block transfers %d: %w", height, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (BlockTransfer, error) {
		var x BlockTransfer
		var senderName, senderKind, recipientName, recipientKind *string
		err := row.Scan(&x.TxIndex, &x.TxHash, &x.Seq, &x.MsgIndex, &x.Kind, &x.Sender, &x.Recipient,
			&x.Denom, &x.Amount, &x.Symbol, &x.Exponent, &senderName, &senderKind, &recipientName, &recipientKind)
		x.SenderLabel = label(x.Sender, senderName, senderKind)
		x.RecipientLabel = label(x.Recipient, recipientName, recipientKind)
		return x, err
	})
	if err != nil {
		return nil, fmt.Errorf("block transfers %d: %w", height, err)
	}
	return out, nil
}

// windowStartSQL finds the lowest height of the blocks newer than the
// window before the latest indexed block (not the wall clock, like the home
// tiles). Block time grows with height (CometBFT's BFT time), so that is the
// oldest block of the window by time: both ends are read on the time index
// of each partition, never walking a partition's rows. A min(height) over
// the same filter walks every partition's primary key from its lowest
// height instead, filtering on time: about a second per ten million blocks.
const windowStartSQL = `SELECT b.height FROM explorer.blocks b
	WHERE b.time > (SELECT max(time) FROM explorer.blocks) - $1 * interval '1 second'
	ORDER BY b.time LIMIT 1`

// denomVolumeSQL counts and sums the transfers rows of a denom from a
// height on. The height is a parameter, not a subquery, so the planner
// prunes the partitions below it and costs the scan on the (denom, height)
// index for what it is.
const denomVolumeSQL = `SELECT count(*), coalesce(sum(x.amount), 0)::text
	FROM explorer.transfers x
	WHERE x.denom = $1 AND x.height >= $2`

// DenomVolume returns the transfers of denom in the window before the
// latest indexed block; none before the first block.
func (r *Explorer) DenomVolume(ctx context.Context, denom string, window time.Duration) (*TransferVolume, error) {
	var from int64
	err := r.db.QueryRow(ctx, windowStartSQL, window.Seconds()).Scan(&from)
	if errors.Is(err, pgx.ErrNoRows) {
		return &TransferVolume{Amount: "0"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("denom volume %s: window start: %w", denom, err)
	}
	var v TransferVolume
	if err := r.db.QueryRow(ctx, denomVolumeSQL, denom, from).Scan(&v.Count, &v.Amount); err != nil {
		return nil, fmt.Errorf("denom volume %s: %w", denom, err)
	}
	return &v, nil
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
