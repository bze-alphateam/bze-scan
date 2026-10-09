package aggregator_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/aggregator"
)

func server(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/prices" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/"
}

func TestPricesByCoingeckoID(t *testing.T) {
	// The aggregator's answer as getbze.com/api/prices gives it (2026-10-09),
	// plus entries the client leaves out.
	url := server(t, http.StatusOK, `[{"denom":"bzedge","price":0.00016806,"price_denom":"usd"},
		{"denom":"osmosis","price":0.03403806,"price_denom":"usd"},{"denom":"cosmos","price":1.96,"price_denom":"USD"},
		{"denom":"tiny","price":1e-9,"price_denom":"usd"},
		{"denom":"euro","price":1,"price_denom":"eur"},{"denom":"","price":1,"price_denom":"usd"},
		{"denom":"negative","price":-1,"price_denom":"usd"}]`)
	got, err := aggregator.New(http.DefaultClient, url).Prices(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"bzedge": "0.000168060000", "osmosis": "0.034038060000", "cosmos": "1.960000000000", "tiny": "0.000000001000",
	}, got)
}

func TestPricesFailures(t *testing.T) {
	ctx := context.Background()
	for name, url := range map[string]string{
		"an error status": server(t, http.StatusBadGateway, `bad gateway`),
		"not json":        server(t, http.StatusOK, `<html>`),
		"not a list":      server(t, http.StatusOK, `{"price":1}`),
		"unreachable":     "http://127.0.0.1:1",
	} {
		_, err := aggregator.New(http.DefaultClient, url).Prices(ctx)
		assert.Error(t, err, name)
	}
}
