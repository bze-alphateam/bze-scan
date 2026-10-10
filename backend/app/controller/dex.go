package controller

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// OrderFillsInline is how many fills the order route carries, the newest.
const OrderFillsInline = MaxLimit

// accountParam is the bze1 address of an account route, or a 400.
func accountParam(c *echo.Context) (string, error) {
	hrp, address, ok := bech32Address(c.Param("address"))
	if !ok || hrp != accountPrefix {
		return "", echo.NewHTTPError(http.StatusBadRequest, "address must be a bze1 address")
	}
	return address, nil
}

// Orders serves GET /api/v1/accounts/{address}/orders?status&cursor&limit:
// the orders the address placed on the book, newest first, optionally of
// one status (open, filled or cancelled). A taker order that filled
// completely never rested, so it is no order: its fills list it.
func (h *AccountController) Orders(c *echo.Context) error {
	address, err := accountParam(c)
	if err != nil {
		return err
	}
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	var status *string
	if s := c.QueryParam("status"); s != "" {
		if !slices.Contains(dto.OrderStatuses, s) {
			return echo.NewHTTPError(http.StatusBadRequest, "status must be open, filled or cancelled")
		}
		status = &s
	}
	var before *repository.OrderKey
	if s := c.QueryParam("cursor"); s != "" {
		if before, err = dto.DecodeOrderCursor(s); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "cursor is not valid")
		}
	}
	rows, err := h.repo.AccountOrders(c.Request().Context(), address, status, before, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.Order]{Items: make([]dto.Order, 0, min(len(rows), limit))}
	for _, o := range rows[:min(len(rows), limit)] {
		resp.Items = append(resp.Items, dto.NewOrder(o))
	}
	if len(rows) > limit {
		next := dto.EncodeOrderCursor(rows[limit-1])
		resp.NextCursor = &next
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// Fills serves GET /api/v1/accounts/{address}/fills?cursor&limit: the
// fills where the address is the maker or the taker, newest first.
func (h *AccountController) Fills(c *echo.Context) error {
	address, err := accountParam(c)
	if err != nil {
		return err
	}
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	var before *repository.FillKey
	if s := c.QueryParam("cursor"); s != "" {
		if before, err = dto.DecodeFillCursor(s); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "cursor is not valid")
		}
	}
	rows, err := h.repo.AccountFills(c.Request().Context(), address, before, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.Fill]{Items: make([]dto.Fill, 0, min(len(rows), limit))}
	for _, f := range rows[:min(len(rows), limit)] {
		resp.Items = append(resp.Items, dto.NewFill(f))
	}
	if len(rows) > limit {
		next := dto.EncodeFillCursor(rows[limit-1])
		resp.NextCursor = &next
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// Order serves GET /api/v1/orders/{id}: the order (id given as up to 24
// digits, zero-filled), its number of fills and the newest
// OrderFillsInline of them. An order the explorer has not indexed is a 404.
func (h *ExplorerController) Order(c *echo.Context) error {
	id, ok := dto.NormaliseOrderID(c.Param("id"))
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "id must be an order id of up to 24 digits")
	}
	ctx := c.Request().Context()
	o, err := h.repo.Order(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("order %s is not indexed", id))
	}
	if err != nil {
		return err
	}
	count, fills, err := h.repo.OrderFills(ctx, id, OrderFillsInline)
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.NewOrderDetail(o, count, fills))
}
