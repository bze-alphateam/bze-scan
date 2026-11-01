package controller

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainstate"
	"github.com/bze-alphateam/bze-scan/backend/internal/openorders"
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
	// ActivityPage returns up to limit activity rows of address, newest
	// first, of category when not nil, skipping the newest offset rows.
	ActivityPage(ctx context.Context, address string, category *string, offset int64,
		limit int) ([]repository.Activity, error)
	// ActivityTotal returns the number of activity rows of address, of
	// category when not nil.
	ActivityTotal(ctx context.Context, address string, category *string) (int64, error)
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

// OpenOrdersReader answers what an account has locked in open DEX orders.
// Implemented by openorders.Reader; a node path failure wraps
// openorders.ErrUpstream.
type OpenOrdersReader interface {
	OpenOrders(ctx context.Context, address string) (openorders.Summary, error)
}

// AccountController serves GET /api/v1/accounts/{address} and its
// activity list.
type AccountController struct {
	repo       AccountReader
	live       AccountState
	openOrders OpenOrdersReader
}

// NewAccountController returns a controller reading the explorer tables
// from repo, the live state from live and the open orders from openOrders;
// a nil live reports the live state as unavailable, a nil openOrders
// answers the open orders route with a 404.
func NewAccountController(repo AccountReader, live AccountState, openOrders OpenOrdersReader) *AccountController {
	return &AccountController{repo: repo, live: live, openOrders: openOrders}
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

// Activity serves GET /api/v1/accounts/{address}/activity?category&cursor|page&limit:
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
	pageNo, err := parsePage(c)
	if err != nil {
		return err
	}
	if pageNo > 0 && before != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "use either page or cursor")
	}

	ctx := c.Request().Context()
	var rows []repository.Activity
	var total *int64
	if pageNo > 0 {
		n, err := h.repo.ActivityTotal(ctx, address, category)
		if err != nil {
			return err
		}
		total = &n
		rows, err = h.repo.ActivityPage(ctx, address, category, (pageNo-1)*int64(limit), limit+1)
		if err != nil {
			return err
		}
	} else if rows, err = h.repo.Activity(ctx, address, category, before, limit+1); err != nil {
		return err
	}
	page := rows[:min(len(rows), limit)]
	denoms, err := h.repo.Denoms(ctx, dto.ActivityDenoms(page))
	if err != nil {
		return err
	}
	resp := dto.PagedList[dto.Activity]{List: dto.List[dto.Activity]{Items: make([]dto.Activity, 0, len(page))},
		Total: total}
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

// OpenOrders serves GET /api/v1/accounts/{address}/open-orders: what the
// address has locked in open DEX orders, per denom, then per market, from
// the explorer's orders table when its history is complete and from the node
// otherwise. Anything but a bze1 address is a 400; an account without open
// orders is a 200 with no assets; nodes that do not answer are a 502.
func (h *AccountController) OpenOrders(c *echo.Context) error {
	if h.openOrders == nil {
		return echo.ErrNotFound
	}
	address, err := accountParam(c)
	if err != nil {
		return err
	}
	s, err := h.openOrders.OpenOrders(c.Request().Context(), address)
	if errors.Is(err, openorders.ErrUpstream) {
		return echo.NewHTTPError(http.StatusBadGateway, "the nodes did not answer").Wrap(err)
	}
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.NewOpenOrders(s))
}
