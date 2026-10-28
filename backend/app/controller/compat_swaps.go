package controller

import (
	"context"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/swaphistory"
)

// SwapHistory serves an address's pool swaps in display units; implemented
// by swaphistory.Service.
type SwapHistory interface {
	ByAddress(ctx context.Context, address string) ([]swaphistory.Trade, error)
}

// CompatSwapsController serves the aggregator-compatible GET
// /aggregator/api/dex/swaps.
type CompatSwapsController struct {
	swaps SwapHistory
	log   log.FieldLogger
}

// NewCompatSwapsController returns a controller over swaps. A nil lg is the
// standard logger.
func NewCompatSwapsController(swaps SwapHistory, lg log.FieldLogger) *CompatSwapsController {
	if lg == nil {
		lg = log.StandardLogger()
	}
	return &CompatSwapsController{swaps: swaps, log: lg}
}

// Swaps serves GET /aggregator/api/dex/swaps?address= as the aggregator
// does: the address's last 100 swaps, newest first, [] when none. Unlike
// the aggregator, which never validates the address and answers everyone's
// last swaps without one, an empty address is a 400.
func (h *CompatSwapsController) Swaps(c *echo.Context) error {
	address := c.Request().URL.Query().Get("address")
	if address == "" {
		return c.JSON(http.StatusBadRequest, dto.CompatError{Message: dto.CompatInvalidRequest})
	}
	trades, err := h.swaps.ByAddress(c.Request().Context(), address)
	if err != nil {
		h.log.WithError(err).Error("dex swaps")
		return c.JSON(http.StatusInternalServerError, dto.CompatError{Message: "failed to get swap history"})
	}
	out := make([]dto.CompatHistoryTrade, len(trades))
	for i, t := range trades {
		out[i] = dto.CompatHistoryTrade{
			OrderID: t.TradeID, Price: t.Price, BaseVolume: t.BaseVolume, QuoteVolume: t.QuoteVolume,
			ExecutedAt: strconv.FormatInt(t.TimeMs, 10), OrderType: t.Side, Maker: "", Taker: t.Creator,
			PoolID: t.PoolID, Base: t.Base, Quote: t.Quote,
		}
	}
	return c.JSON(http.StatusOK, out)
}
