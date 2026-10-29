package server_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/server"
	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
	"github.com/bze-alphateam/bze-scan/backend/internal/orderbook"
)

const ivMarket = "ubze/uvdl"

// ivReader serves the candles built from trades and the first trades.
type ivReader struct {
	candles []candles.Candle
	firsts  map[string]candles.First
}

func (r *ivReader) Candles(_ context.Context, market string, minutes int, from time.Time, limit int) ([]candles.Candle, error) {
	var out []candles.Candle
	for i := len(r.candles) - 1; i >= 0; i-- {
		c := r.candles[i]
		if c.Market == market && c.Minutes == minutes && !c.Start.Before(from) && len(out) < limit {
			out = append(out, c)
		}
	}
	return out, nil
}

func (r *ivReader) First(_ context.Context, market string) (candles.First, bool, error) {
	f, ok := r.firsts[market]
	return f, ok, nil
}

func (r *ivReader) Created(context.Context, string, string) (time.Time, error) {
	return time.Time{}, nil
}

type ivMarkets struct{}

func (ivMarkets) Market(_ context.Context, id string) (orderbook.Market, error) {
	if id == ivMarket {
		return orderbook.Market{ID: id, Base: "ubze", Quote: "uvdl"}, nil
	}
	return orderbook.Market{}, orderbook.ErrUnknownMarket
}

type ivClock time.Time

func (c ivClock) Now() time.Time { return time.Time(c) }

func ivTime(hh, mm, ss int) time.Time { return time.Date(2026, 10, 28, hh, mm, ss, 0, time.UTC) }

func ivTrade(id int64, t time.Time, price string, base int64) candles.Trade {
	tr, err := candles.FillTrade(id, t, price, fmt.Sprint(base))
	if err != nil {
		panic(err)
	}
	return tr
}

// ivServer serves ubze/uvdl (6 and 6 decimals) at 10:07:30, with trades in
// the 10:00 bucket and the floor at 09:54.
func ivServer() *echo.Echo {
	trades := []candles.Trade{
		ivTrade(1, ivTime(9, 55, 0), "0.5", 4_000_000),
		ivTrade(2, ivTime(10, 1, 0), "1", 10_000_000),
		ivTrade(3, ivTime(10, 2, 0), "1.5", 10_000_000),
	}
	svc := candles.NewService(candles.ServiceDeps{
		Reader:    &ivReader{candles: candles.Build(ivMarket, trades), firsts: map[string]candles.First{ivMarket: {Time: ivTime(9, 55, 0)}}},
		Markets:   ivMarkets{},
		Exponents: swapExponents{"ubze": 6, "uvdl": 6},
		Clock:     ivClock(ivTime(10, 7, 30)),
	})
	lg, _ := logtest.NewNullLogger()
	return server.New(server.Deps{CompatIntervals: svc, Log: lg})
}

// The aggregator's default shape byte for byte: strings in display units,
// RFC3339 times (UTC: an allowed difference), zero candles for the empty
// buckets, ascending.
func TestCompatIntervalsDefaultFormat(t *testing.T) {
	rec := serve(t, ivServer(), http.MethodGet, "/aggregator/api/dex/intervals?market_id=ubze/uvdl&minutes=5&limit=3")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, `[{"market_id":"ubze/uvdl","minutes":5,"start_at":"2026-10-28T09:55:00Z","end_at":"2026-10-28T10:00:00Z",`+
		`"lowest_price":"0.5","open_price":"0.5","average_price":"0.5","highest_price":"0.5","close_price":"0.5","base_volume":"4","quote_volume":"2"},`+
		`{"market_id":"ubze/uvdl","minutes":5,"start_at":"2026-10-28T10:00:00Z","end_at":"2026-10-28T10:05:00Z",`+
		`"lowest_price":"1","open_price":"1","average_price":"1.25","highest_price":"1.5","close_price":"1.5","base_volume":"20","quote_volume":"25"},`+
		`{"market_id":"ubze/uvdl","minutes":5,"start_at":"2026-10-28T10:05:00Z","end_at":"2026-10-28T10:10:00Z",`+
		`"lowest_price":"0","open_price":"0","average_price":"0","highest_price":"0","close_price":"0","base_volume":"0","quote_volume":"0"}]`+"\n",
		rec.Body.String())
}

// format=tv: unix seconds and numbers, TradingView's shape.
func TestCompatIntervalsTradingViewFormat(t *testing.T) {
	rec := serve(t, ivServer(), http.MethodGet, "/aggregator/api/dex/intervals?ticker_id=ubze_uvdl&minutes=5&limit=3&format=tv")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, `[{"time":1793181300,"low":0.5,"open":0.5,"high":0.5,"close":0.5,"value":4},`+
		`{"time":1793181600,"low":1,"open":1,"high":1.5,"close":1.5,"value":20},`+
		`{"time":1793181900,"low":0,"open":0,"high":0,"close":0,"value":0}]`+"\n", rec.Body.String())
}

func TestCompatIntervalsValidation(t *testing.T) {
	e := ivServer()
	for _, tc := range []struct {
		query, message string
	}{
		{"market_id=ubze/uvdl", "invalid minutes. expected: 5, 15, 60, 240, 1440"},
		{"market_id=ubze/uvdl&minutes=30", "invalid minutes. expected: 5, 15, 60, 240, 1440"},
		{"market_id=ubze/uvdl&minutes=", "invalid minutes. expected: 5, 15, 60, 240, 1440"},
		{"market_id=ubze/uvdl&minutes=five", dto.CompatInvalidRequest},
		{"market_id=ubze/uvdl&minutes=5&limit=x", dto.CompatInvalidRequest},
		{"market_id=ubze/uvdl&minutes=60&limit=5001", "limit can not be greater than 5000"},
		{"minutes=5", "please provide market_id or ticker_id"},
		{"market_id=u&ticker_id=v&minutes=5", "please provide market_id or ticker_id"},
		{"market_id=uatom/ubze&minutes=5", dto.CompatUnknownError},
	} {
		rec := serve(t, e, http.MethodGet, "/aggregator/api/dex/intervals?"+tc.query)
		assert.Equal(t, http.StatusBadRequest, rec.Code, tc.query)
		assert.JSONEq(t, `{"message":"`+tc.message+`"}`, rec.Body.String(), tc.query)
	}
}

// Daily candles have no maximum limit; the others default to 500 when the
// limit is absent or not positive; names bind without case, as echo v4's.
func TestCompatIntervalsLimits(t *testing.T) {
	e := ivServer()
	// The day's bucket starts below the floor (09:54): no candle.
	rec := serve(t, e, http.MethodGet, "/aggregator/api/dex/intervals?market_id=ubze/uvdl&minutes=1440&limit=100000")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]\n", rec.Body.String())
	for _, query := range []string{
		"market_id=ubze/uvdl&minutes=5&limit=-3",
		"market_id=ubze/uvdl&minutes=5&limit=",
		"Market_ID=ubze/uvdl&MINUTES=5",
	} {
		rec := serve(t, e, http.MethodGet, "/aggregator/api/dex/intervals?"+query)
		assert.Equal(t, http.StatusOK, rec.Code, query)
		assert.NotEqual(t, "[]\n", rec.Body.String(), query)
	}
}

// stubIntervals answers what it is given.
type stubIntervals struct {
	ivs []candles.Interval
	err error
}

func (s stubIntervals) Intervals(context.Context, string, int, int) ([]candles.Interval, error) {
	return s.ivs, s.err
}

func TestCompatIntervalsEmptyAndFailing(t *testing.T) {
	lg, _ := logtest.NewNullLogger()
	e := server.New(server.Deps{CompatIntervals: stubIntervals{}, Log: lg})
	rec := serve(t, e, http.MethodGet, "/aggregator/api/dex/intervals?market_id=ubze/unew&minutes=5")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]\n", rec.Body.String(), "[], never null")
	rec = serve(t, e, http.MethodGet, "/aggregator/api/dex/intervals?market_id=ubze/unew&minutes=5&format=tv")
	assert.Equal(t, "[]\n", rec.Body.String())

	e = server.New(server.Deps{CompatIntervals: stubIntervals{err: errors.New("database is down")}, Log: lg})
	rec = serve(t, e, http.MethodGet, "/aggregator/api/dex/intervals?market_id=ubze/uvdl&minutes=5")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"message":"`+dto.CompatUnknownError+`"}`, rec.Body.String())

	rec = serve(t, server.New(server.Deps{}), http.MethodGet, "/aggregator/api/dex/intervals?market_id=ubze/uvdl&minutes=5")
	assert.Equal(t, http.StatusNotFound, rec.Code, "left out without a service")
}

// Allowed difference: prices are right where the quote has three or more
// decimals more than the base (BZE-133), where the aggregator answers 0.
func TestCompatIntervalsBigExponentGap(t *testing.T) {
	pool := "ubze_utoken"
	tr, err := candles.SwapTrade(1, ivTime(10, 1, 0), "1000000", "2500000000")
	require.NoError(t, err)
	svc := candles.NewService(candles.ServiceDeps{
		Reader:    &ivReader{candles: candles.Build(pool, []candles.Trade{tr}), firsts: map[string]candles.First{pool: {Time: ivTime(9, 0, 0), Base: "ubze", Quote: "utoken"}}},
		Markets:   ivMarkets{},
		Exponents: swapExponents{"ubze": 6, "utoken": 9},
		Clock:     ivClock(ivTime(10, 7, 30)),
	})
	e := server.New(server.Deps{CompatIntervals: svc})
	rec := serve(t, e, http.MethodGet, "/aggregator/api/dex/intervals?market_id=ubze_utoken&minutes=5&limit=2&format=tv")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, `[{"time":1793181600,"low":2.5,"open":2.5,"high":2.5,"close":2.5,"value":1},`+
		`{"time":1793181900,"low":0,"open":0,"high":0,"close":0,"value":0}]`+"\n", rec.Body.String())
}
