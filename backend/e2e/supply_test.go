//go:build e2e

package e2e_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The aggregator-compatible supply routes over the real wiring: the
// exponents the sync wrote to explorer.denoms, the supplies and the
// community pool recorded in the fake gRPC server.
func TestCompatSupply(t *testing.T) {
	e := newSyncEnv(t)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	base := strings.TrimSuffix(e.serveAPI(t), "/api/v1")
	e.grpc.ResetRequests() // the sync read the pool too

	text := func(path string, status int, body string) {
		t.Helper()
		r := fetch(t, base+path, "Origin", "https://getbze.com")
		assert.Equal(t, status, r.status, path)
		assert.Equal(t, body, string(r.body), path)
		assert.Equal(t, "text/plain; charset=UTF-8", r.header.Get("Content-Type"), path)
		assert.Equal(t, "*", r.header.Get("Access-Control-Allow-Origin"), path)
	}

	text("/api/supply/total", http.StatusOK, "281824500.44")
	text("/api/supply/circulating", http.StatusOK, "246885353.87")
	text("/api/supply/total?denom="+vdlDenom, http.StatusOK, "20983185.27")
	text("/api/supply/circulating?denom="+vdlDenom, http.StatusOK, "20983185.27")
	assert.Equal(t, 1, e.grpc.Requests("distribution", "CommunityPool"), "the pool counts only for BZE")

	e.grpc.SetResponse("bank", "SupplyOf", "unope", []byte(`{"amount":{"denom":"unope","amount":"0"}}`))
	text("/api/supply/total?denom=unope", http.StatusBadRequest, "denom unope not found in registry")

	// The node goes down: the reads held are served, a new process has none.
	e.grpc.Stop()
	text("/api/supply/total", http.StatusOK, "281824500.44")
	down := strings.TrimSuffix(e.serveAPI(t), "/api/v1")
	r := fetch(t, down+"/api/supply/circulating")
	assert.Equal(t, http.StatusServiceUnavailable, r.status)
	assert.Equal(t, "supply is unavailable, try again later", string(r.body))
}
