package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/server"
	"github.com/bze-alphateam/bze-scan/backend/internal/marketmerge"
	"github.com/bze-alphateam/bze-scan/backend/internal/swaphistory"
	"github.com/bze-alphateam/bze-scan/backend/internal/tradehistory"
)

const (
	histMaker  = "bze15pqjgk4la0mfphwddce00d05n3th3u66n3ptcv"
	histHoney  = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney"
	histMarket = histHoney + "/ubze"
	histUSDC   = "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4"
	histInj    = "ibc/USDCINJ"
	histPool   = swapAtom + "_ubze"
	// A block time with milliseconds: the aggregator has whole seconds.
	histTime = int64(1777391551683)
)

// historyStore answers Fills and Swaps from its rows (newest first) as the
// database filters and pages them, and records what it was asked.
type historyStore struct {
	fills   []tradehistory.Fill
	swaps   []swaphistory.Swap
	fail    bool
	filters []tradehistory.Filter
	kinds   []string
	limits  []int
}

func (s *historyStore) ask(kind string, f tradehistory.Filter, limit int) error {
	s.filters, s.kinds, s.limits = append(s.filters, f), append(s.kinds, kind), append(s.limits, limit)
	if s.fail {
		return errors.New("database is down")
	}
	return nil
}

func keep(f tradehistory.Filter, before, id, timeMs int64, side string) bool {
	return (before == 0 || id < before) && (!f.Sided() || side == f.Side) &&
		(!f.Window() || timeMs >= f.FromMs && timeMs <= f.ToMs)
}

func (s *historyStore) Fills(_ context.Context, f tradehistory.Filter, before int64, limit int) ([]tradehistory.Fill, error) {
	if err := s.ask("fills", f, limit); err != nil {
		return nil, err
	}
	out := []tradehistory.Fill{}
	for _, fl := range s.fills {
		if keep(f, before, fl.TradeID, fl.TimeMs, fl.Side) && (f.MarketID == "" || fl.MarketID == f.MarketID) &&
			(f.Address == "" || fl.Maker == f.Address || fl.Taker == f.Address) && len(out) < limit {
			out = append(out, fl)
		}
	}
	return out, nil
}

func (s *historyStore) Swaps(_ context.Context, f tradehistory.Filter, before int64, limit int) ([]swaphistory.Swap, error) {
	if err := s.ask("swaps", f, limit); err != nil {
		return nil, err
	}
	out := []swaphistory.Swap{}
	for _, sw := range s.swaps {
		if keep(f, before, sw.TradeID, sw.TimeMs, sw.Side) && (f.MarketID == "" || sw.PoolID == f.MarketID) &&
			(f.Address == "" || sw.Creator == f.Address) && len(out) < limit {
			out = append(out, sw)
		}
	}
	return out, nil
}

var histExps = swapExponents{swapAtom: 6, "ubze": 6, histHoney: 6, histUSDC: 6, histInj: 6}

func historyServer(store *historyStore, exps swapExponents, merges marketmerge.Merges) *echo.Echo {
	lg, _ := logtest.NewNullLogger()
	return server.New(server.Deps{CoinGeckoMerges: merges,
		CompatHistory: tradehistory.New(tradehistory.Deps{Store: store, Exponents: exps, Log: lg})})
}

func fillRow(id int64, market, side, price, amount string) tradehistory.Fill {
	return tradehistory.Fill{TradeID: id, TimeMs: histTime, MarketID: market, Side: side, Price: price, Amount: amount,
		Maker: histMaker, Taker: swapAddr}
}

func histSwap(id int64, side string) swaphistory.Swap {
	sw := swapRow(id, swapAtom, "ubze", side, "2000000", "1000000")
	sw.TimeMs = histTime
	return sw
}

func historyGet(t *testing.T, e *echo.Echo, query string) (int, string) {
	t.Helper()
	rec := serve(t, e, http.MethodGet, "/aggregator/api/dex/history"+query)
	return rec.Code, rec.Body.String()
}

func orderIDs(t *testing.T, body string) []int64 {
	t.Helper()
	var got []struct {
		OrderID int64 `json:"order_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &got), body)
	ids := []int64{}
	for _, g := range got {
		ids = append(ids, g.OrderID)
	}
	return ids
}

// An order book market's fills in the aggregator's shape byte for byte:
// the stable trade id as order_id, executed_at the block time in unix
// milliseconds (allowed differences), quote_volume = amount × price
// truncated to the quote's decimals, the resting order's side.
func TestCompatHistoryOfAnOrderBookMarket(t *testing.T) {
	store := &historyStore{fills: []tradehistory.Fill{
		fillRow(24999134000002, histMarket, "sell", "0.333333333333333333", "1000001"),
		fillRow(24999134000001, histMarket, "buy", "2.000000000000000000", "10"),
	}}
	e := historyServer(store, histExps, nil)
	code, body := historyGet(t, e, "?market_id="+histMarket)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, `[{"order_id":24999134000002,"price":"0.333333333333333333","base_volume":"1.000001",`+
		`"quote_volume":"0.333333","executed_at":"1777391551683","order_type":"sell","maker":"`+histMaker+`","taker":"`+swapAddr+`"},`+
		`{"order_id":24999134000001,"price":"2","base_volume":"0.00001","quote_volume":"0.00002",`+
		`"executed_at":"1777391551683","order_type":"buy","maker":"`+histMaker+`","taker":"`+swapAddr+`"}]`+"\n", body)
	assert.Equal(t, []string{"fills", "swaps"}, store.kinds, "the market id is matched against both")
	assert.Equal(t, tradehistory.Filter{MarketID: histMarket}, store.filters[0])

	rec := corsRequest(e, http.MethodGet, "/aggregator/api/dex/history?market_id="+histMarket, "https://dex.getbze.com")
	assert.Equal(t, "*", rec.Header().Get(echo.HeaderAccessControlAllowOrigin))
	assert.Empty(t, rec.Header().Get(echo.HeaderCacheControl), "the aggregator sends none")
}

// A pool's trades are its swaps, as /dex/swaps converts them, without the
// pool fields /dex/swaps adds. An IBC denom puts a "/" in the pool id.
func TestCompatHistoryOfAPool(t *testing.T) {
	store := &historyStore{swaps: []swaphistory.Swap{histSwap(22551811000001, "sell")}}
	code, body := historyGet(t, historyServer(store, histExps, nil), "?market_id="+histPool)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, `[{"order_id":22551811000001,"price":"0.5","base_volume":"2","quote_volume":"1",`+
		`"executed_at":"1777391551683","order_type":"sell","maker":"","taker":"`+swapAddr+`"}]`+"\n", body)
	assert.Equal(t, histPool, store.filters[1].MarketID)
}

// An address without a market: its fills as maker or taker and its swaps,
// merged newest first by trade id, cut to the limit.
func TestCompatHistoryOfAnAddressAcrossFillsAndSwaps(t *testing.T) {
	store := &historyStore{
		fills: []tradehistory.Fill{fillRow(300, histMarket, "buy", "1", "1"), fillRow(100, histMarket, "buy", "1", "1")},
		swaps: []swaphistory.Swap{histSwap(400, "buy"), histSwap(200, "sell")},
	}
	e := historyServer(store, histExps, nil)
	code, body := historyGet(t, e, "?address="+swapAddr)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []int64{400, 300, 200, 100}, orderIDs(t, body))
	assert.Equal(t, []string{"fills", "swaps"}, store.kinds)
	assert.Equal(t, tradehistory.Filter{Address: swapAddr}, store.filters[1])

	_, body = historyGet(t, e, "?address="+swapAddr+"&limit=3")
	assert.Equal(t, []int64{400, 300, 200}, orderIDs(t, body))

	// The maker side matches too.
	_, body = historyGet(t, e, "?address="+histMaker)
	assert.Equal(t, []int64{300, 100}, orderIDs(t, body))

	// With a market, only that market's trades of the address.
	store.kinds = nil
	_, body = historyGet(t, e, "?address="+swapAddr+"&market_id="+histMarket)
	assert.Equal(t, []int64{300, 100}, orderIDs(t, body))
}

func TestCompatHistoryValidation(t *testing.T) {
	store := &historyStore{}
	e := historyServer(store, histExps, nil)
	for _, query := range []string{"", "?market_id=", "?market_id=x", "?ticker_id=x", "?type=buy&limit=5"} {
		code, body := historyGet(t, e, query)
		assert.Equal(t, http.StatusBadRequest, code, query)
		assert.Equal(t, `{"message":"please provide market_id or ticker_id"}`+"\n", body, query)
	}
	for _, query := range []string{"?market_id=ab&limit=abc", "?market_id=ab&limit=1.5", "?market_id=ab&start_time=x",
		"?market_id=ab&end_time=1e3", "?address=a&limit=99999999999999999999"} {
		code, body := historyGet(t, e, query)
		assert.Equal(t, http.StatusBadRequest, code, query)
		assert.Equal(t, `{"message":"invalid request"}`+"\n", body, query)
	}
	assert.Empty(t, store.kinds)

	// An address alone is enough, a one-character one too; so are two
	// characters of a market or ticker id.
	for _, query := range []string{"?address=b", "?market_id=ab", "?ticker_id=ab", "?market_id=x&address=b"} {
		code, _ := historyGet(t, e, query)
		assert.Equal(t, http.StatusOK, code, query)
	}
}

// ticker_id is the market id with "_" for "/": a pool's ticker names the
// order book market of its denoms, as the aggregator reads it.
func TestCompatHistoryTickerID(t *testing.T) {
	store := &historyStore{}
	e := historyServer(store, histExps, nil)
	historyGet(t, e, "?ticker_id=ubze_"+strings.ReplaceAll(histUSDC, "/", "_"))
	historyGet(t, e, "?ticker_id=zzz&market_id="+histPool)
	require.Len(t, store.filters, 4)
	assert.Equal(t, "ubze/ibc/"+histUSDC[4:], store.filters[0].MarketID)
	assert.Equal(t, histPool, store.filters[2].MarketID, "market_id wins over ticker_id")
}

func TestCompatHistoryLimit(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"", 500}, {"&limit=", 500}, {"&limit=0", 500}, {"&limit=-3", 500},
		{"&limit=20", 20}, {"&limit=1000", 1000}, {"&limit=1001", 1000}, {"&limit=99999", 1000},
	} {
		store := &historyStore{}
		historyGet(t, historyServer(store, histExps, nil), "?market_id="+histMarket+tc.query)
		assert.Equal(t, []int{tc.want, tc.want}, store.limits, tc.query)
	}

	var fills []tradehistory.Fill
	for id := int64(1200); id > 0; id-- {
		fills = append(fills, fillRow(id, histMarket, "buy", "1", "1"))
	}
	e := historyServer(&historyStore{fills: fills}, histExps, nil)
	_, body := historyGet(t, e, "?market_id="+histMarket)
	assert.Len(t, orderIDs(t, body), 500)
	_, body = historyGet(t, e, "?market_id="+histMarket+"&limit=5000")
	assert.Len(t, orderIDs(t, body), 1000)
}

// The time bounds are unix milliseconds, inclusive at both ends, and apply
// only when both are above 0.
func TestCompatHistoryTimeWindow(t *testing.T) {
	store := &historyStore{}
	for ms := int64(1000); ms <= 5000; ms += 1000 {
		fl := fillRow(ms, histMarket, "buy", "1", "1")
		fl.TimeMs = ms
		store.fills = append([]tradehistory.Fill{fl}, store.fills...)
	}
	e := historyServer(store, histExps, nil)
	for _, tc := range []struct {
		window string
		want   []int64
		filter [2]int64
	}{
		{"&start_time=2000&end_time=4000", []int64{4000, 3000, 2000}, [2]int64{2000, 4000}},
		{"&start_time=2001&end_time=3999", []int64{3000}, [2]int64{2001, 3999}},
		{"&start_time=3000&end_time=3000", []int64{3000}, [2]int64{3000, 3000}},
		{"&start_time=4000&end_time=2000", []int64{}, [2]int64{4000, 2000}},
		{"&start_time=2000", []int64{5000, 4000, 3000, 2000, 1000}, [2]int64{2000, 0}},
		{"&end_time=2000", []int64{5000, 4000, 3000, 2000, 1000}, [2]int64{0, 2000}},
		{"&start_time=0&end_time=2000", []int64{5000, 4000, 3000, 2000, 1000}, [2]int64{0, 2000}},
		{"&start_time=-5&end_time=2000", []int64{5000, 4000, 3000, 2000, 1000}, [2]int64{-5, 2000}},
	} {
		store.filters = nil
		code, body := historyGet(t, e, "?market_id="+histMarket+tc.window)
		require.Equal(t, http.StatusOK, code, tc.window)
		assert.Equal(t, tc.want, orderIDs(t, body), tc.window)
		f := store.filters[0]
		assert.Equal(t, tc.filter, [2]int64{f.FromMs, f.ToMs}, tc.window)
		assert.Equal(t, tc.filter[0] > 0 && tc.filter[1] > 0, f.Window(), tc.window)
	}
}

func TestCompatHistoryType(t *testing.T) {
	store := &historyStore{fills: []tradehistory.Fill{
		fillRow(3, histMarket, "sell", "1", "1"), fillRow(2, histMarket, "buy", "1", "1"), fillRow(1, histMarket, "sell", "1", "1"),
	}}
	e := historyServer(store, histExps, nil)
	for query, want := range map[string][]int64{
		"&type=sell": {3, 1}, "&type=buy": {2}, "&type=": {3, 2, 1}, "&type=BUY": {3, 2, 1}, "&type=all": {3, 2, 1},
	} {
		_, body := historyGet(t, e, "?market_id="+histMarket+query)
		assert.Equal(t, want, orderIDs(t, body), query)
	}
}

// format=coingecko splits the trades by side under CoinGecko's names; a
// side without trades is left out, as the aggregator answers.
func TestCompatHistoryCoinGeckoFormat(t *testing.T) {
	store := &historyStore{fills: []tradehistory.Fill{
		fillRow(3, histMarket, "sell", "2", "1000000"), fillRow(2, histMarket, "buy", "4", "2000000"),
		fillRow(1, histMarket, "sell", "1", "3000000"),
	}}
	e := historyServer(store, histExps, nil)
	code, body := historyGet(t, e, "?format=coingecko&ticker_id="+strings.ReplaceAll(histMarket, "/", "_"))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, `{"buy":[{"trade_id":2,"price":"4","base_volume":"2","target_volume":"8","trade_timestamp":"1777391551683","type":"buy"}],`+
		`"sell":[{"trade_id":3,"price":"2","base_volume":"1","target_volume":"2","trade_timestamp":"1777391551683","type":"sell"},`+
		`{"trade_id":1,"price":"1","base_volume":"3","target_volume":"3","trade_timestamp":"1777391551683","type":"sell"}]}`+"\n", body)
	assert.Equal(t, histMarket, store.filters[0].MarketID, "the ticker id's market")

	_, body = historyGet(t, e, "?format=coingecko&market_id="+histMarket+"&type=buy")
	assert.Equal(t, `{"buy":[{"trade_id":2,"price":"4","base_volume":"2","target_volume":"8","trade_timestamp":"1777391551683","type":"buy"}]}`+"\n", body)
	_, body = historyGet(t, e, "?format=coingecko&market_id=ubze/nothing")
	assert.Equal(t, "{}\n", body)
	_, body = historyGet(t, e, "?format=CoinGecko&market_id=ubze/nothing")
	assert.Equal(t, "[]\n", body, "any other format is the default")
}

// For a market others are folded into, CoinGecko gets the trades of all of
// them, newest first, cut to the limit; the default format keeps them apart.
func TestCompatHistoryCoinGeckoMergedMarkets(t *testing.T) {
	tracked, folded := "ubze/"+histUSDC, "ubze/"+histInj
	store := &historyStore{fills: []tradehistory.Fill{
		fillRow(50, folded, "buy", "1", "1"), fillRow(40, tracked, "sell", "1", "1"),
		fillRow(30, folded, "sell", "1", "1"), fillRow(20, tracked, "buy", "1", "1"),
	}}
	merges, err := marketmerge.New(map[string]string{folded: tracked})
	require.NoError(t, err)
	e := historyServer(store, histExps, merges)

	_, body := historyGet(t, e, "?format=coingecko&market_id="+tracked+"&limit=3")
	var got struct {
		Buy, Sell []struct {
			ID int64 `json:"trade_id"`
		}
	}
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	ids := func(l []struct {
		ID int64 `json:"trade_id"`
	}) []int64 {
		out := []int64{}
		for _, x := range l {
			out = append(out, x.ID)
		}
		return out
	}
	assert.Equal(t, []int64{50}, ids(got.Buy), "20 is cut by the limit of 3")
	assert.Equal(t, []int64{40, 30}, ids(got.Sell))

	_, body = historyGet(t, e, "?market_id="+tracked)
	assert.Equal(t, []int64{40, 20}, orderIDs(t, body), "the default format does not merge")
	_, body = historyGet(t, e, "?format=coingecko&market_id="+folded)
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	assert.Equal(t, []int64{50}, ids(got.Buy), "a folded market alone is itself")
}

func TestCompatHistoryWithoutTradesIsAnEmptyList(t *testing.T) {
	code, body := historyGet(t, historyServer(&historyStore{}, histExps, nil), "?address="+swapAddr)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "[]\n", body)
}

func TestCompatHistoryStoreErrorIsTheAggregators400(t *testing.T) {
	e := historyServer(&historyStore{fail: true}, histExps, nil)
	for _, query := range []string{"?market_id=" + histMarket, "?format=coingecko&market_id=" + histMarket} {
		code, body := historyGet(t, e, query)
		assert.Equal(t, http.StatusBadRequest, code, query)
		assert.Equal(t, `{"message":"Unknown error! Please report this issue to the administrators at alphateam@getbze.com"}`+"\n", body, query)
	}
}

func TestCompatHistoryNeedsAService(t *testing.T) {
	rec := serve(t, server.New(server.Deps{}), http.MethodGet, "/aggregator/api/dex/history?market_id="+histMarket)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// A market id is cut at the "/" where both sides are known denoms, so
// factory and IBC denoms keep their own slashes. Trades of a market whose
// denoms have no known exponent are left out, as the aggregator never
// stores them, and older ones take their place.
func TestCompatHistoryLeavesOutWhatCannotBeConverted(t *testing.T) {
	var fills []tradehistory.Fill
	for id := int64(30); id > 0; id-- {
		market := histMarket
		if id > 20 {
			market = "factory/bze1x/unknown/ubze"
		}
		fills = append(fills, fillRow(id, market, "buy", "1", "1"))
	}
	store := &historyStore{fills: fills}
	_, body := historyGet(t, historyServer(store, histExps, nil), "?address="+swapAddr+"&limit=10")
	assert.Equal(t, []int64{20, 19, 18, 17, 16, 15, 14, 13, 12, 11}, orderIDs(t, body))
	assert.Equal(t, []string{"fills", "fills", "swaps"}, store.kinds, "a page of unknowns, then one that fills up")
	assert.Equal(t, []int{10, 10, 10}, store.limits, "pages of the limit")

	store = &historyStore{fills: []tradehistory.Fill{fillRow(1, "ibc/AAA/ibc/BBB", "buy", "3", "1000000")}}
	_, body = historyGet(t, historyServer(store, swapExponents{"ibc/AAA": 6, "ibc/BBB": 8}, nil), "?market_id=ibc/AAA/ibc/BBB")
	assert.Contains(t, body, `"price":"0.03","base_volume":"1","quote_volume":"0.03"`)
}

// Same-second trades are never lost (the aggregator drops rows of one second
// at its sync page boundaries): every trade of a block is there, in the
// chain's order.
func TestCompatHistoryKeepsSameSecondTrades(t *testing.T) {
	var fills []tradehistory.Fill
	for n := int64(7); n > 0; n-- {
		fills = append(fills, fillRow(25000000_000000+n, histMarket, "buy", "1", "1"))
	}
	_, body := historyGet(t, historyServer(&historyStore{fills: fills}, histExps, nil), "?market_id="+histMarket)
	assert.Equal(t, []int64{25000000000007, 25000000000006, 25000000000005, 25000000000004, 25000000000003,
		25000000000002, 25000000000001}, orderIDs(t, body))
}

// Exponents shift the price as the aggregator does, except (allowed
// difference, BZE-133) from three decimals more on the quote side, where
// the aggregator's price is 0 and Scan's exact.
func TestCompatHistoryPriceInDisplayUnits(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		baseExp, quoteExp          int
		price, amount              string
		wantPrice, wantBase, wantQ string
	}{
		{"same exponents", 6, 6, "0.000334000000000000", "123456789", "0.000334", "123.456789", "0.041234"},
		{"base has two more decimals", 8, 6, "0.05", "100000000", "5", "1", "5"},
		{"quote has one more decimal, rounded to 18 decimals", 6, 7, "0.666666666666666667", "3000000",
			"0.066666666666666667", "3", "0.2"},
		{"quote has two more decimals", 6, 8, "33.333333333333333333", "3000000", "0.333333333333333333", "3", "0.99999999"},
		{"quote has twelve more decimals", 6, 18, "1500000000000", "2000000", "1.5", "2", "3"},
		{"quote has eighteen decimals, past a LegacyDec", 0, 18, "0.000000000000000001", "7",
			"0.000000000000000000000000000000000001", "7", "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &historyStore{fills: []tradehistory.Fill{fillRow(9, "ubze/"+histUSDC, "buy", tc.price, tc.amount)}}
			_, body := historyGet(t, historyServer(store, swapExponents{"ubze": tc.baseExp, histUSDC: tc.quoteExp}, nil),
				"?market_id=ubze/"+histUSDC)
			assert.Contains(t, body, `"price":"`+tc.wantPrice+`","base_volume":"`+tc.wantBase+`","quote_volume":"`+tc.wantQ+`"`)
		})
	}
}
