package writer_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

const guardianProposalMsgs = `[{"@type":"/bze.guardian.v1.MsgCloseChannel","signer":"` + nestedDao + `","channel_id":"channel-3"}]`

// The nested emergency actions of a flush's transactions are written with
// the other history rows, in the flush's mode.
func TestNestedEmergencyActionsOfTransactionsAreWritten(t *testing.T) {
	for _, mode := range []writer.Mode{writer.ModeInsert, writer.ModeUpdate} {
		db := newMockDB(49)
		ents := block(10)
		ents.NestedEmergencyActions = []transform.NestedEmergencyAction{{Height: 10, TxIndex: 1, MsgIndex: 2, Seq: 0, TxHash: "EXEC",
			Time: time.Unix(10, 0).UTC(), TypeURL: "/bze.guardian.v1.MsgBlockDenom", Signer: "bze1guardian",
			Body: json.RawMessage(`{"denom":"ibc/AAA"}`), Via: transform.ViaAuthz, Grantee: "bze1hot"}}
		require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{ents}, mode))

		ins := statementsWith(db.txs[0], "INSERT INTO explorer.emergency_nested_actions")
		require.Len(t, ins, 1, mode)
		rows := payload(t, ins[0])
		require.Len(t, rows, 1)
		assert.Equal(t, "authz", rows[0]["via_kind"])
		assert.Equal(t, "bze1hot", rows[0]["grantee"])
		assert.Nil(t, rows[0]["dao_id"])
		assert.Nil(t, rows[0]["proposal_id"])
		assert.EqualValues(t, 2, rows[0]["msg_index"])
		assert.Equal(t, "bze1guardian", rows[0]["signer"])
		assert.Equal(t, map[string]any{"denom": "ibc/AAA"}, rows[0]["body"])
		updates := statementsWith(db.txs[0], "UPDATE explorer.emergency_nested_actions")
		if mode == writer.ModeUpdate {
			assert.Len(t, updates, 1, "a reindex rewrites the rows in place")
		} else {
			assert.Empty(t, updates)
		}
	}
}

// An executed proposal's guardian messages are written by the settle hook,
// at the settle's transaction and message, in the proposal's seq range,
// naming the DAO and the proposal.
func TestSettledProposalActionsWriteTheNestedEmergencyActions(t *testing.T) {
	at := time.Unix(300, 0).UTC()
	db := newMockDB(49)
	db.returning = map[string][][]any{"WITH s AS (": {{6, guardianProposalMsgs, 10, 2, at, 1, 3, "SETTLE", 4}}}
	ents := block(10)
	ok := true
	ents.DaoProposalOutcomes = []transform.DaoProposalOutcome{{ID: 6, Status: "executed", Height: 10, TxIndex: 2, Executed: &ok}}
	tx := settledFlush(t, db, writer.ModeInsert, ents)

	ins := statementsWith(tx, "INSERT INTO explorer.emergency_nested_actions")
	require.Len(t, ins, 1)
	rows := payload(t, ins[0])
	require.Len(t, rows, 1)
	assert.Equal(t, "dao", rows[0]["via_kind"])
	assert.EqualValues(t, 3, rows[0]["dao_id"])
	assert.EqualValues(t, 6, rows[0]["proposal_id"])
	assert.Nil(t, rows[0]["grantee"])
	assert.EqualValues(t, 10, rows[0]["height"])
	assert.EqualValues(t, 2, rows[0]["tx_index"])
	assert.EqualValues(t, 4, rows[0]["msg_index"])
	assert.EqualValues(t, 2000, rows[0]["seq"], "second proposal executed by the transaction")
	assert.Equal(t, "SETTLE", rows[0]["tx_hash"])
	assert.Equal(t, nestedDao, rows[0]["signer"])
	assert.Equal(t, "/bze.guardian.v1.MsgCloseChannel", rows[0]["type_url"])
}

// A proposal whose execution failed ran nothing: no read, no row.
func TestAFailedProposalExecutionGivesNoNestedEmergencyAction(t *testing.T) {
	db := newMockDB(49)
	db.returning = map[string][][]any{"WITH s AS (": {{6, guardianProposalMsgs, 10, 2, time.Unix(1, 0), 0, 3, "SETTLE", 0}}}
	ents := block(10)
	failed := false
	ents.DaoProposalOutcomes = []transform.DaoProposalOutcome{{ID: 6, Status: "execution_failed", Executed: &failed}}
	tx := settledFlush(t, db, writer.ModeInsert, ents)
	assert.Empty(t, statementsWith(tx, "WITH s AS ("))
	assert.Empty(t, statementsWith(tx, "explorer.emergency_nested_actions"))
}
