//go:build e2e

package e2e_test

import (
	"context"
	"crypto/rand"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/ordercoverage"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// ordersH is the height the market maker's order book is recorded at, both
// as gRPC answers and as the archive's abci_query answers.
const ordersH = int64(25_159_888)

// serveOpenOrders runs the real serve wiring (indexer off) on chainID, the
// local node being the env's fake node and fake gRPC server, the archive at
// archiveURL, and the coverage checker on starts. It returns the API's base
// URL.
func (e *syncEnv) serveOpenOrders(t *testing.T, chainID, archiveURL string, starts map[string]int64) string {
	t.Helper()
	cfg := &config.Config{
		HTTPAddr: "127.0.0.1:0", LogLevel: "warn", LogFormat: config.LogFormatText,
		DatabaseURL: e.url, NodeRPCURL: e.node.URL, ChainID: chainID, NodeGRPCAddr: e.grpc.Addr,
		ArchiveRPCURL: archiveURL,
	}
	ctx, cancel := context.WithCancel(context.Background())
	listening := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- serve.Run(ctx, cfg, serve.Options{
			OnListen:      func(a net.Addr) { listening <- a },
			OrderCoverage: ordercoverage.Config{Starts: starts},
		})
	}()
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-done)
	})
	select {
	case a := <-listening:
		return "http://" + a.String() + "/api/v1"
	case err := <-done:
		t.Fatalf("serve exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not listen")
	}
	return ""
}

// waitForIndex polls the route with fresh addresses (each one a cache miss)
// until the first coverage check has found the index complete.
func waitForIndex(t *testing.T, base string) {
	t.Helper()
	require.Eventually(t, func() bool {
		raw := make([]byte, 20)
		_, _ = rand.Read(raw)
		addr, err := bech32.ConvertAndEncode("bze", raw)
		require.NoError(t, err)
		r := fetch(t, base+"/accounts/"+addr+"/open-orders")
		return r.status == http.StatusOK && into[dto.OpenOrders](t, r).Source == "index"
	}, 10*time.Second, 20*time.Millisecond)
}

func openOrdersOf(t *testing.T, base, address string) dto.OpenOrders {
	t.Helper()
	r := fetch(t, base+"/accounts/"+address+"/open-orders")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
	return into[dto.OpenOrders](t, r)
}

// withHoneyMarket adds the hand-made DEX heights' market to the recorded
// market list.
func withHoneyMarket(t *testing.T, g *fakenode.GRPC) {
	t.Helper()
	var markets tradebintypes.QueryAllMarketsResponse
	fakenode.LoadGRPCFixture(t, "tradebin", "AllMarkets", "", &markets)
	markets.Market = append(markets.Market, tradebintypes.Market{Base: "factory/x/uhoney", Quote: "ubze", Creator: dexOwner})
	codec, err := chain.NewCodec()
	require.NoError(t, err)
	body, err := codec.ProtoJSON(&markets)
	require.NoError(t, err)
	g.SetResponse("tradebin", "AllMarkets", "", body)
}

func noOrdersOnTheNode(g *fakenode.GRPC, owners ...string) {
	for _, o := range owners {
		g.SetResponse("tradebin", "UserMarketOrders", o, []byte(`{"list":[],"pagination":{"next_key":null,"total":"0"}}`))
	}
}

// The hand-made DEX heights hold, per owner, an open, a partly filled, a
// filled and a cancelled order: with the history complete from the DEX's
// start, the route answers what is still on the book from the table. A gap,
// an open failure in range or a chain without a DEX start height send it to
// the node.
func TestOpenOrdersFromTheIndex(t *testing.T) {
	e := newSyncEnv(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	live := writer.NewLiveWriter(pool, writer.AttributeOrders)
	for _, ents := range dexHeightsByHand() {
		require.NoError(t, live.WriteBlock(ctx, ents))
	}
	withHoneyMarket(t, e.grpc)
	noOrdersOnTheNode(e.grpc, dexMaker, dexOwner)
	starts := map[string]int64{"beezee-1": dexH - 1}

	base := e.serveOpenOrders(t, "beezee-1", e.node.URL, starts)
	waitForIndex(t, base)
	honey := func(amount string) []dto.OpenOrdersAsset {
		return []dto.OpenOrdersAsset{{Denom: "factory/x/uhoney", Amount: amount, OrdersCount: 1,
			Markets: []dto.OpenOrdersMarket{{MarketID: dexMarket, Base: "factory/x/uhoney", Quote: "ubze",
				Amount: amount, OrdersCount: 1}}}}
	}
	// Order 10 filled; order 11 rested 50 and was filled 20 + 5 + 5 + 10.
	assert.Equal(t, dto.OpenOrders{Source: "index", Height: dexH + 1, OrdersCount: 1, MarketsCount: 1, Assets: honey("10")},
		openOrdersOf(t, base, dexMaker))
	// Order 12 cancelled; order 20 rested 70.
	assert.Equal(t, dto.OpenOrders{Source: "index", Height: dexH + 1, OrdersCount: 1, MarketsCount: 1, Assets: honey("70")},
		openOrdersOf(t, base, dexOwner))
	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/accounts/bzevaloper1x/open-orders").status)

	// Each process below starts with an empty cache and one first check.
	nodeAnswers := func(chainID string) {
		t.Helper()
		base := e.serveOpenOrders(t, chainID, e.node.URL, starts)
		time.Sleep(200 * time.Millisecond) // the first check has run
		got := openOrdersOf(t, base, dexMaker)
		assert.Equal(t, "node", got.Source, chainID)
		assert.Empty(t, got.Assets)
	}
	nodeAnswers("beezee-testnet") // no DEX start height

	_, err = e.db.Exec(`INSERT INTO explorer.index_failures (height, source, attempts, error) VALUES ($1, 'live', 3, 'x')`, dexH)
	require.NoError(t, err)
	nodeAnswers("beezee-1") // an open failure in range
	_, err = e.db.Exec(`UPDATE explorer.index_failures SET resolved_at = now()`)
	require.NoError(t, err)
	waitForIndex(t, e.serveOpenOrders(t, "beezee-1", e.node.URL, starts))

	_, err = e.db.Exec(`DELETE FROM explorer.blocks WHERE height = $1`, dexH)
	require.NoError(t, err)
	nodeAnswers("beezee-1") // a gap
}

// marketMakerBook is the recorded book of the market maker at ordersH: 29
// orders in the USDC.inj/BZE market, 9 sells locking the base, 20 buys the
// quote.
func marketMakerBook() dto.OpenOrders {
	const market = "ibc/ED07A3391A112B175915CD8FAF43A2DA8E4790EDE12566649D0C2F97716B8518/ubze"
	const base = "ibc/ED07A3391A112B175915CD8FAF43A2DA8E4790EDE12566649D0C2F97716B8518"
	return dto.OpenOrders{Source: "node", Height: ordersH, OrdersCount: 29, MarketsCount: 1, Assets: []dto.OpenOrdersAsset{
		{Denom: base, Amount: "5310088", OrdersCount: 9, Markets: []dto.OpenOrdersMarket{
			{MarketID: market, Base: base, Quote: "ubze", Amount: "5310088", OrdersCount: 9}}},
		{Denom: "ubze", Amount: "2191943447", OrdersCount: 20, Markets: []dto.OpenOrdersMarket{
			{MarketID: market, Base: base, Quote: "ubze", Amount: "2191943447", OrdersCount: 20}}},
	}}
}

// Without the history, the route reads the local node's gRPC; with the
// local node down, the archive's abci_query at its latest height; with both
// down it is a 502.
func TestOpenOrdersFromTheNode(t *testing.T) {
	e := newSyncEnv(t)
	e.node.SetStatusHeight(ordersH)

	base := e.serveOpenOrders(t, "beezee-1", "http://127.0.0.1:1", nil)
	assert.Equal(t, marketMakerBook(), openOrdersOf(t, base, marketMaker))
	assert.Equal(t, 1, e.grpc.RequestsFor("tradebin", "UserMarketOrders", marketMaker))
	assert.Equal(t, 29, e.grpc.Requests("tradebin", "MarketOrder"))
	openOrdersOf(t, base, marketMaker)
	assert.Equal(t, 1, e.grpc.RequestsFor("tradebin", "UserMarketOrders", marketMaker), "cached")

	e.grpc.Stop()
	archive := e.serveOpenOrders(t, "beezee-1", e.node.URL, nil)
	assert.Equal(t, marketMakerBook(), openOrdersOf(t, archive, marketMaker), "the archive answers the same")
	assert.Equal(t, 31, e.node.RequestsAt(fakenode.RouteABCIQuery, ordersH), "markets, the list and 29 orders")

	down := e.serveOpenOrders(t, "beezee-1", "http://127.0.0.1:1", nil)
	r := fetch(t, down+"/accounts/"+marketMaker+"/open-orders")
	assert.Equal(t, http.StatusBadGateway, r.status)
	assert.Contains(t, string(r.body), "upstream_error")
}

// The market maker's recorded book indexed as orders rows answers the same
// from the table as from the node, but for source and height.
func TestOpenOrdersBothPathsAgree(t *testing.T) {
	e := newSyncEnv(t)
	e.node.SetStatusHeight(ordersH)
	var refs tradebintypes.QueryUserMarketOrdersResponse
	fakenode.LoadGRPCFixture(t, "tradebin", "UserMarketOrders", marketMaker, &refs)
	at := dexBlock(ordersH)
	for _, ref := range refs.List {
		var o tradebintypes.QueryMarketOrderResponse
		fakenode.LoadGRPCFixture(t, "tradebin", "MarketOrder",
			url.PathEscape(ref.MarketId)+"."+ref.OrderType+"."+ref.Id, &o)
		at.Orders = append(at.Orders, transform.Order{OrderID: o.Order.Id, MarketID: o.Order.MarketId, Owner: o.Order.Owner,
			Side: o.Order.OrderType, Price: o.Order.Price, Amount: o.Order.Amount, Height: ordersH, Time: at.Blocks[0].Time})
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, writer.NewLiveWriter(pool).WriteBlock(ctx, at))

	fromNode := openOrdersOf(t, e.serveOpenOrders(t, "beezee-testnet", "", nil), marketMaker)
	indexed := e.serveOpenOrders(t, "beezee-1", "", map[string]int64{"beezee-1": ordersH})
	waitForIndex(t, indexed)
	fromIndex := openOrdersOf(t, indexed, marketMaker)
	assert.Equal(t, "index", fromIndex.Source)
	fromIndex.Source = fromNode.Source
	assert.Equal(t, fromNode, fromIndex)
	assert.Equal(t, marketMakerBook(), fromNode)
}
