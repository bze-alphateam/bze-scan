package writer_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

const (
	nestedDao   = "bze1daoaddressdaoaddressdaoaddressdaoaddress0"
	nestedDenom = "factory/" + nestedDao + "/uhoney"
	nestedMsgs  = `[{"@type":"/bze.tokenfactory.MsgBurn","creator":"` + nestedDao + `","coins":"5` + nestedDenom + `"},` +
		`{"@type":"/bze.tokenfactory.MsgSetDenomMetadata","creator":"` + nestedDao + `","metadata":{"base":"` +
		nestedDenom + `","symbol":"HNY","name":"Honey","display":"HNY"}}]`
)

// statementsWith are the statements of tx whose SQL contains frag.
func statementsWith(tx *mockTx, frag string) []statement {
	var out []statement
	for _, st := range tx.stmts {
		if strings.Contains(st.sql, frag) {
			out = append(out, st)
		}
	}
	return out
}

func settledFlush(t *testing.T, db *mockDB, mode writer.Mode, ents *transform.Entities) *mockTx {
	t.Helper()
	require.NoError(t, writer.NewBatchWriter(db, writer.SettledProposalActions).
		Write(context.Background(), []*transform.Entities{ents}, mode))
	return db.txs[0]
}

// The settle of an executed proposal asks for the stored proposal and writes
// the rows only its messages' bodies give (the burn is the settle's events'),
// numbered from NestedSeqBase by the proposal's rank in the settle.
func TestSettledProposalActionsWriteTheBodyRows(t *testing.T) {
	at := time.Unix(300, 0).UTC()
	db := newMockDB(49)
	db.returning = map[string][][]any{"WITH s AS (": {{6, nestedMsgs, 10, 2, at, 1}}}
	ents := block(10)
	ok := true
	ents.DaoProposalOutcomes = []transform.DaoProposalOutcome{{ID: 6, Status: "executed", Height: 10, TxIndex: 2, Executed: &ok}}
	tx := settledFlush(t, db, writer.ModeInsert, ents)

	q := statementsWith(tx, "WITH s AS (")
	require.Len(t, q, 1)
	assert.Equal(t, []int64{6}, q[0].args[0])

	tokens := statementsWith(tx, "INSERT INTO explorer.token_events")
	require.Len(t, tokens, 1, "the flush has no token rows of its own: only the hook's")
	rows := payload(t, tokens[0])
	require.Len(t, rows, 1, "no burn: the settle's events gave it")
	assert.Equal(t, "metadata_changed", rows[0]["kind"])
	assert.EqualValues(t, 2000, rows[0]["seq"], "second proposal executed by the transaction")
	assert.EqualValues(t, 2, rows[0]["tx_index"])
	assert.Equal(t, nestedDao, rows[0]["actor"])
	assert.Empty(t, statementsWith(tx, "UPDATE explorer.token_events"), "insert mode keeps existing rows")
}

// A reindex rewrites the same rows; a submission written after its settle
// (the backfill walks down) triggers the same read.
func TestSettledProposalActionsOnTheSubmissionInUpdateMode(t *testing.T) {
	db := newMockDB(49)
	db.returning = map[string][][]any{"WITH s AS (": {{6, nestedMsgs, 20, 0, time.Unix(1, 0), 0}}}
	ents := block(10)
	ents.DaoProposals = []transform.DaoProposal{{ID: 6, HasBody: true}, {ID: 7}}
	tx := settledFlush(t, db, writer.ModeUpdate, ents)

	q := statementsWith(tx, "WITH s AS (")
	require.Len(t, q, 1)
	assert.Equal(t, []int64{6}, q[0].args[0], "a submission without a body brings no msgs")
	updates := statementsWith(tx, "UPDATE explorer.token_events")
	require.NotEmpty(t, updates)
	rows := payload(t, updates[len(updates)-1])
	assert.EqualValues(t, 1000, rows[0]["seq"])
}

func TestSettledProposalActionsWithoutCandidatesOrMsgs(t *testing.T) {
	db := newMockDB(49)
	ents := block(10)
	failed := false
	ents.DaoProposalOutcomes = []transform.DaoProposalOutcome{{ID: 6, Status: "execution_failed", Executed: &failed}, {ID: 8, Status: "rejected"}}
	tx := settledFlush(t, db, writer.ModeInsert, ents)
	assert.Empty(t, statementsWith(tx, "WITH s AS ("), "nothing executed: no read")

	db = newMockDB(49)
	db.returning = map[string][][]any{"WITH s AS (": {{6, "", 10, 0, time.Unix(1, 0), 0}}}
	ok := true
	ents = block(10)
	ents.DaoProposalOutcomes = []transform.DaoProposalOutcome{{ID: 6, Status: "executed", Executed: &ok}}
	tx = settledFlush(t, db, writer.ModeInsert, ents)
	assert.Len(t, statementsWith(tx, "INSERT INTO explorer.token_events"), 0, "msgs not stored yet: no rows")
}

// The live writer publishes the flush's Dirty: the hook marks the denoms it
// wrote rows for.
func TestSettledProposalActionsMarkTheirDenomsOnTheLivePath(t *testing.T) {
	db := newMockDB(49)
	db.returning = map[string][][]any{"WITH s AS (": {{6, nestedMsgs, 10, 0, time.Unix(1, 0), 0}}}
	ents := block(10)
	ok := true
	ents.DaoProposalOutcomes = []transform.DaoProposalOutcome{{ID: 6, Status: "executed", Executed: &ok}}
	require.NoError(t, writer.NewLiveWriter(db, writer.SettledProposalActions).WriteBlock(context.Background(), ents))
	assert.Contains(t, ents.Dirty.Keys(statesync.Denoms), nestedDenom)
}
