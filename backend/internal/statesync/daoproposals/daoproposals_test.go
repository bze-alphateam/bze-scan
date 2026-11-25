package daoproposals_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/query"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/daoproposals"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

type fakeClient struct {
	daos      []daotypes.Dao
	proposals map[uint64]daotypes.QueryProposalResponse
	err       error
	// listed are the Proposals requests.
	listed []*daotypes.QueryProposalsRequest
	asked  []uint64
}

func (f *fakeClient) Daos(_ context.Context, in *daotypes.QueryDaosRequest, _ ...grpc.CallOption) (*daotypes.QueryDaosResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	// Two pages of one DAO each when there are two.
	if len(in.Pagination.Key) == 0 && len(f.daos) > 1 {
		return &daotypes.QueryDaosResponse{Daos: f.daos[:1], Pagination: &query.PageResponse{NextKey: []byte{1}}}, nil
	}
	if len(in.Pagination.Key) > 0 {
		return &daotypes.QueryDaosResponse{Daos: f.daos[1:], Pagination: &query.PageResponse{}}, nil
	}
	return &daotypes.QueryDaosResponse{Daos: f.daos, Pagination: &query.PageResponse{}}, nil
}

func (f *fakeClient) Proposals(_ context.Context, in *daotypes.QueryProposalsRequest, _ ...grpc.CallOption) (*daotypes.QueryProposalsResponse, error) {
	f.listed = append(f.listed, in)
	if f.err != nil {
		return nil, f.err
	}
	resp := &daotypes.QueryProposalsResponse{Pagination: &query.PageResponse{}}
	for _, p := range f.proposals {
		if p.Proposal.DaoId != in.DaoId {
			continue
		}
		for _, st := range in.Statuses {
			if p.Proposal.Status == st {
				resp.Proposals = append(resp.Proposals, p.Proposal)
			}
		}
	}
	return resp, nil
}

func (f *fakeClient) Proposal(_ context.Context, in *daotypes.QueryProposalRequest, _ ...grpc.CallOption) (*daotypes.QueryProposalResponse, error) {
	f.asked = append(f.asked, in.Id)
	if f.err != nil {
		return nil, f.err
	}
	p, ok := f.proposals[in.Id]
	if !ok {
		return nil, status.Error(codes.NotFound, "proposal not found")
	}
	return &p, nil
}

// fakeJSON renders the fields the set reads, like the codec would.
type fakeJSON struct{}

func (fakeJSON) ProtoJSON(msg gogoproto.Message) ([]byte, error) {
	p := msg.(*daotypes.Proposal)
	return json.Marshal(map[string]any{
		"msgs":          []map[string]string{{"@type": "/cosmos.bank.v1beta1.MsgSend"}},
		"pinned_config": map[string]string{"min_quorum": p.PinnedConfig.MinQuorum.String()},
	})
}

type memStore struct {
	saved  [][]daoproposals.Proposal
	voting []uint64
}

func (m *memStore) Save(_ context.Context, ps []daoproposals.Proposal) error {
	m.saved = append(m.saved, ps)
	return nil
}

func (m *memStore) Voting(context.Context) ([]uint64, error) { return m.voting, nil }

func proposal(id, daoID uint64, st daotypes.ProposalStatus, yes int64) daotypes.QueryProposalResponse {
	return daotypes.QueryProposalResponse{
		Proposal: daotypes.Proposal{
			Id: id, DaoId: daoID, Proposer: "bze1proposer", Title: "Fund the bees", Summary: "s", Expedited: true,
			Status: st, SubmitHeight: 100, SubmitTime: 1_700_000_000, VotingEnd: 1_700_086_400,
			PinnedConfig:      daotypes.DaoConfig{MinQuorum: math.LegacyMustNewDecFromStr("0.334"), Threshold: math.LegacyMustNewDecFromStr("0.5")},
			PinnedDenominator: math.NewInt(1000), Yes: math.NewInt(yes), No: math.NewInt(1), Abstain: math.ZeroInt(),
		},
		ExpediteSettleable: st == daotypes.ProposalStatus_VOTING,
	}
}

func newSet(c *fakeClient, s *memStore) *daoproposals.Set {
	return daoproposals.New(daoproposals.Deps{Client: c, JSON: fakeJSON{}, Store: s, Now: func() time.Time { return now }}, 0)
}

func TestSetIdentity(t *testing.T) {
	s := newSet(&fakeClient{}, &memStore{})
	assert.Equal(t, statesync.DaoProposals, s.Name())
	assert.Equal(t, daoproposals.DefaultInterval, s.Interval())
	assert.Equal(t, "7", daoproposals.Key(7))
}

func TestFirstRunListsTheOpenProposalsOfEveryDao(t *testing.T) {
	failed := proposal(2, 1, daotypes.ProposalStatus_EXECUTION_FAILED, 900)
	failed.RetryDeadline = 1_700_100_000
	c := &fakeClient{
		daos: []daotypes.Dao{{Id: 1}, {Id: 2}},
		proposals: map[uint64]daotypes.QueryProposalResponse{
			1: proposal(1, 1, daotypes.ProposalStatus_VOTING, 10),
			2: failed,
			3: proposal(3, 2, daotypes.ProposalStatus_EXECUTED, 999),
			4: proposal(4, 2, daotypes.ProposalStatus_VOTING, 5),
		},
	}
	st := &memStore{}
	require.NoError(t, newSet(c, st).FullResync(context.Background()))

	require.Len(t, c.listed, 2, "one list per DAO, across the DAO pages")
	for _, req := range c.listed {
		assert.Equal(t, []daotypes.ProposalStatus{daotypes.ProposalStatus_VOTING, daotypes.ProposalStatus_EXECUTION_FAILED}, req.Statuses)
	}
	require.Len(t, st.saved, 1)
	byID := map[uint64]daoproposals.Proposal{}
	for _, p := range st.saved[0] {
		byID[p.ID] = p
	}
	require.Len(t, byID, 3, "the executed proposal is not listed")

	voting := byID[1]
	assert.Equal(t, "voting", voting.Status)
	assert.Equal(t, uint64(1), voting.DaoID)
	assert.Equal(t, "Fund the bees", voting.Title)
	assert.Equal(t, []string{"10", "1", "0"}, []string{voting.Yes, voting.No, voting.Abstain})
	assert.Equal(t, "1000", voting.Denominator)
	assert.Equal(t, now, voting.TallyAt)
	assert.True(t, voting.ExpediteSettleable)
	assert.Equal(t, time.Unix(1_700_086_400, 0).UTC(), voting.VotingEnd)
	assert.JSONEq(t, `[{"@type":"/cosmos.bank.v1beta1.MsgSend"}]`, string(voting.Msgs))
	assert.JSONEq(t, `{"min_quorum":"0.334000000000000000"}`, string(voting.PinnedConfig))
	assert.Nil(t, voting.RetryDeadline)

	f := byID[2]
	assert.Equal(t, "execution_failed", f.Status)
	require.NotNil(t, f.RetryDeadline)
	assert.Equal(t, time.Unix(1_700_100_000, 0).UTC(), *f.RetryDeadline)
}

func TestLaterRunsRefreshTheStoredVotingTallies(t *testing.T) {
	c := &fakeClient{
		daos:      []daotypes.Dao{{Id: 1}},
		proposals: map[uint64]daotypes.QueryProposalResponse{1: proposal(1, 1, daotypes.ProposalStatus_VOTING, 10)},
	}
	st := &memStore{voting: []uint64{1, 9}}
	set := newSet(c, st)
	require.NoError(t, set.FullResync(context.Background()))
	c.listed, c.asked = nil, nil

	// The tally moved; proposal 9 was cleaned up from the chain meanwhile.
	c.proposals[1] = proposal(1, 1, daotypes.ProposalStatus_VOTING, 42)
	require.NoError(t, set.FullResync(context.Background()))

	assert.Empty(t, c.listed, "no DAO walk after the first run")
	assert.Equal(t, []uint64{1, 9}, c.asked)
	require.Len(t, st.saved, 2)
	require.Len(t, st.saved[1], 1, "the cleaned proposal is not written: its row keeps the indexed values")
	assert.Equal(t, "42", st.saved[1][0].Yes)
}

func TestResyncOne(t *testing.T) {
	c := &fakeClient{proposals: map[uint64]daotypes.QueryProposalResponse{5: proposal(5, 1, daotypes.ProposalStatus_REJECTED, 0)}}
	st := &memStore{}
	set := newSet(c, st)

	require.NoError(t, set.ResyncOne(context.Background(), "5"))
	require.Len(t, st.saved, 1)
	assert.Equal(t, "rejected", st.saved[0][0].Status)
	assert.False(t, st.saved[0][0].ExpediteSettleable)

	require.NoError(t, set.ResyncOne(context.Background(), "6"), "a cleaned proposal is kept as indexed")
	assert.Len(t, st.saved, 1)

	assert.Error(t, set.ResyncOne(context.Background(), "x"))
}

func TestUnimplementedIsNotSupportedNotAFailure(t *testing.T) {
	c := &fakeClient{err: status.Error(codes.Unimplemented, "unknown service bze.dao.Query")}
	st := &memStore{voting: []uint64{1}}
	set := newSet(c, st)

	require.NoError(t, set.FullResync(context.Background()))
	require.NoError(t, set.ResyncOne(context.Background(), "1"))
	assert.Empty(t, st.saved)

	// Still not walked: once the node upgrades, the first run lists the DAOs.
	c.err = nil
	c.daos = []daotypes.Dao{{Id: 1}}
	c.proposals = map[uint64]daotypes.QueryProposalResponse{1: proposal(1, 1, daotypes.ProposalStatus_VOTING, 1)}
	require.NoError(t, set.FullResync(context.Background()))
	assert.Len(t, c.listed, 1)
	assert.Len(t, st.saved, 1)
}

func TestNodeFailuresAreErrors(t *testing.T) {
	c := &fakeClient{err: errors.New("connection refused")}
	set := newSet(c, &memStore{})
	assert.Error(t, set.FullResync(context.Background()))
	assert.Error(t, set.ResyncOne(context.Background(), "1"))
}
