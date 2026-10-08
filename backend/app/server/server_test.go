package server

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
)

func serve(t *testing.T, e *echo.Echo, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestHealthRoute(t *testing.T) {
	rec := serve(t, New(), http.MethodGet, "/health")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.Bytes())
	assert.NotEmpty(t, rec.Header().Get(appmw.RequestIDHeader))
}

func TestUnknownPathAnswersNotFoundEnvelope(t *testing.T) {
	rec := serve(t, New(), http.MethodGet, "/nope")

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Header().Get(echo.HeaderContentType), echo.MIMEApplicationJSON)
	assert.JSONEq(t, `{"error":{"code":"not_found","message":"Not Found"}}`, rec.Body.String())
}

func TestWrongMethodAnswersEnvelope(t *testing.T) {
	rec := serve(t, New(), http.MethodPost, "/health")

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
	go func() { done <- Run(ctx, New(), "127.0.0.1:0", func(a net.Addr) { addrCh <- a }) }()

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
	case <-time.After(ShutdownTimeout + 5*time.Second):
		t.Fatal("server did not shut down")
	}

	_, err = http.Get("http://" + addr.String() + "/health")
	assert.Error(t, err, "listener must be closed after shutdown")
}

func TestRunDrainsInFlightRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e := New()
	entered := make(chan struct{})
	release := make(chan struct{})
	e.GET("/slow", func(c *echo.Context) error {
		close(entered)
		<-release
		return c.String(http.StatusOK, "done")
	})

	addrCh := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- Run(ctx, e, "127.0.0.1:0", func(a net.Addr) { addrCh <- a }) }()
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

	err = Run(context.Background(), New(), ln.Addr().String(), nil)
	require.Error(t, err)
	assert.False(t, errors.Is(err, context.Canceled))
}
