package controller_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/rawcache"
)

// fakeRaw answers fixed bodies; err fails every call.
type fakeRaw struct {
	err       error
	gotRoute  rawcache.Route
	gotHeight int64
	gotIndex  int
}

func (f *fakeRaw) Get(_ context.Context, route rawcache.Route, height int64) ([]byte, error) {
	f.gotRoute, f.gotHeight = route, height
	if f.err != nil {
		return nil, f.err
	}
	return []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":-1,"result":{"route":%q}}`, route)), nil
}

func (f *fakeRaw) Tx(_ context.Context, height int64, index int) (*rawcache.Tx, error) {
	f.gotHeight, f.gotIndex = height, index
	if f.err != nil {
		return nil, f.err
	}
	return &rawcache.Tx{Tx: "CpQBCpEB", Result: []byte(`{"code":0,"events":[]}`)}, nil
}

func newRawAPI(raw controller.RawReader, r controller.ExplorerReader) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	h := controller.NewRawController(raw, r)
	e.GET("/raw/block/:height", h.Block)
	e.GET("/raw/block_results/:height", h.BlockResults)
	e.GET("/raw/commit/:height", h.Commit)
	e.GET("/raw/tx/:hash", h.Tx)
	return e
}

func TestRawRoutesAnswerTheBodyVerbatim(t *testing.T) {
	raw := &fakeRaw{}
	e := newRawAPI(raw, &fakeReader{})
	for path, route := range map[string]rawcache.Route{
		"/raw/block/7":         rawcache.RouteBlock,
		"/raw/block_results/7": rawcache.RouteBlockResults,
		"/raw/commit/7":        rawcache.RouteCommit,
	} {
		rec := get(t, e, path)
		require.Equal(t, http.StatusOK, rec.Code, path)
		assert.Equal(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":-1,"result":{"route":%q}}`, route), rec.Body.String(), path)
		assert.Equal(t, echo.MIMEApplicationJSON, rec.Header().Get(echo.HeaderContentType), path)
		assert.Equal(t, controller.CacheImmutable, rec.Header().Get(echo.HeaderCacheControl), path)
		assert.Equal(t, route, raw.gotRoute)
		assert.Equal(t, int64(7), raw.gotHeight)
	}
}

func TestRawHeightValidation(t *testing.T) {
	e := newRawAPI(&fakeRaw{}, &fakeReader{})
	for _, h := range []string{"0", "-1", "abc", "1.5", "+3", "99999999999999999999"} {
		rec := get(t, e, "/raw/block/"+h)
		assert.Equal(t, http.StatusBadRequest, rec.Code, h)
		assert.Equal(t, "bad_request", errorCode(t, rec), h)
	}
}

func TestRawUpstreamFailureIsBadGateway(t *testing.T) {
	raw := &fakeRaw{err: fmt.Errorf("%w: block 9: connection refused", rawcache.ErrUpstream)}
	f := &fakeReader{tx: &repository.Tx{TxSummary: repository.TxSummary{Height: 9, TxIndex: 1, Hash: txHash}}}
	e := newRawAPI(raw, f)

	for _, path := range []string{"/raw/commit/9", "/raw/tx/" + txHash} {
		rec := get(t, e, path)
		assert.Equal(t, http.StatusBadGateway, rec.Code, path)
		assert.Equal(t, "upstream_error", errorCode(t, rec), path)
		assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl), path)
	}
}

func TestRawOtherFailuresAreInternal(t *testing.T) {
	e := newRawAPI(&fakeRaw{err: errors.New("boom")}, &fakeReader{})
	rec := get(t, e, "/raw/block/9")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "internal", errorCode(t, rec))
}

const txHash = "E580BFA56DE28886E51DDB9BE50DD610C1C832C579CA08FCF9177B04D2F7B7B9"

func TestRawTx(t *testing.T) {
	raw := &fakeRaw{}
	f := &fakeReader{tx: &repository.Tx{TxSummary: repository.TxSummary{Height: 25000894, TxIndex: 2, Hash: txHash}}}
	e := newRawAPI(raw, f)

	rec := get(t, e, "/raw/tx/"+strings.ToLower(txHash))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, txHash, f.gotHash, "the hash is looked up upper case")
	assert.Equal(t, int64(25000894), raw.gotHeight)
	assert.Equal(t, 2, raw.gotIndex)
	assert.Equal(t, controller.CacheImmutable, rec.Header().Get(echo.HeaderCacheControl))
	assert.JSONEq(t, `{"height":25000894,"index":2,"tx":"CpQBCpEB","tx_result":{"code":0,"events":[]}}`, rec.Body.String())
}

func TestRawTxErrors(t *testing.T) {
	e := newRawAPI(&fakeRaw{}, &fakeReader{})

	rec := get(t, e, "/raw/tx/nothex")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "bad_request", errorCode(t, rec))

	rec = get(t, e, "/raw/tx/"+txHash)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "not_found", errorCode(t, rec))

	rec = get(t, newRawAPI(&fakeRaw{}, &fakeReader{err: errors.New("db down")}), "/raw/tx/"+txHash)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
