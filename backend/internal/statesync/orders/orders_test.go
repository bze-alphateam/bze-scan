package orders_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/orders"
)

type fakeNode struct {
	height int64
	err    error
}

var nodeTime = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

func (n fakeNode) Status(context.Context) (*node.Status, []byte, error) {
	if n.err != nil {
		return nil, nil, n.err
	}
	return &node.Status{LatestBlockHeight: n.height, LatestBlockTime: nodeTime}, nil, nil
}

// fakeTradebin lists each owner's order ids two per page; a page key is
// "<owner>#<i>".
type fakeTradebin struct {
	book    map[string][]string
	failFor string
	heights []string
	calls   []string
}

func (f *fakeTradebin) UserMarketOrders(ctx context.Context, in *tradebintypes.QueryUserMarketOrdersRequest,
	_ ...grpc.CallOption) (*tradebintypes.QueryUserMarketOrdersResponse, error) {
	md, _ := metadata.FromOutgoingContext(ctx)
	f.heights = append(f.heights, md.Get(grpctypes.GRPCBlockHeightHeader)...)
	key := string(in.Pagination.Key)
	f.calls = append(f.calls, in.Market+in.Address+"@"+key)
	if in.Address == f.failFor {
		return nil, errors.New("node down")
	}
	start := 0
	if key != "" {
		_, _ = fmt.Sscanf(key[len(in.Address)+1:], "%d", &start)
	}
	ids := f.book[in.Address]
	end := min(start+2, len(ids))
	res := &tradebintypes.QueryUserMarketOrdersResponse{Pagination: &query.PageResponse{}}
	for _, id := range ids[start:end] {
		res.List = append(res.List, tradebintypes.OrderReference{Id: id, MarketId: "m", OrderType: "buy"})
	}
	if end < len(ids) {
		res.Pagination.NextKey = fmt.Appendf(nil, "%s#%d", in.Address, end)
	}
	return res, nil
}

type fakeStore struct {
	cursor   *orders.Position
	open     []orders.OpenOrder
	openAt   int64
	closed   []string
	closedAt orders.Position
	closeErr error
}

func (s *fakeStore) Cursor(context.Context) (orders.Position, bool, error) {
	if s.cursor == nil {
		return orders.Position{}, false, nil
	}
	return *s.cursor, true, nil
}

func (s *fakeStore) Open(_ context.Context, height int64) ([]orders.OpenOrder, error) {
	s.openAt = height
	return s.open, nil
}

func (s *fakeStore) Close(_ context.Context, ids []string, pos orders.Position) (int64, error) {
	if s.closeErr != nil {
		return 0, s.closeErr
	}
	s.closed, s.closedAt = ids, pos
	return int64(len(ids)), nil
}

func open(owner string, ids ...string) []orders.OpenOrder {
	var out []orders.OpenOrder
	for _, id := range ids {
		out = append(out, orders.OpenOrder{OrderID: id, Owner: owner})
	}
	return out
}

func newSet(n fakeNode, tb *fakeTradebin, st *fakeStore) (*orders.Set, *logtest.Hook) {
	logger, hook := logtest.NewNullLogger()
	return orders.New(orders.Deps{Node: n, Tradebin: tb, Store: st, Log: logger}, 0), hook
}

func TestTheSetIsHourlyAndNamedOrders(t *testing.T) {
	s, _ := newSet(fakeNode{}, &fakeTradebin{}, &fakeStore{})
	assert.Equal(t, statesync.Orders, s.Name())
	assert.Equal(t, time.Hour, s.Interval())
}

// Only the open rows the node no longer lists are closed, at the live
// cursor, every page of every owner read at that height.
func TestClosesOnlyTheRowsTheNodeNoLongerLists(t *testing.T) {
	cur := orders.Position{Height: 90, Time: time.Date(2026, 10, 11, 11, 59, 0, 0, time.UTC)}
	st := &fakeStore{cursor: &cur, open: append(open("bze1alice", "1", "2", "3", "4", "5"), open("bze1bob", "6")...)}
	tb := &fakeTradebin{book: map[string][]string{"bze1alice": {"1", "3", "4", "5", "77"}, "bze1bob": {}}}
	s, hook := newSet(fakeNode{height: 100}, tb, st)

	require.NoError(t, s.FullResync(context.Background()))
	assert.Equal(t, []string{"2", "6"}, st.closed)
	assert.Equal(t, cur, st.closedAt, "the indexer's cursor, not the node's tip")
	assert.Equal(t, int64(90), st.openAt)
	assert.Equal(t, []string{"bze1alice@", "bze1alice@bze1alice#2", "bze1alice@bze1alice#4", "bze1bob@"}, tb.calls,
		"every market of the owner at once")
	for _, h := range tb.heights {
		assert.Equal(t, "90", h)
	}
	require.Len(t, hook.AllEntries(), 1)
	assert.Equal(t, logrus.InfoLevel, hook.LastEntry().Level)
	assert.Equal(t, int64(2), hook.LastEntry().Data["closed"])
}

func TestWithoutALiveCursorTheNodeHeight(t *testing.T) {
	st := &fakeStore{open: open("bze1alice", "1")}
	s, _ := newSet(fakeNode{height: 100}, &fakeTradebin{}, st)
	require.NoError(t, s.FullResync(context.Background()))
	assert.Equal(t, orders.Position{Height: 100, Time: nodeTime}, st.closedAt)
	assert.Equal(t, []string{"1"}, st.closed)
}

func TestNothingGoneClosesNothing(t *testing.T) {
	st := &fakeStore{open: open("bze1alice", "1")}
	s, hook := newSet(fakeNode{height: 100}, &fakeTradebin{book: map[string][]string{"bze1alice": {"1"}}}, st)
	require.NoError(t, s.FullResync(context.Background()))
	assert.Nil(t, st.closed)
	assert.Empty(t, hook.AllEntries())
}

// An owner whose read fails keeps every row; the others are reconciled.
func TestAFailingOwnerKeepsItsRows(t *testing.T) {
	st := &fakeStore{open: append(open("bze1alice", "1"), open("bze1bob", "2")...)}
	tb := &fakeTradebin{failFor: "bze1alice"}
	s, _ := newSet(fakeNode{height: 100}, tb, st)
	err := s.FullResync(context.Background())
	require.ErrorContains(t, err, "orders of bze1alice: node down")
	assert.Equal(t, []string{"2"}, st.closed)
}

func TestFailuresBeforeAnyRead(t *testing.T) {
	s, _ := newSet(fakeNode{err: errors.New("rpc down")}, &fakeTradebin{}, &fakeStore{})
	require.ErrorContains(t, s.FullResync(context.Background()), "node status: rpc down")

	st := &fakeStore{open: open("bze1alice", "1"), closeErr: errors.New("db down")}
	s, _ = newSet(fakeNode{height: 5}, &fakeTradebin{}, st)
	require.ErrorContains(t, s.ResyncOne(context.Background(), statesync.All), "db down")
}
