//go:build e2e

package e2e_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// The recorded governance heights of proposal 47 (an upgrade to v8.1.1).
const (
	hSubmit           int64 = 23745024 // MsgSubmitProposal with the whole deposit: voting opens
	hVoteVal          int64 = 23745061 // a yes by the owner of BZE Alpha Team
	hVote             int64 = 23745456 // a yes by a delegator
	hResolve          int64 = 23821225 // active_proposal: passed
	alphaOwner              = "bze1k27v68x9gtppsgt9scr3649tpjfxnqlj04lk9x"
	alphaOperator           = "bzevaloper1k27v68x9gtppsgt9scr3649tpjfxnqljh3qa3e"
	recordedProposals       = 47
)

// votingAgain turns proposal 47 of a recorded gov answer (the Proposal one,
// or the Proposals list) back into its voting period, as the node answered
// before it was tallied.
func votingAgain(t *testing.T, method, key string) []byte {
	t.Helper()
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(fakenode.ReadGRPCFixture(t, "gov", method, key), &body))
	open := func(raw json.RawMessage) json.RawMessage {
		var p map[string]any
		require.NoError(t, json.Unmarshal(raw, &p))
		if p["id"] == "47" {
			p["status"] = "PROPOSAL_STATUS_VOTING_PERIOD"
			p["final_tally_result"] = map[string]string{"yes_count": "0", "abstain_count": "0", "no_count": "0", "no_with_veto_count": "0"}
		}
		out, err := json.Marshal(p)
		require.NoError(t, err)
		return out
	}
	if raw, ok := body["proposal"]; ok {
		body["proposal"] = open(raw)
	} else {
		var list []json.RawMessage
		require.NoError(t, json.Unmarshal(body["proposals"], &list))
		for i := range list {
			list[i] = open(list[i])
		}
		out, err := json.Marshal(list)
		require.NoError(t, err)
		body["proposals"] = out
	}
	out, err := json.Marshal(body)
	require.NoError(t, err)
	return out
}

func TestProposalsVotesAndTheFrozenTally(t *testing.T) {
	e := newSyncEnv(t)
	e.grpc.SetResponse("gov", "Proposal", "47", votingAgain(t, "Proposal", "47"))
	e.grpc.SetResponse("gov", "Proposals", "", votingAgain(t, "Proposals", ""))

	// Proposal 47 submitted and voted on; the sync lists every proposal, so
	// the 46 submitted before the indexed heights exist too.
	e.index(t, hSubmit, hVoteVal, hVote)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	assert.Equal(t, recordedProposals, e.count(t, `SELECT count(*) FROM explorer.proposals`))
	base := e.serveAPI(t)

	list := into[dto.List[dto.ProposalSummary]](t, fetch(t, base+"/proposals?limit=5"))
	require.Len(t, list.Items, 5)
	assert.Equal(t, int64(47), list.Items[0].ID, "newest first")
	require.NotNil(t, list.NextCursor)
	voting := into[dto.List[dto.ProposalSummary]](t, fetch(t, base+"/proposals?status=voting_period"))
	require.Len(t, voting.Items, 1)
	assert.Equal(t, "software_upgrade", voting.Items[0].Kind)
	require.NotNil(t, voting.Items[0].Tally, "the running tally")
	require.NotNil(t, voting.Items[0].TurnoutPct)
	assert.Equal(t, "72.49317", *voting.Items[0].TurnoutPct, "against the bonded tokens of the staking pool")

	r := fetch(t, base+"/proposals/47")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	p := into[dto.Proposal](t, r)
	assert.Equal(t, "voting_period", p.Status)
	assert.Equal(t, hSubmit, *p.SubmitHeight, "from the indexed submission")
	assert.NotEmpty(t, *p.SubmitTxHash)
	assert.Equal(t, "147027371221650", *p.TallyBondedTokens)
	assert.Equal(t, []string{"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade"}, p.MessageTypes)
	assert.JSONEq(t, `[{"denom":"ubze","amount":"100000000000"}]`, string(p.TotalDeposit))
	assert.Equal(t, 1, p.ValidatorsVoted.Voted, "one of the two voters owns a bonded validator")
	assert.Positive(t, p.ValidatorsVoted.Total)
	assert.Nil(t, p.ResolvedHeight)

	votes := into[dto.List[dto.ProposalVote]](t, fetch(t, base+"/proposals/47/votes"))
	require.Len(t, votes.Items, 2)
	assert.Equal(t, hVote, votes.Items[0].Height, "newest first")
	assert.Nil(t, votes.Items[0].Validator, "a delegator")
	val := votes.Items[1]
	assert.Equal(t, alphaOwner, val.Voter)
	assert.Equal(t, "yes", *val.Option)
	require.NotNil(t, val.Validator)
	assert.Equal(t, "BZE Alpha Team", *val.Validator)
	assert.Equal(t, alphaOperator, *val.ValidatorOperator)
	assert.NotEmpty(t, *val.VotingPowerPct)
	assert.Empty(t, into[dto.List[dto.ProposalVote]](t, fetch(t, base+"/proposals/47/votes?option=no")).Items)

	deposits := into[dto.List[dto.ProposalDeposit]](t, fetch(t, base+"/proposals/47/deposits"))
	require.Len(t, deposits.Items, 1)
	assert.Equal(t, hSubmit, deposits.Items[0].Height)

	// The resolution: the event sets the status and its height, the next
	// sync the final tally; another sync changes nothing.
	e.grpc.SetResponse("gov", "Proposal", "47", fakenode.ReadGRPCFixture(t, "gov", "Proposal", "47"))
	e.grpc.SetResponse("gov", "Proposals", "", fakenode.ReadGRPCFixture(t, "gov", "Proposals", ""))
	e.index(t, hResolve)
	assert.Equal(t, []string{"passed 23821225"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', status, resolved_height) FROM explorer.proposals WHERE id = 47`))
	code, out = e.syncState(t)
	require.Equal(t, 0, code, out)
	tally := `SELECT concat_ws(' ', status, tally_yes, tally_no, tally_abstain, tally_veto, tally_bonded_tokens,
		tally_updated_at, resolved_height) FROM explorer.proposals WHERE id = 47`
	final := queryStrings(t, e.db, tally)
	assert.Contains(t, final[0], "passed 106584798486204 0 0 0 147027371221650", "the final tally, the last bonded tokens kept")
	code, out = e.syncState(t)
	require.Equal(t, 0, code, out)
	assert.Equal(t, final, queryStrings(t, e.db, tally), "a resolved proposal's tally never moves")

	p = into[dto.Proposal](t, fetch(t, base+"/proposals/47"))
	assert.Equal(t, hResolve, *p.ResolvedHeight)
	assert.Equal(t, p.VotingEndTime, p.TallyUpdatedAt, "the final tally is as of the voting end")

	assert.Equal(t, http.StatusNotFound, fetch(t, base+"/proposals/48").status)
}

// A resolution indexed before the sync ever wrote its proposal (one
// submitted before the live floor) changes no row; the sync then takes the
// resolution height from the stored block event.
func TestAResolutionBeforeTheProposalRowIsKept(t *testing.T) {
	e := newSyncEnv(t)
	e.index(t, hResolve)
	assert.Zero(t, e.count(t, `SELECT count(*) FROM explorer.proposals`))
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	assert.Equal(t, []string{"passed 23821225 "}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', status, resolved_height, coalesce(submit_height::text, '')) FROM explorer.proposals WHERE id = 47`),
		"no submit height: the submission is below the indexed range")
	assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM explorer.proposals WHERE resolved_height IS NOT NULL`))
}
