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
		if r.URL.Path != "/api/prices" && r.URL.Path != "/api/dex/tickers" {
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

// recordedTickers are the two BZE/USDC.n markets of getbze.com/api/dex/tickers
// on 2026-10-09: the order book (BZE priced in USDC.n) and the liquidity pool
// (USDC.n priced in ubze).
const recordedTickers = `[
{"base":"ubze","quote":"ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4","market_id":"ubze/ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4","last_price":0.00018,"base_volume":544336.988678,"quote_volume":103.836199,"bid":0.00017,"ask":0.00022,"high":0.00021,"low":0.00018,"open_price":0.00018,"change":0},
{"base":"ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4","quote":"ubze","market_id":"ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4_ubze","last_price":5747.1387947581325,"base_volume":144.033022,"quote_volume":828644.939685,"bid":0,"ask":0,"high":6040.911873237037,"low":5604.113494443305,"open_price":5624.368717574341,"change":2.18}]`

const poolID = "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4_ubze"

func TestTickerAndTheChangeOfEitherSide(t *testing.T) {
	c := aggregator.New(http.DefaultClient, server(t, http.StatusOK, recordedTickers))
	ctx := context.Background()

	pool, err := c.Ticker(ctx, poolID)
	require.NoError(t, err)
	require.NotNil(t, pool)
	assert.Equal(t, "ubze", pool.Quote)
	assert.Equal(t, "5624.368717574341", pool.OpenPrice, "prices kept as the aggregator wrote them")
	pct, ok := aggregator.ChangePct(pool, "ubze")
	require.True(t, ok)
	assert.Equal(t, "-2.1362", pct, "USDC.n +2.18 % in ubze is BZE -2.1362 % in USDC.n")
	pct, ok = aggregator.ChangePct(pool, pool.Base)
	require.True(t, ok)
	assert.Equal(t, "2.1828", pct, "the base side as the ticker's own change")

	book, err := c.Ticker(ctx, "ubze/ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4")
	require.NoError(t, err)
	pct, ok = aggregator.ChangePct(book, "ubze")
	require.True(t, ok)
	assert.Equal(t, "0.0000", pct)

	none, err := c.Ticker(ctx, "ulp/none")
	require.NoError(t, err)
	assert.Nil(t, none, "a market the aggregator does not list")

	_, err = aggregator.New(http.DefaultClient, server(t, http.StatusInternalServerError, ``)).Ticker(ctx, poolID)
	require.Error(t, err)
}

func TestChangePctNeedsPricesAndTheDenom(t *testing.T) {
	for name, tk := range map[string]*aggregator.Ticker{
		"no ticker":     nil,
		"no trade":      {Base: "ubze", Quote: "uusdc", OpenPrice: "0", LastPrice: "1"},
		"no last price": {Base: "ubze", Quote: "uusdc", OpenPrice: "1", LastPrice: "0"},
		"not a number":  {Base: "ubze", Quote: "uusdc", OpenPrice: "x", LastPrice: "1"},
		"another pair":  {Base: "uatom", Quote: "uusdc", OpenPrice: "1", LastPrice: "1"},
	} {
		_, ok := aggregator.ChangePct(tk, "ubze")
		assert.False(t, ok, name)
	}
	pct, ok := aggregator.ChangePct(&aggregator.Ticker{Base: "ubze", Quote: "u", OpenPrice: "3", LastPrice: "2"}, "ubze")
	require.True(t, ok)
	assert.Equal(t, "-33.3333", pct)
	pct, _ = aggregator.ChangePct(&aggregator.Ticker{Base: "ubze", Quote: "u", OpenPrice: "200000", LastPrice: "200001"}, "ubze")
	assert.Equal(t, "0.0005", pct, "0.0005 rounds away from zero")
}
