//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/marketmerge"
)

// The recorded pools of the fake node (AllLiquidityPools): VDL/BZE (1000
// VDL, 2000 BZE), COREY/BZE (1 and 1) and USDC.n/BZE (5000 USDC.n, 900000
// BZE).
const (
	tickerVDL        = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uvdl"
	tickerCorey      = "factory/bze1f0qgels0eu96ev6a67znu70q7rquy9eragn8nw/ucorey"
	tickerVDLPool    = tickerVDL + "_ubze"
	tickerCoreyPool  = tickerCorey + "_ubze"
	tickerUSDCNPool  = recordedPool
	tickerBlocksFrom = 30_000_001
)

// seedTickerBlocks writes blocks at heights from, from+1, … at the given
// ages (a Postgres interval each), for the trades of the ticker tests.
func seedTickerBlocks(t *testing.T, e *syncEnv, from int64, ages ...string) {
	t.Helper()
	for i, age := range ages {
		_, err := e.db.Exec(`INSERT INTO explorer.blocks (height, time, hash) VALUES ($1, now() - $2::interval, $3)`,
			from+int64(i), age, fmt.Sprintf("TICKER%d", from+int64(i)))
		require.NoError(t, err)
	}
}

// seedSwap writes a swap of pool at height (trade ordinal n) with its
// block's time.
func seedSwap(t *testing.T, e *syncEnv, height int64, n int, pool, base, quote, baseAmount, quoteAmount string) {
	t.Helper()
	_, err := e.db.Exec(`INSERT INTO explorer.swaps (height, trade_id, time, pool_id, base, quote, side, base_amount,
			quote_amount, creator) SELECT height, height * 1000000 + $2, time, $3, $4, $5, 'buy', $6, $7, 'bze1swapper'
		  FROM explorer.blocks WHERE height = $1`, height, n, pool, base, quote, baseAmount, quoteAmount)
	require.NoError(t, err)
}

// seedFill writes an order book fill of market at height (trade ordinal n)
// with its block's time.
func seedFill(t *testing.T, e *syncEnv, height int64, n int, market, price, amount string) {
	t.Helper()
	_, err := e.db.Exec(`INSERT INTO explorer.order_fills (height, seq, order_id, market_id, side, price, amount, maker,
			taker, time, trade_id) SELECT height, $2, $2::text, $3, 'buy', $4, $5, 'bze1maker', 'bze1taker', time,
			height * 1000000 + $2 FROM explorer.blocks WHERE height = $1`, height, n, market, price, amount)
	require.NoError(t, err)
}

// rebuildCandles recomputes every market's candles from the seeded trades.
func rebuildCandles(t *testing.T, e *syncEnv) {
	t.Helper()
	_, _, err := rebuilder(t, e).All(context.Background(), "")
	require.NoError(t, err)
}

// seedChangeSwaps gives the USDC.n/BZE pool two swaps in the last 24 hours
// at the open and last price getbze.com's ticker had on 2026-10-09
// (5624.368717574341 then 5747.1387947581325 BZE per USDC.n), and their
// candles.
func seedChangeSwaps(t *testing.T, e *syncEnv) {
	t.Helper()
	seedTickerBlocks(t, e, tickerBlocksFrom, "3 hours", "1 hour")
	seedSwap(t, e, tickerBlocksFrom, 1, tickerUSDCNPool, usdcDenom, "ubze", "1000000000000", "5624368717574341")
	seedSwap(t, e, tickerBlocksFrom+1, 1, tickerUSDCNPool, usdcDenom, "ubze", "10000000000000", "57471387947581325")
	rebuildCandles(t, e)
}

func jsonFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// GET /aggregator/api/dex/tickers over the real wiring: fills, swaps and
// their candles in the database, the books, pools and LP supplies of the
// fake node, both formats.
func TestCompatTickersOverTheStoredTrades(t *testing.T) {
	e := newSyncEnv(t)
	_, err := e.db.Exec(`INSERT INTO explorer.denoms (denom, exponent, kind, updated_at)
		VALUES ('ubze', 6, 'native', now()), ($1, 6, 'ibc', now()), ($2, 6, 'factory', now()), ($3, 6, 'factory', now())
		ON CONFLICT (denom) DO UPDATE SET exponent = 6`, usdcDenom, tickerVDL, tickerCorey)
	require.NoError(t, err)

	// 25 hours ago (out of the window), 3 hours ago and an hour ago: BZE/USDC.n
	// fills and VDL/BZE swaps.
	const h = 30_100_001
	seedTickerBlocks(t, e, h, "25 hours", "3 hours", "1 hour")
	seedFill(t, e, h, 1, usdcNMarket, "0.0002", "1000000000")
	seedFill(t, e, h+1, 1, usdcNMarket, "0.00018", "1000000000")
	seedFill(t, e, h+2, 1, usdcNMarket, "0.00019", "2000000000")
	seedSwap(t, e, h, 2, tickerVDLPool, tickerVDL, "ubze", "1000000", "3000000")
	seedSwap(t, e, h+1, 2, tickerVDLPool, tickerVDL, "ubze", "1000000", "2000000")
	seedSwap(t, e, h+2, 2, tickerVDLPool, tickerVDL, "ubze", "1000000", "2500000")
	rebuildCandles(t, e)

	// One order book market (the recorded BZE/USDC.n book: asks only) and
	// the LP supplies.
	e.grpc.SetResponse("tradebin", "AllMarkets", "", []byte(`{"market":[{"base":"ubze","quote":"`+usdcDenom+
		`","creator":"bze1creator"}],"pagination":{"next_key":null,"total":"1"}}`))
	for _, pool := range []string{tickerVDLPool, tickerCoreyPool, tickerUSDCNPool} {
		e.grpc.SetResponse("bank", "SupplyOf", url.PathEscape("ulp_"+pool),
			[]byte(`{"amount":{"denom":"ulp_`+pool+`","amount":"1000000000000"}}`))
	}

	base := e.serveCompat(t, "https://explorer.getbze.com", func(cfg *config.Config) {
		cfg.NodeGRPCAddr = e.grpc.Addr
		cfg.USDCDenom = usdcDenom
		cfg.CoinGeckoMergedMarkets = marketmerge.Merges{tickerCoreyPool: tickerVDLPool}
	})
	get := func(query string) string {
		r := fetch(t, base+"/aggregator/api/dex/tickers"+query, "Origin", "https://dex.getbze.com")
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		assert.Equal(t, "*", r.header.Get("Access-Control-Allow-Origin"))
		return string(r.body)
	}

	// By market id: the pools with their 24-hour swaps (the one of 25 hours
	// ago left out), the order book with its fills and best ask.
	zero := `"last_price":0,"base_volume":0,"quote_volume":0,"bid":0,"ask":0,"high":0,"low":0,"open_price":0,"change":0}`
	assert.Equal(t, `[{"base":"`+tickerVDL+`","quote":"ubze","market_id":"`+tickerVDLPool+`","last_price":2.5,"base_volume":2,`+
		`"quote_volume":4.5,"bid":0,"ask":0,"high":2.5,"low":2,"open_price":2,"change":25},`+
		`{"base":"`+tickerCorey+`","quote":"ubze","market_id":"`+tickerCoreyPool+`",`+zero+`,`+
		`{"base":"`+usdcDenom+`","quote":"ubze","market_id":"`+tickerUSDCNPool+`",`+zero+`,`+
		`{"base":"ubze","quote":"`+usdcDenom+`","market_id":"`+usdcNMarket+`","last_price":0.00019,"base_volume":3000,`+
		`"quote_volume":0.56,"bid":0,"ask":0.0006,"high":0.00019,"low":0.00018,"open_price":0.00018,"change":5.56}]`+"\n",
		get(""))

	// CoinGecko: lp_ ids, the liquidity in USD (BZE at 1/180 USD.n through
	// its pool, VDL at 2 BZE, COREY at 1 BZE), COREY/BZE folded into
	// VDL/BZE.
	assert.Equal(t, `[{"ticker_id":"lp_`+tickerVDLPool+`","base_currency":"`+tickerVDL+`","target_currency":"ubze",`+
		`"pool_id":"lp_`+tickerVDLPool+`","last_price":2.5,"base_volume":2,"target_volume":4.5,"bid":0,"ask":0,"high":2.5,"low":2,`+
		`"liquidity_in_usd":`+jsonFloat(200.0/9+1.0/90)+`},`+
		`{"ticker_id":"lp_`+tickerUSDCNPool+`","base_currency":"`+usdcDenom+`","target_currency":"ubze",`+
		`"pool_id":"lp_`+tickerUSDCNPool+`","last_price":0,"base_volume":0,"target_volume":0,"bid":0,"ask":0,"high":0,"low":0,`+
		`"liquidity_in_usd":10000},`+
		`{"ticker_id":"ubze_`+usdcDenom+`","base_currency":"ubze","target_currency":"`+usdcDenom+`","pool_id":"`+usdcNMarket+`",`+
		`"last_price":0.00019,"base_volume":3000,"target_volume":0.56,"bid":0,"ask":0.0006,"high":0.00019,"low":0.00018}]`+"\n",
		get("?format=coingecko"))

	// Both answers came from one build: every book side and the pools were
	// read once.
	assert.Equal(t, 1, e.grpc.RequestsFor("tradebin", "MarketAggregatedOrders", url.PathEscape(usdcNMarket)+".sell"))
	assert.Equal(t, 1, e.grpc.Requests("tradebin", "AllLiquidityPools"))
}
