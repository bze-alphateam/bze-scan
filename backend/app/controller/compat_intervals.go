package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
)

// Intervals serves the candles in display units; implemented by
// candles.Service.
type Intervals interface {
	Intervals(ctx context.Context, market string, minutes, limit int) ([]candles.Interval, error)
}

// The aggregator's intervals limits.
const (
	compatIntervalsDefaultLimit = 500
	compatIntervalsMaxLimit     = 5000
)

// CompatIntervalsController serves the aggregator-compatible GET
// /aggregator/api/dex/intervals.
type CompatIntervalsController struct {
	intervals Intervals
	log       log.FieldLogger
}

// NewCompatIntervalsController returns a controller over intervals. A nil
// lg is the standard logger.
func NewCompatIntervalsController(intervals Intervals, lg log.FieldLogger) *CompatIntervalsController {
	if lg == nil {
		lg = log.StandardLogger()
	}
	return &CompatIntervalsController{intervals: intervals, log: lg}
}

// Intervals serves GET
// /aggregator/api/dex/intervals?market_id=|ticker_id=&minutes=&limit=&format=
// as the aggregator does: minutes one of candles.Lengths, limit 500 when
// absent or not positive and at most 5000 except for daily candles,
// ticker_id a market id with "_" for "/" (a pool is asked by its market_id),
// format=tv the TradingView shape. The candles ascend; [] when none. The
// parameters are bound the way echo v4's binder does (a name matched
// without case when not exactly, an empty number is 0, a non-number is
// "invalid request"); every error is a 400 with the aggregator's messages,
// an unknown market its "unknown error".
func (h *CompatIntervalsController) Intervals(c *echo.Context) error {
	q := c.Request().URL.Query()
	var minutes, limit int
	for _, p := range []struct {
		name string
		to   *int
	}{{"minutes", &minutes}, {"limit", &limit}} {
		v, ok := queryParam(q, p.name)
		if !ok {
			continue
		}
		if v == "" {
			v = "0"
		}
		n, err := strconv.ParseInt(v, 10, 0)
		if err != nil {
			return c.JSON(http.StatusBadRequest, dto.CompatError{Message: dto.CompatInvalidRequest})
		}
		*p.to = int(n)
	}
	marketID, _ := queryParam(q, "market_id")
	tickerID, _ := queryParam(q, "ticker_id")
	format, _ := queryParam(q, "format")

	if !slices.Contains(candles.Lengths, minutes) {
		return c.JSON(http.StatusBadRequest, dto.CompatError{Message: fmt.Sprintf("invalid minutes. expected: %d, %d, %d, %d, %d",
			candles.Lengths[0], candles.Lengths[1], candles.Lengths[2], candles.Lengths[3], candles.Lengths[4])})
	}
	if limit <= 0 {
		limit = compatIntervalsDefaultLimit
	} else if minutes != 1440 && limit > compatIntervalsMaxLimit {
		return c.JSON(http.StatusBadRequest, dto.CompatError{Message: fmt.Sprintf("limit can not be greater than %d", compatIntervalsMaxLimit)})
	}
	// The aggregator's checks, lengths included: a one-character id is no id.
	if len(marketID) <= 1 && len(tickerID) <= 1 {
		return c.JSON(http.StatusBadRequest, dto.CompatError{Message: "please provide market_id or ticker_id"})
	}
	if marketID == "" {
		marketID = strings.ReplaceAll(tickerID, "_", "/")
	}

	ivs, err := h.intervals.Intervals(c.Request().Context(), marketID, minutes, limit)
	if err != nil {
		lg := h.log.WithError(err).WithField("market", marketID)
		if errors.Is(err, candles.ErrUnknownMarket) {
			lg.Info("dex intervals: unknown market")
		} else {
			lg.Error("dex intervals")
		}
		return c.JSON(http.StatusBadRequest, dto.CompatError{Message: dto.CompatUnknownError})
	}
	if format == "tv" {
		out := make([]dto.CompatTVInterval, len(ivs))
		for i, iv := range ivs {
			out[i] = dto.CompatTVInterval{Time: iv.Start.Unix(), Low: compatFloat(iv.Low), Open: compatFloat(iv.Open),
				High: compatFloat(iv.High), Close: compatFloat(iv.Close), Value: compatFloat(iv.BaseVolume)}
		}
		return c.JSON(http.StatusOK, out)
	}
	out := make([]dto.CompatInterval, len(ivs))
	for i, iv := range ivs {
		out[i] = dto.CompatInterval{MarketID: marketID, Minutes: minutes, StartAt: iv.Start, EndAt: iv.End,
			LowestPrice: iv.Low, OpenPrice: iv.Open, AveragePrice: iv.Average, HighestPrice: iv.High,
			ClosePrice: iv.Close, BaseVolume: iv.BaseVolume, QuoteVolume: iv.QuoteVolume}
	}
	return c.JSON(http.StatusOK, out)
}

// queryParam is the first value of name, else of a parameter named name in
// another case (echo v4's binder); false when there is none.
func queryParam(q url.Values, name string) (string, bool) {
	if v, ok := q[name]; ok && len(v) > 0 {
		return v[0], true
	}
	for k, v := range q {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0], true
		}
	}
	return "", false
}

// compatFloat is a display string as the aggregator's database driver
// scans it into a float64.
func compatFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}
