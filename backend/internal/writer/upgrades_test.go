package writer_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/upgrades"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// The upsert and the supersede run against PostgreSQL in the e2e tests;
// these pin what the writers send.

const emergencyInsert = "INSERT INTO explorer.upgrades AS u"

func haltBlock(h int64, halts ...transform.EmergencyHalt) *transform.Entities {
	ents := block(h)
	ents.EmergencyHalts = halts
	return ents
}

// An emergency halt is written as an upgrade row, then the governance
// upgrades it replaced are marked.
func TestEmergencyHaltWritesTheRowThenSupersedes(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewLiveWriter(db).WriteBlock(context.Background(), haltBlock(100,
		transform.EmergencyHalt{Height: 101, Name: "emergency-halt-101", Reason: "exploit", ScheduledBy: "bze1g",
			ScheduledHeight: 100, TxHash: "AB"})))
	stmts := db.txs[0].stmts
	insert, supersede := -1, -1
	for i, st := range stmts {
		switch {
		case st.sql == upgrades.SupersedeSQL:
			supersede = i
		case strings.Contains(st.sql, emergencyInsert):
			insert = i
		}
	}
	require.GreaterOrEqual(t, insert, 0)
	assert.Greater(t, supersede, insert, "marked after the row is written")
	rows := payload(t, statementsOf(db.txs[0], emergencyInsert)[0])
	require.Len(t, rows, 1)
	assert.Equal(t, map[string]any{"height": float64(101), "name": "emergency-halt-101", "reason": "exploit",
		"scheduled_by": "bze1g", "scheduled_tx_hash": "AB"}, rows[0])
}

// Two schedulings of the same height in a flush (two transactions of one
// block, or a replay) are written once, the block-level one winning.
func TestEmergencyHaltOneRowPerHeight(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{
		haltBlock(100,
			transform.EmergencyHalt{Height: 101, Name: "emergency-halt-101", Reason: "tx", ScheduledHeight: 100, TxHash: "AB"},
			transform.EmergencyHalt{Height: 101, Name: "emergency-halt-101", Reason: "gov", ScheduledHeight: 100},
			transform.EmergencyHalt{Height: 101, Name: "emergency-halt-101", Reason: "later tx", ScheduledHeight: 100, TxHash: "CD"}),
		haltBlock(90, transform.EmergencyHalt{Height: 91, Name: "emergency-halt-91", ScheduledHeight: 90}),
	}, writer.ModeUpdate))
	rows := payload(t, statementsOf(db.txs[0], emergencyInsert)[0])
	require.Len(t, rows, 2)
	assert.Equal(t, float64(91), rows[0]["height"], "in height order")
	assert.Nil(t, rows[0]["reason"], "an empty reason is NULL")
	assert.Equal(t, "gov", rows[1]["reason"])
	assert.Nil(t, rows[1]["scheduled_tx_hash"])
}

func TestNoEmergencyHaltNoWrite(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewLiveWriter(db).WriteBlock(context.Background(), block(100)))
	assert.Empty(t, statementsOf(db.txs[0], "explorer.upgrades"))
}
