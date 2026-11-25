//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// daoMember is the second member of the static-list DAO #1 fixture.
const daoMember = "bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk"

// rawDaoEvent is a typed event whose values are given as JSON (bools,
// quoted integers).
func rawDaoEvent(typ, msgIndex string, kv ...string) node.Event {
	ev := node.Event{Type: typ}
	for i := 0; i < len(kv); i += 2 {
		ev.Attributes = append(ev.Attributes, node.Attribute{Key: kv[i], Value: kv[i+1]})
	}
	return withMsgIndex(ev, msgIndex)
}

func q(s string) string { return `"` + s + `"` }

func daoSubmittedEvent(id, daoID, den, votingEnd string, expedited bool) node.Event {
	return rawDaoEvent("bze.dao.EventProposalSubmitted", "0", "id", q(id), "dao_id", q(daoID), "proposer", q(daoAdmin),
		"expedited", fmt.Sprint(expedited), "voting_end", q(votingEnd), "denominator", q(den))
}

func daoVoteEvent(id, voter, option, weight string) node.Event {
	return daoEvent("bze.dao.EventVote", "0", "proposal_id", id, "voter", voter, "option", option, "weight", weight)
}

func daoReducedEvent(id, voter, amount string) node.Event {
	return daoEvent("bze.dao.EventBallotReduced", "0", "proposal_id", id, "voter", voter, "source", "sr:sr-1", "amount", amount)
}

func daoSettledEvents(id, status, yes, den string, ran *bool, early bool) []node.Event {
	var out []node.Event
	if ran != nil {
		reason := ""
		if !*ran {
			reason = "insufficient funds"
		}
		out = append(out, rawDaoEvent("bze.dao.EventProposalExecuted", "0", "id", q(id), "success", fmt.Sprint(*ran), "error", q(reason)))
	}
	return append(out, rawDaoEvent("bze.dao.EventProposalSettled", "0", "id", q(id), "status", q(status), "yes", q(yes),
		"no", q("0"), "abstain", q("0"), "denominator", q(den), "expedited_early", fmt.Sprint(early)))
}

// daoTxBlock transforms block h: one transaction per message, each with its
// events.
func daoTxBlock(t *testing.T, h int64, txs []sdk.Msg, events [][]node.Event) *transform.Entities {
	t.Helper()
	raws := make([]string, len(txs))
	results := make([]node.TxResult, len(txs))
	for i, m := range txs {
		raws[i] = v8RawTx(t, m)
		results[i] = node.TxResult{Events: events[i]}
	}
	in := transform.Input{
		Block: &node.Block{Height: h, Time: time.Date(2026, 12, 1, 9, 0, int(h%60), 0, time.UTC), Hash: fmt.Sprintf("%064X", h),
			ProposerAddress: "AA", Raw: json.RawMessage(`{}`), Txs: raws},
		Results: &node.BlockResults{Height: h, TxsResults: results},
		Commit:  &node.Commit{Height: h},
	}
	ents, err := newTransformer(t).Transform(in)
	require.NoError(t, err)
	return ents
}

func boolPtr(b bool) *bool { return &b }

// daoProposalBlocks: proposal #1 of the stake DAO #2 (votes, a ballot
// reduction, a revote) and proposal #2 of the static-list DAO #1, settled
// early and executed.
func daoProposalBlocks(t *testing.T) []*transform.Entities {
	t.Helper()
	send, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{FromAddress: daoTwo, ToAddress: daoAdmin,
		Amount: sdk.NewCoins(sdk.NewInt64Coin("ubze", 500000))})
	require.NoError(t, err)
	return []*transform.Entities{
		daoTxBlock(t, daoH2+1,
			[]sdk.Msg{
				&daotypes.MsgSubmitProposal{Proposer: daoAdmin, DaoId: 2, Title: "Fund the hive", Summary: "Pay the beekeeper from the treasury",
					Msgs: []*codectypes.Any{send}},
				&daotypes.MsgSubmitProposal{Proposer: daoAdmin, DaoId: 1, Title: "Welcome a member", Expedited: true},
			},
			[][]node.Event{
				{daoSubmittedEvent("1", "2", "1250000", "4070908800", false)},
				{daoSubmittedEvent("2", "1", "2", "1764666002", true)},
			}),
		daoTxBlock(t, daoH2+2,
			[]sdk.Msg{
				&daotypes.MsgVote{Voter: daoAdmin, ProposalId: 1, Option: daotypes.VoteOption_YES},
				&daotypes.MsgVote{Voter: daoMember, ProposalId: 1, Option: daotypes.VoteOption_NO},
				&daotypes.MsgVote{Voter: daoAdmin, ProposalId: 2, Option: daotypes.VoteOption_YES},
			},
			[][]node.Event{
				{daoVoteEvent("1", daoAdmin, "YES", "600000")},
				{daoVoteEvent("1", daoMember, "NO", "300000")},
				{daoVoteEvent("2", daoAdmin, "YES", "1")},
			}),
		// A stake exit takes 100000 out of the admin's ballot; then the member
		// changes their mind.
		daoTxBlock(t, daoH2+3,
			[]sdk.Msg{
				&daotypes.MsgUpdateMembers{Authority: daoAdmin, DaoId: 2},
				&daotypes.MsgVote{Voter: daoMember, ProposalId: 1, Option: daotypes.VoteOption_YES},
			},
			[][]node.Event{
				{daoReducedEvent("1", daoAdmin, "100000")},
				{daoVoteEvent("1", daoMember, "YES", "300000")},
			}),
		daoTxBlock(t, daoH2+4,
			[]sdk.Msg{&daotypes.MsgSettleProposal{Sender: daoMember, ProposalId: 2}},
			[][]node.Event{append([]node.Event{rawDaoEvent("bze.dao.EventDepositReleased", "0", "id", q("2"), "refunded", "true",
				"destination", q(daoAdmin), "amount", q("1000ubze"))}, daoSettledEvents("2", "EXECUTED", "1", "2", boolPtr(true), true)...)}),
	}
}

func (e *syncEnv) daoProposalRows(t *testing.T) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', id, dao_id, status, title, submit_height, settled_height,
		executed::int, deposit_refunded::int, expedited_early::int, yes, cleaned_height) FROM explorer.dao_proposals ORDER BY id`)
}

func (e *syncEnv) daoVoteRows(t *testing.T) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', proposal_id, voter = $1, option, weight, height, tx_index)
		FROM explorer.dao_votes ORDER BY proposal_id, voter`, daoAdmin)
}

func TestDaoProposalsAreIndexedSyncedAndServed(t *testing.T) {
	e := newSyncEnv(t)
	indexTheDaos(t, e)
	pool, err := pgxpool.New(context.Background(), e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	ents := daoProposalBlocks(t)
	for _, b := range ents {
		require.NoError(t, writer.NewLiveWriter(pool).WriteBlock(context.Background(), b))
	}

	// The events alone: the submissions with their bodies, the settle.
	rows := e.daoProposalRows(t)
	assert.Equal(t, []string{
		"1 2 voting Fund the hive 25100002",
		"2 1 executed Welcome a member 25100002 25100005 1 1 1 1",
	}, rows)
	votes := e.daoVoteRows(t)
	assert.Equal(t, []string{
		"1 false yes 300000 25100004 1", // the member's revote replaced their no
		"1 true yes 600000 25100003 0",  // never lowered by the reduction
		"2 true yes 1 25100003 2",
	}, votes)
	assert.Equal(t, []string{"1 100000"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', proposal_id, amount) FROM explorer.dao_ballot_reductions`))

	// Reindexing the same heights changes nothing.
	require.NoError(t, writer.NewBatchWriter(pool).Write(context.Background(), ents, writer.ModeUpdate))
	assert.Equal(t, rows, e.daoProposalRows(t), "a reindex of the same heights")
	assert.Equal(t, votes, e.daoVoteRows(t))
	assert.Equal(t, []string{"1"}, queryStrings(t, e.db, `SELECT count(*)::text FROM explorer.dao_ballot_reductions`))

	// The state sync adds the running tally and the pinned config of the
	// voting proposal.
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	assert.Equal(t, []string{"800000 1 0"}, queryStrings(t, e.db, `SELECT concat_ws(' ', yes, (pinned_config IS NOT NULL)::int,
		(tally_updated_at IS NULL)::int) FROM explorer.dao_proposals WHERE id = 1`))
	assert.Empty(t, queryStrings(t, e.db, `SELECT last_error FROM explorer.sync_jobs WHERE job = 'dao_proposals' AND last_error IS NOT NULL`))

	base := e.serveAPI(t)
	list := into[dto.DaoList](t, fetch(t, base+"/daos"))
	require.Len(t, list.Items, 2)
	assert.Equal(t, int64(1), list.Items[0].ProposalsOpen, "DAO #2 has one proposal in voting")
	assert.Equal(t, int64(0), list.Items[1].ProposalsOpen)
	assert.Equal(t, int64(1), into[dto.Dao](t, fetch(t, base+"/daos/2")).ProposalsOpen)

	props := into[dto.List[dto.DaoProposalSummary]](t, fetch(t, base+"/daos/2/proposals"))
	require.Len(t, props.Items, 1)
	assert.Equal(t, int64(1), props.Items[0].ID)
	assert.Equal(t, []string{"/cosmos.bank.v1beta1.MsgSend"}, props.Items[0].MessageTypes)
	assert.Empty(t, into[dto.List[dto.DaoProposalSummary]](t, fetch(t, base+"/daos/2/proposals?status=executed")).Items)
	assert.Len(t, into[dto.List[dto.DaoProposalSummary]](t, fetch(t, base+"/daos/1/proposals?status=executed")).Items, 1)
	assert.Equal(t, http.StatusNotFound, fetch(t, base+"/daos/99/proposals").status)

	r := fetch(t, base+"/dao-proposals/1")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	p := into[dto.DaoProposal](t, r)
	assert.Equal(t, &dto.DaoRef{ID: 2, Name: "Stakers club", Type: "stake"}, p.Dao)
	assert.Equal(t, "Fund the hive", *p.Title)
	assert.Equal(t, "voting", p.Status)
	assert.False(t, p.VotingEnded)
	assert.Equal(t, int64(2), p.VotesCount)
	assert.Equal(t, "800000", *p.Yes)
	assert.Equal(t, "1250000", *p.Denominator)
	six := 6
	assert.Equal(t, &dto.WeightDenom{Denom: "ubze", Symbol: strp("BZE"), Exponent: &six}, p.WeightDenom)
	require.NotNil(t, p.Checks)
	assert.True(t, p.Checks.Regular.QuorumMet, "800000 of 1250000 is 0.64, over 0.4")
	assert.True(t, p.Checks.Regular.Passes)
	assert.Nil(t, p.Checks.Expedite, "not expedited")
	assert.JSONEq(t, `[{"@type":"/cosmos.bank.v1beta1.MsgSend","from_address":"`+daoTwo+`","to_address":"`+daoAdmin+
		`","amount":[{"denom":"ubze","amount":"500000"}]}]`, string(p.Msgs))

	settledP := into[dto.DaoProposal](t, fetch(t, base+"/dao-proposals/2"))
	assert.Equal(t, "executed", settledP.Status)
	assert.Equal(t, boolPtr(true), settledP.Executed)
	assert.Equal(t, boolPtr(true), settledP.ExpeditedEarly)
	assert.Equal(t, boolPtr(true), settledP.DepositRefunded)
	assert.Equal(t, strp("1000ubze"), settledP.DepositAmount)
	assert.Nil(t, settledP.WeightDenom, "a static-list DAO counts member votes")
	assert.Nil(t, settledP.Checks, "settled before the sync saw its pinned config")
	assert.Equal(t, http.StatusNotFound, fetch(t, base+"/dao-proposals/99").status)

	// Votes, heaviest effective weight first: the admin's 600000 less the
	// later 100000 reduction, then the member's revote (after the
	// reduction: untouched).
	page := into[dto.DaoVoteList](t, fetch(t, base+"/dao-proposals/1/votes?limit=1"))
	require.Len(t, page.Items, 1)
	assert.Equal(t, daoAdmin, page.Items[0].Voter)
	assert.Equal(t, "600000", page.Items[0].Weight)
	assert.Equal(t, "500000", page.Items[0].EffectiveWeight)
	require.NotNil(t, page.NextCursor)
	rest := into[dto.DaoVoteList](t, fetch(t, base+"/dao-proposals/1/votes?limit=1&cursor="+*page.NextCursor))
	require.Len(t, rest.Items, 1)
	assert.Equal(t, daoMember, rest.Items[0].Voter)
	assert.Equal(t, "300000", rest.Items[0].EffectiveWeight)
	assert.Nil(t, rest.NextCursor)
	assert.Empty(t, into[dto.DaoVoteList](t, fetch(t, base+"/dao-proposals/1/votes?option=no")).Items)
}

// The backfill walks down: a proposal's later events are written before its
// earlier ones. A failed execution retried successfully ends executed; a
// settle written before the submission is not lost; the effective weight
// counts only the reductions after the ballot that stands.
func TestDaoProposalHistoryWrittenInAnyOrder(t *testing.T) {
	e := newSyncEnv(t)
	pool, err := pgxpool.New(context.Background(), e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	const h = int64(25_200_000)
	vote := func(option daotypes.VoteOption, name, weight string) ([]sdk.Msg, [][]node.Event) {
		return []sdk.Msg{&daotypes.MsgVote{Voter: daoAdmin, ProposalId: 5, Option: option}},
			[][]node.Event{{daoVoteEvent("5", daoAdmin, name, weight)}}
	}
	reduce := func(amount string) ([]sdk.Msg, [][]node.Event) {
		return []sdk.Msg{&daotypes.MsgUpdateMembers{Authority: daoAdmin, DaoId: 1}}, [][]node.Event{{daoReducedEvent("5", daoAdmin, amount)}}
	}
	settle := func(status string, ran bool) ([]sdk.Msg, [][]node.Event) {
		return []sdk.Msg{&daotypes.MsgSettleProposal{Sender: daoAdmin, ProposalId: 5}},
			[][]node.Event{daoSettledEvents("5", status, "60", "100", &ran, false)}
	}
	steps := []func() ([]sdk.Msg, [][]node.Event){
		func() ([]sdk.Msg, [][]node.Event) {
			return []sdk.Msg{&daotypes.MsgSubmitProposal{Proposer: daoAdmin, DaoId: 1, Title: "Order"}},
				[][]node.Event{{daoSubmittedEvent("5", "1", "100", "1764666002", false)}}
		},
		func() ([]sdk.Msg, [][]node.Event) { return vote(daotypes.VoteOption_YES, "YES", "100") },
		func() ([]sdk.Msg, [][]node.Event) { return reduce("40") }, // before the revote: already in its weight
		func() ([]sdk.Msg, [][]node.Event) { return vote(daotypes.VoteOption_NO, "NO", "60") },
		func() ([]sdk.Msg, [][]node.Event) { return reduce("10") }, // after it: counts
		func() ([]sdk.Msg, [][]node.Event) { return settle("EXECUTION_FAILED", false) },
		func() ([]sdk.Msg, [][]node.Event) { return settle("EXECUTED", true) },
	}
	var blocks []*transform.Entities
	for i, step := range steps {
		msgs, events := step()
		blocks = append(blocks, daoTxBlock(t, h+int64(i), msgs, events))
	}
	for i := len(blocks) - 1; i >= 0; i-- {
		require.NoError(t, writer.NewBatchWriter(pool).Write(context.Background(), blocks[i:i+1], writer.ModeInsert))
	}

	assert.Equal(t, []string{"5 1 executed Order 25200000 25200006 1 60"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', id, dao_id, status, title, submit_height, settled_height, executed::int, yes)
		   FROM explorer.dao_proposals`), "the retry stands; the submission filled the settle's row")
	assert.Equal(t, []string{"no 60 25200003"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', option, weight, height) FROM explorer.dao_votes`), "the later ballot stands")

	// A reindex of the failed settle does not move it back.
	require.NoError(t, writer.NewBatchWriter(pool).Write(context.Background(), blocks[5:6], writer.ModeUpdate))
	assert.Equal(t, []string{"executed 25200006"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', status, settled_height) FROM explorer.dao_proposals`))

	votes := into[dto.DaoVoteList](t, fetch(t, e.serveAPI(t)+"/dao-proposals/5/votes"))
	require.Len(t, votes.Items, 1)
	assert.Equal(t, "60", votes.Items[0].Weight)
	assert.Equal(t, "50", votes.Items[0].EffectiveWeight, "only the reduction after the revote")
}

// The facts of every settle merge whatever the order the backfill writes
// them in: an early first settle that failed stays early after its retry;
// an expiry keeps the failed run's outcome.
func TestDaoSettleFactsMergeInAnyOrder(t *testing.T) {
	e := newSyncEnv(t)
	pool, err := pgxpool.New(context.Background(), e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	type settle struct {
		status string
		ran    *bool
		early  bool
	}
	scenarios := []struct {
		name  string
		steps []settle
		want  string
	}{
		{"early failure then retry", []settle{{"EXECUTION_FAILED", boolPtr(false), true}, {"EXECUTED", boolPtr(true), false}},
			"executed 1 1"},
		{"failure then expiry", []settle{{"EXECUTION_FAILED", boolPtr(false), false}, {"EXPIRED", nil, false}},
			"expired 0 0 insufficient funds"},
	}
	h := int64(25_300_000)
	id := 20
	for _, sc := range scenarios {
		for _, newestFirst := range []bool{false, true} {
			id++
			pid := fmt.Sprint(id)
			var blocks []*transform.Entities
			for _, st := range sc.steps {
				blocks = append(blocks, daoTxBlock(t, h, []sdk.Msg{&daotypes.MsgSettleProposal{Sender: daoAdmin, ProposalId: uint64(id)}},
					[][]node.Event{daoSettledEvents(pid, st.status, "9", "10", st.ran, st.early)}))
				h++
			}
			if newestFirst {
				for i, j := 0, len(blocks)-1; i < j; i, j = i+1, j-1 {
					blocks[i], blocks[j] = blocks[j], blocks[i]
				}
			}
			for _, b := range blocks {
				require.NoError(t, writer.NewBatchWriter(pool).Write(context.Background(), []*transform.Entities{b}, writer.ModeInsert))
			}
			got := queryStrings(t, e.db, `SELECT concat_ws(' ', status, executed::int, expedited_early::int, execution_error)
				FROM explorer.dao_proposals WHERE id = $1`, id)
			assert.Equal(t, []string{sc.want}, got, "%s, newest first: %v", sc.name, newestFirst)
			assert.Equal(t, []string{fmt.Sprint(h - 1)}, queryStrings(t, e.db,
				`SELECT settled_height::text FROM explorer.dao_proposals WHERE id = $1`, id), "the latest settle's height")
		}
	}
}
