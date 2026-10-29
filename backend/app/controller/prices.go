package controller

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// PriceReader reads the priced denoms. Implemented by repository.Explorer.
type PriceReader interface {
	// Prices returns every denom with a price, by denom.
	Prices(ctx context.Context) ([]repository.Price, error)
}

// PriceController serves GET /api/v1/prices: the prices job's USD prices of
// every denom it priced, read from explorer.denoms (no computation at
// request time).
type PriceController struct {
	repo PriceReader
}

// NewPriceController returns a controller over repo.
func NewPriceController(repo PriceReader) *PriceController {
	return &PriceController{repo: repo}
}

// Prices serves the priced denoms. The job writes every minute, so the
// answer is cached for CacheIndexed.
func (h *PriceController) Prices(c *echo.Context) error {
	rows, err := h.repo.Prices(c.Request().Context())
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheIndexed)
	return c.JSON(http.StatusOK, dto.NewPrices(rows))
}
