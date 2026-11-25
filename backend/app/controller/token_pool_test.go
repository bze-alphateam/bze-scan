package controller_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
)

type fakePool struct {
	pool *dto.PoolPrice
	err  error
	got  []string
}

func (f *fakePool) Pool(_ context.Context, lpDenom string) (*dto.PoolPrice, error) {
	f.got = append(f.got, lpDenom)
	return f.pool, f.err
}

func poolServer(f *fakeTokens, p controller.PoolPriceReader) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	h := controller.NewTokenController(f)
	h.Pools = p
	e.GET("/api/v1/tokens/:denom", h.Token)
	return e
}

func lpToken() *fakeTokens {
	tok := honeyToken()
	tok.Denom, tok.Kind = "ulp_x_ubze", "lp"
	return &fakeTokens{token: tok, events: tokenEvents(0)}
}

func TestTheLPTokenPageCarriesThePool(t *testing.T) {
	f := &fakePool{pool: &dto.PoolPrice{PoolID: "x_ubze"}}
	e := poolServer(lpToken(), f)
	rec := get(t, e, "/api/v1/tokens/"+url.PathEscape("ulp_x_ubze"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	tok := decode[dto.Token](t, rec)
	require.NotNil(t, tok.Pool)
	assert.Equal(t, "x_ubze", tok.Pool.PoolID)
	assert.Equal(t, []string{"ulp_x_ubze"}, f.got)
}

func TestOtherDenomsAndFailuresHaveNoPool(t *testing.T) {
	f := &fakePool{pool: &dto.PoolPrice{PoolID: "x_ubze"}}
	rec := get(t, poolServer(&fakeTokens{token: honeyToken(), events: tokenEvents(0)}, f), "/api/v1/tokens/"+url.PathEscape(honeyDenom))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Nil(t, decode[dto.Token](t, rec).Pool)
	assert.Empty(t, f.got, "not asked for a factory denom")

	rec = get(t, poolServer(lpToken(), &fakePool{err: errors.New("node down")}), "/api/v1/tokens/ulp_x_ubze")
	require.Equal(t, http.StatusOK, rec.Code, "the page does not fail with the node")
	assert.Nil(t, decode[dto.Token](t, rec).Pool)

	rec = get(t, poolServer(lpToken(), nil), "/api/v1/tokens/ulp_x_ubze")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, decode[dto.Token](t, rec).Pool, "not wired")
}
