package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// proposalStatuses are the values of the status filter.
var proposalStatuses = []string{"deposit_period", "voting_period", "passed", "rejected", "failed", "canceled"}

// voteOptions are the values of the votes' option filter: a single option,
// or the split votes.
var voteOptions = []string{"yes", "no", "abstain", "no_with_veto", "weighted"}

// ProposalReader is the read side of the governance tables the proposals
// routes need. Implemented by repository.Explorer.
type ProposalReader interface {
	Proposals(ctx context.Context, status *string, before *int64, limit int) ([]repository.ProposalSummary, error)
	Proposal(ctx context.Context, id int64) (*repository.Proposal, error)
	ProposalVotes(ctx context.Context, id int64, option *string, before *repository.VoteKey, limit int) ([]repository.ProposalVote, error)
	ProposalDeposits(ctx context.Context, id int64, before *repository.DepositKey, limit int) ([]repository.ProposalDeposit, error)
}

// ProposalController serves the /api/v1/proposals routes. Status and tally
// move until a proposal resolves, so nothing here is cached.
type ProposalController struct {
	repo ProposalReader
}

// NewProposalController returns a controller over repo.
func NewProposalController(repo ProposalReader) *ProposalController {
	return &ProposalController{repo: repo}
}

// Proposals serves GET /api/v1/proposals?status&cursor&limit, newest first.
func (h *ProposalController) Proposals(c *echo.Context) error {
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	status, err := oneOf(c, "status", proposalStatuses)
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
	rows, err := h.repo.Proposals(c.Request().Context(), status, before, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.ProposalSummary]{Items: make([]dto.ProposalSummary, 0, min(len(rows), limit))}
	for i, p := range rows {
		if i == limit {
			next := dto.EncodeCursor(rows[i-1].ID)
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewProposalSummary(p))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// Proposal serves GET /api/v1/proposals/{id}.
func (h *ProposalController) Proposal(c *echo.Context) error {
	id, err := proposalID(c)
	if err != nil {
		return err
	}
	p, err := h.proposal(c.Request().Context(), id)
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.NewProposal(p))
}

// ProposalVotes serves GET /api/v1/proposals/{id}/votes?option&cursor&limit,
// newest first: the explorer keeps every vote, the chain deletes them once
// tallied.
func (h *ProposalController) ProposalVotes(c *echo.Context) error {
	id, limit, err := h.page(c)
	if err != nil {
		return err
	}
	option, err := oneOf(c, "option", voteOptions)
	if err != nil {
		return err
	}
	var before *repository.VoteKey
	if s := c.QueryParam("cursor"); s != "" {
		if before, err = dto.DecodeVoteCursor(s); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "cursor is not valid")
		}
	}
	rows, err := h.repo.ProposalVotes(c.Request().Context(), id, option, before, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.ProposalVote]{Items: make([]dto.ProposalVote, 0, min(len(rows), limit))}
	for i, v := range rows {
		if i == limit {
			prev := rows[i-1]
			next := dto.EncodeVoteCursor(repository.VoteKey{Height: prev.Height, TxIndex: prev.TxIndex, Voter: prev.Voter})
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewProposalVote(v))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// ProposalDeposits serves GET /api/v1/proposals/{id}/deposits?cursor&limit,
// newest first.
func (h *ProposalController) ProposalDeposits(c *echo.Context) error {
	id, limit, err := h.page(c)
	if err != nil {
		return err
	}
	var before *repository.DepositKey
	if s := c.QueryParam("cursor"); s != "" {
		if before, err = dto.DecodeDepositCursor(s); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "cursor is not valid")
		}
	}
	rows, err := h.repo.ProposalDeposits(c.Request().Context(), id, before, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.ProposalDeposit]{Items: make([]dto.ProposalDeposit, 0, min(len(rows), limit))}
	for i, d := range rows {
		if i == limit {
			prev := rows[i-1]
			next := dto.EncodeDepositCursor(repository.DepositKey{Height: prev.Height, TxIndex: prev.TxIndex, Depositor: prev.Depositor})
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewProposalDeposit(d))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// page parses the id and the limit of a proposal list route and checks the
// proposal is known.
func (h *ProposalController) page(c *echo.Context) (int64, int, error) {
	id, err := proposalID(c)
	if err != nil {
		return 0, 0, err
	}
	limit, err := parseLimit(c)
	if err != nil {
		return 0, 0, err
	}
	if _, err := h.proposal(c.Request().Context(), id); err != nil {
		return 0, 0, err
	}
	return id, limit, nil
}

func (h *ProposalController) proposal(ctx context.Context, id int64) (*repository.Proposal, error) {
	p, err := h.repo.Proposal(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("proposal %d is not known", id))
	}
	return p, err
}

func proposalID(c *echo.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, "id must be a positive integer")
	}
	return id, nil
}

// oneOf is the query parameter name when it is one of values, nil when
// absent.
func oneOf(c *echo.Context, name string, values []string) (*string, error) {
	v := c.QueryParam(name)
	if v == "" {
		return nil, nil
	}
	if !slices.Contains(values, v) {
		return nil, echo.NewHTTPError(http.StatusBadRequest, name+" must be one of "+strings.Join(values, ", "))
	}
	return &v, nil
}
