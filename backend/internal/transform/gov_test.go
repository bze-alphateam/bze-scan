package transform_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// The recorded governance heights: proposal 47 (an upgrade to v8.1.1)
// submitted with its whole deposit, voted by a validator owner and by a
// delegator, then passed; and the legacy (v1beta1) submission of proposal 44
// on the SDK 0.45 chain.
var govHeights = []int64{23745024, 23745061, 23745456, 23821225, 20121960}

// goldenGov is every governance row of one height and the proposals it
// marks for the state sync.
type goldenGov struct {
	Proposals []transform.ProposalSubmission `json:"proposals"`
	Deposits  []transform.ProposalDeposit    `json:"deposits"`
	Votes     []transform.ProposalVote       `json:"votes"`
	Statuses  []transform.ProposalStatus     `json:"statuses"`
	Dirty     []string                       `json:"dirty"`
}

func TestGovGolden(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	for _, h := range govHeights {
		t.Run(fmt.Sprint(h), func(t *testing.T) {
			ents, err := tr.Transform(fetchInput(t, n, h))
			require.NoError(t, err)
			got, err := json.MarshalIndent(goldenGov{
				Proposals: ents.Proposals, Deposits: ents.ProposalDeposits, Votes: ents.ProposalVotes,
				Statuses: ents.ProposalStatuses, Dirty: ents.Dirty.Keys(statesync.Proposals),
			}, "", "  ")
			require.NoError(t, err)
			got = append(got, '\n')

			file := filepath.Join("testdata", fmt.Sprintf("gov_%d.golden.json", h))
			if *update {
				require.NoError(t, os.WriteFile(file, got, 0o644))
			}
			want, err := os.ReadFile(file)
			require.NoError(t, err, "run go test ./internal/transform -golden to create it")
			assert.Equal(t, string(want), string(got))
		})
	}
}

// What the goldens show, stated: proposal 47's row and deposit, a vote with
// its single option, the resolution.
func TestGovRowsOfTheRecordedHeights(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)

	submit, err := tr.Transform(fetchInput(t, n, 23745024))
	require.NoError(t, err)
	require.Len(t, submit.Proposals, 1)
	p := submit.Proposals[0]
	assert.Equal(t, uint64(47), p.ID)
	assert.Equal(t, "Upgrade network to v8.1.1", p.Title)
	assert.Equal(t, "software_upgrade", p.Kind)
	assert.Equal(t, []string{"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade"}, p.MessageTypes)
	assert.Equal(t, "voting_period", p.Status, "the initial deposit opened the voting")
	assert.Equal(t, "bze1dte8cgjyxnsg4zmrlhfv4h4hnxv5vy8khzfx4f", p.Proposer)
	assert.Equal(t, submit.Transactions[0].Hash, p.TxHash)
	require.Len(t, submit.ProposalDeposits, 1)
	assert.Equal(t, []chain.Coin{{Denom: "ubze", Amount: "100000000000"}}, submit.ProposalDeposits[0].Amount)

	vote, err := tr.Transform(fetchInput(t, n, 23745061))
	require.NoError(t, err)
	require.Len(t, vote.ProposalVotes, 1)
	assert.Equal(t, "bze1k27v68x9gtppsgt9scr3649tpjfxnqlj04lk9x", vote.ProposalVotes[0].Voter)
	assert.Equal(t, "yes", vote.ProposalVotes[0].Option)
	assert.JSONEq(t, `[{"option":1,"weight":"1.000000000000000000"}]`, string(vote.ProposalVotes[0].Options))

	end, err := tr.Transform(fetchInput(t, n, 23821225))
	require.NoError(t, err)
	assert.Equal(t, []transform.ProposalStatus{{ProposalID: 47, Status: "passed", Height: 23821225, Resolved: true}},
		end.ProposalStatuses)
	assert.Equal(t, []string{"47"}, end.Dirty.Keys(statesync.Proposals))

	legacy, err := tr.Transform(fetchInput(t, n, 20121960))
	require.NoError(t, err)
	require.Len(t, legacy.Proposals, 1)
	assert.Equal(t, "V8 Upgrade", legacy.Proposals[0].Title)
	assert.Equal(t, "software_upgrade", legacy.Proposals[0].Kind, "classified by its legacy content")
	assert.Equal(t, []string{"/cosmos.gov.v1.MsgExecLegacyContent"}, legacy.Proposals[0].MessageTypes,
		"wrapped as gov v1 queries show it")
}

func plain(typ, msgIndex string, kv ...string) node.Event {
	ev := node.Event{Type: typ}
	for i := 0; i < len(kv); i += 2 {
		ev.Attributes = append(ev.Attributes, node.Attribute{Key: kv[i], Value: kv[i+1]})
	}
	if msgIndex != "" {
		ev = withIndex(ev, msgIndex)
	}
	return ev
}

const (
	voter     = "bze1k27v68x9gtppsgt9scr3649tpjfxnqlj04lk9x"
	depositor = "bze1dte8cgjyxnsg4zmrlhfv4h4hnxv5vy8khzfx4f"
)

// No recorded height has a MsgDeposit, a weighted vote, a cancel or the
// other resolutions; these are encoded with the chain codec and given the
// events SDK v0.50 emits. The cases are verified live during the mainnet
// soak.
func govInput(t *testing.T) transform.Input {
	in := baseInput()
	half := sdkmath.LegacyNewDecWithPrec(5, 1)
	in.Block.Txs = []string{
		encodedTx(t,
			&govv1.MsgDeposit{ProposalId: 50, Depositor: depositor, Amount: sdk.NewCoins(sdk.NewInt64Coin("ubze", 7))},
			&govv1.MsgDeposit{ProposalId: 50, Depositor: depositor, Amount: sdk.NewCoins(sdk.NewInt64Coin("ubze", 3))},
			&govv1.MsgVoteWeighted{ProposalId: 51, Voter: voter, Options: []*govv1.WeightedVoteOption{
				{Option: govv1.OptionYes, Weight: half.String()}, {Option: govv1.OptionNo, Weight: half.String()}}},
			&govv1.MsgVote{ProposalId: 51, Voter: voter, Option: govv1.OptionNoWithVeto},
			&govv1.MsgCancelProposal{ProposalId: 52, Proposer: depositor},
		),
		encodedTx(t, &govv1.MsgVote{ProposalId: 53, Voter: voter, Option: govv1.OptionAbstain}),
	}
	in.Results.TxsResults[0].Events = []node.Event{
		plain("proposal_deposit", "0", "depositor", depositor, "amount", "7ubze", "proposal_id", "50"),
		plain("proposal_deposit", "1", "depositor", depositor, "amount", "3ubze", "proposal_id", "50"),
		plain("proposal_deposit", "1", "voting_period_start", "50"),
		plain("proposal_vote", "2", "voter", voter, "proposal_id", "51",
			"option", `[{"option":1,"weight":"0.500000000000000000"},{"option":3,"weight":"0.500000000000000000"}]`),
		plain("proposal_vote", "3", "voter", voter, "proposal_id", "51", "option", `[{"option":4,"weight":"1.000000000000000000"}]`),
		plain("cancel_proposal", "4", "sender", depositor, "proposal_id", "52"),
	}
	// The second transaction failed: its vote is no row.
	in.Results.TxsResults[1].Events = []node.Event{
		plain("proposal_vote", "0", "voter", voter, "proposal_id", "53", "option", `[{"option":2,"weight":"1.0"}]`),
	}
	in.Results.FinalizeBlockEvents = []node.Event{
		plain("inactive_proposal", "", "proposal_id", "54", "proposal_result", "proposal_dropped"),
		plain("active_proposal", "", "proposal_id", "55", "proposal_result", "proposal_rejected"),
		plain("active_proposal", "", "proposal_id", "56", "proposal_result", "expedited_proposal_rejected"),
		plain("active_proposal", "", "proposal_id", "57", "proposal_result", "proposal_failed"),
		plain("inactive_proposal", "", "proposal_id", "58", "proposal_result", "proposal_failed"),
		plain("active_proposal", "", "proposal_id", "59", "proposal_result", "proposal_passed"),
	}
	return in
}

func TestGovDepositsVotesAndResolutions(t *testing.T) {
	ents, err := realTransformer(t).Transform(govInput(t))
	require.NoError(t, err)
	at := time.Unix(100, 0).UTC()

	assert.Equal(t, []transform.ProposalDeposit{
		{ProposalID: 50, Depositor: depositor, Height: 10, TxIndex: 0, Amount: []chain.Coin{{Denom: "ubze", Amount: "10"}}, Time: at},
	}, ents.ProposalDeposits, "one depositor's deposits in one transaction are one row")

	require.Len(t, ents.ProposalVotes, 2)
	weighted := ents.ProposalVotes[0]
	assert.Empty(t, weighted.Option, "a split vote has no single option")
	assert.JSONEq(t, `[{"option":1,"weight":"0.500000000000000000"},{"option":3,"weight":"0.500000000000000000"}]`,
		string(weighted.Options))
	assert.Equal(t, transform.ProposalVote{ProposalID: 51, Voter: voter, Options: ents.ProposalVotes[1].Options,
		Option: "no_with_veto", Height: 10, TxIndex: 0, MsgIndex: 3, Time: at}, ents.ProposalVotes[1])

	assert.Equal(t, []transform.ProposalStatus{
		{ProposalID: 54, Status: "rejected", Height: 10, Resolved: true},
		{ProposalID: 55, Status: "rejected", Height: 10, Resolved: true},
		{ProposalID: 56, Status: "voting_period", Height: 10, ExpeditedOff: true},
		{ProposalID: 57, Status: "failed", Height: 10, Resolved: true},
		{ProposalID: 58, Status: "failed", Height: 10, Resolved: true},
		{ProposalID: 59, Status: "passed", Height: 10, Resolved: true},
		{ProposalID: 50, Status: "voting_period", Height: 10},
		{ProposalID: 52, Status: "canceled", Height: 10, Resolved: true},
	}, ents.ProposalStatuses)
	assert.Equal(t, []string{"50", "51", "52", "54", "55", "56", "57", "58", "59"}, ents.Dirty.Keys(statesync.Proposals),
		"the failed transaction's proposal 53 is not dirty")
	assert.Equal(t, []string{"59"}, ents.Dirty.Keys(statesync.Params),
		"only a passed proposal asks for a parameters snapshot")
}

func TestVoteOptions(t *testing.T) {
	for _, tc := range []struct {
		name, raw, options, option string
	}{
		{"v0.50", `[{"option":1,"weight":"1.000000000000000000"}]`, `[{"option":1,"weight":"1.000000000000000000"}]`, "yes"},
		{"by name", `[{"option":"VOTE_OPTION_NO","weight":"1.000000000000000000"}]`, `[{"option":3,"weight":"1.000000000000000000"}]`, "no"},
		{"split", `[{"option":2,"weight":"0.3"},{"option":4,"weight":"0.7"}]`, `[{"option":2,"weight":"0.3"},{"option":4,"weight":"0.7"}]`, ""},
		{"sdk 0.45", "option: VOTE_OPTION_ABSTAIN\nweight: \"1.000000000000000000\"", `[{"option":2,"weight":"1.000000000000000000"}]`, "abstain"},
		{"bare name", "VOTE_OPTION_NO_WITH_VETO", `[{"option":4,"weight":"1.000000000000000000"}]`, "no_with_veto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options, option, err := transform.VoteOptions(tc.raw)
			require.NoError(t, err)
			assert.JSONEq(t, tc.options, string(options))
			assert.Equal(t, tc.option, option)
		})
	}
	_, _, err := transform.VoteOptions("maybe")
	require.Error(t, err)
}
