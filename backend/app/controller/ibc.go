package controller

import (
	"net/http"
	"slices"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// IBCTransfers serves GET /api/v1/accounts/{address}/ibc?direction&cursor&limit:
// the cross-chain transfers the address sent (direction=out), received
// (in) or both (no direction), newest first, with their status.
func (h *AccountController) IBCTransfers(c *echo.Context) error {
	address, err := accountParam(c)
	if err != nil {
		return err
	}
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	direction := c.QueryParam("direction")
	if direction != "" && !slices.Contains(dto.IBCDirections, direction) {
		return echo.NewHTTPError(http.StatusBadRequest, "direction must be out or in")
	}
	var before *repository.IBCKey
	if s := c.QueryParam("cursor"); s != "" {
		if before, err = dto.DecodeIBCCursor(s); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "cursor is not valid")
		}
	}
	rows, err := h.repo.AccountIBCTransfers(c.Request().Context(), address, direction, before, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.IBCTransfer]{Items: make([]dto.IBCTransfer, 0, min(len(rows), limit))}
	for _, t := range rows[:min(len(rows), limit)] {
		resp.Items = append(resp.Items, dto.NewIBCTransfer(t))
	}
	if len(rows) > limit {
		next := dto.EncodeIBCCursor(rows[limit-1])
		resp.NextCursor = &next
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// IBCChannels serves GET /api/v1/ibc/channels: every IBC channel with its
// state and counterparty chain, by channel number. The list is short (one
// row per connected chain), so it has no pages.
func (h *ExplorerController) IBCChannels(c *echo.Context) error {
	rows, err := h.repo.IBCChannels(c.Request().Context())
	if err != nil {
		return err
	}
	resp := dto.List[dto.IBCChannel]{Items: make([]dto.IBCChannel, 0, len(rows))}
	for _, ch := range rows {
		resp.Items = append(resp.Items, dto.NewIBCChannel(ch))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}
