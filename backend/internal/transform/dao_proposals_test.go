package transform_test

import (
	"testing"
	"time"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

const voterA = "bze1voteravoteravoteravoteravoteravotera0"

func submitted(msgIndex, id, daoID string, expedited bool) node.Event {
	exp := "false"
	if expedited {
		exp = "true"
	}
	return typedRaw("bze.dao.EventProposalSubmitted", msgIndex, "id", `"`+id+`"`, "dao_id", `"`+daoID+`"`,
		"proposer", `"`+creator+`"`, "expedited", exp, "voting_end", `"1700086400"`, "denominator", `"1000"`)
}

func settled(msgIndex, id, status, yes, no, abstain string, early bool) node.Event {
	e := "false"
	if early {
		e = "true"
	}
	return typedRaw("bze.dao.EventProposalSettled", msgIndex, "id", `"`+id+`"`, "status", `"`+status+`"`,
		"yes", `"`+yes+`"`, "no", `"`+no+`"`, "abstain", `"`+abstain+`"`, "denominator", `"1000"`, "expedited_early", e)
}

func executedEv(msgIndex, id string, ok bool, reason string) node.Event {
	s := "false"
	if ok {
		s = "true"
	}
	return typedRaw("bze.dao.EventProposalExecuted", msgIndex, "id", `"`+id+`"`, "success", s, "error", `"`+reason+`"`)
}

func voteEv(msgIndex, id, voter, option, weight string) node.Event {
	return typed("bze.dao.EventVote", msgIndex, "proposal_id", id, "voter", voter, "option", option, "weight", weight)
}

func reducedEv(msgIndex, id, voter, source, amount string) node.Event {
	return typed("bze.dao.EventBallotReduced", msgIndex, "proposal_id", id, "voter", voter, "source", source, "amount", amount)
}

// The submission's row comes from the event and the MsgSubmitProposal body:
// title, summary and msgs as proto JSON.
func TestDaoProposalSubmissionReadsTheBody(t *testing.T) {
	send, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{FromAddress: daoAddr, ToAddress: creator,
		Amount: sdk.NewCoins(sdk.NewInt64Coin("ubze", 5))})
	require.NoError(t, err)
	raw := encodedTx(t, &daotypes.MsgSubmitProposal{Proposer: creator, DaoId: 3, Title: "Pay the beekeeper",
		Summary: "Five BZE", Msgs: []*codectypes.Any{send}, Expedited: true})
	ents := transformDaoTx(t, []node.Event{submitted("0", "11", "3", true)}, raw)
	require.Len(t, ents.DaoProposals, 1)
	p := ents.DaoProposals[0]
	assert.Equal(t, int64(11), p.ID)
	assert.Equal(t, int64(3), p.DaoID)
	assert.Equal(t, creator, p.Proposer)
	assert.True(t, p.Expedited)
	assert.Equal(t, time.Unix(1_700_086_400, 0).UTC(), p.VotingEnd)
	assert.Equal(t, "1000", p.Denominator)
	assert.True(t, p.HasBody)
	assert.Equal(t, "Pay the beekeeper", p.Title)
	assert.Equal(t, "Five BZE", p.Summary)
	assert.JSONEq(t, `[{"@type":"/cosmos.bank.v1beta1.MsgSend","from_address":"`+daoAddr+`","to_address":"`+creator+
		`","amount":[{"denom":"ubze","amount":"5"}]}]`, string(p.Msgs))
	assert.Equal(t, int64(10), p.Height)
	assert.Equal(t, ents.Transactions[0].Hash, p.TxHash)
	assert.Equal(t, []string{"11"}, ents.Dirty.Keys(statesync.DaoProposals))
	assert.Empty(t, ents.DaoEvents, "proposal events are not DAO history rows")
}

// A submission the transaction's own message does not carry (an authz
// MsgExec, an admin execution, another DAO's proposal) leaves the body to
// the state sync.
func TestNestedDaoProposalSubmissionHasNoBody(t *testing.T) {
	raw := encodedTx(t, &daotypes.MsgAdminExecute{Admin: creator, DaoId: 3})
	ents := transformDaoTx(t, []node.Event{submitted("0", "12", "3", false)}, raw)
	require.Len(t, ents.DaoProposals, 1)
	assert.False(t, ents.DaoProposals[0].HasBody)
	assert.Empty(t, ents.DaoProposals[0].Title)
	assert.Nil(t, ents.DaoProposals[0].Msgs)
}

// Votes, a revote and ballot reductions: every event is a row, seq counts
// the reductions per message, the writer keeps the latest ballot.
func TestDaoVotesRevotesAndReductions(t *testing.T) {
	raw := encodedTx(t,
		&daotypes.MsgVote{Voter: voterA, ProposalId: 4, Option: daotypes.VoteOption_YES},
		&daotypes.MsgVote{Voter: voterA, ProposalId: 4, Option: daotypes.VoteOption_NO},
		&daotypes.MsgUpdateMembers{Authority: creator, DaoId: 1},
	)
	ents := transformDaoTx(t, []node.Event{
		voteEv("0", "4", voterA, "YES", "600"),
		voteEv("1", "4", voterA, "NO", "600"),
		reducedEv("2", "4", voterA, "member", "1"),
		reducedEv("2", "5", voterA, "member", "1"),
	}, raw)
	require.Len(t, ents.DaoVotes, 2)
	assert.Equal(t, "yes", ents.DaoVotes[0].Option)
	assert.Equal(t, "no", ents.DaoVotes[1].Option)
	assert.Equal(t, "600", ents.DaoVotes[1].Weight)
	assert.Equal(t, 1, ents.DaoVotes[1].MsgIndex)
	require.Len(t, ents.DaoBallotReductions, 2)
	r := ents.DaoBallotReductions[1]
	assert.Equal(t, transform.DaoBallotReduction{Height: 10, TxIndex: 0, MsgIndex: 2, Seq: 1, ProposalID: 5, Voter: voterA,
		Source: "member", Amount: "1", TxHash: ents.Transactions[0].Hash}, r)
	assert.Equal(t, 0, ents.DaoBallotReductions[0].Seq)
	assert.ElementsMatch(t, []string{"4", "5"}, ents.Dirty.Keys(statesync.DaoProposals))
}

// A settle that passes and runs: the deposit, the run and the settle are
// one outcome with the tally.
func TestDaoProposalSettledPassedAndExecuted(t *testing.T) {
	raw := encodedTx(t, &daotypes.MsgSettleProposal{Sender: creator, ProposalId: 6})
	ents := transformDaoTx(t, []node.Event{
		typedRaw("bze.dao.EventDepositReleased", "0", "id", `"6"`, "refunded", "true", "destination", `"`+creator+`"`, "amount", `"100ubze"`),
		executedEv("0", "6", true, ""),
		settled("0", "6", "EXECUTED", "700", "100", "50", false),
	}, raw)
	require.Len(t, ents.DaoProposalOutcomes, 1)
	o := ents.DaoProposalOutcomes[0]
	ok := true
	assert.Equal(t, transform.DaoProposalOutcome{ID: 6, Status: "executed", Height: 10, TxIndex: 0, MsgIndex: 0, Seq: 2,
		TxHash: ents.Transactions[0].Hash, Time: ents.Blocks[0].Time, Yes: "700", No: "100", Abstain: "50", Denominator: "1000",
		Executed: &ok}, o)
	assert.Equal(t, []transform.DaoProposalDeposit{{ID: 6, Refunded: true, Destination: creator, Amount: "100ubze", Height: 10}},
		ents.DaoProposalDeposits)
}

func TestDaoProposalSettledRejectedRunsNothing(t *testing.T) {
	raw := encodedTx(t, &daotypes.MsgSettleProposal{Sender: creator, ProposalId: 7})
	ents := transformDaoTx(t, []node.Event{settled("0", "7", "REJECTED", "1", "2", "0", false)}, raw)
	require.Len(t, ents.DaoProposalOutcomes, 1)
	assert.Equal(t, "rejected", ents.DaoProposalOutcomes[0].Status)
	assert.Nil(t, ents.DaoProposalOutcomes[0].Executed)
}

// A failed execution, then its retry and the expiry of another.
func TestDaoProposalExecutionFailedRetriedAndExpired(t *testing.T) {
	raw := encodedTx(t,
		&daotypes.MsgSettleProposal{Sender: creator, ProposalId: 8},
		&daotypes.MsgSettleProposal{Sender: creator, ProposalId: 9},
	)
	ents := transformDaoTx(t, []node.Event{
		executedEv("0", "8", false, "insufficient funds"),
		settled("0", "8", "EXECUTION_FAILED", "9", "0", "0", true),
		settled("1", "9", "EXPIRED", "5", "0", "0", false),
	}, raw)
	require.Len(t, ents.DaoProposalOutcomes, 2)
	failed := ents.DaoProposalOutcomes[0]
	assert.Equal(t, "execution_failed", failed.Status)
	require.NotNil(t, failed.Executed)
	assert.False(t, *failed.Executed)
	assert.Equal(t, "insufficient funds", failed.ExecutionError)
	assert.True(t, failed.ExpeditedEarly)
	assert.Equal(t, "expired", ents.DaoProposalOutcomes[1].Status)
	assert.Nil(t, ents.DaoProposalOutcomes[1].Executed, "an expiry runs nothing")
}

func TestDaoProposalCancelledAndCleaned(t *testing.T) {
	raw := encodedTx(t,
		&daotypes.MsgCancelProposal{Proposer: creator, ProposalId: 10},
		&daotypes.MsgCleanupProposal{Sender: creator, ProposalId: 3},
		&daotypes.MsgCleanupProposal{Sender: creator, ProposalId: 2},
	)
	ents := transformDaoTx(t, []node.Event{
		typedRaw("bze.dao.EventDepositReleased", "0", "id", `"10"`, "refunded", "false", "destination", `"bze1burner"`, "amount", `"5ubze"`),
		typed("bze.dao.EventProposalCancelled", "0", "id", "10"),
		typedRaw("bze.dao.EventProposalCleaned", "1", "id", `"3"`, "processed", `"100"`, "completed", "false"),
		typedRaw("bze.dao.EventProposalCleaned", "2", "id", `"2"`, "processed", `"4"`, "completed", "true"),
	}, raw)
	require.Len(t, ents.DaoProposalOutcomes, 1)
	c := ents.DaoProposalOutcomes[0]
	assert.True(t, c.Cancelled)
	assert.Equal(t, "cancelled", c.Status)
	assert.False(t, ents.DaoProposalDeposits[0].Refunded)
	assert.Equal(t, []transform.DaoProposalCleaned{{ID: 2, Height: 10}}, ents.DaoProposalsCleaned,
		"only the batch that completes the cleanup")
	assert.ElementsMatch(t, []string{"10", "3", "2"}, ents.Dirty.Keys(statesync.DaoProposals))
}

// At block level (governance running a DAO message): no transaction, no
// message index.
func TestBlockLevelDaoProposalEvents(t *testing.T) {
	in := baseInput()
	in.Block.Txs = nil
	in.Results.TxsResults = nil
	in.Results.FinalizeBlockEvents = []node.Event{
		typedRaw("bze.dao.EventProposalSubmitted", "", "id", `"13"`, "dao_id", `"1"`, "proposer", `"`+daoAddr+`"`,
			"expedited", "false", "voting_end", `"1700086400"`, "denominator", `"3"`),
		typed("bze.dao.EventVote", "", "proposal_id", "13", "voter", daoAddr, "option", "ABSTAIN", "weight", "1"),
	}
	tr, _ := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	require.Len(t, ents.DaoProposals, 1)
	assert.Empty(t, ents.DaoProposals[0].TxHash)
	assert.False(t, ents.DaoProposals[0].HasBody)
	require.Len(t, ents.DaoVotes, 1)
	assert.Equal(t, -1, ents.DaoVotes[0].TxIndex)
	assert.Equal(t, -1, ents.DaoVotes[0].MsgIndex)
	assert.Equal(t, "abstain", ents.DaoVotes[0].Option)
}

// An event without a usable id is skipped.
func TestDaoProposalEventWithoutAnIdIsSkipped(t *testing.T) {
	raw := encodedTx(t, &daotypes.MsgSettleProposal{Sender: creator, ProposalId: 1})
	ents := transformDaoTx(t, []node.Event{typed("bze.dao.EventProposalCancelled", "0", "id", "x")}, raw)
	assert.Empty(t, ents.DaoProposalOutcomes)
	assert.Empty(t, ents.Dirty.Keys(statesync.DaoProposals))
}
