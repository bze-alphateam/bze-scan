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

// daoStatement is the statement of tx whose SQL contains frag.
func daoStatement(t *testing.T, tx *mockTx, frag string) statement {
	t.Helper()
	for _, st := range tx.stmts {
		if strings.Contains(st.sql, frag) {
			return st
		}
	}
	t.Fatalf("no statement with %q", frag)
	return statement{}
}

func boolp(b bool) *bool { return &b }

// One flush holding a failed execution and its successful retry writes one
// change: the retry. An expiry after a failure keeps the failed run's
// outcome. The SQL never lets an older change replace a newer one.
func TestOneDaoOutcomePerProposalTheLatest(t *testing.T) {
	db := newMockDB(49)
	a, b := block(101), block(100)
	a.DaoProposalOutcomes = []transform.DaoProposalOutcome{
		{ID: 8, Status: "executed", Height: 101, TxIndex: 0, TxHash: "R", Yes: "9", No: "0", Abstain: "0", Denominator: "10",
			Executed: boolp(true)},
		{ID: 9, Status: "expired", Height: 101, TxIndex: 1, TxHash: "E", Yes: "5", No: "0", Abstain: "0", Denominator: "10"},
	}
	b.DaoProposalOutcomes = []transform.DaoProposalOutcome{
		{ID: 8, Status: "execution_failed", Height: 100, TxHash: "F", Yes: "9", No: "0", Abstain: "0", Denominator: "10",
			Executed: boolp(false), ExecutionError: "boom"},
		{ID: 9, Status: "execution_failed", Height: 100, TxIndex: 2, TxHash: "G", Yes: "5", No: "0", Abstain: "0", Denominator: "10",
			Executed: boolp(false), ExecutionError: "out of gas"},
		{ID: 10, Status: "cancelled", Height: 100, TxHash: "C", Cancelled: true},
	}
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{a, b}, writer.ModeInsert))

	st := daoStatement(t, db.txs[0], "cancelled_height, updated_at)")
	rows := payload(t, st)
	require.Len(t, rows, 3)
	assert.Equal(t, "executed", rows[0]["status"])
	assert.Equal(t, "R", rows[0]["tx_hash"])
	assert.Equal(t, true, rows[0]["executed"])
	assert.Nil(t, rows[0]["execution_error"], "a successful retry clears the error")
	assert.Equal(t, "expired", rows[1]["status"])
	assert.Equal(t, false, rows[1]["executed"], "the expiry keeps the failed run's outcome")
	assert.Equal(t, "out of gas", rows[1]["execution_error"])
	assert.Equal(t, "cancelled", rows[2]["status"])
	assert.Equal(t, true, rows[2]["cancelled"])
	assert.Nil(t, rows[2]["yes"], "a cancel has no tally")
	assert.Contains(t, st.sql, "p.status_height <= EXCLUDED.status_height", "an older change never replaces a newer one")
	assert.Contains(t, st.sql, "p.status = 'execution_failed' AND EXCLUDED.status IN ('executed', 'expired')",
		"over a status the sync set, only forward")
	assert.True(t, strings.HasPrefix(strings.TrimSpace(st.sql), "INSERT"), "a settle indexed before its submission writes a row")
}

// The submission only fills: it never writes the status.
func TestDaoProposalSubmissionFillsOnly(t *testing.T) {
	db := newMockDB(49)
	a := block(100)
	a.DaoProposals = []transform.DaoProposal{
		{ID: 5, DaoID: 2, Proposer: "bze1p", HasBody: true, Title: "T", Msgs: []byte(`[]`), Height: 100, TxHash: "S",
			VotingEnd: time.Unix(200, 0).UTC(), Denominator: "10"},
		{ID: 6, DaoID: 2, Proposer: "bze1p", Height: 100, Denominator: "10"},
	}
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{a}, writer.ModeUpdate))
	st := daoStatement(t, db.txs[0], "submit_height, submit_time, submit_tx_hash")
	rows := payload(t, st)
	require.Len(t, rows, 2)
	assert.Equal(t, "T", rows[0]["title"])
	assert.Nil(t, rows[1]["title"], "a submission without its body leaves the title to the sync")
	assert.Nil(t, rows[1]["voting_end"])
	assert.NotContains(t, st.sql, "status", "the submission never touches the status")
	assert.Contains(t, st.sql, "title = COALESCE(p.title, EXCLUDED.title)")
}

// Votes: the latest ballot per voter; reductions: every row, insert-only
// (a reindex updates them in place like the other history tables).
func TestDaoVotesAndReductions(t *testing.T) {
	db := newMockDB(49)
	a, b := block(101), block(100)
	a.DaoVotes = []transform.DaoVote{
		{ProposalID: 4, Voter: "bze1a", Option: "no", Weight: "300", Height: 101, TxIndex: 0},
		{ProposalID: 4, Voter: "bze1b", Option: "yes", Weight: "1", Height: 101, TxIndex: 1, MsgIndex: 0},
		{ProposalID: 4, Voter: "bze1b", Option: "abstain", Weight: "1", Height: 101, TxIndex: 1, MsgIndex: 1},
	}
	b.DaoVotes = []transform.DaoVote{{ProposalID: 4, Voter: "bze1a", Option: "yes", Weight: "600", Height: 100}}
	b.DaoBallotReductions = []transform.DaoBallotReduction{{Height: 100, TxIndex: 3, ProposalID: 4, Voter: "bze1a", Source: "sr:1", Amount: "50"}}
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{a, b}, writer.ModeUpdate))

	st := daoStatement(t, db.txs[0], "INSERT INTO explorer.dao_votes")
	rows := payload(t, st)
	require.Len(t, rows, 2)
	assert.Equal(t, "no", rows[0]["option"], "height 101 beats height 100")
	assert.Equal(t, "abstain", rows[1]["option"], "the later message")
	assert.Contains(t, st.sql, "(EXCLUDED.height, EXCLUDED.tx_index, EXCLUDED.msg_index) >= (v.height, v.tx_index, v.msg_index)")

	red := payload(t, daoStatement(t, db.txs[0], "INSERT INTO explorer.dao_ballot_reductions"))
	require.Len(t, red, 1)
	assert.Equal(t, "50", red[0]["amount"])
	daoStatement(t, db.txs[0], "UPDATE explorer.dao_ballot_reductions AS t")
}

func TestNoDaoProposalWritesWithoutRows(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{block(100)}, writer.ModeInsert))
	for _, st := range db.txs[0].stmts {
		assert.NotContains(t, st.sql, "dao_")
	}
}
