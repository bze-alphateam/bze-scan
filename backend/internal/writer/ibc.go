package writer

import (
	"fmt"
	"slices"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// The cross-chain transfer writes. A row is keyed by the BZE-side channel,
// the packet sequence and the direction, and comes from two sides: the
// transaction that creates it (the MsgTransfer of an outbound transfer, the
// MsgRecvPacket of an inbound one) and, for an outbound transfer, the
// acknowledgement or timeout that settles it. The backfill walks down, so
// the settling side can be written first: it carries the whole packet and
// inserts a complete row, which the creating side then completes with its
// own transaction instead of overwriting. Both orders end with the same
// row.

// ibcTransferRow is the JSON shape of an ibc_transfers write.
type ibcTransferRow struct {
	ChannelID      string     `json:"channel_id"`
	Sequence       uint64     `json:"sequence"`
	Direction      string     `json:"direction"`
	Status         string     `json:"status"`
	Sender         string     `json:"sender"`
	Receiver       string     `json:"receiver"`
	Denom          string     `json:"denom"`
	Amount         string     `json:"amount"`
	Memo           *string    `json:"memo"`
	CreatedHeight  int64      `json:"created_height"`
	CreatedTxIndex int        `json:"created_tx_index"`
	CreatedTxHash  string     `json:"created_tx_hash"`
	CreatedTime    time.Time  `json:"created_time"`
	TimeoutHeight  *string    `json:"timeout_height"`
	TimeoutTime    *time.Time `json:"timeout_time"`
	ResultHeight   *int64     `json:"result_height"`
	ResultTxHash   *string    `json:"result_tx_hash"`
	ResultTime     *time.Time `json:"result_time"`
	AckError       *string    `json:"ack_error"`
}

// ibcInsert inserts ibcTransferRow payloads, the counterparty chain taken
// from the channel when it is known.
const ibcInsert = `INSERT INTO explorer.ibc_transfers AS t (channel_id, sequence, direction, status, sender, receiver,
		denom, amount, memo, counterparty_chain_id, created_height, created_tx_index, created_tx_hash, created_time,
		timeout_height, timeout_time, result_height, result_tx_hash, result_time, ack_error)
	SELECT r.channel_id, r.sequence, r.direction, r.status, r.sender, r.receiver, r.denom, r.amount, r.memo,
		c.counterparty_chain_id, r.created_height, r.created_tx_index, r.created_tx_hash, r.created_time,
		r.timeout_height, r.timeout_time, r.result_height, r.result_tx_hash, r.result_time, r.ack_error
	  FROM jsonb_to_recordset($1::jsonb) AS r(channel_id text, sequence bigint, direction text, status text,
		sender text, receiver text, denom text, amount numeric, memo text, created_height bigint,
		created_tx_index integer, created_tx_hash text, created_time timestamptz, timeout_height text,
		timeout_time timestamptz, result_height bigint, result_tx_hash text, result_time timestamptz, ack_error text)
	  LEFT JOIN explorer.ibc_channels c ON c.channel_id = r.channel_id
	 ORDER BY r.channel_id, r.sequence, r.direction
	ON CONFLICT (channel_id, sequence, direction) DO UPDATE SET `

// ibcCreatedSQL writes the creating side: the transaction and the timeouts
// replace those of a row the settling side created, never its status or
// result. An inbound row is final at creation, so its outcome is rewritten
// too (a reindex).
const ibcCreatedSQL = ibcInsert + `
		created_height = EXCLUDED.created_height, created_tx_index = EXCLUDED.created_tx_index,
		created_tx_hash = EXCLUDED.created_tx_hash, created_time = EXCLUDED.created_time,
		timeout_height = EXCLUDED.timeout_height, timeout_time = EXCLUDED.timeout_time,
		counterparty_chain_id = COALESCE(t.counterparty_chain_id, EXCLUDED.counterparty_chain_id),
		status         = CASE WHEN t.direction = 'in' THEN EXCLUDED.status ELSE t.status END,
		result_height  = CASE WHEN t.direction = 'in' THEN EXCLUDED.result_height ELSE t.result_height END,
		result_tx_hash = CASE WHEN t.direction = 'in' THEN EXCLUDED.result_tx_hash ELSE t.result_tx_hash END,
		result_time    = CASE WHEN t.direction = 'in' THEN EXCLUDED.result_time ELSE t.result_time END,
		ack_error      = CASE WHEN t.direction = 'in' THEN EXCLUDED.ack_error ELSE t.ack_error END
	WHERE (t.created_height, t.created_tx_index, t.created_tx_hash, t.created_time, t.timeout_height,
	       t.timeout_time) IS DISTINCT FROM
	      (EXCLUDED.created_height, EXCLUDED.created_tx_index, EXCLUDED.created_tx_hash, EXCLUDED.created_time,
	       EXCLUDED.timeout_height, EXCLUDED.timeout_time)
	   OR (t.counterparty_chain_id IS NULL AND EXCLUDED.counterparty_chain_id IS NOT NULL)
	   OR (t.direction = 'in' AND (t.status, t.result_height, t.result_tx_hash, t.result_time, t.ack_error)
	       IS DISTINCT FROM (EXCLUDED.status, EXCLUDED.result_height, EXCLUDED.result_tx_hash,
	       EXCLUDED.result_time, EXCLUDED.ack_error))`

// ibcResultSQL writes the settling side: a complete row when none is
// stored, else the outcome of a pending row only, which keeps replays and
// backfill overlap idempotent.
const ibcResultSQL = ibcInsert + `
		status = EXCLUDED.status, result_height = EXCLUDED.result_height, result_tx_hash = EXCLUDED.result_tx_hash,
		result_time = EXCLUDED.result_time, ack_error = EXCLUDED.ack_error,
		counterparty_chain_id = COALESCE(t.counterparty_chain_id, EXCLUDED.counterparty_chain_id)
	WHERE t.status = 'pending'`

// ibcStatements returns the cross-chain transfer writes of ents: the
// creating side, then the settling side, each one row per key in key order.
// The mode does not matter: both sides are upserts.
func ibcStatements(top int64, ents *transform.Entities) ([]statement, error) {
	var created, settled []transform.IBCTransfer
	for _, r := range ents.IBCTransfers {
		if r.Resolves {
			settled = append(settled, r)
		} else {
			created = append(created, r)
		}
	}
	createdStmts, err := plainStatements(top, "ibc_transfers", ibcCreatedSQL, ibcRows(created))
	if err != nil {
		return nil, err
	}
	settledStmts, err := plainStatements(top, "ibc_transfers settled", ibcResultSQL, ibcRows(settled))
	if err != nil {
		return nil, err
	}
	return slices.Concat(createdStmts, settledStmts), nil
}

// ibcRows keeps one row per key, the earliest (a packet is created and
// settled once; one statement cannot upsert a key twice), in key order.
func ibcRows(rows []transform.IBCTransfer) []ibcTransferRow {
	merged := mergeDeltas(rows, func(r transform.IBCTransfer) string {
		return fmt.Sprintf("%s\x00%020d\x00%s", r.ChannelID, r.Sequence, r.Direction)
	}, func(a, b transform.IBCTransfer) transform.IBCTransfer {
		if b.CreatedHeight < a.CreatedHeight {
			return b
		}
		return a
	})
	out := make([]ibcTransferRow, len(merged))
	for i, r := range merged {
		out[i] = ibcTransferRow{
			ChannelID: r.ChannelID, Sequence: r.Sequence, Direction: r.Direction, Status: r.Status,
			Sender: r.Sender, Receiver: r.Receiver, Denom: r.Denom, Amount: r.Amount, Memo: nullIfEmpty(r.Memo),
			CreatedHeight: r.CreatedHeight, CreatedTxIndex: r.CreatedTxIndex, CreatedTxHash: r.CreatedTxHash,
			CreatedTime: r.CreatedTime, TimeoutHeight: nullIfEmpty(r.TimeoutHeight), TimeoutTime: r.TimeoutTime,
			ResultHeight: r.ResultHeight, ResultTxHash: nullIfEmpty(r.ResultTxHash), ResultTime: r.ResultTime,
			AckError: nullIfEmpty(r.AckError),
		}
	}
	return out
}
