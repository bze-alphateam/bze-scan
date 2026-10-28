package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/labstack/echo/v5"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/bze-alphateam/bze-scan/backend/app/server"
	"github.com/bze-alphateam/bze-scan/backend/internal/marketmerge"
	"github.com/bze-alphateam/bze-scan/backend/internal/orderbook"
)

const (
	usdcMarket  = "ubze/uusdc"  // both 6 decimals
	newMarket   = "ubze/unew"   // folded into usdcMarket
	emptyMarket = "ubze/uempty" // folded into usdcMarket, never has orders
	weiMarket   = "ubze/uwei"   // the quote has 12 decimals more
	unknownMsg  = `{"message":"Unknown error! Please report this issue to the administrators at alphateam@getbze.com"}` + "\n"
)

// bookNode is a tradebin query client answering from books, two levels a
// page, in the order it holds them (bids reversed, as the node does with
// Reverse), counting the calls.
type bookNode struct {
	mu          sync.Mutex
	books       map[string]map[string][]tradebintypes.AggregatedOrder // market → side → levels, by ascending price
	fail        bool
	orderCalls  map[string]int
	marketCalls int
	reverse     map[string]bool
}

func newBookNode() *bookNode {
	return &bookNode{
		books: map[string]map[string][]tradebintypes.AggregatedOrder{
			usdcMarket: {
				"buy":  levels("0.000330", "1000000", "0.000331", "2500000", "0.000332", "473334620", "0.000333", "20818779"),
				"sell": levels("0.000334", "647064654", "0.000335", "511637133", "0.0006", "4919636072"),
			},
			newMarket: {
				"buy":  levels("0.0004", "1000000"),
				"sell": levels("0.0005", "2000000"),
			},
			emptyMarket: {},
			weiMarket: {
				"sell": levels("1234567890", "3000000"),
			},
		},
		orderCalls: map[string]int{},
		reverse:    map[string]bool{},
	}
}

// levels builds aggregated orders from price, amount pairs.
func levels(pairs ...string) []tradebintypes.AggregatedOrder {
	var out []tradebintypes.AggregatedOrder
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, tradebintypes.AggregatedOrder{Price: pairs[i], Amount: pairs[i+1]})
	}
	return out
}

func (n *bookNode) MarketAggregatedOrders(_ context.Context, in *tradebintypes.QueryMarketAggregatedOrdersRequest,
	_ ...grpc.CallOption) (*tradebintypes.QueryMarketAggregatedOrdersResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.orderCalls[in.Market+" "+in.OrderType]++
	n.reverse[in.OrderType] = in.Pagination.GetReverse()
	if n.fail {
		return nil, errors.New("node down")
	}
	list := append([]tradebintypes.AggregatedOrder(nil), n.books[in.Market][in.OrderType]...)
	if in.Pagination.GetReverse() {
		for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
			list[i], list[j] = list[j], list[i]
		}
	}
	start := 0
	if k := in.Pagination.GetKey(); len(k) > 0 {
		start, _ = strconv.Atoi(string(k))
	}
	end := min(start+2, len(list))
	res := &tradebintypes.QueryMarketAggregatedOrdersResponse{List: list[start:end], Pagination: &query.PageResponse{}}
	if end < len(list) {
		res.Pagination.NextKey = []byte(strconv.Itoa(end))
	}
	return res, nil
}

func (n *bookNode) AllMarkets(context.Context, *tradebintypes.QueryAllMarketsRequest,
	...grpc.CallOption) (*tradebintypes.QueryAllMarketsResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.marketCalls++
	if n.fail {
		return nil, errors.New("node down")
	}
	return &tradebintypes.QueryAllMarketsResponse{Market: []tradebintypes.Market{
		{Base: "ubze", Quote: "uusdc"}, {Base: "ubze", Quote: "unew"},
		{Base: "ubze", Quote: "uempty"}, {Base: "ubze", Quote: "uwei"},
		{Base: "unoexp", Quote: "ubze"},
	}}, nil
}

func (n *bookNode) calls(market, side string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.orderCalls[market+" "+side]
}

type exponents map[string]int

func (e exponents) Exponents(_ context.Context, denoms []string) (map[string]int, error) {
	out := map[string]int{}
	for _, d := range denoms {
		if v, ok := e[d]; ok {
			out[d] = v
		}
	}
	return out, nil
}

var testExponents = exponents{"ubze": 6, "uusdc": 6, "unew": 6, "uempty": 6, "uwei": 18}

func ordersServer(t *testing.T, node *bookNode, clock *compatClock, merges marketmerge.Merges) *echo.Echo {
	t.Helper()
	lg, _ := logtest.NewNullLogger()
	books := orderbook.New(orderbook.Config{}, orderbook.Deps{Node: node, Exponents: testExponents, Log: lg, Clock: clock})
	return server.New(server.Deps{CompatOrders: books, CoinGeckoMerges: merges, Log: lg})
}

var mainnetLikeMerges = marketmerge.Merges{newMarket: usdcMarket, emptyMarket: usdcMarket}

// getOrders serves path and returns the status and the body without its
// timestamp, which is checked to be now.
func getOrders(t *testing.T, e *echo.Echo, path string) (int, map[string]any) {
	t.Helper()
	before := time.Now().Unix()
	rec := serve(t, e, http.MethodGet, path)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	if ts, ok := body["timestamp"]; ok {
		sec, err := strconv.ParseInt(ts.(string), 10, 64)
		require.NoError(t, err, "a unix time in seconds, as a string")
		assert.True(t, sec >= before && sec <= time.Now().Unix())
		delete(body, "timestamp")
	}
	return rec.Code, body
}

func pairs(kv ...string) []any {
	out := []any{}
	for i := 0; i < len(kv); i += 2 {
		out = append(out, map[string]any{"price": kv[i], "volume": kv[i+1]})
	}
	return out
}

func TestCompatOrdersWholeBookByAscendingPrice(t *testing.T) {
	node := newBookNode()
	e := ordersServer(t, node, &compatClock{now: time.Unix(1000, 0)}, nil)

	code, body := getOrders(t, e, "/api/dex/orders?market_id="+usdcMarket)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, map[string]any{
		"market_id": usdcMarket,
		// read from the highest down, then reversed
		"bids": pairs("0.00033", "1", "0.000331", "2.5", "0.000332", "473.33462", "0.000333", "20.818779"),
		"asks": pairs("0.000334", "647.064654", "0.000335", "511.637133", "0.0006", "4919.636072"),
	}, body)
	assert.True(t, node.reverse["buy"], "bids are read from the highest price")
	assert.False(t, node.reverse["sell"])
	assert.Equal(t, 2, node.calls(usdcMarket, "buy"), "every page is read")
}

func TestCompatOrdersDepth(t *testing.T) {
	e := ordersServer(t, newBookNode(), &compatClock{now: time.Unix(1000, 0)}, nil)
	nearest := map[string]any{
		"market_id": usdcMarket,
		"bids":      pairs("0.000332", "473.33462", "0.000333", "20.818779"),
		"asks":      pairs("0.000334", "647.064654", "0.000335", "511.637133"),
	}

	for _, depth := range []string{"4", "5"} {
		code, body := getOrders(t, e, "/api/dex/orders?market_id="+usdcMarket+"&depth="+depth)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, nearest, body, "depth/2 levels per side, nearest the spread (depth %s)", depth)
	}

	// depth 1 is 0 per side: the whole book, as depth 0.
	_, whole := getOrders(t, e, "/api/dex/orders?market_id="+usdcMarket)
	for _, depth := range []string{"1", "0", ""} {
		_, body := getOrders(t, e, "/api/dex/orders?market_id="+usdcMarket+"&depth="+depth)
		assert.Equal(t, whole, body, "depth %q", depth)
	}
	assert.Len(t, whole["bids"], 4)

	// A depth beyond the book is the whole book.
	_, body := getOrders(t, e, "/api/dex/orders?market_id="+usdcMarket+"&depth=100")
	assert.Equal(t, whole, body)
}

func TestCompatOrdersTickerID(t *testing.T) {
	e := ordersServer(t, newBookNode(), &compatClock{now: time.Unix(1000, 0)}, nil)

	code, byTicker := getOrders(t, e, "/api/dex/orders?ticker_id=ubze_uusdc&depth=2")
	require.Equal(t, http.StatusOK, code)
	_, byID := getOrders(t, e, "/api/dex/orders?market_id="+usdcMarket+"&depth=2")
	assert.Equal(t, byID, byTicker)
	assert.Equal(t, usdcMarket, byTicker["market_id"])

	// "_" becomes "/" everywhere, so an LP ticker never resolves.
	rec := serve(t, e, http.MethodGet, "/api/dex/orders?ticker_id=lp_ubze_uusdc")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, unknownMsg, rec.Body.String())

	// market_id wins over ticker_id.
	rec = serve(t, e, http.MethodGet, "/api/dex/orders?market_id=nope/x&ticker_id=ubze_uusdc")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCompatOrdersRejects(t *testing.T) {
	node := newBookNode()
	e := ordersServer(t, node, &compatClock{now: time.Unix(1000, 0)}, nil)

	for path, want := range map[string]string{
		"/api/dex/orders":                                      `{"message":"please provide market_id or ticker_id"}`,
		"/api/dex/orders?market_id=u":                          `{"message":"please provide market_id or ticker_id"}`,
		"/api/dex/orders?market_id=ubze/uusdc&depth=-2":        `{"message":"depth must be a positive number"}`,
		"/api/dex/orders?market_id=ubze/uusdc&depth=abc":       `{"message":"invalid request"}`,
		"/api/dex/orders?market_id=ubze/uusdc&depth=1.5":       `{"message":"invalid request"}`,
		"/api/dex/orders?market_id=ubze/nope":                  unknownMsg[:len(unknownMsg)-1],
		"/api/dex/orders?market_id=unoexp/ubze":                unknownMsg[:len(unknownMsg)-1],
		"/api/dex/orders?ticker_id=ubze_nope&format=coingecko": unknownMsg[:len(unknownMsg)-1],
	} {
		rec := serve(t, e, http.MethodGet, path)
		assert.Equal(t, http.StatusBadRequest, rec.Code, path)
		assert.Equal(t, want+"\n", rec.Body.String(), path)
	}
	assert.Zero(t, node.calls("ubze/nope", "buy"), "an unknown market never reaches the book")

	// A node failure is the aggregator's unknown error too.
	node.fail = true
	rec := serve(t, e, http.MethodGet, "/api/dex/orders?market_id="+weiMarket)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, unknownMsg, rec.Body.String())
}

func TestCompatOrdersCoinGeckoFormat(t *testing.T) {
	e := ordersServer(t, newBookNode(), &compatClock{now: time.Unix(1000, 0)}, nil)

	code, body := getOrders(t, e, "/api/dex/orders?ticker_id=ubze_uusdc&depth=4&format=coingecko")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, map[string]any{
		"ticker_id": "ubze_uusdc",
		"bids":      []any{[]any{"0.000332", "473.33462"}, []any{"0.000333", "20.818779"}},
		"asks":      []any{[]any{"0.000334", "647.064654"}, []any{"0.000335", "511.637133"}},
	}, body)

	// Any other format is the default one.
	_, body = getOrders(t, e, "/api/dex/orders?market_id=ubze/uusdc&depth=2&format=tv")
	assert.Equal(t, usdcMarket, body["market_id"])
}

func TestCompatOrdersMergedMarkets(t *testing.T) {
	t.Run("the first folded market with orders", func(t *testing.T) {
		node := newBookNode()
		e := ordersServer(t, node, &compatClock{now: time.Unix(1000, 0)}, mainnetLikeMerges)
		_, body := getOrders(t, e, "/api/dex/orders?ticker_id=ubze_uusdc&format=coingecko")
		assert.Equal(t, map[string]any{
			"ticker_id": "ubze_uusdc",
			"bids":      []any{[]any{"0.0004", "1"}},
			"asks":      []any{[]any{"0.0005", "2"}},
		}, body, "the folded book, never mixed with the reported one")
		assert.Equal(t, 1, node.calls(emptyMarket, "buy"), "ubze/uempty sorts first and is tried first")

		// The default format keeps every market separate.
		_, body = getOrders(t, e, "/api/dex/orders?market_id="+usdcMarket+"&depth=2")
		assert.Equal(t, pairs("0.000333", "20.818779"), body["bids"])
	})

	t.Run("no folded market with orders", func(t *testing.T) {
		node := newBookNode()
		node.books[newMarket] = nil
		e := ordersServer(t, node, &compatClock{now: time.Unix(1000, 0)}, mainnetLikeMerges)
		_, body := getOrders(t, e, "/api/dex/orders?ticker_id=ubze_uusdc&depth=2&format=coingecko")
		assert.Equal(t, []any{[]any{"0.000333", "20.818779"}}, body["bids"], "the market's own book")
	})

	t.Run("a folded market the chain does not have", func(t *testing.T) {
		e := ordersServer(t, newBookNode(), &compatClock{now: time.Unix(1000, 0)}, marketmerge.Merges{"ubze/ugone": usdcMarket})
		code, body := getOrders(t, e, "/api/dex/orders?ticker_id=ubze_uusdc&depth=2&format=coingecko")
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, []any{[]any{"0.000333", "20.818779"}}, body["bids"])
	})

	t.Run("no folds", func(t *testing.T) {
		node := newBookNode()
		e := ordersServer(t, node, &compatClock{now: time.Unix(1000, 0)}, mainnetLikeMerges)
		_, body := getOrders(t, e, "/api/dex/orders?ticker_id=ubze_unew&format=coingecko")
		assert.Equal(t, "ubze_unew", body["ticker_id"], "a folded market still answers under its own ticker")
		assert.Equal(t, []any{[]any{"0.0004", "1"}}, body["bids"])
		assert.Zero(t, node.calls(usdcMarket, "buy"))
	})
}

func TestCompatOrdersAreCachedTenSecondsPerMarket(t *testing.T) {
	node := newBookNode()
	clock := &compatClock{now: time.Unix(1000, 0)}
	e := ordersServer(t, node, clock, nil)

	for range 3 {
		code, _ := getOrders(t, e, "/api/dex/orders?market_id="+usdcMarket)
		require.Equal(t, http.StatusOK, code)
		code, _ = getOrders(t, e, "/api/dex/orders?ticker_id=ubze_uusdc&depth=2&format=coingecko")
		require.Equal(t, http.StatusOK, code)
	}
	assert.Equal(t, 2, node.calls(usdcMarket, "buy"), "one read of the book's two pages for every request")
	assert.Equal(t, 2, node.calls(usdcMarket, "sell"), "and of the sell side")

	// Another market is read on its own.
	getOrders(t, e, "/api/dex/orders?market_id="+weiMarket)
	assert.Equal(t, 1, node.calls(weiMarket, "sell"))

	clock.now = clock.now.Add(9 * time.Second)
	getOrders(t, e, "/api/dex/orders?market_id="+usdcMarket)
	assert.Equal(t, 2, node.calls(usdcMarket, "buy"))

	clock.now = clock.now.Add(time.Second)
	getOrders(t, e, "/api/dex/orders?market_id="+usdcMarket)
	assert.Equal(t, 4, node.calls(usdcMarket, "buy"), "read again after 10 s")

	// The market list is kept 60 s.
	assert.Equal(t, 1, node.marketCalls)
	clock.now = clock.now.Add(50 * time.Second)
	getOrders(t, e, "/api/dex/orders?market_id="+usdcMarket)
	assert.Equal(t, 2, node.marketCalls)
}

// Allowed difference: when a book empties on chain the sides are empty ([]),
// where the aggregator keeps its last rows forever.
func TestCompatOrdersAnEmptiedBookIsEmpty(t *testing.T) {
	node := newBookNode()
	clock := &compatClock{now: time.Unix(1000, 0)}
	e := ordersServer(t, node, clock, nil)
	_, body := getOrders(t, e, "/api/dex/orders?market_id="+usdcMarket)
	require.Len(t, body["asks"], 3)

	node.mu.Lock()
	node.books[usdcMarket] = nil
	node.mu.Unlock()
	clock.now = clock.now.Add(10 * time.Second)
	rec := serve(t, e, http.MethodGet, "/api/dex/orders?market_id="+usdcMarket)
	assert.Contains(t, rec.Body.String(), `"bids":[],"asks":[]`)
	rec = serve(t, e, http.MethodGet, "/api/dex/orders?market_id="+usdcMarket+"&format=coingecko")
	assert.Contains(t, rec.Body.String(), `"bids":[],"asks":[]`)
}

// Allowed difference (BZE-133): a quote with three or more decimals more
// than the base keeps its price, where the aggregator's answers 0.
func TestCompatOrdersBigExponentGap(t *testing.T) {
	e := ordersServer(t, newBookNode(), &compatClock{now: time.Unix(1000, 0)}, nil)
	_, body := getOrders(t, e, "/api/dex/orders?market_id="+weiMarket)
	assert.Equal(t, map[string]any{
		"market_id": weiMarket,
		"bids":      []any{},
		"asks":      pairs("0.00123456789", "3"),
	}, body)
}

func TestCompatOrdersAnswerAnyOrigin(t *testing.T) {
	e := ordersServer(t, newBookNode(), &compatClock{now: time.Unix(1000, 0)}, nil)
	rec := corsRequest(e, http.MethodGet, "/api/dex/orders?market_id="+usdcMarket, "https://dex.getbze.com")
	assert.Equal(t, "*", rec.Header().Get(echo.HeaderAccessControlAllowOrigin))
}
