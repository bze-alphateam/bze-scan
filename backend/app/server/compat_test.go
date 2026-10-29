package server_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/server"
	"github.com/bze-alphateam/bze-scan/backend/internal/coingecko"
)

// geckoDoer answers CoinGecko's simple price call with body until it is
// told to fail, and counts the calls.
type geckoDoer struct {
	mu    sync.Mutex
	body  string
	fail  bool
	calls int
	urls  []string
}

func (d *geckoDoer) Do(req *http.Request) (*http.Response, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	d.urls = append(d.urls, req.URL.String())
	if d.fail {
		return nil, errors.New("connection refused")
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(d.body))}, nil
}

type compatClock struct{ now time.Time }

func (c *compatClock) Now() time.Time { return c.now }

// recordedGecko is CoinGecko's answer for mainnet's ids, in its own order.
const recordedGecko = `{"cosmos":{"usd":1.96},"osmosis":{"usd":0.03403806},"bzedge":{"usd":0.00016806}}`

func compatServer(t *testing.T, doer *geckoDoer, clock *compatClock, cors ...string) *echo.Echo {
	t.Helper()
	lg, _ := logtest.NewNullLogger()
	feed := coingecko.NewFeed(coingecko.FeedDeps{
		Source: coingecko.New(doer, "https://gecko.test/"), Log: lg, Clock: clock,
	}, []string{"bzedge", "osmosis", "cosmos"})
	return server.New(server.Deps{CompatPrices: feed, CORSAllowedOrigins: cors})
}

func TestCompatPricesAnswerTheAggregatorsShapeInTheConfiguredOrder(t *testing.T) {
	doer := &geckoDoer{body: recordedGecko}
	e := compatServer(t, doer, &compatClock{now: time.Unix(1000, 0)})

	rec := serve(t, e, http.MethodGet, "/aggregator/api/prices")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, `[{"denom":"bzedge","price":0.00016806,"price_denom":"usd"},`+
		`{"denom":"osmosis","price":0.03403806,"price_denom":"usd"},{"denom":"cosmos","price":1.96,"price_denom":"usd"}]`+"\n",
		rec.Body.String())
	assert.Equal(t, []string{"https://gecko.test/api/v3/simple/price?ids=bzedge,osmosis,cosmos&vs_currencies=usd"}, doer.urls)
	assert.Empty(t, rec.Header().Get(echo.HeaderCacheControl), "the aggregator sends none")
}

func TestCompatPricesAreCachedThenBackedUp(t *testing.T) {
	doer := &geckoDoer{body: `{"bzedge":{"usd":0.0002}}`}
	clock := &compatClock{now: time.Unix(1000, 0)}
	e := compatServer(t, doer, clock)
	const first = `[{"denom":"bzedge","price":0.0002,"price_denom":"usd"}]` + "\n"

	assert.Equal(t, first, serve(t, e, http.MethodGet, "/aggregator/api/prices").Body.String())

	// Within 180 s CoinGecko is not asked again.
	doer.body = `{"bzedge":{"usd":0.0003}}`
	clock.now = clock.now.Add(coingecko.FreshFor - time.Second)
	assert.Equal(t, first, serve(t, e, http.MethodGet, "/aggregator/api/prices").Body.String())
	assert.Equal(t, 1, doer.calls)

	// After 180 s CoinGecko fails: the last read is served, and CoinGecko
	// is asked on every request meanwhile.
	doer.fail = true
	clock.now = clock.now.Add(time.Second)
	assert.Equal(t, first, serve(t, e, http.MethodGet, "/aggregator/api/prices").Body.String())
	clock.now = time.Unix(1000, 0).Add(coingecko.BackupFor - time.Second)
	assert.Equal(t, first, serve(t, e, http.MethodGet, "/aggregator/api/prices").Body.String())
	assert.Equal(t, 3, doer.calls)

	// 24 hours after the last read: nothing to serve, an empty list.
	clock.now = clock.now.Add(time.Second)
	rec := serve(t, e, http.MethodGet, "/aggregator/api/prices")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]\n", rec.Body.String(), "[] where the aggregator answers null")

	// CoinGecko answers again: the new price.
	doer.fail = false
	assert.Equal(t, `[{"denom":"bzedge","price":0.0003,"price_denom":"usd"}]`+"\n",
		serve(t, e, http.MethodGet, "/aggregator/api/prices").Body.String())
}

func TestCompatPricesWithNothingReadAnswerAnEmptyList(t *testing.T) {
	rec := serve(t, compatServer(t, &geckoDoer{fail: true}, &compatClock{now: time.Unix(1000, 0)}), http.MethodGet, "/aggregator/api/prices")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]\n", rec.Body.String())
}

func TestCompatPricesNeedAFeed(t *testing.T) {
	assert.Equal(t, http.StatusNotFound, serve(t, server.New(server.Deps{}), http.MethodGet, "/aggregator/api/prices").Code)
}

// corsRequest sends method to path from origin, a preflight for OPTIONS.
func corsRequest(e *echo.Echo, method, path, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set(echo.HeaderOrigin, origin)
	if method == http.MethodOptions {
		req.Header.Set(echo.HeaderAccessControlRequestMethod, http.MethodGet)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestCompatRoutesAllowAnyOriginWhateverTheConfiguredOnes(t *testing.T) {
	const other = "https://app.getbze.com"
	for name, cors := range map[string][]string{
		"none":       nil,
		"one origin": {"https://explorer.getbze.com"},
		"any":        {"*"},
	} {
		e := compatServer(t, &geckoDoer{body: recordedGecko}, &compatClock{now: time.Unix(1000, 0)}, cors...)
		rec := corsRequest(e, http.MethodGet, "/aggregator/api/prices", other)
		assert.Equal(t, http.StatusOK, rec.Code, name)
		assert.Equal(t, "*", rec.Header().Get(echo.HeaderAccessControlAllowOrigin), name)

		rec = corsRequest(e, http.MethodOptions, "/aggregator/api/prices", other)
		assert.Equal(t, http.StatusNoContent, rec.Code, name)
		assert.Equal(t, "*", rec.Header().Get(echo.HeaderAccessControlAllowOrigin), name)
		assert.Contains(t, rec.Header().Get(echo.HeaderAccessControlAllowMethods), http.MethodGet, name)
	}
}

func TestV1CORSIsUnchangedByTheCompatRoutes(t *testing.T) {
	const allowed, other = "https://explorer.getbze.com", "https://app.getbze.com"
	e := compatServer(t, &geckoDoer{body: recordedGecko}, &compatClock{now: time.Unix(1000, 0)}, allowed)
	for _, path := range []string{server.APIPrefix + "/blocks/x", server.APIPrefix, "/health"} {
		assert.Equal(t, allowed, corsRequest(e, http.MethodGet, path, allowed).Header().Get(echo.HeaderAccessControlAllowOrigin), path)
		assert.Empty(t, corsRequest(e, http.MethodGet, path, other).Header().Get(echo.HeaderAccessControlAllowOrigin), path)
	}

	// Without configured origins /api/v1 sends no CORS headers at all.
	e = compatServer(t, &geckoDoer{body: recordedGecko}, &compatClock{now: time.Unix(1000, 0)})
	assert.Empty(t, corsRequest(e, http.MethodGet, server.APIPrefix+"/blocks/x", other).Header().Get(echo.HeaderAccessControlAllowOrigin))
	assert.Equal(t, "*", corsRequest(e, http.MethodGet, "/aggregator/api/prices", other).Header().Get(echo.HeaderAccessControlAllowOrigin))
}

func TestCompatRoutesLiveOnlyUnderTheAggregatorPrefix(t *testing.T) {
	e := compatServer(t, &geckoDoer{body: recordedGecko}, &compatClock{now: time.Unix(1000, 0)}, "https://explorer.getbze.com")
	assert.Equal(t, http.StatusOK, serve(t, e, http.MethodGet, server.CompatPrefix+"/prices").Code)

	// The aggregator's own path is not served: callers add /aggregator to the host.
	rec := corsRequest(e, http.MethodGet, "/api/prices", "https://app.getbze.com")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, rec.Header().Get(echo.HeaderAccessControlAllowOrigin))
}
