//go:build e2e

package e2e_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/marketmerge"
)

// historyIDs reads the ids of a default-format history answer.
func historyIDs(t *testing.T, body []byte) []int64 {
	t.Helper()
	var got []struct {
		OrderID int64 `json:"order_id"`
	}
	require.NoError(t, json.Unmarshal(body, &got), string(body))
	ids := []int64{}
	for _, g := range got {
		ids = append(ids, g.OrderID)
	}
	return ids
}

// GET /aggregator/api/dex/history over the stored fills and swaps: an order
// book market, a pool, an address across both, the time window, type,
// limit and the coingecko format with a folded market.
func TestCompatHistoryFromTheStoredTrades(t *testing.T) {
	e := newSyncEnv(t)
	indexTrades(t, e, false)
	_, err := e.db.Exec(`INSERT INTO explorer.denoms (denom, exponent, kind, updated_at)
		VALUES ('ubze', 6, 'native', now()), ($1, 6, 'ibc', now()), ($2, 6, 'factory', now()) ON CONFLICT (denom) DO UPDATE SET exponent = 6`, swapAtom, swapHoney)
	require.NoError(t, err)
	// A self-trade (maker = taker) and a fill of a market folded into the
	// honey one for CoinGecko.
	_, err = e.db.Exec(`INSERT INTO explorer.order_fills (height, seq, order_id, market_id, side, price, amount, maker, taker, time, trade_id)
		SELECT height, 90, '90', $1, 'buy', 3, 5000000, 'bze1self', 'bze1self', time, height * 1000000 + 90 FROM explorer.blocks WHERE height = $3
		UNION ALL
		SELECT height, 91, '91', $2, 'buy', 4, 1000000, $4, 'bze1other', time, height * 1000000 + 91 FROM explorer.blocks WHERE height = $3`,
		swapMarket, swapAtom+"/ubze", swapH, swapMaker)
	require.NoError(t, err)
	base := e.serveCompat(t, "https://explorer.getbze.com", func(c *config.Config) {
		c.CoinGeckoMergedMarkets = marketmerge.Merges{swapAtom + "/ubze": swapMarket}
	})
	get := func(query string) []byte {
		r := fetch(t, base+"/aggregator/api/dex/history"+query, "Origin", "https://dex.getbze.com")
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		assert.Equal(t, "*", r.header.Get("Access-Control-Allow-Origin"))
		return r.body
	}
	id := func(n int64) int64 { return swapH*1_000_000 + n }

	// The order book market: its fills, newest first, in display units.
	assert.Equal(t, `[{"order_id":`+jsonInt(id(90))+`,"price":"3","base_volume":"5","quote_volume":"15","executed_at":"1800000000123",`+
		`"order_type":"buy","maker":"bze1self","taker":"bze1self"},`+
		`{"order_id":`+jsonInt(id(5))+`,"price":"2","base_volume":"0.00001","quote_volume":"0.00002","executed_at":"1800000000123",`+
		`"order_type":"sell","maker":"`+swapMaker+`","taker":"`+swapTrader+`"},`+
		`{"order_id":`+jsonInt(id(3))+`,"price":"2","base_volume":"0.00001","quote_volume":"0.00002","executed_at":"1800000000123",`+
		`"order_type":"sell","maker":"`+swapMaker+`","taker":"`+swapTrader+`"}]`+"\n",
		string(get("?market_id="+swapMarket)))

	// A pool (an IBC denom puts a "/" in its id): its swaps, no maker.
	assert.Equal(t, `[{"order_id":`+jsonInt(id(1))+`,"price":"24","base_volume":"0.00005","quote_volume":"0.0012",`+
		`"executed_at":"1800000000123","order_type":"sell","maker":"","taker":"`+swapTrader+`"},`+
		`{"order_id":22551811000001,"price":"68.221447370623774038","base_volume":"16.665987","quote_volume":"1136.977755",`+
		`"executed_at":"1777391551683","order_type":"sell","maker":"","taker":"`+swapTrader+`"}]`+"\n",
		string(get("?market_id="+swapAtom+"_ubze")))

	// An address: fills as taker and swaps, merged newest first.
	ids := historyIDs(t, get("?address="+swapTrader))
	require.GreaterOrEqual(t, len(ids), 5)
	assert.Equal(t, []int64{id(5), id(3), id(2), id(1)}, ids[:4])
	assert.Contains(t, ids, int64(22551811000001))
	assert.IsDecreasing(t, ids)
	assert.Equal(t, []int64{id(5), id(3)}, historyIDs(t, get("?address="+swapTrader+"&market_id="+swapMarket)))
	assert.Equal(t, []int64{id(5), id(3)}, historyIDs(t, get("?address="+swapTrader+"&limit=2")))
	// As maker, and a self-trade once.
	assert.Equal(t, []int64{id(91), id(5), id(3)}, historyIDs(t, get("?address="+swapMaker)))
	assert.Equal(t, []int64{id(90)}, historyIDs(t, get("?address=bze1self")))
	// type.
	assert.Equal(t, []int64{id(2)}, historyIDs(t, get("?address="+swapTrader+"&type=buy")))

	// The window, inclusive to the millisecond: swapH's block is at …123.
	assert.Equal(t, []int64{id(5), id(3), id(2), id(1)},
		historyIDs(t, get("?address="+swapTrader+"&start_time=1800000000123&end_time=1800000000123")))
	assert.Equal(t, []int64{}, historyIDs(t, get("?address="+swapTrader+"&start_time=1800000000124&end_time=1900000000000")))
	older := historyIDs(t, get("?address="+swapTrader+"&start_time=1&end_time=1800000000122"))
	assert.Contains(t, older, int64(22551811000001))
	assert.NotContains(t, older, id(1))
	assert.Equal(t, ids, historyIDs(t, get("?address="+swapTrader+"&start_time=0&end_time=1800000000122")),
		"one bound at 0: no window")

	// CoinGecko: the folded market's fill merged in.
	assert.Equal(t, `{"buy":[{"trade_id":`+jsonInt(id(91))+`,"price":"4","base_volume":"1","target_volume":"4",`+
		`"trade_timestamp":"1800000000123","type":"buy"},{"trade_id":`+jsonInt(id(90))+`,"price":"3","base_volume":"5",`+
		`"target_volume":"15","trade_timestamp":"1800000000123","type":"buy"}],`+
		`"sell":[{"trade_id":`+jsonInt(id(5))+`,"price":"2","base_volume":"0.00001","target_volume":"0.00002",`+
		`"trade_timestamp":"1800000000123","type":"sell"}]}`+"\n",
		string(get("?format=coingecko&limit=3&ticker_id="+strings.ReplaceAll(swapMarket, "/", "_"))))

	// The second page over real SQL: the address's newest fill is of a
	// market with no known exponent, so with limit=1 the store is read again
	// below it and the older, convertible fill answers.
	_, err = e.db.Exec(`INSERT INTO explorer.order_fills (height, seq, order_id, market_id, side, price, amount, maker, taker, time, trade_id)
		SELECT height, 95, '95', 'factory/bze1x/unknown/ubze', 'buy', 1, 1, 'bze1page', 'bze1other', time, height * 1000000 + 95
		  FROM explorer.blocks WHERE height = $1
		UNION ALL
		SELECT height, 900, '96', $2, 'sell', 2, 3000000, 'bze1page', 'bze1other', time, height * 1000000 + 900
		  FROM explorer.blocks WHERE height = 22551811`, swapH, swapMarket)
	require.NoError(t, err)
	assert.Equal(t, `[{"order_id":22551811000900,"price":"2","base_volume":"3","quote_volume":"6","executed_at":"1777391551683",`+
		`"order_type":"sell","maker":"bze1page","taker":"bze1other"}]`+"\n", string(get("?address=bze1page&limit=1")))

	r := fetch(t, base+"/aggregator/api/dex/history")
	assert.Equal(t, http.StatusBadRequest, r.status)
	assert.Equal(t, `{"message":"please provide market_id or ticker_id"}`+"\n", string(r.body))
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
