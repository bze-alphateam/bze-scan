package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
)

// newEcho returns an echo instance with the API's middleware and error
// handler, plus one route per behaviour under test.
func newEcho() *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	e.Use(middleware.RequestID())
	e.Use(middleware.Recover())

	e.GET("/panic", func(c *echo.Context) error { panic("boom") })
	e.GET("/fail", func(c *echo.Context) error { return errors.New("db password is hunter2") })
	e.GET("/bad", func(c *echo.Context) error {
		return echo.NewHTTPError(http.StatusBadRequest, "height must be a positive integer")
	})
	e.GET("/id", func(c *echo.Context) error {
		id, _ := c.Get(middleware.RequestIDContextKey).(string)
		return c.String(http.StatusOK, id)
	})
	return e
}

func do(e *echo.Echo, method, path string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestRequestIDGenerated(t *testing.T) {
	rec := do(newEcho(), http.MethodGet, "/id", nil)

	id := rec.Header().Get(middleware.RequestIDHeader)
	assert.Len(t, id, 32)
	assert.Equal(t, id, rec.Body.String(), "the id is stored in the context")
}

func TestRequestIDReusedFromClient(t *testing.T) {
	rec := do(newEcho(), http.MethodGet, "/id", http.Header{middleware.RequestIDHeader: {"abc-123"}})

	assert.Equal(t, "abc-123", rec.Header().Get(middleware.RequestIDHeader))
	assert.Equal(t, "abc-123", rec.Body.String())
}

func TestRequestIDTooLongIsReplaced(t *testing.T) {
	// One character over the 128-character limit.
	long := strings.Repeat("x", 129)
	rec := do(newEcho(), http.MethodGet, "/id", http.Header{middleware.RequestIDHeader: {long}})

	assert.Len(t, rec.Header().Get(middleware.RequestIDHeader), 32)
}

func TestRecoverAnswersGeneric500(t *testing.T) {
	rec := do(newEcho(), http.MethodGet, "/panic", nil)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"internal","message":"An unexpected error occurred"}}`, rec.Body.String())
}

func TestInternalErrorDetailsAreNotExposed(t *testing.T) {
	rec := do(newEcho(), http.MethodGet, "/fail", nil)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "hunter2")
	assert.JSONEq(t, `{"error":{"code":"internal","message":"An unexpected error occurred"}}`, rec.Body.String())
}

func TestHTTPErrorKeepsStatusAndMessage(t *testing.T) {
	rec := do(newEcho(), http.MethodGet, "/bad", nil)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"bad_request","message":"height must be a positive integer"}}`, rec.Body.String())
}

func TestNotFoundEnvelope(t *testing.T) {
	rec := do(newEcho(), http.MethodGet, "/nope", nil)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"not_found","message":"Not Found"}}`, rec.Body.String())
}

func TestHeadErrorHasNoBody(t *testing.T) {
	rec := do(newEcho(), http.MethodHead, "/nope", nil)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, rec.Body.Bytes())
}

func TestErrorsAreNotCached(t *testing.T) {
	for _, path := range []string{"/bad", "/fail", "/nope"} {
		rec := do(newEcho(), http.MethodGet, path, nil)
		assert.Equal(t, "no-store", rec.Header().Get(echo.HeaderCacheControl), path)
	}
}

func TestUpstreamStatusesHaveTheirCode(t *testing.T) {
	e := newEcho()
	e.GET("/upstream", func(c *echo.Context) error {
		return echo.NewHTTPError(http.StatusBadGateway, "archive node unreachable")
	})
	e.GET("/timeout", func(c *echo.Context) error { return echo.NewHTTPError(http.StatusGatewayTimeout, "") })

	rec := do(e, http.MethodGet, "/upstream", nil)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"upstream_error","message":"An unexpected error occurred"}}`, rec.Body.String())
	assert.Contains(t, do(e, http.MethodGet, "/timeout", nil).Body.String(), `"upstream_error"`)
}

func TestRequestLogWritesOneLinePerRequest(t *testing.T) {
	logger, hook := logtest.NewNullLogger()
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	e.Use(middleware.RequestID())
	e.Use(middleware.RequestLog(logger))
	e.Use(middleware.Recover())
	e.GET("/ok", func(c *echo.Context) error { return c.String(http.StatusOK, "ok") })
	e.GET("/bad", func(c *echo.Context) error { return echo.NewHTTPError(http.StatusBadRequest, "no") })
	e.GET("/panic", func(c *echo.Context) error { panic("boom") })

	cases := []struct {
		path   string
		status int
	}{{"/ok", 200}, {"/bad", 400}, {"/panic", 500}, {"/nope", 404}}
	for _, tc := range cases {
		hook.Reset()
		do(e, http.MethodGet, tc.path+"?q=1", http.Header{middleware.RequestIDHeader: {"rid"}})

		var lines []*log.Entry
		for _, entry := range hook.AllEntries() {
			if entry.Message == "request" {
				lines = append(lines, entry)
			}
		}
		require.Len(t, lines, 1, tc.path)
		assert.Equal(t, log.InfoLevel, lines[0].Level)
		assert.Equal(t, http.MethodGet, lines[0].Data["method"])
		assert.Equal(t, tc.path, lines[0].Data["path"])
		assert.Equal(t, tc.status, lines[0].Data["status"], tc.path)
		assert.Equal(t, "rid", lines[0].Data["request_id"])
		assert.Contains(t, lines[0].Data, "duration_ms")
	}
}
