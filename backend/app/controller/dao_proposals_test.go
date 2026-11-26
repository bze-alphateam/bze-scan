package controller_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/daostate"
)

var proposalNow = time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC)

type fakeDaoProposals struct {
	daos      []repository.Dao
	proposals []repository.DaoProposal
	votes     []repository.DaoVote
	denoms    map[string]repository.Denom
	err       error

	gotStatus *string
	gotBefore *int64
	gotOption *string
	gotAfter  *repository.DaoVoteKey
	gotLimit  int

	ballots        []repository.VoterBallot
	gotStatuses    []string
	gotVoting      bool
	gotAfterVoting *repository.OpenDaoProposalKey
	gotVoter       string
	gotBallotKey   *repository.VoterBallotKey
}

func (f *fakeDaoProposals) DaoProposalsAcross(_ context.Context, statuses []string, voting bool, before *int64,
	afterVoting *repository.OpenDaoProposalKey, limit int) ([]repository.DaoProposal, error) {
	f.gotStatuses, f.gotVoting, f.gotBefore, f.gotAfterVoting, f.gotLimit = statuses, voting, before, afterVoting, limit
	return f.proposals[:min(limit, len(f.proposals))], f.err
}

func (f *fakeDaoProposals) DaoVotesByVoter(_ context.Context, voter string, before *repository.VoterBallotKey, limit int) ([]repository.VoterBallot, error) {
	f.gotVoter, f.gotBallotKey, f.gotLimit = voter, before, limit
	return f.ballots[:min(limit, len(f.ballots))], f.err
}

func (f *fakeDaoProposals) Dao(_ context.Context, id int64) (*repository.Dao, error) {
	for _, d := range f.daos {
		if d.ID == id {
			return &d, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeDaoProposals) DaoProposals(_ context.Context, _ int64, status *string, before *int64, limit int) ([]repository.DaoProposal, error) {
	f.gotStatus, f.gotBefore, f.gotLimit = status, before, limit
	return f.proposals[:min(limit, len(f.proposals))], f.err
}

func (f *fakeDaoProposals) DaoProposal(_ context.Context, id int64) (*repository.DaoProposal, int64, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	for _, p := range f.proposals {
		if p.ID == id {
			return &p, int64(len(f.votes)), nil
		}
	}
	return nil, 0, repository.ErrNotFound
}

func (f *fakeDaoProposals) DaoVotes(_ context.Context, _ int64, option *string, after *repository.DaoVoteKey, limit int) ([]repository.DaoVote, error) {
	f.gotOption, f.gotAfter, f.gotLimit = option, after, limit
	return f.votes[:min(limit, len(f.votes))], nil
}

func (f *fakeDaoProposals) Denoms(context.Context, []string) (map[string]repository.Denom, error) {
	return f.denoms, nil
}

func daoProposalServer(r controller.DaoProposalReader, s controller.DaoState) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	h := controller.NewDaoProposalController(r, s, func() time.Time { return proposalNow })
	e.GET("/api/v1/daos/:id/proposals", h.DaoProposals)
	e.GET("/api/v1/dao-proposals", h.AllDaoProposals)
	e.GET("/api/v1/dao-votes", h.VoterBallots)
	e.GET("/api/v1/dao-proposals/:id", h.DaoProposal)
	e.GET("/api/v1/dao-proposals/:id/votes", h.DaoVotes)
	return e
}

func sp(s string) *string { return &s }
func ip(i int64) *int64   { return &i }
func bp(b bool) *bool     { return &b }

func stakeProposal(id int64, status string, votingEnd time.Time) repository.DaoProposal {
	return repository.DaoProposal{
		ID: id, DaoID: ip(2), DaoName: sp("Stakers"), DaoType: sp("stake"), Proposer: sp(daoAdmin),
		ProposerLabel: &repository.Label{Address: daoAdmin, Name: "Alpha", Kind: "known"}, Title: sp("Spend"),
		Summary: sp("s"), Msgs: json.RawMessage(`[{"@type":"/cosmos.bank.v1beta1.MsgSend"}]`), Expedited: bp(true),
		VotingEnd: &votingEnd, Denominator: sp("1000"), Status: &status, Yes: sp("700"), No: sp("0"), Abstain: sp("0"),
		PinnedConfig: json.RawMessage(`{"min_quorum":"0.400000000000000000","threshold":"0.500000000000000000",
			"expedite_quorum":"0.600000000000000000","expedite_threshold":"0.700000000000000000"}`),
		ExpediteSettleable: true,
	}
}

func TestDaoProposalsListNewestFirstPaged(t *testing.T) {
	end := proposalNow.Add(time.Hour)
	f := &fakeDaoProposals{daos: []repository.Dao{indexedDao(2, "Stakers", "stake")},
		proposals: []repository.DaoProposal{stakeProposal(9, "voting", end), stakeProposal(7, "voting", end), stakeProposal(3, "voting", end)}}
	e := daoProposalServer(f, nil)

	rec := get(t, e, "/api/v1/daos/2/proposals?limit=2&status=voting")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "voting", *f.gotStatus)
	assert.Equal(t, 3, f.gotLimit, "one extra row tells there is a next page")
	var list dto.List[dto.DaoProposalSummary]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Items, 2)
	assert.Equal(t, int64(9), list.Items[0].ID)
	assert.Equal(t, []string{"/cosmos.bank.v1beta1.MsgSend"}, list.Items[0].MessageTypes)
	assert.Equal(t, &dto.Label{Name: "Alpha", Kind: "known"}, list.Items[0].ProposerLabel)
	require.NotNil(t, list.NextCursor)

	get(t, e, "/api/v1/daos/2/proposals?cursor="+*list.NextCursor)
	assert.Equal(t, int64(7), *f.gotBefore)

	assert.Equal(t, http.StatusBadRequest, get(t, e, "/api/v1/daos/2/proposals?status=passed").Code)
	assert.Equal(t, http.StatusBadRequest, get(t, e, "/api/v1/daos/2/proposals?cursor=@@").Code)
	assert.Equal(t, http.StatusNotFound, get(t, e, "/api/v1/daos/5/proposals").Code)
}

func TestDaoProposalDetailWithChecksAndWeightDenom(t *testing.T) {
	ended := proposalNow.Add(-time.Minute)
	p := stakeProposal(9, "voting", ended)
	f := &fakeDaoProposals{proposals: []repository.DaoProposal{p}, votes: make([]repository.DaoVote, 3),
		denoms: map[string]repository.Denom{"ubze": {Symbol: sp("BZE"), Exponent: 6}}}
	e := daoProposalServer(f, &fakeDaoState{live: map[uint64]*daostate.Live{2: liveStake()}})

	rec := get(t, e, "/api/v1/dao-proposals/9")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	var d dto.DaoProposal
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &d))
	assert.Equal(t, &dto.DaoRef{ID: 2, Name: "Stakers", Type: "stake"}, d.Dao)
	assert.True(t, d.VotingEnded, "voting and past the voting end")
	assert.True(t, d.ExpediteSettleable)
	assert.Equal(t, int64(3), d.VotesCount)
	six := 6
	assert.Equal(t, &dto.WeightDenom{Denom: "ubze", Symbol: sp("BZE"), Exponent: &six}, d.WeightDenom)
	require.NotNil(t, d.Checks)
	// 700 of 1000 voted (≥ 0.4), 700/700 yes (> 0.5).
	assert.Equal(t, true, d.Checks.Regular.Passes)
	require.NotNil(t, d.Checks.Expedite)
	// Expedite: 700/1000 is exactly the 0.7 bar: not above it.
	assert.True(t, d.Checks.Expedite.QuorumMet)
	assert.False(t, d.Checks.Expedite.YesMet)
	assert.False(t, d.Checks.Expedite.Passes)
	assert.JSONEq(t, `[{"@type":"/cosmos.bank.v1beta1.MsgSend"}]`, string(d.Msgs))
}

func TestDaoProposalDetailWithoutPinnedConfigOrNode(t *testing.T) {
	p := stakeProposal(9, "executed", proposalNow.Add(-time.Hour))
	p.PinnedConfig = nil
	f := &fakeDaoProposals{proposals: []repository.DaoProposal{p}}
	d := decode[dto.DaoProposal](t, get(t, daoProposalServer(f, &fakeDaoState{err: errors.New("down")}), "/api/v1/dao-proposals/9"))
	assert.Nil(t, d.Checks, "no pinned config: no checks")
	assert.Nil(t, d.WeightDenom, "the node is down")
	assert.False(t, d.VotingEnded, "not voting any more")
	assert.False(t, d.ExpediteSettleable)

	// A row whose submission is not indexed: no DAO, no checks.
	f.proposals = []repository.DaoProposal{{ID: 4, Status: sp("rejected")}}
	d = decode[dto.DaoProposal](t, get(t, daoProposalServer(f, nil), "/api/v1/dao-proposals/4"))
	assert.Nil(t, d.Dao)
	assert.Nil(t, d.Checks)
	assert.Equal(t, []string{}, d.MessageTypes)

	assert.Equal(t, http.StatusNotFound, get(t, daoProposalServer(f, nil), "/api/v1/dao-proposals/5").Code)
	assert.Equal(t, http.StatusBadRequest, get(t, daoProposalServer(f, nil), "/api/v1/dao-proposals/x").Code)
}

func TestDaoVotesPagedByEffectiveWeight(t *testing.T) {
	f := &fakeDaoProposals{
		proposals: []repository.DaoProposal{stakeProposal(9, "voting", proposalNow)},
		votes: []repository.DaoVote{
			{Voter: "bze1a", Option: "yes", Weight: "600", EffectiveWeight: "500", Height: 10},
			{Voter: "bze1b", Option: "no", Weight: "300", EffectiveWeight: "300", Height: 11},
			{Voter: "bze1c", Option: "no", Weight: "1", EffectiveWeight: "0", Height: 12},
		},
		denoms: map[string]repository.Denom{},
	}
	e := daoProposalServer(f, &fakeDaoState{live: map[uint64]*daostate.Live{2: liveStake()}})
	rec := get(t, e, "/api/v1/dao-proposals/9/votes?limit=2&option=no")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "no", *f.gotOption)
	list := decode[dto.DaoVoteList](t, rec)
	require.Len(t, list.Items, 2)
	assert.Equal(t, "500", list.Items[0].EffectiveWeight)
	assert.Equal(t, "600", list.Items[0].Weight)
	require.NotNil(t, list.NextCursor)
	assert.Equal(t, &dto.WeightDenom{Denom: "ubze"}, list.WeightDenom, "a denom without metadata has no symbol")

	get(t, e, "/api/v1/dao-proposals/9/votes?cursor="+*list.NextCursor)
	assert.Equal(t, &repository.DaoVoteKey{EffectiveWeight: "300", Voter: "bze1b"}, f.gotAfter)

	assert.Equal(t, http.StatusBadRequest, get(t, e, "/api/v1/dao-proposals/9/votes?option=veto").Code)
	assert.Equal(t, http.StatusBadRequest, get(t, e, "/api/v1/dao-proposals/9/votes?cursor=eyJ3IjoiLTEiLCJ2IjoieCJ9").Code,
		"a negative weight is not a cursor")
	assert.Equal(t, http.StatusNotFound, get(t, e, "/api/v1/dao-proposals/8/votes").Code)
}

func TestAllDaoProposalsOpenOnesSoonestFirst(t *testing.T) {
	end := proposalNow.Add(time.Hour)
	f := &fakeDaoProposals{proposals: []repository.DaoProposal{stakeProposal(9, "voting", end), stakeProposal(7, "voting", end)}}
	e := daoProposalServer(f, &fakeDaoState{supported: true})

	rec := get(t, e, "/api/v1/dao-proposals?status=voting&status=voting&limit=1")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.True(t, f.gotVoting)
	assert.Equal(t, []string{"voting", "voting"}, f.gotStatuses)
	feed := decode[dto.DaoProposalFeed](t, rec)
	assert.True(t, feed.Supported)
	require.Len(t, feed.Items, 1)
	assert.Equal(t, &dto.DaoRef{ID: 2, Name: "Stakers", Type: "stake"}, feed.Items[0].Dao)
	require.NotNil(t, feed.NextCursor)

	get(t, e, "/api/v1/dao-proposals?status=voting&cursor="+*feed.NextCursor)
	assert.Equal(t, &repository.OpenDaoProposalKey{VotingEndMicros: end.UnixMicro(), ID: 9}, f.gotAfterVoting,
		"the cursor holds the last row's voting_end and id")
}

func TestAllDaoProposalsOtherStatusesNewestFirst(t *testing.T) {
	f := &fakeDaoProposals{proposals: []repository.DaoProposal{stakeProposal(9, "executed", proposalNow), stakeProposal(7, "rejected", proposalNow)}}
	e := daoProposalServer(f, nil)

	for _, q := range []string{"status=executed&status=rejected", "status=voting&status=executed", ""} {
		rec := get(t, e, "/api/v1/dao-proposals?limit=1&"+q)
		require.Equal(t, http.StatusOK, rec.Code, q)
		assert.False(t, f.gotVoting, q)
		feed := decode[dto.DaoProposalFeed](t, rec)
		assert.False(t, feed.Supported, "no node")
		require.NotNil(t, feed.NextCursor)
		get(t, e, "/api/v1/dao-proposals?cursor="+*feed.NextCursor)
		assert.Equal(t, int64(9), *f.gotBefore, q)
	}
}

func TestAllDaoProposalsBadRequests(t *testing.T) {
	e := daoProposalServer(&fakeDaoProposals{}, nil)
	for _, path := range []string{"/api/v1/dao-proposals?status=passed", "/api/v1/dao-proposals?status=voting&status=nope",
		"/api/v1/dao-proposals?cursor=@@", "/api/v1/dao-proposals?status=voting&cursor=1", "/api/v1/dao-proposals?limit=0"} {
		assert.Equal(t, http.StatusBadRequest, get(t, e, path).Code, path)
	}
}

func TestVoterBallotsNewestFirstPaged(t *testing.T) {
	end := proposalNow.Add(time.Hour)
	f := &fakeDaoProposals{ballots: []repository.VoterBallot{
		{DaoVote: repository.DaoVote{Voter: daoAdmin, Option: "yes", Weight: "10", EffectiveWeight: "4", Height: 90, Time: proposalNow},
			ProposalID: 5, ProposalTitle: sp("Spend"), ProposalStatus: sp("voting"), VotingEnd: &end, DaoID: ip(2), DaoName: sp("Stakers")},
		{DaoVote: repository.DaoVote{Voter: daoAdmin, Option: "no", Weight: "1", EffectiveWeight: "1", Height: 80, Time: proposalNow}, ProposalID: 4},
	}}
	e := daoProposalServer(f, &fakeDaoState{supported: true})

	rec := get(t, e, "/api/v1/dao-votes?limit=1&voter="+strings.ToUpper(daoAdmin))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, daoAdmin, f.gotVoter, "the address is lower-cased")
	list := decode[dto.VoterBallotList](t, rec)
	assert.True(t, list.Supported)
	require.Len(t, list.Items, 1)
	b := list.Items[0]
	assert.Equal(t, int64(5), b.Proposal.ID)
	assert.Equal(t, "voting", *b.Proposal.Status)
	assert.Equal(t, &dto.VoterDao{ID: 2, Name: "Stakers"}, b.Dao)
	assert.Equal(t, "10", b.Weight)
	assert.Equal(t, "4", b.EffectiveWeight)
	require.NotNil(t, list.NextCursor)

	rec = get(t, e, "/api/v1/dao-votes?voter="+daoAdmin+"&cursor="+*list.NextCursor)
	assert.Equal(t, &repository.VoterBallotKey{Height: 90, ProposalID: 5}, f.gotBallotKey)
	assert.Nil(t, decode[dto.VoterBallotList](t, rec).Items[1].Dao, "a ballot whose proposal is not indexed has no DAO")
}

func TestVoterBallotsBadRequests(t *testing.T) {
	e := daoProposalServer(&fakeDaoProposals{}, nil)
	for _, path := range []string{"/api/v1/dao-votes", "/api/v1/dao-votes?voter=nope", "/api/v1/dao-votes?voter=cosmos1" + daoAdmin[4:],
		"/api/v1/dao-votes?voter=" + daoAdmin + "&cursor=@@", "/api/v1/dao-votes?voter=" + daoAdmin + "&limit=0"} {
		assert.Equal(t, http.StatusBadRequest, get(t, e, path).Code, path)
	}
}
