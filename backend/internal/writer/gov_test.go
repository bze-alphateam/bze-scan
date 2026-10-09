package writer_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

func vote(h int64, txIndex, msgIndex int, voter, option string) transform.ProposalVote {
	return transform.ProposalVote{ProposalID: 47, Voter: voter, Option: option,
		Options: json.RawMessage(`[{"option":1,"weight":"1.000000000000000000"}]`),
		Height:  h, TxIndex: txIndex, MsgIndex: msgIndex, Time: time.Unix(h, 0)}
}

// govBlock carries one of each governance row.
func govBlock(h int64) *transform.Entities {
	ents := block(h)
	ents.Proposals = []transform.ProposalSubmission{{ID: 47, Title: "Upgrade", Kind: "software_upgrade",
		MessageTypes: []string{"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade"}, Messages: json.RawMessage(`[{"@type":"x"}]`),
		Status: "voting_period", Proposer: "bze1p", Height: h, TxHash: "H0", Time: time.Unix(h, 0)}}
	ents.ProposalDeposits = []transform.ProposalDeposit{{ProposalID: 47, Depositor: "bze1p", Height: h, TxIndex: 0,
		Amount: []chain.Coin{{Denom: "ubze", Amount: "100"}}, Time: time.Unix(h, 0)}}
	ents.ProposalVotes = []transform.ProposalVote{vote(h, 0, 0, "bze1v", "yes")}
	ents.ProposalStatuses = []transform.ProposalStatus{{ProposalID: 47, Status: "passed", Height: h, Resolved: true}}
	return ents
}

func govStatement(t *testing.T, tx *mockTx, prefix string) statement {
	t.Helper()
	for _, st := range tx.stmts {
		if strings.HasPrefix(strings.TrimSpace(st.sql), prefix) {
			return st
		}
	}
	t.Fatalf("no statement %q", prefix)
	return statement{}
}

func TestGovernanceRowsJoinTheBlockTransaction(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewLiveWriter(db).WriteBlock(context.Background(), govBlock(100)))
	tx := db.txs[0]
	var order []string
	for _, st := range tx.stmts {
		for _, name := range []string{"INSERT INTO explorer.proposals AS p", "INSERT INTO explorer.proposal_deposits",
			"INSERT INTO explorer.proposal_votes", "UPDATE explorer.proposals AS p"} {
			if strings.Contains(st.sql, name) {
				order = append(order, name)
			}
		}
	}
	assert.Equal(t, []string{"INSERT INTO explorer.proposals AS p", "INSERT INTO explorer.proposal_deposits",
		"INSERT INTO explorer.proposal_votes", "UPDATE explorer.proposals AS p"}, order,
		"the status update after the insert, so a proposal submitted and resolved in one flush ends resolved")
	assert.True(t, tx.committed)

	p := payload(t, govStatement(t, tx, "INSERT INTO explorer.proposals"))
	assert.Equal(t, []map[string]any{{
		"id": 47.0, "title": "Upgrade", "summary": nil, "metadata": nil, "proposer": "bze1p", "kind": "software_upgrade",
		"message_types": []any{"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade"}, "messages": []any{map[string]any{"@type": "x"}},
		"status": "voting_period", "expedited": false, "time": "1970-01-01T00:01:40Z", "height": 100.0, "tx_hash": "H0",
	}}, p)
	d := payload(t, govStatement(t, tx, "INSERT INTO explorer.proposal_deposits"))
	assert.Equal(t, []any{map[string]any{"denom": "ubze", "amount": "100"}}, d[0]["amount"])
}

// One statement cannot upsert a key twice: a flush keeps each voter's
// latest vote, by height, then transaction, then message.
func TestOnlyTheLatestVoteOfAVoterIsWritten(t *testing.T) {
	db := newMockDB(49)
	a, b := block(101), block(100)
	a.ProposalVotes = []transform.ProposalVote{vote(101, 0, 0, "bze1v", "no"), vote(101, 2, 1, "bze1w", "abstain"),
		vote(101, 2, 0, "bze1w", "yes")}
	b.ProposalVotes = []transform.ProposalVote{vote(100, 5, 0, "bze1v", "yes")}
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{a, b}, writer.ModeInsert))

	st := govStatement(t, db.txs[0], "INSERT INTO explorer.proposal_votes")
	rows := payload(t, st)
	require.Len(t, rows, 2)
	assert.Equal(t, "bze1v", rows[0]["voter"])
	assert.Equal(t, "no", rows[0]["option"], "height 101 beats height 100")
	assert.Equal(t, "bze1w", rows[1]["voter"])
	assert.Equal(t, "abstain", rows[1]["option"], "the later message of the same transaction")
	assert.Contains(t, st.sql, "WHERE (EXCLUDED.height, EXCLUDED.tx_index) >= (v.height, v.tx_index)",
		"an earlier vote written later (the backfill walks down) never replaces a stored one")
	assert.Contains(t, st.sql, "IS DISTINCT FROM", "the same vote written again changes nothing")
}

func TestOneStatusChangePerProposal(t *testing.T) {
	db := newMockDB(49)
	a, b := block(101), block(100)
	a.ProposalStatuses = []transform.ProposalStatus{
		{ProposalID: 48, Status: "voting_period", Height: 101},
		{ProposalID: 48, Status: "passed", Height: 101, Resolved: true},
		{ProposalID: 47, Status: "voting_period", Height: 101, ExpeditedOff: true},
	}
	b.ProposalStatuses = []transform.ProposalStatus{{ProposalID: 47, Status: "voting_period", Height: 100}}
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{a, b}, writer.ModeUpdate))

	st := govStatement(t, db.txs[0], "UPDATE explorer.proposals AS p")
	assert.Equal(t, []map[string]any{
		{"id": 47.0, "status": "voting_period", "height": 101.0, "resolved": false, "expedited_off": true},
		{"id": 48.0, "status": "passed", "height": 101.0, "resolved": true, "expedited_off": false},
	}, payload(t, st), "the highest height; at one height EndBlock's resolution after the transactions")
	assert.Contains(t, st.sql, "p.resolved_height IS NULL OR p.resolved_height <= r.height", "a resolution never goes back")
	assert.Contains(t, st.sql, "p.resolved_height IS NULL AND p.status = 'deposit_period'",
		"the voting period opening never undoes a resolution")
}

func TestProposalInsertOnlyFillsWhatTheSyncLacks(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), []*transform.Entities{govBlock(100)}, writer.ModeUpdate))
	st := govStatement(t, db.txs[0], "INSERT INTO explorer.proposals")
	assert.Contains(t, st.sql, "submit_height = COALESCE(p.submit_height, EXCLUDED.submit_height)")
	assert.NotContains(t, st.sql, "status = EXCLUDED.status", "the status is the sync's and the events' to move")
	deposits := 0
	for _, s := range db.txs[0].stmts {
		if strings.Contains(s.sql, "UPDATE explorer.proposal_deposits AS t") {
			deposits++
		}
	}
	assert.Equal(t, 1, deposits, "deposits are rewritten by a reindex like every event table")
}
