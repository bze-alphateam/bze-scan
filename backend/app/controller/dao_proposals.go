package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// daoProposalStatuses are the values of the status filter.
var daoProposalStatuses = []string{"voting", "rejected", "executed", "execution_failed", "expired", "cancelled"}

// daoVoteOptions are the values of the option filter.
var daoVoteOptions = []string{"yes", "no", "abstain"}

// DaoProposalReader is the read side of the DAO proposal tables the routes
// need. Implemented by repository.Explorer; a missing row is
// repository.ErrNotFound.
type DaoProposalReader interface {
	Dao(ctx context.Context, id int64) (*repository.Dao, error)
	DaoProposals(ctx context.Context, daoID int64, status *string, before *int64, limit int) ([]repository.DaoProposal, error)
	DaoProposal(ctx context.Context, id int64) (*repository.DaoProposal, int64, error)
	DaoVotes(ctx context.Context, id int64, option *string, after *repository.DaoVoteKey, limit int) ([]repository.DaoVote, error)
	Denoms(ctx context.Context, denoms []string) (map[string]repository.Denom, error)
}

// DaoProposalController serves the DAO proposal routes.
type DaoProposalController struct {
	repo DaoProposalReader
	live DaoState
	now  func() time.Time
}

// NewDaoProposalController returns a controller over repo. live gives a
// stake DAO's weight denom; nil (no gRPC address) leaves it null. now is the
// clock of voting_ended; nil is time.Now.
func NewDaoProposalController(repo DaoProposalReader, live DaoState, now func() time.Time) *DaoProposalController {
	if now == nil {
		now = time.Now
	}
	return &DaoProposalController{repo: repo, live: live, now: now}
}

// DaoProposals serves GET /api/v1/daos/{id}/proposals?status&cursor&limit,
// newest first. A DAO the index does not know is a 404.
func (h *DaoProposalController) DaoProposals(c *echo.Context) error {
	id, err := daoID(c)
	if err != nil {
		return err
	}
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	st, err := oneOf(c, "status", daoProposalStatuses)
	if err != nil {
		return err
	}
	var before *int64
	if s := c.QueryParam("cursor"); s != "" {
		keys, err := dto.DecodeCursor(s, 1)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "cursor is not valid")
		}
		before = &keys[0]
	}
	ctx := c.Request().Context()
	if _, err := h.repo.Dao(ctx, id); errors.Is(err, repository.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("dao %d is not known", id))
	} else if err != nil {
		return err
	}
	rows, err := h.repo.DaoProposals(ctx, id, st, before, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.DaoProposalSummary]{Items: make([]dto.DaoProposalSummary, 0, min(len(rows), limit))}
	for i, p := range rows {
		if i == limit {
			next := dto.EncodeCursor(rows[i-1].ID)
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewDaoProposalSummary(p))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// DaoProposal serves GET /api/v1/dao-proposals/{id}: the indexed proposal,
// its DAO, voting_ended, expedite_settleable and the decision checks.
func (h *DaoProposalController) DaoProposal(c *echo.Context) error {
	id, err := daoID(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	p, votes, err := h.repo.DaoProposal(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("dao proposal %d is not known", id))
	}
	if err != nil {
		return err
	}
	weight, err := h.weightDenom(ctx, p.DaoID, p.DaoType)
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.NewDaoProposal(*p, votes, weight, h.now()))
}

// DaoVotes serves GET /api/v1/dao-proposals/{id}/votes?option&cursor&limit:
// heaviest effective weight first, then by voter.
func (h *DaoProposalController) DaoVotes(c *echo.Context) error {
	id, err := daoID(c)
	if err != nil {
		return err
	}
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	option, err := oneOf(c, "option", daoVoteOptions)
	if err != nil {
		return err
	}
	var after *repository.DaoVoteKey
	if s := c.QueryParam("cursor"); s != "" {
		if after, err = dto.DecodeDaoVoteCursor(s); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "cursor is not valid")
		}
	}
	ctx := c.Request().Context()
	p, _, err := h.repo.DaoProposal(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("dao proposal %d is not known", id))
	}
	if err != nil {
		return err
	}
	rows, err := h.repo.DaoVotes(ctx, id, option, after, limit+1)
	if err != nil {
		return err
	}
	resp := dto.DaoVoteList{Items: make([]dto.DaoVote, 0, min(len(rows), limit))}
	for i, v := range rows {
		if i == limit {
			next := dto.EncodeDaoVoteCursor(rows[i-1])
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewDaoVote(v))
	}
	if resp.WeightDenom, err = h.weightDenom(ctx, p.DaoID, p.DaoType); err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// weightDenom is the denom a stake DAO counts its votes in: the voting stake
// denom of its live electorate. Nil for a static-list DAO (member votes),
// and when the node cannot tell.
func (h *DaoProposalController) weightDenom(ctx context.Context, daoID *int64, daoType *string) (*dto.WeightDenom, error) {
	if h.live == nil || daoID == nil || daoType == nil || *daoType != "stake" {
		return nil, nil
	}
	live, err := h.live.Live(ctx, uint64(*daoID))
	if err != nil || live.Stake == nil || live.Stake.VotingStake.Denom == "" {
		return nil, nil // a node that is down leaves the denom out
	}
	denom := live.Stake.VotingStake.Denom
	known, err := h.repo.Denoms(ctx, []string{denom})
	if err != nil {
		return nil, err
	}
	return dto.NewWeightDenom(denom, known), nil
}
