package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
)

// newEcho returns an echo instance with the API's middleware and error
// handler, plus one route per behaviour under test.
func newEcho() *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = ErrorHandler
	e.Use(RequestID())
	e.Use(Recover())

	e.GET("/panic", func(c *echo.Context) error { panic("boom") })
	e.GET("/fail", func(c *echo.Context) error { return errors.New("db password is hunter2") })
	e.GET("/bad", func(c *echo.Context) error {
		return echo.NewHTTPError(http.StatusBadRequest, "height must be a positive integer")
	})
	e.GET("/id", func(c *echo.Context) error {
		id, _ := c.Get(RequestIDContextKey).(string)
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

	id := rec.Header().Get(RequestIDHeader)
	assert.Len(t, id, 32)
	assert.Equal(t, id, rec.Body.String(), "the id is stored in the context")
}

func TestRequestIDReusedFromClient(t *testing.T) {
	rec := do(newEcho(), http.MethodGet, "/id", http.Header{RequestIDHeader: {"abc-123"}})

	assert.Equal(t, "abc-123", rec.Header().Get(RequestIDHeader))
	assert.Equal(t, "abc-123", rec.Body.String())
}

func TestRequestIDTooLongIsReplaced(t *testing.T) {
	long := strings.Repeat("x", maxClientRequestIDLen+1)
	rec := do(newEcho(), http.MethodGet, "/id", http.Header{RequestIDHeader: {long}})

	assert.Len(t, rec.Header().Get(RequestIDHeader), 32)
}

func TestRecoverAnswersGeneric500(t *testing.T) {
	rec := do(newEcho(), http.MethodGet, "/panic", nil)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"internal_error","message":"An unexpected error occurred"}}`, rec.Body.String())
}

func TestInternalErrorDetailsAreNotExposed(t *testing.T) {
	rec := do(newEcho(), http.MethodGet, "/fail", nil)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "hunter2")
	assert.JSONEq(t, `{"error":{"code":"internal_error","message":"An unexpected error occurred"}}`, rec.Body.String())
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
