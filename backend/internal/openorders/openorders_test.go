package openorders_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	"github.com/cosmos/cosmos-sdk/types/query"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/openorders"
)

const owner = "bze1owner"

// book is the chain's state both fake nodes answer from.
type book struct {
	mu       sync.Mutex
	refs     []tradebintypes.OrderReference // the owner's, in order
	orders   map[string]tradebintypes.Order // by id; a ref without one is NotFound
	markets  []tradebintypes.Market
	pageSize int // UserMarketOrders page size; 0 = all in one page

	userCalls, orderCalls, marketCalls atomic.Int32
	inFlight, maxInFlight              atomic.Int32
	gate                               chan struct{} // when set, MarketOrder waits on it
}

func newBook() *book {
	b := &book{orders: map[string]tradebintypes.Order{}, markets: []tradebintypes.Market{
		{Base: vdl.Base, Quote: vdl.Quote}, {Base: usdc.Base, Quote: usdc.Quote},
	}}
	b.add("1", usdc, "sell", "100", "2")
	b.add("2", usdc, "buy", "3", "0.5")
	b.add("3", vdl, "buy", "1000", "0.2")
	return b
}

func (b *book) add(id string, m openorders.Market, side, amount, price string) {
	b.refs = append(b.refs, tradebintypes.OrderReference{Id: id, MarketId: m.ID, OrderType: side})
	b.orders[id] = tradebintypes.Order{Id: id, MarketId: m.ID, OrderType: side, Amount: amount, Price: price, Owner: owner}
}

func (b *book) userMarketOrders(in *tradebintypes.QueryUserMarketOrdersRequest) (*tradebintypes.QueryUserMarketOrdersResponse, error) {
	b.userCalls.Add(1)
	if in.Market != "" || in.Pagination == nil || in.Pagination.Limit != openorders.PageLimit {
		return nil, fmt.Errorf("unexpected request %v", in)
	}
	if in.Address != owner {
		return &tradebintypes.QueryUserMarketOrdersResponse{Pagination: &query.PageResponse{}}, nil
	}
	from := 0
	if len(in.Pagination.Key) > 0 {
		from, _ = strconv.Atoi(string(in.Pagination.Key))
	}
	to := len(b.refs)
	if b.pageSize > 0 && from+b.pageSize < to {
		to = from + b.pageSize
	}
	res := &tradebintypes.QueryUserMarketOrdersResponse{List: b.refs[from:to], Pagination: &query.PageResponse{}}
	if to < len(b.refs) {
		res.Pagination.NextKey = []byte(strconv.Itoa(to))
	}
	return res, nil
}

func (b *book) marketOrder(in *tradebintypes.QueryMarketOrderRequest, notFound error) (*tradebintypes.QueryMarketOrderResponse, error) {
	b.orderCalls.Add(1)
	n := b.inFlight.Add(1)
	defer b.inFlight.Add(-1)
	for {
		m := b.maxInFlight.Load()
		if n <= m || b.maxInFlight.CompareAndSwap(m, n) {
			break
		}
	}
	if b.gate != nil {
		<-b.gate
	}
	b.mu.Lock()
	o, ok := b.orders[in.OrderId]
	b.mu.Unlock()
	if !ok || o.MarketId != in.Market || o.OrderType != in.OrderType {
		return nil, notFound
	}
	return &tradebintypes.QueryMarketOrderResponse{Order: o}, nil
}

func (b *book) allMarkets() *tradebintypes.QueryAllMarketsResponse {
	b.marketCalls.Add(1)
	b.mu.Lock()
	defer b.mu.Unlock()
	return &tradebintypes.QueryAllMarketsResponse{Market: append([]tradebintypes.Market(nil), b.markets...),
		Pagination: &query.PageResponse{}}
}

// rpc is a node's /status.
type rpc struct {
	height int64
	err    error
}

func (r *rpc) Status(context.Context) (*node.Status, []byte, error) {
	if r.err != nil {
		return nil, nil, r.err
	}
	return &node.Status{LatestBlockHeight: r.height}, nil, nil
}

// tradebin is the local node's gRPC over a book.
type tradebin struct {
	book *book
	err  error // every call fails with it

	mu      sync.Mutex
	heights map[string]bool // the x-cosmos-block-height of every call
}

func (t *tradebin) pinned(ctx context.Context) {
	md, _ := metadata.FromOutgoingContext(ctx)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.heights == nil {
		t.heights = map[string]bool{}
	}
	for _, h := range md.Get(grpctypes.GRPCBlockHeightHeader) {
		t.heights[h] = true
	}
	if len(md.Get(grpctypes.GRPCBlockHeightHeader)) == 0 {
		t.heights["unpinned"] = true
	}
}

func (t *tradebin) UserMarketOrders(ctx context.Context, in *tradebintypes.QueryUserMarketOrdersRequest,
	_ ...grpc.CallOption) (*tradebintypes.QueryUserMarketOrdersResponse, error) {
	t.pinned(ctx)
	if t.err != nil {
		return nil, t.err
	}
	return t.book.userMarketOrders(in)
}

func (t *tradebin) MarketOrder(ctx context.Context, in *tradebintypes.QueryMarketOrderRequest,
	_ ...grpc.CallOption) (*tradebintypes.QueryMarketOrderResponse, error) {
	t.pinned(ctx)
	if t.err != nil {
		return nil, t.err
	}
	return t.book.marketOrder(in, status.Error(codes.NotFound, "not found"))
}

func (t *tradebin) AllMarkets(ctx context.Context, _ *tradebintypes.QueryAllMarketsRequest,
	_ ...grpc.CallOption) (*tradebintypes.QueryAllMarketsResponse, error) {
	t.pinned(ctx)
	if t.err != nil {
		return nil, t.err
	}
	return t.book.allMarkets(), nil
}

// archive is the archive node's /abci_query over a book.
type archive struct {
	rpc
	book *book

	mu      sync.Mutex
	heights map[int64]bool
}

func (a *archive) Query(_ context.Context, path string, req, resp gogoproto.Message, height int64) error {
	a.mu.Lock()
	if a.heights == nil {
		a.heights = map[int64]bool{}
	}
	a.heights[height] = true
	a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	var out gogoproto.Message
	var err error
	switch path {
	case "/bze.tradebin.Query/UserMarketOrders":
		out, err = a.book.userMarketOrders(req.(*tradebintypes.QueryUserMarketOrdersRequest))
	case "/bze.tradebin.Query/MarketOrder":
		out, err = a.book.marketOrder(req.(*tradebintypes.QueryMarketOrderRequest), &node.ABCIError{
			Path: path, Height: height, Code: sdkerrors.ErrKeyNotFound.ABCICode(), Codespace: sdkerrors.ErrKeyNotFound.Codespace(),
		})
	case "/bze.tradebin.Query/AllMarkets":
		out = a.book.allMarkets()
	default:
		return fmt.Errorf("unexpected path %s", path)
	}
	if err != nil {
		return err
	}
	bz, err := gogoproto.Marshal(out)
	if err != nil {
		return err
	}
	return gogoproto.Unmarshal(bz, resp)
}

// index is the orders table.
type index struct {
	orders []openorders.Order
	height int64
	err    error
	calls  atomic.Int32
}

func (i *index) OpenOrders(_ context.Context, address string) ([]openorders.Order, int64, error) {
	i.calls.Add(1)
	if address != owner {
		return nil, i.height, i.err
	}
	return i.orders, i.height, i.err
}

type coverage bool

func (c coverage) Complete() bool { return bool(c) }

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type env struct {
	book    *book
	local   *tradebin
	rpc     *rpc
	archive *archive
	index   *index
	clock   *clock
}

func newEnv() *env {
	b := newBook()
	return &env{
		book:    b,
		local:   &tradebin{book: b},
		rpc:     &rpc{height: 500},
		archive: &archive{rpc: rpc{height: 499}, book: b},
		index: &index{height: 480, orders: []openorders.Order{
			order("1", usdc, "sell", "100", "2"), order("2", usdc, "buy", "3", "0.5"), order("3", vdl, "buy", "1000", "0.2"),
		}},
		clock: &clock{now: time.Unix(1_800_000_000, 0)},
	}
}

func (e *env) reader(complete bool) *openorders.Reader {
	return openorders.New(openorders.Config{Now: e.clock.Now}, openorders.Deps{
		Index:    e.index,
		Coverage: coverage(complete),
		Local:    openorders.NewGRPCSource(e.rpc, e.local),
		Archive:  openorders.NewABCISource(e.archive),
	})
}

// flat is a summary as one line per market of each asset.
func flat(s openorders.Summary) []string {
	out := []string{fmt.Sprintf("%s@%d orders=%d markets=%d", s.Source, s.Height, s.OrdersCount, s.MarketsCount)}
	for _, a := range s.Assets {
		out = append(out, fmt.Sprintf("%s %s (%d)", a.Denom, a.Amount, a.OrdersCount))
		for _, m := range a.Markets {
			out = append(out, fmt.Sprintf("  %s %s/%s %s (%d)", m.Market.ID, m.Market.Base, m.Market.Quote, m.Amount, m.OrdersCount))
		}
	}
	return out
}

var wantBook = []string{
	"ibc/USDC 1 (1)",
	"  " + usdc.ID + " ubze/ibc/USDC 1 (1)",
	"ubze 300 (2)",
	"  " + vdl.ID + " factory/bze1creator/uvdl/ubze 200 (1)",
	"  " + usdc.ID + " ubze/ibc/USDC 100 (1)",
}

func TestACompleteIndexAnswersFromTheTable(t *testing.T) {
	e := newEnv()
	s, err := e.reader(true).OpenOrders(context.Background(), owner)
	require.NoError(t, err)
	assert.Equal(t, append([]string{"index@480 orders=3 markets=2"}, wantBook...), flat(s))
	assert.Zero(t, e.book.userCalls.Load(), "the node's orders are not read")
	assert.Equal(t, int32(1), e.book.marketCalls.Load(), "the markets come from the node")
}

func TestAnIncompleteIndexAsksTheLocalNodeAtOneHeight(t *testing.T) {
	e := newEnv()
	s, err := e.reader(false).OpenOrders(context.Background(), owner)
	require.NoError(t, err)
	assert.Equal(t, append([]string{"node@500 orders=3 markets=2"}, wantBook...), flat(s))
	assert.Zero(t, e.index.calls.Load())
	assert.Equal(t, map[string]bool{"500": true}, e.local.heights, "every read is pinned to the status height")
	assert.Empty(t, e.archive.heights)
}

func TestBothPathsGiveTheSameAnswer(t *testing.T) {
	e := newEnv()
	fromIndex, err := e.reader(true).OpenOrders(context.Background(), owner)
	require.NoError(t, err)
	fromNode, err := e.reader(false).OpenOrders(context.Background(), owner)
	require.NoError(t, err)
	assert.Equal(t, flat(fromIndex)[1:], flat(fromNode)[1:])
}

func TestAFailingIndexFallsBackToTheNode(t *testing.T) {
	e := newEnv()
	e.index.err = errors.New("connection refused")
	s, err := e.reader(true).OpenOrders(context.Background(), owner)
	require.NoError(t, err)
	assert.Equal(t, openorders.SourceNode, s.Source)
}

func TestAnOrderGoneBetweenTheTwoReadsIsSkipped(t *testing.T) {
	for _, local := range []bool{true, false} {
		t.Run(map[bool]string{true: "local", false: "archive"}[local], func(t *testing.T) {
			e := newEnv()
			delete(e.book.orders, "3") // listed, then filled
			if !local {
				e.local.err = status.Error(codes.Unavailable, "connection refused")
			}
			s, err := e.reader(false).OpenOrders(context.Background(), owner)
			require.NoError(t, err)
			assert.Equal(t, 2, s.OrdersCount)
			assert.Equal(t, 1, s.MarketsCount)
		})
	}
}

func TestAnUnreachableLocalNodeFallsBackToTheArchive(t *testing.T) {
	for name, fail := range map[string]func(e *env){
		"gRPC unavailable": func(e *env) { e.local.err = status.Error(codes.Unavailable, "connection refused") },
		"RPC unavailable":  func(e *env) { e.rpc.err = fmt.Errorf("status: %w", node.ErrUnavailable) },
		"gRPC timeout":     func(e *env) { e.local.err = status.Error(codes.DeadlineExceeded, "deadline") },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv()
			fail(e)
			s, err := e.reader(false).OpenOrders(context.Background(), owner)
			require.NoError(t, err)
			assert.Equal(t, append([]string{"node@499 orders=3 markets=2"}, wantBook...), flat(s))
			assert.Equal(t, map[int64]bool{499: true}, e.archive.heights, "the archive's reads are at its status height")
		})
	}
}

func TestBothNodesUnreachableIsUpstream(t *testing.T) {
	e := newEnv()
	e.local.err = status.Error(codes.Unavailable, "connection refused")
	e.archive.err = fmt.Errorf("abci_query: %w", node.ErrUnavailable)
	_, err := e.reader(false).OpenOrders(context.Background(), owner)
	require.ErrorIs(t, err, openorders.ErrUpstream)
}

func TestALocalErrorIsUpstreamWithoutTheArchive(t *testing.T) {
	e := newEnv()
	e.local.err = status.Error(codes.Internal, "boom")
	_, err := e.reader(false).OpenOrders(context.Background(), owner)
	require.ErrorIs(t, err, openorders.ErrUpstream)
	assert.Empty(t, e.archive.heights, "the local node answered: no fallback")
}

func TestTheOrderListIsReadPageByPage(t *testing.T) {
	e := newEnv()
	e.book.pageSize = 2
	s, err := e.reader(false).OpenOrders(context.Background(), owner)
	require.NoError(t, err)
	assert.Equal(t, 3, s.OrdersCount)
	assert.Equal(t, int32(2), e.book.userCalls.Load(), "two pages")
}

func TestAtMostEightOrdersAreReadAtOnce(t *testing.T) {
	e := newEnv()
	for i := range 30 {
		e.book.add(strconv.Itoa(100+i), usdc, "sell", "1", "1")
	}
	e.book.gate = make(chan struct{})
	done := make(chan error)
	go func() {
		_, err := e.reader(false).OpenOrders(context.Background(), owner)
		done <- err
	}()
	require.Eventually(t, func() bool { return e.book.inFlight.Load() == openorders.DefaultInFlight },
		time.Second, time.Millisecond)
	close(e.book.gate)
	require.NoError(t, <-done)
	assert.Equal(t, int32(openorders.DefaultInFlight), e.book.maxInFlight.Load())
	assert.Equal(t, int32(33), e.book.orderCalls.Load())
}

func TestAnswersAreCachedPerAddress(t *testing.T) {
	e := newEnv()
	r := e.reader(false)
	ctx := context.Background()
	_, err := r.OpenOrders(ctx, owner)
	require.NoError(t, err)
	_, err = r.OpenOrders(ctx, owner)
	require.NoError(t, err)
	assert.Equal(t, int32(1), e.book.userCalls.Load(), "the second read is cached")

	_, err = r.OpenOrders(ctx, "bze1other")
	require.NoError(t, err)
	assert.Equal(t, int32(2), e.book.userCalls.Load(), "per address")

	e.clock.advance(openorders.DefaultTTL)
	_, err = r.OpenOrders(ctx, owner)
	require.NoError(t, err)
	assert.Equal(t, int32(3), e.book.userCalls.Load(), "expired after the TTL")
}

func TestErrorsAreNotCached(t *testing.T) {
	e := newEnv()
	r := e.reader(false)
	e.local.err = status.Error(codes.Internal, "boom")
	_, err := r.OpenOrders(context.Background(), owner)
	require.Error(t, err)
	e.local.err = nil
	_, err = r.OpenOrders(context.Background(), owner)
	require.NoError(t, err)
}

func TestConcurrentMissesShareOneRead(t *testing.T) {
	e := newEnv()
	r := e.reader(false)
	e.book.gate = make(chan struct{})
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			_, err := r.OpenOrders(context.Background(), owner)
			assert.NoError(t, err)
		})
	}
	require.Eventually(t, func() bool { return e.book.inFlight.Load() > 0 }, time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond) // let every caller join
	close(e.book.gate)
	wg.Wait()
	assert.Equal(t, int32(1), e.book.userCalls.Load())
}

func TestMarketsAreCachedAndRefreshedOnceForAnUnknownOne(t *testing.T) {
	e := newEnv()
	r := e.reader(false)
	ctx := context.Background()
	_, err := r.OpenOrders(ctx, owner)
	require.NoError(t, err)
	e.clock.advance(openorders.DefaultTTL)
	_, err = r.OpenOrders(ctx, owner)
	require.NoError(t, err)
	assert.Equal(t, int32(1), e.book.marketCalls.Load(), "the market list is cached for minutes")

	// A market created since: the order names it, the cached list does not.
	gge := openorders.Market{ID: "factory/bze1x/ugge/ubze", Base: "factory/bze1x/ugge", Quote: "ubze"}
	e.book.mu.Lock()
	e.book.markets = append(e.book.markets, tradebintypes.Market{Base: gge.Base, Quote: gge.Quote})
	e.book.mu.Unlock()
	e.book.add("9", gge, "sell", "5", "1")
	e.clock.advance(openorders.DefaultTTL)
	s, err := r.OpenOrders(ctx, owner)
	require.NoError(t, err)
	assert.Equal(t, 3, s.MarketsCount)
	assert.Equal(t, int32(2), e.book.marketCalls.Load(), "one refresh")

	// A market the fresh list does not have either fails the read.
	e.book.add("10", openorders.Market{ID: "nope/ubze"}, "sell", "5", "1")
	e.clock.advance(openorders.DefaultTTL)
	_, err = r.OpenOrders(ctx, owner)
	require.ErrorIs(t, err, openorders.ErrUpstream)
	assert.Equal(t, int32(3), e.book.marketCalls.Load())

}

func TestAnAccountWithoutOrdersIsEmpty(t *testing.T) {
	e := newEnv()
	for _, complete := range []bool{true, false} {
		s, err := e.reader(complete).OpenOrders(context.Background(), "bze1nobody")
		require.NoError(t, err)
		assert.Zero(t, s.OrdersCount)
		assert.Empty(t, s.Assets)
	}
}
