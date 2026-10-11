package debugserver_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/debugserver"
)

func TestHandlerServesTheProfiles(t *testing.T) {
	srv := httptest.NewServer(debugserver.Handler())
	defer srv.Close()

	for path, want := range map[string]string{
		"/debug/pprof/":                    "goroutine",
		"/debug/pprof/goroutine?debug=1":   "goroutine profile: total",
		"/debug/pprof/heap?debug=1":        "heap profile",
		"/debug/pprof/cmdline":             "",
		"/debug/pprof/profile?seconds=1":   "",
		"/debug/pprof/symbol":              "num_symbols",
		"/debug/pprof/trace?seconds=0.001": "",
	} {
		res, err := http.Get(srv.URL + path)
		require.NoError(t, err, path)
		body, err := io.ReadAll(res.Body)
		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
		assert.Equal(t, http.StatusOK, res.StatusCode, path)
		assert.Contains(t, string(body), want, path)
	}
}

func TestHandlerServesNothingElse(t *testing.T) {
	srv := httptest.NewServer(debugserver.Handler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/v1/status")
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestRunServesUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	listening := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- debugserver.Run(ctx, "127.0.0.1:0", func(a net.Addr) { listening <- a }) }()

	addr := <-listening
	res, err := http.Get("http://" + addr.String() + "/debug/pprof/goroutine?debug=1")
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, http.StatusOK, res.StatusCode)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	_, err = new(net.Dialer).DialContext(context.Background(), "tcp", addr.String())
	assert.Error(t, err, "the listener is closed")
}

func TestRunFailsOnABadAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	err = debugserver.Run(context.Background(), ln.Addr().String(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "debug server listen")
}
