package controller

import (
	"context"
	"net/http"
	"slices"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainstate"
)

// AccountReader is the read side of the explorer tables the account route
// needs. Implemented by repository.Explorer.
type AccountReader interface {
	// Account returns the accounts row and label of an address; an address
	// the explorer has never seen has neither, which is not an error.
	Account(ctx context.Context, address string) (*repository.Account, error)
	// Activity returns up to limit activity rows of address, newest first,
	// of category when not nil, after before when not nil.
	Activity(ctx context.Context, address string, category *string, before *repository.ActivityKey,
		limit int) ([]repository.Activity, error)
	// AccountOrders returns up to limit orders owned by owner, newest
	// first, of status when not nil, after before when not nil.
	AccountOrders(ctx context.Context, owner string, status *string, before *repository.OrderKey,
		limit int) ([]repository.Order, error)
	// AccountFills returns up to limit fills where address is the maker or
	// the taker, newest first, after before when not nil.
	AccountFills(ctx context.Context, address string, before *repository.FillKey, limit int) ([]repository.OrderFill, error)
	// AccountIBCTransfers returns up to limit cross-chain transfers address
	// sent (direction out), received (in) or both (empty), newest first,
	// after before when not nil.
	AccountIBCTransfers(ctx context.Context, address, direction string, before *repository.IBCKey,
		limit int) ([]repository.IBCTransfer, error)
	Denoms(ctx context.Context, denoms []string) (map[string]repository.Denom, error)
	Monikers(ctx context.Context, operators []string) (map[string]string, error)
}

// AccountState reads an account's current state live from the node.
// Implemented by chainstate.Reader.
type AccountState interface {
	Account(ctx context.Context, address string) (*chainstate.Account, error)
}

// AccountController serves GET /api/v1/accounts/{address} and its
// activity list.
type AccountController struct {
	repo AccountReader
	live AccountState
}

// NewAccountController returns a controller reading the explorer tables
// from repo and the live state from live; a nil live reports the live state
// as unavailable.
func NewAccountController(repo AccountReader, live AccountState) *AccountController {
	return &AccountController{repo: repo, live: live}
}

// Account serves GET /api/v1/accounts/{address}: the explorer's first sight,
// counters and label, with the balances, delegations, unbonding entries and
// rewards read live. Anything but a bze1 address is a 400; an address the
// explorer has never seen is a 200 with null first_seen (the chain may know
// it). A failed live read is a 200 with "live": {"available": false}, so the
// page still renders.
func (h *AccountController) Account(c *echo.Context) error {
	hrp, address, ok := bech32Address(c.Param("address"))
	if !ok || hrp != accountPrefix {
		return echo.NewHTTPError(http.StatusBadRequest, "address must be a bze1 address")
	}
	ctx := c.Request().Context()
	acc, err := h.repo.Account(ctx, address)
	if err != nil {
		return err
	}

	var live *chainstate.Account
	if h.live != nil {
		// A node that is down leaves the live part out; the explorer's part
		// still answers.
		if live, err = h.live.Account(ctx, address); err != nil {
			live = nil
		}
	}
	denoms := map[string]repository.Denom{}
	monikers := map[string]string{}
	if live != nil {
		denomList := make([]string, 0, len(live.Balances))
		for _, b := range live.Balances {
			denomList = append(denomList, b.Denom)
		}
		if denoms, err = h.repo.Denoms(ctx, denomList); err != nil {
			return err
		}
		var operators []string
		for _, d := range live.Delegations {
			operators = append(operators, d.Validator)
		}
		for _, u := range live.Unbonding {
			operators = append(operators, u.Validator)
		}
		if monikers, err = h.repo.Monikers(ctx, operators); err != nil {
			return err
		}
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.NewAccount(acc, live, denoms, monikers))
}

// Activity serves GET /api/v1/accounts/{address}/activity?category&cursor&limit:
// the address's activity rows, newest first, one per transaction or block
// that touched it, optionally of one category. Anything but a bze1 address
// or a known category is a 400; an address without activity is an empty
// list.
func (h *AccountController) Activity(c *echo.Context) error {
	hrp, address, ok := bech32Address(c.Param("address"))
	if !ok || hrp != accountPrefix {
		return echo.NewHTTPError(http.StatusBadRequest, "address must be a bze1 address")
	}
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	var category *string
	if s := c.QueryParam("category"); s != "" {
		if !slices.Contains(dto.ActivityCategories, s) {
			return echo.NewHTTPError(http.StatusBadRequest, "category is not known")
		}
		category = &s
	}
	var before *repository.ActivityKey
	if s := c.QueryParam("cursor"); s != "" {
		if before, err = dto.DecodeActivityCursor(s); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "cursor is not valid")
		}
	}

	ctx := c.Request().Context()
	rows, err := h.repo.Activity(ctx, address, category, before, limit+1)
	if err != nil {
		return err
	}
	page := rows[:min(len(rows), limit)]
	denoms, err := h.repo.Denoms(ctx, dto.ActivityDenoms(page))
	if err != nil {
		return err
	}
	resp := dto.List[dto.Activity]{Items: make([]dto.Activity, 0, len(page))}
	for _, a := range page {
		resp.Items = append(resp.Items, dto.NewActivity(a, denoms))
	}
	if len(rows) > limit {
		next := dto.EncodeActivityCursor(rows[limit-1])
		resp.NextCursor = &next
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}
