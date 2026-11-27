package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainstate"
	"github.com/bze-alphateam/bze-scan/backend/internal/daostate"
)

// daoTypes are the values of the type filter.
var daoTypes = []string{"static_list", "stake"}

// DaoReader is the read side of the DAO tables the routes need. Implemented
// by repository.Explorer; a missing DAO is repository.ErrNotFound.
type DaoReader interface {
	Daos(ctx context.Context, f repository.DaoFilter, before *int64, limit int) ([]repository.Dao, error)
	DaosPage(ctx context.Context, f repository.DaoFilter, offset int64, limit int) ([]repository.Dao, error)
	DaosTotal(ctx context.Context, f repository.DaoFilter) (int64, error)
	Dao(ctx context.Context, id int64) (*repository.Dao, error)
	DaoEvents(ctx context.Context, id int64, before *repository.DaoEventKey, limit int) ([]repository.DaoEvent, error)
	Denoms(ctx context.Context, denoms []string) (map[string]repository.Denom, error)
	Labels(ctx context.Context, addresses []string) (map[string]repository.Label, error)
}

// DaoState reads DAOs live from the node. Implemented by daostate.Reader.
// Live answers daostate.ErrNotFound for a DAO the chain does not have and
// daostate.ErrUnsupported for a node without x/dao.
type DaoState interface {
	Live(ctx context.Context, id uint64) (*daostate.Live, error)
	Supported(ctx context.Context) bool
}

// DaoTreasury reads the balances of a DAO's treasury account. Implemented by
// chainstate.Reader.
type DaoTreasury interface {
	Balances(ctx context.Context, address string) ([]chainstate.Coin, error)
}

// DaoController serves the /api/v1/daos routes.
type DaoController struct {
	repo     DaoReader
	live     DaoState
	treasury DaoTreasury
}

// NewDaoController returns a controller over repo. A nil live (no gRPC
// address) reports every DAO's live part unavailable and the list as not
// supported; a nil treasury leaves the treasury empty.
func NewDaoController(repo DaoReader, live DaoState, treasury DaoTreasury) *DaoController {
	return &DaoController{repo: repo, live: live, treasury: treasury}
}

// Daos serves GET /api/v1/daos?type&admin&member&source&pending_admin&q&cursor&page&limit,
// newest first. page (1-based, not with cursor) answers numbered pages and
// adds total, the number of DAOs the filters match. admin lists the DAOs an address administers, the
// self-governed ones left out; member the DAOs whose member list holds an
// address; source (repeatable, up to maxDaoSources, OR-ed) the DAOs whose
// electorate uses one of the sources (sr/{id} or dr/{denom}); pending_admin
// the DAOs with a nomination pending for an address; q the DAOs whose name
// contains the text (at least 2 characters, case-insensitive). The filters
// combine with AND.
// supported is false while the node does not run x/dao yet.
func (h *DaoController) Daos(c *echo.Context) error {
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	var f repository.DaoFilter
	if f.Type, err = oneOf(c, "type", daoTypes); err != nil {
		return err
	}
	for _, a := range []struct {
		name string
		dst  **string
	}{{"admin", &f.Admin}, {"member", &f.Member}, {"pending_admin", &f.PendingAdmin}} {
		if s := c.QueryParam(a.name); s != "" {
			hrp, addr, ok := bech32Address(s)
			if !ok || hrp != accountPrefix {
				return echo.NewHTTPError(http.StatusBadRequest, a.name+" must be a bze1 address")
			}
			*a.dst = &addr
		}
	}
	if f.Sources, err = daoSources(c); err != nil {
		return err
	}
	if f.Name, err = daoNameQuery(c); err != nil {
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
	pageNo, err := parsePage(c)
	if err != nil {
		return err
	}
	if pageNo > 0 && before != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "use either page or cursor")
	}
	ctx := c.Request().Context()
	resp := dto.DaoList{Items: make([]dto.DaoSummary, 0, limit), Supported: h.live != nil && h.live.Supported(ctx)}
	var rows []repository.Dao
	if pageNo > 0 {
		var total int64
		if resp.Supported {
			if total, err = h.repo.DaosTotal(ctx, f); err != nil {
				return err
			}
			if rows, err = h.repo.DaosPage(ctx, f, (pageNo-1)*int64(limit), limit+1); err != nil {
				return err
			}
		}
		resp.Total = &total
	} else if rows, err = h.repo.Daos(ctx, f, before, limit+1); err != nil {
		return err
	}
	for i, d := range rows {
		if i == limit {
			next := dto.EncodeCursor(rows[i-1].ID)
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewDaoSummary(d))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// Dao serves GET /api/v1/daos/{id}: the indexed row with the chain's DAO, its
// controller and its treasury read live. A failed live read is a 200 with
// "live": {"available": false}; the DAO is a 404 when the chain and the index
// both lack it.
func (h *DaoController) Dao(c *echo.Context) error {
	id, err := daoID(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	row, err := h.repo.Dao(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		row, err = nil, nil
	}
	if err != nil {
		return err
	}
	var live *daostate.Live
	if h.live != nil {
		// A node that is down, or without x/dao, leaves the live part out.
		if live, err = h.live.Live(ctx, uint64(id)); err != nil {
			live = nil
		}
	}
	if row == nil {
		if live == nil {
			return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("dao %d is not known", id))
		}
		// The chain has it and the state sync has not written it yet.
		d := live.Dao
		row = &repository.Dao{ID: id, Address: d.Address, Type: d.Type, Name: d.Name, Admin: d.Admin,
			SelfGoverned: d.Admin == d.Address}
	}

	var treasury []chainstate.Coin
	if live != nil && h.treasury != nil {
		if treasury, err = h.treasury.Balances(ctx, row.Address); err != nil {
			live, treasury = nil, nil
		}
	}
	lookups := dto.DaoLookups{Denoms: map[string]repository.Denom{}, Labels: map[string]repository.Label{}}
	if live != nil {
		if lookups, err = h.lookups(ctx, row, live, treasury); err != nil {
			return err
		}
	} else if lookups.Labels, err = h.repo.Labels(ctx, []string{row.Address}); err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.NewDao(*row, live, treasury, lookups))
}

// lookups reads the display data of the denoms and the addresses the live
// answer names.
func (h *DaoController) lookups(ctx context.Context, row *repository.Dao, live *daostate.Live,
	treasury []chainstate.Coin) (dto.DaoLookups, error) {
	denoms := []string{}
	for _, c := range treasury {
		denoms = append(denoms, c.Denom)
	}
	cfg := live.Dao.Config
	for _, c := range []*daostate.Coin{cfg.ProposalCost, cfg.ProposalDeposit} {
		if c != nil {
			denoms = append(denoms, c.Denom)
		}
	}
	if live.Stake != nil {
		denoms = append(denoms, live.Stake.VotingStake.Denom)
		for _, s := range live.Stake.Sources {
			denoms = append(denoms, s.StakedAmount.Denom)
		}
	}
	var lk dto.DaoLookups
	var err error
	if lk.Denoms, err = h.repo.Denoms(ctx, denoms); err != nil {
		return lk, err
	}
	addresses := []string{row.Address, live.Controller.Address}
	if live.Dao.PendingAdmin != "" {
		addresses = append(addresses, live.Dao.PendingAdmin)
	}
	lk.Labels, err = h.repo.Labels(ctx, addresses)
	return lk, err
}

// DaoHistory serves GET /api/v1/daos/{id}/history?cursor&limit: the changes
// made to a DAO (creation, admin handovers, rule, electorate and member
// updates), newest first. A DAO the index has no rows of is an empty list
// when the DAO exists, a 404 otherwise.
func (h *DaoController) DaoHistory(c *echo.Context) error {
	id, err := daoID(c)
	if err != nil {
		return err
	}
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	var before *repository.DaoEventKey
	if s := c.QueryParam("cursor"); s != "" {
		if before, err = dto.DecodeDaoEventCursor(s); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "cursor is not valid")
		}
	}
	ctx := c.Request().Context()
	if _, err := h.repo.Dao(ctx, id); errors.Is(err, repository.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("dao %d is not known", id))
	} else if err != nil {
		return err
	}
	rows, err := h.repo.DaoEvents(ctx, id, before, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.DaoEvent]{Items: make([]dto.DaoEvent, 0, min(len(rows), limit))}
	for i, e := range rows {
		if i == limit {
			prev := rows[i-1]
			next := dto.EncodeDaoEventCursor(repository.DaoEventKey{Height: prev.Height, TxIndex: prev.TxIndex,
				MsgIndex: prev.MsgIndex, Seq: prev.Seq})
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewDaoEvent(e))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

func daoID(c *echo.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, "id must be a positive integer")
	}
	return id, nil
}

// maxDaoSources is the most source values a DAOs request may carry.
const maxDaoSources = 20

// minDaoNameQuery and maxDaoNameQuery bound the q parameter, in characters.
const (
	minDaoNameQuery = 2
	maxDaoNameQuery = 100
)

// maxSourceLen bounds one source value (a denom is at most 128 characters).
const maxSourceLen = 140

// daoSources reads and validates the repeatable source parameter.
func daoSources(c *echo.Context) ([]string, error) {
	values := c.QueryParams()["source"]
	if len(values) > maxDaoSources {
		return nil, echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("source takes at most %d values", maxDaoSources))
	}
	for _, v := range values {
		kind, rest, ok := strings.Cut(v, "/")
		valid := ok && (kind == "sr" || kind == "dr") && rest != "" && len(v) <= maxSourceLen
		if valid && kind == "sr" {
			// A staking reward id is a uint.
			_, err := strconv.ParseUint(rest, 10, 64)
			valid = err == nil
		}
		if !valid {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "source must be sr/{id} or dr/{denom}")
		}
	}
	return values, nil
}

// daoNameQuery reads and validates the q parameter; nil when absent.
func daoNameQuery(c *echo.Context) (*string, error) {
	if _, present := c.QueryParams()["q"]; !present {
		return nil, nil
	}
	q := strings.TrimSpace(c.QueryParam("q"))
	if n := utf8.RuneCountInString(q); n < minDaoNameQuery || n > maxDaoNameQuery {
		return nil, echo.NewHTTPError(http.StatusBadRequest,
			fmt.Sprintf("q must be %d to %d characters", minDaoNameQuery, maxDaoNameQuery))
	}
	return &q, nil
}
