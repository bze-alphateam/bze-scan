package controller

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/supply"
)

// SupplyReader serves a denom's supply in display units; implemented by
// supply.Service.
type SupplyReader interface {
	Total(ctx context.Context, denom string) (string, error)
	Circulating(ctx context.Context, denom string) (string, error)
}

// CompatSupplyController serves the aggregator-compatible
// GET /api/supply/total and /api/supply/circulating.
type CompatSupplyController struct {
	supply SupplyReader
}

// NewCompatSupplyController returns a controller over r.
func NewCompatSupplyController(r SupplyReader) *CompatSupplyController {
	return &CompatSupplyController{supply: r}
}

// Total serves GET /api/supply/total?denom= (default ubze) as plain text.
func (h *CompatSupplyController) Total(c *echo.Context) error {
	return h.answer(c, h.supply.Total)
}

// Circulating serves GET /api/supply/circulating?denom= (default ubze) as
// plain text.
func (h *CompatSupplyController) Circulating(c *echo.Context) error {
	return h.answer(c, h.supply.Circulating)
}

// answer writes the supply, or the aggregator's 400 for a denom it gets
// wrong. Where the aggregator answers "0" on a node failure, the explorer
// answers a 503.
func (h *CompatSupplyController) answer(c *echo.Context, read func(context.Context, string) (string, error)) error {
	denom := c.QueryParam("denom")
	if denom == "" {
		denom = chain.BondDenom
	}
	v, err := read(c.Request().Context(), denom)
	var bad *supply.BadDenomError
	switch {
	case errors.As(err, &bad):
		return c.String(http.StatusBadRequest, bad.Error())
	case err != nil:
		return c.String(http.StatusServiceUnavailable, supply.ErrUnavailable.Error())
	}
	return c.String(http.StatusOK, v)
}
