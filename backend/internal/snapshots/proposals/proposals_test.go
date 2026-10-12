package proposals_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/snapshots"
	"github.com/bze-alphateam/bze-scan/backend/internal/snapshots/proposals"
)

var genesis = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// archive is a fake archive: block h is at genesis + h·6 s, the state
// answers come from the fields, and every request is recorded.
type archive struct {
	earliest, latest int64
	bonded           map[int64]string
	votes            map[int64][]*govv1.Vote
	deposits         map[int64][]*govv1.Deposit
	// events are the finalize events of a height.
	events map[int64][]node.Event
	// pageSize splits the votes into pages.
	pageSize int

	blocks  []int64
	queries []string
	failOn  string
}

func timeAt(h int64) time.Time { return genesis.Add(time.Duration(h) * 6 * time.Second) }

func (a *archive) Query(_ context.Context, path string, req, resp gogoproto.Message, height int64) error {
	a.queries = append(a.queries, fmt.Sprintf("%s@%d", path, height))
	if path == a.failOn {
		return &node.ABCIError{Path: path, Height: height, Code: 18, Log: "invalid denom"}
	}
	switch r := req.(type) {
	case *banktypes.QueryBalanceRequest:
		if r.Address != proposals.BondedPool || r.Denom != "ubze" {
			return fmt.Errorf("unexpected balance query %v", r)
		}
		amount, ok := sdkmath.NewIntFromString(a.bonded[height])
		if !ok {
			return errors.New("no balance recorded")
		}
		resp.(*banktypes.QueryBalanceResponse).Balance = &sdk.Coin{Denom: "ubze", Amount: amount}
	case *govv1.QueryVotesRequest:
		all := a.votes[height]
		start := 0
		if len(r.Pagination.Key) > 0 {
			_, _ = fmt.Sscan(string(r.Pagination.Key), &start)
		}
		end := len(all)
		if a.pageSize > 0 {
			end = min(start+a.pageSize, len(all))
		}
		out := resp.(*govv1.QueryVotesResponse)
		out.Votes = all[start:end]
		if end < len(all) {
			out.Pagination = &query.PageResponse{NextKey: []byte(fmt.Sprint(end))}
		}
	case *govv1.QueryDepositsRequest:
		resp.(*govv1.QueryDepositsResponse).Deposits = a.deposits[height]
	default:
		return fmt.Errorf("unexpected query %T", req)
	}
	return nil
}

func (a *archive) Status(context.Context) (*node.Status, error) {
	return &node.Status{EarliestBlockHeight: a.earliest, LatestBlockHeight: a.latest}, nil
}

func (a *archive) Block(_ context.Context, h int64) (*node.Block, error) {
	a.blocks = append(a.blocks, h)
	if h < a.earliest || h > a.latest {
		return nil, fmt.Errorf("no block %d", h)
	}
	return &node.Block{Height: h, Time: timeAt(h)}, nil
}

func (a *archive) BlockResults(_ context.Context, h int64) (*node.BlockResults, error) {
	return &node.BlockResults{Height: h, FinalizeBlockEvents: a.events[h]}, nil
}

type store struct {
	resolved []proposals.Proposal
	ids      []uint64
	saved    []proposals.Snapshot
}

func (s *store) Resolved(_ context.Context, ids []uint64) ([]proposals.Proposal, error) {
	s.ids = ids
	return s.resolved, nil
}

func (s *store) Save(_ context.Context, snap proposals.Snapshot) error {
	s.saved = append(s.saved, snap)
	return nil
}

func yes(voter string) *govv1.Vote {
	return &govv1.Vote{ProposalId: 47, Voter: voter, Options: govv1.NewNonSplitVoteOption(govv1.OptionYes)}
}

func activeProposal(id string) []node.Event {
	return []node.Event{{Type: "active_proposal", Attributes: []node.Attribute{
		{Key: "proposal_id", Value: id}, {Key: "proposal_result", Value: "proposal_passed"},
	}}}
}

func i64(v int64) *int64 { return &v }

func TestSnapshotReadsTheStateBeforeTheTally(t *testing.T) {
	split := &govv1.Vote{ProposalId: 47, Voter: "bze1split", Options: govv1.WeightedVoteOptions{
		{Option: govv1.OptionYes, Weight: "0.700000000000000000"}, {Option: govv1.OptionNo, Weight: "0.300000000000000000"},
	}}
	a := &archive{
		earliest: 1, latest: 1000, pageSize: 2,
		bonded: map[int64]string{499: "131537493190125"},
		votes:  map[int64][]*govv1.Vote{499: {yes("bze1a"), split, yes("bze1c")}},
		deposits: map[int64][]*govv1.Deposit{499: {{ProposalId: 47, Depositor: "bze1d",
			Amount: sdk.NewCoins(sdk.NewCoin("ubze", sdkmath.NewInt(100000000000)))}}},
	}
	s := &store{resolved: []proposals.Proposal{{ID: 47, ResolvedHeight: i64(500), VotingEndTime: timeAt(500)}}}
	k := proposals.New(s)
	assert.Equal(t, "proposals", k.Name())

	targets, err := k.Targets(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, "47", targets[0].ID)

	summary, err := k.Snapshot(context.Background(), a, targets[0])
	require.NoError(t, err)
	assert.Equal(t, "at height 499: bonded 131537493190125, 3 votes, 1 deposits", summary)
	assert.Equal(t, []string{
		proposals.PathBalance + "@499", proposals.PathVotes + "@499", proposals.PathVotes + "@499", proposals.PathDeposits + "@499",
	}, a.queries, "every read at H - 1, the votes over two pages")
	assert.Equal(t, []int64{499}, a.blocks, "no search: the resolution height is stored")

	require.Len(t, s.saved, 1)
	snap := s.saved[0]
	assert.Equal(t, uint64(47), snap.ID)
	assert.Zero(t, snap.ResolvedHeight, "the stored height is kept")
	assert.Equal(t, int64(499), snap.Height)
	assert.Equal(t, timeAt(499), snap.Time)
	assert.Equal(t, "131537493190125", snap.BondedTokens)
	require.Len(t, snap.Votes, 3)
	assert.Equal(t, "bze1a", snap.Votes[0].Voter)
	assert.JSONEq(t, `[{"option":1,"weight":"1.000000000000000000"}]`, string(snap.Votes[0].Options), "the events' numeric encoding")
	assert.Equal(t, "yes", snap.Votes[0].Option)
	assert.JSONEq(t, `[{"option":1,"weight":"0.700000000000000000"},{"option":3,"weight":"0.300000000000000000"}]`, string(snap.Votes[1].Options))
	assert.Empty(t, snap.Votes[1].Option, "a split vote has no single option")
	require.Len(t, snap.Deposits, 1)
	assert.Equal(t, "bze1d", snap.Deposits[0].Depositor)
	assert.JSONEq(t, `[{"denom":"ubze","amount":"100000000000"}]`, string(snap.Deposits[0].Amount))
}

func TestSnapshotFindsAResolutionTheExplorerNeverIndexed(t *testing.T) {
	// Voting ends between blocks 9,000 and 9,001: block 9,001 tallies it.
	end := timeAt(9000).Add(time.Second)
	a := &archive{
		earliest: 1, latest: 20000,
		bonded: map[int64]string{9000: "5"},
		events: map[int64][]node.Event{9001: activeProposal("43")},
	}
	s := &store{resolved: []proposals.Proposal{{ID: 43, SubmitHeight: i64(8000), VotingEndTime: end}}}
	k := proposals.New(s)
	targets, err := k.Targets(context.Background(), []string{"43"})
	require.NoError(t, err)
	assert.Equal(t, []uint64{43}, s.ids)

	_, err = k.Snapshot(context.Background(), a, targets[0])
	require.NoError(t, err)
	require.Len(t, s.saved, 1)
	assert.Equal(t, int64(9001), s.saved[0].ResolvedHeight, "stored since the row has none")
	assert.Equal(t, int64(9000), s.saved[0].Height)
	for _, h := range a.blocks {
		assert.GreaterOrEqual(t, h, int64(8000), "the search starts at the submit height")
	}
	assert.LessOrEqual(t, len(a.blocks), 16, "a binary search")

	// The same search from the archive's earliest block.
	s.saved = nil
	a.blocks = nil
	s.resolved[0].SubmitHeight = nil
	targets, err = k.Targets(context.Background(), nil)
	require.NoError(t, err)
	_, err = k.Snapshot(context.Background(), a, targets[0])
	require.NoError(t, err)
	assert.Equal(t, int64(9001), s.saved[0].ResolvedHeight)
}

func TestSnapshotChecksTheResolutionBlock(t *testing.T) {
	end := timeAt(9000).Add(time.Second)
	for name, a := range map[string]*archive{
		"another proposal's tally": {earliest: 1, latest: 20000, events: map[int64][]node.Event{9001: activeProposal("44")}},
		"no tally at all":          {earliest: 1, latest: 20000},
		"no block after the end":   {earliest: 1, latest: 8000},
	} {
		t.Run(name, func(t *testing.T) {
			s := &store{resolved: []proposals.Proposal{{ID: 43, VotingEndTime: end}}}
			k := proposals.New(s)
			targets, err := k.Targets(context.Background(), nil)
			require.NoError(t, err)
			_, err = k.Snapshot(context.Background(), a, targets[0])
			require.ErrorIs(t, err, proposals.ErrNoResolution)
			assert.Empty(t, s.saved, "nothing written")
		})
	}
}

func TestSnapshotFailsWithoutWritingWhenAReadFails(t *testing.T) {
	a := &archive{earliest: 1, latest: 1000, bonded: map[int64]string{499: "1"}, failOn: proposals.PathVotes}
	s := &store{resolved: []proposals.Proposal{{ID: 47, ResolvedHeight: i64(500), VotingEndTime: timeAt(500)}}}
	k := proposals.New(s)
	targets, err := k.Targets(context.Background(), nil)
	require.NoError(t, err)
	_, err = k.Snapshot(context.Background(), a, targets[0])
	require.ErrorContains(t, err, "proposal 47: votes")
	assert.Empty(t, s.saved)
}

func TestTargetsRejectAnIDThatIsNotANumber(t *testing.T) {
	_, err := proposals.New(&store{}).Targets(context.Background(), []string{"forty-seven"})
	require.ErrorContains(t, err, `"forty-seven" is not a proposal id`)
}

func TestSnapshotRejectsATargetOfAnotherKind(t *testing.T) {
	_, err := proposals.New(&store{}).Snapshot(context.Background(), &archive{}, snapshots.Target{ID: "x", Value: "x"})
	require.ErrorContains(t, err, "target x is a string")
}
