//go:build e2e

package e2e_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/marketmerge"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// The mainnet markets of the recorded books: BZE/USDC.n has asks only, and
// BZE/USDC.inj, folded into it for CoinGecko, both sides.
const (
	usdcNMarket   = "ubze/ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4"
	usdcInjMarket = "ubze/ibc/81DA528F4C5546208D1D50F84C2D0B388B45D37AA36FCA622A3CB2D8EF014838"
)

var timestampField = regexp.MustCompile(`"timestamp":"(\d+)",`)

// withoutTimestamp checks that body's timestamp is now in unix seconds and
// drops it, the way the aggregator's answers in testdata/compat were saved.
func withoutTimestamp(t *testing.T, body []byte) string {
	t.Helper()
	m := timestampField.FindSubmatch(body)
	require.NotNil(t, m, string(body))
	sec, err := strconv.ParseInt(string(m[1]), 10, 64)
	require.NoError(t, err)
	assert.InDelta(t, time.Now().Unix(), sec, 5)
	return string(timestampField.ReplaceAll(body, nil))
}

func aggregatorAnswer(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/compat/" + name)
	require.NoError(t, err)
	return string(b)
}

// TestCompatOrdersFromTheNode serves /api/dex/orders over the real wiring:
// the books from the fake gRPC node (mainnet's, recorded at one height), the
// exponents from explorer.denoms as the denoms sync wrote them, compared
// with getbze.com's answers at the same height.
func TestCompatOrdersFromTheNode(t *testing.T) {
	// Read before the sync, which changes the working directory.
	wantInj := aggregatorAnswer(t, "orders_usdc_inj.json")
	wantN := aggregatorAnswer(t, "orders_usdc_n.json")
	wantGecko := aggregatorAnswer(t, "orders_usdc_n_coingecko_depth4.json")

	e := newSyncEnv(t)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	// The recorded chain registry predates USDC.inj, whose bank metadata is
	// the ibc-go placeholder (exponent 0); mainnet's registry has it at 6.
	_, err := e.db.Exec(`UPDATE explorer.denoms SET exponent = 6 WHERE denom = 'ibc/81DA528F4C5546208D1D50F84C2D0B388B45D37AA36FCA622A3CB2D8EF014838'`)
	require.NoError(t, err)
	assert.Equal(t, []string{usdcDenom + " 6", "ibc/81DA528F4C5546208D1D50F84C2D0B388B45D37AA36FCA622A3CB2D8EF014838 6", "ubze 6"},
		queryStrings(t, e.db, `SELECT concat_ws(' ', denom, exponent) FROM explorer.denoms
			WHERE denom IN ('ubze', $1, 'ibc/81DA528F4C5546208D1D50F84C2D0B388B45D37AA36FCA622A3CB2D8EF014838') ORDER BY denom`, usdcDenom))

	// The recorded market list predates BZE/USDC.inj.
	var markets map[string]any
	require.NoError(t, json.Unmarshal(fakenode.ReadGRPCFixture(t, "tradebin", "AllMarkets", ""), &markets))
	markets["market"] = append(markets["market"].([]any), map[string]any{
		"base": "ubze", "quote": "ibc/81DA528F4C5546208D1D50F84C2D0B388B45D37AA36FCA622A3CB2D8EF014838", "creator": "bze1creator",
	})
	body, err := json.Marshal(markets)
	require.NoError(t, err)
	e.grpc.SetResponse("tradebin", "AllMarkets", "", body)
	e.grpc.ResetRequests()

	merges, err := marketmerge.Parse(usdcInjMarket + "=" + usdcNMarket)
	require.NoError(t, err)
	base := e.serveCompat(t, "https://explorer.getbze.com", func(cfg *config.Config) {
		cfg.NodeGRPCAddr = e.grpc.Addr
		cfg.CoinGeckoMergedMarkets = merges
	})
	orders := func(query string) response {
		return fetch(t, base+"/aggregator/api/dex/orders?"+query, "Origin", "https://dex.getbze.com")
	}

	// The whole book, as getbze.com answered it.
	r := orders("market_id=" + url.QueryEscape(usdcInjMarket))
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, wantInj, withoutTimestamp(t, r.body))
	assert.Equal(t, "*", r.header.Get("Access-Control-Allow-Origin"))

	// No bids: [] where the aggregator answers null, the asks the same.
	r = orders("ticker_id=" + url.QueryEscape("ubze_ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4"))
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	want := regexp.MustCompile(`"bids":null`).ReplaceAllString(wantN, `"bids":[]`)
	assert.Equal(t, want, withoutTimestamp(t, r.body))

	// CoinGecko reads BZE/USDC.n's ticker with BZE/USDC.inj's book.
	r = orders("ticker_id=" + url.QueryEscape("ubze_ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4") + "&format=coingecko&depth=4")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, wantGecko, withoutTimestamp(t, r.body))

	// Every book was read once: the answers above came from the 10 s cache.
	for _, key := range []string{
		url.PathEscape(usdcInjMarket) + ".buy", url.PathEscape(usdcInjMarket) + ".sell",
		url.PathEscape(usdcNMarket) + ".buy", url.PathEscape(usdcNMarket) + ".sell",
	} {
		assert.Equal(t, 1, e.grpc.RequestsFor("tradebin", "MarketAggregatedOrders", key), key)
	}
	assert.Equal(t, 1, e.grpc.Requests("tradebin", "AllMarkets"), "the market list is read once")

	r = orders("market_id=ubze/nope")
	assert.Equal(t, http.StatusBadRequest, r.status)
	assert.JSONEq(t, `{"message":"Unknown error! Please report this issue to the administrators at alphateam@getbze.com"}`, string(r.body))
}
