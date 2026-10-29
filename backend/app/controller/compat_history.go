package controller

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/marketmerge"
	"github.com/bze-alphateam/bze-scan/backend/internal/tradehistory"
)

// TradeHistory serves the DEX's trades in display units; implemented by
// tradehistory.Service.
type TradeHistory interface {
	Trades(ctx context.Context, f tradehistory.Filter, limit int, folded ...string) ([]tradehistory.Trade, error)
}

// CompatHistoryController serves the aggregator-compatible GET
// /aggregator/api/dex/history.
type CompatHistoryController struct {
	history TradeHistory
	merges  marketmerge.Merges
	log     log.FieldLogger
}

// NewCompatHistoryController returns a controller over history; merges
// folds markets for the coingecko format. A nil lg is the standard logger.
func NewCompatHistoryController(history TradeHistory, merges marketmerge.Merges, lg log.FieldLogger) *CompatHistoryController {
	if lg == nil {
		lg = log.StandardLogger()
	}
	return &CompatHistoryController{history: history, merges: merges, log: lg}
}

// History serves GET /aggregator/api/dex/history?market_id=|ticker_id=|address=
// &limit=&start_time=&end_time=&type=&format= as the aggregator does: the
// last limit trades (500 by default, 1000 at most) matching every filter
// given, newest first. ticker_id is a market id with "/" for "_". The time
// bounds are unix milliseconds, inclusive, and apply only when both are
// above 0; type filters buy or sell. format=coingecko splits the trades by
// side and, for a market others are folded into (COINGECKO_MERGED_MARKETS),
// merges theirs in. Every error is a 400 with the aggregator's messages.
func (h *CompatHistoryController) History(c *echo.Context) error {
	q := c.Request().URL.Query()
	asked, okL := queryInt(q, "limit")
	from, okF := queryInt(q, "start_time")
	to, okT := queryInt(q, "end_time")
	if !okL || !okF || !okT {
		return c.JSON(http.StatusBadRequest, dto.CompatError{Message: dto.CompatInvalidRequest})
	}
	limit := int(asked)
	switch {
	case asked <= 0:
		limit = tradehistory.DefaultLimit
	case asked > tradehistory.MaxLimit:
		limit = tradehistory.MaxLimit
	}
	// The aggregator's checks, lengths included: a one-character id is no id.
	marketID, tickerID, address := q.Get("market_id"), q.Get("ticker_id"), q.Get("address")
	if address == "" && len(marketID) <= 1 && len(tickerID) <= 1 {
		return c.JSON(http.StatusBadRequest, dto.CompatError{Message: "please provide market_id or ticker_id"})
	}
	if marketID == "" {
		marketID = strings.ReplaceAll(tickerID, "_", "/")
	}
	f := tradehistory.Filter{MarketID: marketID, Address: address, Side: q.Get("type"), FromMs: from, ToMs: to}

	ctx := c.Request().Context()
	if q.Get("format") != dto.CompatFormatCoinGecko {
		trades, err := h.history.Trades(ctx, f, limit)
		if err != nil {
			return h.fail(c, err, f)
		}
		out := make([]dto.CompatHistoryTrade, len(trades))
		for i, t := range trades {
			out[i] = dto.CompatHistoryTrade{
				OrderID: t.TradeID, Price: t.Price, BaseVolume: t.BaseVolume, QuoteVolume: t.QuoteVolume,
				ExecutedAt: strconv.FormatInt(t.TimeMs, 10), OrderType: t.Side, Maker: t.Maker, Taker: t.Taker,
			}
		}
		return c.JSON(http.StatusOK, out)
	}

	trades, err := h.history.Trades(ctx, f, limit, h.merges.FoldedInto(marketID)...)
	if err != nil {
		return h.fail(c, err, f)
	}
	out := dto.CompatCoinGeckoHistory{}
	for _, t := range trades {
		tr := dto.CompatCoinGeckoTrade{TradeID: t.TradeID, Price: t.Price, BaseVolume: t.BaseVolume,
			TargetVolume: t.QuoteVolume, TradeTimestamp: strconv.FormatInt(t.TimeMs, 10), Type: t.Side}
		if t.Side == "buy" {
			out.Buy = append(out.Buy, tr)
		} else {
			out.Sell = append(out.Sell, tr)
		}
	}
	return c.JSON(http.StatusOK, out)
}

// queryInt reads an integer parameter as the aggregator's binding does:
// missing or empty is 0, anything else must be a base-10 integer.
func queryInt(q url.Values, name string) (int64, bool) {
	s := q.Get(name)
	if s == "" {
		return 0, true
	}
	v, err := strconv.ParseInt(s, 10, 64)
	return v, err == nil
}

func (h *CompatHistoryController) fail(c *echo.Context, err error, f tradehistory.Filter) error {
	h.log.WithError(err).WithField("market", f.MarketID).Error("dex history")
	return c.JSON(http.StatusBadRequest, dto.CompatError{Message: dto.CompatUnknownError})
}
