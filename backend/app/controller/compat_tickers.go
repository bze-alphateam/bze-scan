package controller

import (
	"context"
	"math/big"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/marketmerge"
	"github.com/bze-alphateam/bze-scan/backend/internal/tickers"
)

// Tickers serves the DEX's 24-hour tickers by market id; implemented by
// tickers.Service.
type Tickers interface {
	Tickers(ctx context.Context) ([]tickers.Ticker, error)
}

// CompatTickersController serves the aggregator-compatible GET
// /aggregator/api/dex/tickers.
type CompatTickersController struct {
	tickers Tickers
	merges  marketmerge.Merges
	log     log.FieldLogger
}

// NewCompatTickersController returns a controller over t; merges folds
// markets for the coingecko format. A nil lg is the standard logger.
func NewCompatTickersController(t Tickers, merges marketmerge.Merges, lg log.FieldLogger) *CompatTickersController {
	if lg == nil {
		lg = log.StandardLogger()
	}
	return &CompatTickersController{tickers: t, merges: merges, log: lg}
}

// Tickers serves GET /aggregator/api/dex/tickers?format= as the aggregator
// does: every order book market and pool, numbers in display units.
// format=coingecko answers CoinGecko's shape, a market folded into another
// one (COINGECKO_MERGED_MARKETS) added to that one's ticker instead of
// listed. Both formats are by market id, where the aggregator's default one
// comes in random order, and [] when there is no market. A failure is the
// aggregator's 500 "unknown error".
func (h *CompatTickersController) Tickers(c *echo.Context) error {
	list, err := h.tickers.Tickers(c.Request().Context())
	if err != nil {
		h.log.WithError(err).Error("dex tickers")
		return c.JSON(http.StatusInternalServerError, dto.CompatError{Message: dto.CompatUnknownError})
	}
	if format, _ := queryParam(c.Request().URL.Query(), "format"); format == dto.CompatFormatCoinGecko {
		return c.JSON(http.StatusOK, h.coinGecko(list))
	}
	out := make([]dto.CompatTicker, len(list))
	for i, t := range list {
		out[i] = dto.CompatTicker{Base: t.Base, Quote: t.Quote, MarketID: t.MarketID, LastPrice: ratFloat(t.LastPrice),
			BaseVolume: ratFloat(t.BaseVolume), QuoteVolume: ratFloat(t.QuoteVolume), Bid: ratFloat(t.Bid), Ask: ratFloat(t.Ask),
			High: ratFloat(t.High), Low: ratFloat(t.Low), OpenPrice: ratFloat(t.OpenPrice), Change: t.Change}
	}
	return c.JSON(http.StatusOK, out)
}

// coinGecko converts list to CoinGecko's tickers with the folds applied: a
// folded market is left out when the market it reports as is listed.
func (h *CompatTickersController) coinGecko(list []tickers.Ticker) []dto.CompatCoinGeckoTicker {
	byID := make(map[string]dto.CompatCoinGeckoTicker, len(list))
	for _, t := range list {
		ct := dto.CompatCoinGeckoTicker{TickerID: t.Base + "_" + t.Quote, Base: t.Base, Quote: t.Quote, PoolID: t.MarketID,
			LastPrice: ratFloat(t.LastPrice), BaseVolume: ratFloat(t.BaseVolume), TargetVolume: ratFloat(t.QuoteVolume),
			Bid: ratFloat(t.Bid), Ask: ratFloat(t.Ask), High: ratFloat(t.High), Low: ratFloat(t.Low)}
		// The aggregator tells a pool by the "_" in its id.
		if strings.Contains(t.MarketID, "_") {
			ct.TickerID = "lp_" + ct.TickerID
			ct.PoolID = ct.TickerID
		}
		if t.LiquidityUSD != nil {
			ct.LiquidityInUSD = ratFloat(t.LiquidityUSD)
		}
		byID[t.MarketID] = ct
	}
	out := make([]dto.CompatCoinGeckoTicker, 0, len(list))
	for _, t := range list {
		if into, ok := h.merges.ReportedAs(t.MarketID); ok {
			if _, listed := byID[into]; listed {
				continue
			}
		}
		ct := byID[t.MarketID]
		for _, folded := range h.merges.FoldedInto(t.MarketID) {
			if other, ok := byID[folded]; ok {
				ct.Fold(other)
			}
		}
		out = append(out, ct)
	}
	return out
}

// ratFloat is r as the nearest float64, as the aggregator's numbers are.
func ratFloat(r *big.Rat) float64 {
	if r == nil {
		return 0
	}
	f, _ := r.Float64()
	return f
}
