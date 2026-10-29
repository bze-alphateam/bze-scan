//go:build e2e

package e2e_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
)

// serveCompat runs the real serve wiring (no indexer) over the sync env's
// database and fake CoinGecko, CORS configured for one origin, and returns
// the server's base URL. with adjusts the config before serving.
func (e *syncEnv) serveCompat(t *testing.T, corsOrigin string, with ...func(*config.Config)) string {
	t.Helper()
	cfg := &config.Config{
		HTTPAddr: "127.0.0.1:0", LogLevel: "warn", LogFormat: config.LogFormatText,
		DatabaseURL: e.url, NodeRPCURL: e.node.URL, ChainID: "beezee-1",
		CORSAllowedOrigins: []string{corsOrigin},
		CoinGeckoHost:      e.gecko.URL, CoinGeckoPriceIDs: geckoIDs,
	}
	for _, w := range with {
		w(cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	listening := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- serve.Run(ctx, cfg, serve.Options{OnListen: func(a net.Addr) { listening <- a }}) }()
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-done)
	})
	select {
	case a := <-listening:
		return "http://" + a.String()
	case err := <-done:
		t.Fatalf("serve exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not listen")
	}
	return ""
}

func TestCompatPricesFromCoinGecko(t *testing.T) {
	e := newSyncEnv(t)
	const explorer, dapp = "https://explorer.getbze.com", "https://dex.getbze.com"
	base := e.serveCompat(t, explorer)

	// The aggregator's shape, in COINGECKO_PRICE_IDS' order, any origin.
	r := fetch(t, base+"/aggregator/api/prices", "Origin", dapp)
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, recordedPrices+"\n", string(r.body), "getbze.com's answer")
	assert.Equal(t, "*", r.header.Get("Access-Control-Allow-Origin"))
	calls, query := e.gecko.answered()
	assert.Equal(t, 1, calls)
	assert.Equal(t, "ids=bzedge,osmosis,cosmos&vs_currencies=usd", query)

	// Read once per 180 s; a CoinGecko failure meanwhile changes nothing.
	e.gecko.setDown(true)
	assert.Equal(t, r.body, fetch(t, base+"/aggregator/api/prices", "Origin", explorer).body)
	calls, _ = e.gecko.answered()
	assert.Equal(t, 1, calls)

	// /api/v1 keeps its configured origins.
	assert.Equal(t, explorer, fetch(t, base+"/api/v1/blocks/x", "Origin", explorer).header.Get("Access-Control-Allow-Origin"))
	assert.Empty(t, fetch(t, base+"/api/v1/blocks/x", "Origin", dapp).header.Get("Access-Control-Allow-Origin"))
}

// Without a trade of the change market in the last 24 hours the prices job
// still fills price_usd (the pools' spot prices need no trade); the 24-hour
// change stays empty.
func TestThePricesJobWithoutTradesLeavesTheChangeEmpty(t *testing.T) {
	e := newSyncEnv(t)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	assert.Equal(t, recordedPriceRows, queryStrings(t, e.db, pricedDenomsSQL))
	assert.Zero(t, e.count(t, `SELECT count(*) FROM explorer.denoms WHERE price_change_24h_pct IS NOT NULL`))
}
