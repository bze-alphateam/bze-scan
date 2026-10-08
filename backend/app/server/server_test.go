package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmw "github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/server"
)

func serve(t *testing.T, e *echo.Echo, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestHealthRoute(t *testing.T) {
	rec := serve(t, server.New(server.Deps{}), http.MethodGet, "/health")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.Bytes())
	assert.NotEmpty(t, rec.Header().Get(appmw.RequestIDHeader))
}

func TestUnknownPathAnswersNotFoundEnvelope(t *testing.T) {
	rec := serve(t, server.New(server.Deps{}), http.MethodGet, "/nope")

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Header().Get(echo.HeaderContentType), echo.MIMEApplicationJSON)
	assert.JSONEq(t, `{"error":{"code":"not_found","message":"Not Found"}}`, rec.Body.String())
}

func TestWrongMethodAnswersEnvelope(t *testing.T) {
	rec := serve(t, server.New(server.Deps{}), http.MethodPost, "/health")

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	var body appmw.ErrorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "method_not_allowed", body.Error.Code)
}

func TestRunServesAndShutsDownCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addrCh := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx, server.New(server.Deps{}), "127.0.0.1:0", func(a net.Addr) { addrCh <- a })
	}()

	var addr net.Addr
	select {
	case addr = <-addrCh:
	case err := <-done:
		t.Fatalf("server exited before listening: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start listening")
	}

	resp, err := http.Get("http://" + addr.String() + "/health")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, body)

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(server.ShutdownTimeout + 5*time.Second):
		t.Fatal("server did not shut down")
	}

	resp, err = http.Get("http://" + addr.String() + "/health")
	if err == nil {
		_ = resp.Body.Close()
	}
	assert.Error(t, err, "listener must be closed after shutdown")
}

func TestRunDrainsInFlightRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e := server.New(server.Deps{})
	entered := make(chan struct{})
	release := make(chan struct{})
	e.GET("/slow", func(c *echo.Context) error {
		close(entered)
		<-release
		return c.String(http.StatusOK, "done")
	})

	addrCh := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, e, "127.0.0.1:0", func(a net.Addr) { addrCh <- a }) }()
	addr := <-addrCh

	type result struct {
		body string
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + addr.String() + "/slow")
		if err != nil {
			resCh <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		resCh <- result{body: string(b), err: err}
	}()

	<-entered
	cancel() // shutdown starts while /slow is in flight
	time.Sleep(100 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("Run returned before the in-flight request finished: %v", err)
	default:
	}
	close(release)

	res := <-resCh
	require.NoError(t, res.err)
	assert.Equal(t, "done", res.body)
	assert.NoError(t, <-done)
}

func TestRunFailsWhenAddressIsTaken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	err = server.Run(context.Background(), server.New(server.Deps{}), ln.Addr().String(), nil)
	require.Error(t, err)
	assert.False(t, errors.Is(err, context.Canceled))
}

func TestAPIRoutesLiveUnderTheVersionPrefix(t *testing.T) {
	e := server.New(server.Deps{})
	for _, path := range []string{"/blocks", "/txs", "/search?q=1"} {
		assert.Equal(t, http.StatusNotFound, serve(t, e, http.MethodGet, path).Code, path)
	}
	// Validation answers before any repository call, so a nil repository is
	// enough to prove the routes exist.
	for _, path := range []string{"/blocks/x", "/txs/x", "/blocks?limit=0", "/txs?limit=0", "/search"} {
		rec := serve(t, e, http.MethodGet, server.APIPrefix+path)
		assert.Equal(t, http.StatusBadRequest, rec.Code, path)
	}
}

func TestCORSHeadersOnlyWhenConfigured(t *testing.T) {
	request := func(e *echo.Echo, method string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, server.APIPrefix+"/blocks/x", nil)
		req.Header.Set(echo.HeaderOrigin, "https://scan.getbze.com")
		if method == http.MethodOptions {
			req.Header.Set(echo.HeaderAccessControlRequestMethod, http.MethodGet)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	rec := request(server.New(server.Deps{}), http.MethodGet)
	assert.Empty(t, rec.Header().Get(echo.HeaderAccessControlAllowOrigin))

	e := server.New(server.Deps{CORSAllowedOrigins: []string{"https://scan.getbze.com"}})
	rec = request(e, http.MethodGet)
	assert.Equal(t, "https://scan.getbze.com", rec.Header().Get(echo.HeaderAccessControlAllowOrigin), "errors carry CORS headers too")
	rec = request(e, http.MethodOptions)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Contains(t, rec.Header().Get(echo.HeaderAccessControlAllowMethods), http.MethodGet)

	e = server.New(server.Deps{CORSAllowedOrigins: []string{"*"}})
	assert.Equal(t, "*", request(e, http.MethodGet).Header().Get(echo.HeaderAccessControlAllowOrigin))
}
