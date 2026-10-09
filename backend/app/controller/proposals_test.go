package controller_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// fakeProposals answers the proposal reads from fixed rows and records the
// arguments it got.
type fakeProposals struct {
	list     []repository.ProposalSummary
	proposal *repository.Proposal
	votes    []repository.ProposalVote
	deposits []repository.ProposalDeposit
	err      error

	gotStatus, gotOption *string
	gotBefore            *int64
	gotVoteKey           *repository.VoteKey
	gotDepositKey        *repository.DepositKey
	gotLimit             int
}

func (f *fakeProposals) Proposals(_ context.Context, status *string, before *int64, limit int) ([]repository.ProposalSummary, error) {
	f.gotStatus, f.gotBefore, f.gotLimit = status, before, limit
	return f.list[:min(limit, len(f.list))], f.err
}

func (f *fakeProposals) Proposal(_ context.Context, id int64) (*repository.Proposal, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.proposal == nil || f.proposal.ID != id {
		return nil, repository.ErrNotFound
	}
	return f.proposal, nil
}

func (f *fakeProposals) ProposalVotes(_ context.Context, _ int64, option *string, before *repository.VoteKey, limit int) ([]repository.ProposalVote, error) {
	f.gotOption, f.gotVoteKey, f.gotLimit = option, before, limit
	return f.votes[:min(limit, len(f.votes))], nil
}

func (f *fakeProposals) ProposalDeposits(_ context.Context, _ int64, before *repository.DepositKey, limit int) ([]repository.ProposalDeposit, error) {
	f.gotDepositKey, f.gotLimit = before, limit
	return f.deposits[:min(limit, len(f.deposits))], nil
}

func proposalServer(f *fakeProposals) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	h := controller.NewProposalController(f)
	e.GET("/api/v1/proposals", h.Proposals)
	e.GET("/api/v1/proposals/:id", h.Proposal)
	e.GET("/api/v1/proposals/:id/votes", h.ProposalVotes)
	e.GET("/api/v1/proposals/:id/deposits", h.ProposalDeposits)
	return e
}

func summary(id int64) repository.ProposalSummary {
	return repository.ProposalSummary{ID: id, Title: "Upgrade", Kind: "software_upgrade", Status: "passed",
		SubmitTime: time.Unix(1, 0), TallyYes: strp("60"), TallyNo: strp("10"), TallyAbstain: strp("5"),
		TallyVeto: strp("0"), TallyBondedTokens: strp("300")}
}

func TestProposalsListNewestFirstAndPages(t *testing.T) {
	f := &fakeProposals{list: []repository.ProposalSummary{summary(47), summary(46), {ID: 45, Title: "Spend", Status: "voting_period"}}}
	e := proposalServer(f)

	rec := get(t, e, "/api/v1/proposals?limit=2&status=passed")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.Equal(t, "passed", *f.gotStatus)
	assert.Equal(t, 3, f.gotLimit)
	page := decode[dto.List[dto.ProposalSummary]](t, rec)
	require.Len(t, page.Items, 2)
	assert.Equal(t, &dto.Tally{Yes: "60", No: "10", Abstain: "5", NoWithVeto: "0"}, page.Items[0].Tally)
	assert.Equal(t, "25.00000", *page.Items[0].TurnoutPct, "75 of 300 bonded")
	require.NotNil(t, page.NextCursor)

	rec = get(t, e, "/api/v1/proposals?cursor="+*page.NextCursor)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int64(46), *f.gotBefore)
	assert.Nil(t, f.gotStatus)
	last := decode[dto.List[dto.ProposalSummary]](t, rec)
	assert.Nil(t, last.Items[2].Tally, "not tallied yet")
	assert.Nil(t, last.Items[2].TurnoutPct)

	for _, path := range []string{"/api/v1/proposals?status=open", "/api/v1/proposals?cursor=!!", "/api/v1/proposals?limit=0"} {
		rec := get(t, e, path)
		assert.Equal(t, http.StatusBadRequest, rec.Code, path)
		assert.Equal(t, "bad_request", errorCode(t, rec), path)
	}
}

func TestProposalPage(t *testing.T) {
	p := &repository.Proposal{ProposalSummary: summary(47), Proposer: strp("bze1p"),
		MessageTypes: []string{"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade"},
		Messages:     json.RawMessage(`[{"@type":"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade"}]`),
		TotalDeposit: json.RawMessage(`[{"denom":"ubze","amount":"100"}]`), ValidatorsVoted: 7, ValidatorsTotal: 30,
		ProposerLabel: &repository.Label{Address: "bze1p", Name: "Alpha", Kind: "known"}, UpdatedAt: time.Unix(5, 0)}
	e := proposalServer(&fakeProposals{proposal: p})

	rec := get(t, e, "/api/v1/proposals/47")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	got := decode[dto.Proposal](t, rec)
	assert.Equal(t, dto.ValidatorsVoted{Voted: 7, Total: 30}, got.ValidatorsVoted)
	assert.Equal(t, "Alpha", got.ProposerLabel.Name)
	assert.JSONEq(t, `[{"denom":"ubze","amount":"100"}]`, string(got.TotalDeposit))
	assert.Nil(t, got.ResolvedHeight)

	assert.Equal(t, http.StatusNotFound, get(t, e, "/api/v1/proposals/48").Code)
	for _, path := range []string{"/api/v1/proposals/0", "/api/v1/proposals/x", "/api/v1/proposals/-1"} {
		assert.Equal(t, http.StatusBadRequest, get(t, e, path).Code, path)
	}
}

func TestProposalVotesNameValidatorsAndPage(t *testing.T) {
	f := &fakeProposals{proposal: &repository.Proposal{ProposalSummary: summary(47)}, votes: []repository.ProposalVote{
		{Voter: "bze1v", Option: strp("yes"), Options: json.RawMessage(`[{"option":1,"weight":"1.0"}]`), Height: 9, TxIndex: 1,
			TxHash: strp("AB"), Time: time.Unix(9, 0), ValidatorMoniker: strp("ChainTools"),
			ValidatorOperator: strp("bzevaloper1v"), VotingPowerPct: strp("4.12000")},
		{Voter: "bze1d", Options: json.RawMessage(`[{"option":1,"weight":"0.5"},{"option":3,"weight":"0.5"}]`), Height: 8, Time: time.Unix(8, 0)},
	}}
	e := proposalServer(f)

	rec := get(t, e, "/api/v1/proposals/47/votes?limit=1&option=yes")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "yes", *f.gotOption)
	page := decode[dto.List[dto.ProposalVote]](t, rec)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "ChainTools", *page.Items[0].Validator)
	assert.Equal(t, "bzevaloper1v", *page.Items[0].ValidatorOperator)
	assert.Equal(t, "4.12000", *page.Items[0].VotingPowerPct)
	require.NotNil(t, page.NextCursor)

	rec = get(t, e, "/api/v1/proposals/47/votes?option=weighted&cursor="+*page.NextCursor)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, &repository.VoteKey{Height: 9, TxIndex: 1, Voter: "bze1v"}, f.gotVoteKey)
	rest := decode[dto.List[dto.ProposalVote]](t, rec)
	assert.Nil(t, rest.Items[1].Option, "a split vote")
	assert.Nil(t, rest.Items[1].Validator, "not a validator owner")

	assert.Equal(t, http.StatusBadRequest, get(t, e, "/api/v1/proposals/47/votes?option=maybe").Code)
	assert.Equal(t, http.StatusBadRequest, get(t, e, "/api/v1/proposals/47/votes?cursor="+dto.EncodeCursor(1)).Code)
	assert.Equal(t, http.StatusNotFound, get(t, e, "/api/v1/proposals/9/votes").Code)
}

func TestProposalDepositsPage(t *testing.T) {
	f := &fakeProposals{proposal: &repository.Proposal{ProposalSummary: summary(47)}, deposits: []repository.ProposalDeposit{
		{Depositor: "bze1p", Amount: json.RawMessage(`[{"denom":"ubze","amount":"7"}]`), Height: 9, TxIndex: 0, Time: time.Unix(9, 0)},
		{Depositor: "bze1q", Amount: json.RawMessage(`[{"denom":"ubze","amount":"3"}]`), Height: 8, TxIndex: 2, Time: time.Unix(8, 0)},
	}}
	e := proposalServer(f)
	rec := get(t, e, "/api/v1/proposals/47/deposits?limit=1")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	page := decode[dto.List[dto.ProposalDeposit]](t, rec)
	require.Len(t, page.Items, 1)
	assert.JSONEq(t, `[{"denom":"ubze","amount":"7"}]`, string(page.Items[0].Amount))
	require.NotNil(t, page.NextCursor)
	require.Equal(t, http.StatusOK, get(t, e, "/api/v1/proposals/47/deposits?cursor="+*page.NextCursor).Code)
	assert.Equal(t, &repository.DepositKey{Height: 9, TxIndex: 0, Depositor: "bze1p"}, f.gotDepositKey)
	assert.Equal(t, http.StatusBadRequest, get(t, e, "/api/v1/proposals/47/deposits?cursor=x").Code)
}

func TestProposalReadFailuresAreInternal(t *testing.T) {
	e := proposalServer(&fakeProposals{err: errors.New("db down")})
	for _, path := range []string{"/api/v1/proposals", "/api/v1/proposals/1", "/api/v1/proposals/1/votes"} {
		assert.Equal(t, http.StatusInternalServerError, get(t, e, path).Code, path)
	}
}
