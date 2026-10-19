package controller_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"cosmossdk.io/math"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/internal/openorders"
)

type fakeOpenOrders struct {
	summary openorders.Summary
	err     error
	asked   []string
}

func (f *fakeOpenOrders) OpenOrders(_ context.Context, address string) (openorders.Summary, error) {
	f.asked = append(f.asked, address)
	return f.summary, f.err
}

func newOpenOrdersAPI(r controller.OpenOrdersReader) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	e.GET("/accounts/:address/open-orders", controller.NewAccountController(&fakeAccounts{}, nil, r).OpenOrders)
	return e
}

func TestOpenOrders(t *testing.T) {
	usdc := openorders.Market{ID: "ubze/ibc/USDC", Base: "ubze", Quote: "ibc/USDC"}
	r := &fakeOpenOrders{summary: openorders.Summary{
		Source: openorders.SourceNode, Height: 25159613, OrdersCount: 3, MarketsCount: 1,
		Assets: []openorders.Asset{
			{Denom: "ibc/USDC", Amount: math.NewInt(15), OrdersCount: 1,
				Markets: []openorders.AssetMarket{{Market: usdc, Amount: math.NewInt(15), OrdersCount: 1}}},
			{Denom: "ubze", Amount: math.NewInt(104800000000), OrdersCount: 2,
				Markets: []openorders.AssetMarket{{Market: usdc, Amount: math.NewInt(104800000000), OrdersCount: 2}}},
		},
	}}
	rec := get(t, newOpenOrdersAPI(r), "/accounts/"+account+"/open-orders")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get(echo.HeaderCacheControl))
	assert.JSONEq(t, `{"source":"node","height":25159613,"orders_count":3,"markets_count":1,"assets":[
		{"denom":"ibc/USDC","amount":"15","orders_count":1,"markets":[
			{"market_id":"ubze/ibc/USDC","base":"ubze","quote":"ibc/USDC","amount":"15","orders_count":1}]},
		{"denom":"ubze","amount":"104800000000","orders_count":2,"markets":[
			{"market_id":"ubze/ibc/USDC","base":"ubze","quote":"ibc/USDC","amount":"104800000000","orders_count":2}]}]}`,
		rec.Body.String())
	assert.Equal(t, []string{account}, r.asked)
}

func TestOpenOrdersOfNobodyIsEmpty(t *testing.T) {
	rec := get(t, newOpenOrdersAPI(&fakeOpenOrders{summary: openorders.Summary{Source: "index", Height: 7}}),
		"/accounts/"+account+"/open-orders")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"source":"index","height":7,"orders_count":0,"markets_count":0,"assets":[]}`, rec.Body.String())
}

func TestOpenOrdersErrors(t *testing.T) {
	r := &fakeOpenOrders{}
	rec := get(t, newOpenOrdersAPI(r), "/accounts/bzevaloper1xyz/open-orders")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "bad_request")
	assert.Empty(t, r.asked)

	r.err = fmt.Errorf("%w: down", openorders.ErrUpstream)
	rec = get(t, newOpenOrdersAPI(r), "/accounts/"+account+"/open-orders")
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "upstream_error")

	r.err = errors.New("db")
	rec = get(t, newOpenOrdersAPI(r), "/accounts/"+account+"/open-orders")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	rec = get(t, newOpenOrdersAPI(nil), "/accounts/"+account+"/open-orders")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
