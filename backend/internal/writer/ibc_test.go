package writer_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// The upsert in either order runs against PostgreSQL in the e2e tests;
// these pin what the writers send.

const (
	ibcCreatedPart = "WHEN t.direction = 'in' THEN EXCLUDED.status"
	ibcSettledPart = "WHERE t.status = 'pending'"
)

func sent(h int64, channel string, seq uint64) transform.IBCTransfer {
	at := time.Unix(h, 0).UTC()
	return transform.IBCTransfer{ChannelID: channel, Sequence: seq, Direction: transform.IBCOut,
		Status: transform.IBCPending, Sender: "bze1s", Receiver: "osmo1r", Denom: "ubze", Amount: "5",
		CreatedHeight: h, CreatedTxHash: "SEND", CreatedTime: at, TimeoutHeight: "1-9"}
}

func settledBy(r transform.IBCTransfer, h int64, status string) transform.IBCTransfer {
	at := time.Unix(h, 0).UTC()
	r.Status, r.Resolves = status, true
	r.CreatedHeight, r.CreatedTxHash, r.CreatedTime = h, "ACK", at
	r.ResultHeight, r.ResultTxHash, r.ResultTime = &h, "ACK", &at
	return r
}

func ibcBlock(h int64, rows ...transform.IBCTransfer) *transform.Entities {
	ents := block(h)
	ents.IBCTransfers = rows
	return ents
}

// A flush holding a packet's send and its acknowledgement writes the send
// first, then settles it; each side one row per key, in key order.
func TestIBCTransfersCreatedThenSettled(t *testing.T) {
	db := newMockDB(49)
	send := sent(10, "channel-3", 7)
	other := sent(10, "channel-1", 9)
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{
		ibcBlock(12, settledBy(send, 12, transform.IBCDelivered)),
		ibcBlock(10, send, other),
	}, writer.ModeInsert))

	var order []string
	for _, st := range db.txs[0].stmts {
		switch {
		case strings.Contains(st.sql, ibcCreatedPart):
			order = append(order, "created")
		case strings.Contains(st.sql, ibcSettledPart):
			order = append(order, "settled")
		}
	}
	assert.Equal(t, []string{"created", "settled"}, order)

	created := payload(t, statementsOf(db.txs[0], ibcCreatedPart)[0])
	require.Len(t, created, 2)
	assert.Equal(t, "channel-1", created[0]["channel_id"])
	assert.Equal(t, "channel-3", created[1]["channel_id"])
	assert.Equal(t, "pending", created[1]["status"])
	assert.Nil(t, created[1]["result_height"])
	assert.Nil(t, created[1]["memo"], "an empty memo is NULL")

	settled := payload(t, statementsOf(db.txs[0], ibcSettledPart)[0])
	require.Len(t, settled, 1)
	assert.Equal(t, "delivered", settled[0]["status"])
	assert.Equal(t, float64(12), settled[0]["result_height"])
	assert.Equal(t, "ACK", settled[0]["created_tx_hash"], "the complete row a settlement inserts when no send is stored")
	assert.Equal(t, "1-9", settled[0]["timeout_height"])
}

// One statement cannot upsert a key twice: a key met twice in a flush (a
// replayed height) is written once.
func TestIBCTransfersOneRowPerKey(t *testing.T) {
	db := newMockDB(49)
	send := sent(10, "channel-3", 7)
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{
		ibcBlock(10, send), ibcBlock(11, sent(11, "channel-3", 7)),
	}, writer.ModeUpdate))
	created := payload(t, statementsOf(db.txs[0], ibcCreatedPart)[0])
	require.Len(t, created, 1)
	assert.Equal(t, float64(10), created[0]["created_height"])
	assert.Empty(t, statementsOf(db.txs[0], ibcSettledPart))
}

func TestNoIBCTransfersNoWrite(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewLiveWriter(db).WriteBlock(context.Background(), block(100)))
	assert.Empty(t, statementsOf(db.txs[0], "explorer.ibc_transfers"))
}
