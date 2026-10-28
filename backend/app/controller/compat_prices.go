package controller

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/coingecko"
)

// PriceFeed serves CoinGecko's USD prices from memory; implemented by
// coingecko.Feed.
type PriceFeed interface {
	Prices(ctx context.Context) []coingecko.Price
}

// CompatPricesController serves the aggregator-compatible GET /api/prices.
type CompatPricesController struct {
	feed PriceFeed
}

// NewCompatPricesController returns a controller over feed.
func NewCompatPricesController(feed PriceFeed) *CompatPricesController {
	return &CompatPricesController{feed: feed}
}

// Prices serves GET /api/prices as the aggregator does: the configured
// CoinGecko ids' USD prices, always 200. With no price read in the last 24
// hours the list is empty ([], where the aggregator answers null).
func (h *CompatPricesController) Prices(c *echo.Context) error {
	list := h.feed.Prices(c.Request().Context())
	out := make([]dto.CompatPrice, len(list))
	for i, p := range list {
		out[i] = dto.CompatPrice{Denom: p.ID, Price: p.Price, PriceDenom: coingecko.USD}
	}
	return c.JSON(http.StatusOK, out)
}
