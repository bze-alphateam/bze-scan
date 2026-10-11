package serve_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

func indexerConfig(nodeURL, chainID string) *config.Config {
	return &config.Config{
		HTTPAddr:       "127.0.0.1:0",
		LogLevel:       "warn",
		LogFormat:      config.LogFormatText,
		DatabaseURL:    "postgres://bze@127.0.0.1:1/none?sslmode=disable",
		NodeRPCURL:     nodeURL,
		ChainID:        chainID,
		IndexerEnabled: true,
	}
}

func TestRefusesANodeOfAnotherChain(t *testing.T) {
	n := fakenode.New(t)

	err := serve.Run(context.Background(), indexerConfig(n.URL, "beezee-testnet"), serve.Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `serves chain "beezee-1"`)
	assert.Contains(t, err.Error(), `CHAIN_ID is "beezee-testnet"`)
	assert.Equal(t, 0, n.Requests(fakenode.RouteBlock), "nothing is indexed")
}

func TestRefusesAnUnreachableNode(t *testing.T) {
	n := fakenode.New(t)
	n.Close()

	err := serve.Run(context.Background(), indexerConfig(n.URL, "beezee-1"), serve.Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "node status")
}

// The API reads the database, so serve needs it with or without the
// indexer.
func TestServeRequiresDatabaseURL(t *testing.T) {
	for _, indexer := range []bool{true, false} {
		cfg := indexerConfig("http://127.0.0.1:1", "beezee-1")
		cfg.DatabaseURL = ""
		cfg.IndexerEnabled = indexer

		err := serve.Run(context.Background(), cfg, serve.Options{})
		assert.ErrorIs(t, err, config.ErrDatabaseURLRequired, "indexer %v", indexer)
	}
}

// DEBUG_ADDR serves the pprof profiles next to the API, and stops with it.
func TestServeExposesPprofOnDebugAddr(t *testing.T) {
	cfg := indexerConfig("http://127.0.0.1:1", "beezee-1")
	cfg.IndexerEnabled = false
	cfg.StatusInterval = time.Hour
	cfg.DebugAddr = "127.0.0.1:0"

	ctx, cancel := context.WithCancel(context.Background())
	debug := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- serve.Run(ctx, cfg, serve.Options{OnDebugListen: func(a net.Addr) { debug <- a }})
	}()

	var addr net.Addr
	select {
	case addr = <-debug:
	case err := <-done:
		t.Fatalf("serve stopped: %v", err)
	}
	res, err := http.Get("http://" + addr.String() + "/debug/pprof/goroutine?debug=1")
	require.NoError(t, err)
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, string(body), "goroutine profile: total")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
}
