package controller_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

type fakePrices struct {
	rows []repository.Price
	err  error
}

func (f *fakePrices) Prices(context.Context) ([]repository.Price, error) { return f.rows, f.err }

func priceServer(f *fakePrices) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	e.GET("/api/v1/prices", controller.NewPriceController(f).Prices)
	return e
}

func TestPricesListsEveryPricedDenomAsTheRepositoryOrdersThem(t *testing.T) {
	at := time.Date(2026, 10, 29, 10, 0, 0, 0, time.FixedZone("EET", 2*3600))
	f := &fakePrices{rows: []repository.Price{
		{Denom: honeyDenom, PriceUSD: "0.002000000000", PriceSource: strp("dex"), UpdatedAt: &at},
		{Denom: "ibc/5FEB", PriceUSD: "1.960000000000", PriceSource: strp("coingecko"), UpdatedAt: &at},
		{Denom: "ubze", PriceUSD: "0.000168060000", PriceSource: strp("dex"), PriceChange24hPct: strp("-2.1362"), UpdatedAt: &at},
	}}
	rec := get(t, priceServer(f), "/api/v1/prices")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheIndexed, rec.Header().Get(echo.HeaderCacheControl))
	assert.JSONEq(t, `{"prices":[
		{"denom":"`+honeyDenom+`","price_usd":"0.002000000000","price_source":"dex","price_change_24h_pct":null,"updated_at":"2026-10-29T08:00:00Z"},
		{"denom":"ibc/5FEB","price_usd":"1.960000000000","price_source":"coingecko","price_change_24h_pct":null,"updated_at":"2026-10-29T08:00:00Z"},
		{"denom":"ubze","price_usd":"0.000168060000","price_source":"dex","price_change_24h_pct":"-2.1362","updated_at":"2026-10-29T08:00:00Z"}
	]}`, rec.Body.String(), "decimal strings, never numbers; times in UTC")
}

func TestPricesAnswersAnEmptyListAndFailsOnTheDatabase(t *testing.T) {
	rec := get(t, priceServer(&fakePrices{}), "/api/v1/prices")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"prices":[]}`, rec.Body.String())

	rec = get(t, priceServer(&fakePrices{err: errors.New("down")}), "/api/v1/prices")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl), "errors are never cached")
}
