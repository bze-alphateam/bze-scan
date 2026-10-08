package rawcache_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/rawcache"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

var errDown = errors.New("connection refused")

// fakeNode answers body(route, height) or err; calls counts every request.
// While gate is open (non-nil), requests wait for it to close.
type fakeNode struct {
	mu     sync.Mutex
	calls  map[rawcache.Route]int
	err    error
	gate   chan struct{}
	bodies map[string][]byte
}

func newFakeNode() *fakeNode {
	return &fakeNode{calls: map[rawcache.Route]int{}, bodies: map[string][]byte{}}
}

func key(r rawcache.Route, h int64) string { return string(r) + "/" + strconv.FormatInt(h, 10) }

func (n *fakeNode) answer(r rawcache.Route, h int64) ([]byte, error) {
	n.mu.Lock()
	n.calls[r]++
	gate, err := n.gate, n.err
	n.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if err != nil {
		return nil, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if b, ok := n.bodies[key(r, h)]; ok {
		return b, nil
	}
	return []byte(`{"route":"` + string(r) + `","height":` + strconv.FormatInt(h, 10) + `}`), nil
}

func (n *fakeNode) Block(_ context.Context, h int64) (*node.Block, []byte, error) {
	b, err := n.answer(rawcache.RouteBlock, h)
	return &node.Block{Height: h}, b, err
}

func (n *fakeNode) BlockResults(_ context.Context, h int64) (*node.BlockResults, []byte, error) {
	b, err := n.answer(rawcache.RouteBlockResults, h)
	return &node.BlockResults{Height: h}, b, err
}

func (n *fakeNode) Commit(_ context.Context, h int64) (*node.Commit, []byte, error) {
	b, err := n.answer(rawcache.RouteCommit, h)
	return &node.Commit{Height: h}, b, err
}

func (n *fakeNode) count(r rawcache.Route) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls[r]
}

// clock is a settable fake clock.
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

func quiet() log.FieldLogger {
	l := log.New()
	l.SetOutput(io.Discard)
	return l
}

func newCache(archive, retry *fakeNode, cfg rawcache.Config) *rawcache.Cache {
	deps := rawcache.Deps{Archive: archive, Log: quiet()}
	if retry != nil {
		deps.ArchiveRetry = retry
	}
	return rawcache.New(cfg, deps)
}

func want(r rawcache.Route, h int64) string {
	return `{"route":"` + string(r) + `","height":` + strconv.FormatInt(h, 10) + `}`
}

func TestMissFetchesOnceThenHits(t *testing.T) {
	archive := newFakeNode()
	c := newCache(archive, nil, rawcache.Config{})
	ctx := context.Background()

	for _, r := range []rawcache.Route{rawcache.RouteBlock, rawcache.RouteBlockResults, rawcache.RouteCommit} {
		for range 3 {
			body, err := c.Get(ctx, r, 7)
			require.NoError(t, err)
			assert.JSONEq(t, want(r, 7), string(body))
		}
		assert.Equal(t, 1, archive.count(r), "route %s", r)
	}
}

func TestUnknownRoute(t *testing.T) {
	archive := newFakeNode()
	_, err := newCache(archive, nil, rawcache.Config{}).Get(context.Background(), "tx_search", 7)
	assert.ErrorIs(t, err, rawcache.ErrUnknownRoute)
}

func TestLRUEvictsTheLeastRecentlyUsedPerRoute(t *testing.T) {
	archive := newFakeNode()
	c := newCache(archive, nil, rawcache.Config{MaxEntries: 2})
	ctx := context.Background()
	get := func(r rawcache.Route, h int64) {
		t.Helper()
		_, err := c.Get(ctx, r, h)
		require.NoError(t, err)
	}

	get(rawcache.RouteBlock, 1)
	get(rawcache.RouteBlock, 2)
	get(rawcache.RouteBlock, 1) // 1 is now the most recent
	// Another route has its own bound: it evicts nothing of /block.
	get(rawcache.RouteCommit, 10)
	get(rawcache.RouteCommit, 11)
	get(rawcache.RouteCommit, 12)
	assert.Equal(t, 2, archive.count(rawcache.RouteBlock))

	get(rawcache.RouteBlock, 3) // evicts 2
	get(rawcache.RouteBlock, 1)
	assert.Equal(t, 3, archive.count(rawcache.RouteBlock), "1 stayed")
	get(rawcache.RouteBlock, 2)
	assert.Equal(t, 4, archive.count(rawcache.RouteBlock), "2 was evicted")
}

func TestEntriesExpireAfterTheTTL(t *testing.T) {
	archive := newFakeNode()
	clk := &clock{now: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	c := newCache(archive, nil, rawcache.Config{TTL: time.Minute, Now: clk.Now})
	ctx := context.Background()

	_, err := c.Get(ctx, rawcache.RouteBlock, 5)
	require.NoError(t, err)
	clk.advance(59 * time.Second)
	_, err = c.Get(ctx, rawcache.RouteBlock, 5)
	require.NoError(t, err)
	assert.Equal(t, 1, archive.count(rawcache.RouteBlock), "a hit does not extend the TTL but is still live")

	clk.advance(time.Second)
	_, err = c.Get(ctx, rawcache.RouteBlock, 5)
	require.NoError(t, err)
	assert.Equal(t, 2, archive.count(rawcache.RouteBlock), "expired at exactly the TTL")
}

func TestConcurrentMissesShareOneFetch(t *testing.T) {
	archive := newFakeNode()
	archive.gate = make(chan struct{})
	c := newCache(archive, nil, rawcache.Config{})

	const n = 20
	var wg sync.WaitGroup
	bodies := make([][]byte, n)
	errs := make([]error, n)
	for i := range n {
		wg.Go(func() { bodies[i], errs[i] = c.Get(context.Background(), rawcache.RouteBlockResults, 9) })
	}
	require.Eventually(t, func() bool { return archive.count(rawcache.RouteBlockResults) == 1 },
		time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond) // let the other callers join the flight
	close(archive.gate)
	wg.Wait()

	assert.Equal(t, 1, archive.count(rawcache.RouteBlockResults))
	for i := range n {
		require.NoError(t, errs[i])
		assert.JSONEq(t, want(rawcache.RouteBlockResults, 9), string(bodies[i]))
	}
}

// A request that gives up does not fail the others waiting for the height,
// and the fetch still fills the cache.
func TestACancelledRequestLeavesTheFetchRunning(t *testing.T) {
	archive := newFakeNode()
	archive.gate = make(chan struct{})
	c := newCache(archive, nil, rawcache.Config{})

	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := c.Get(ctx, rawcache.RouteCommit, 4); first <- err }()
	require.Eventually(t, func() bool { return archive.count(rawcache.RouteCommit) == 1 }, time.Second, time.Millisecond)
	second := make(chan []byte, 1)
	go func() { b, _ := c.Get(context.Background(), rawcache.RouteCommit, 4); second <- b }()

	cancel()
	assert.ErrorIs(t, <-first, context.Canceled)
	close(archive.gate)
	assert.JSONEq(t, want(rawcache.RouteCommit, 4), string(<-second))
	_, err := c.Get(context.Background(), rawcache.RouteCommit, 4)
	require.NoError(t, err)
	assert.Equal(t, 1, archive.count(rawcache.RouteCommit))
}

func TestErrorsAreNotCached(t *testing.T) {
	archive := newFakeNode()
	archive.err = errDown
	c := newCache(archive, nil, rawcache.Config{})
	ctx := context.Background()

	_, err := c.Get(ctx, rawcache.RouteBlock, 3)
	require.ErrorIs(t, err, rawcache.ErrUpstream)
	require.ErrorIs(t, err, errDown)
	assert.Equal(t, 2, archive.count(rawcache.RouteBlock), "without a retry node the archive is asked twice")

	archive.mu.Lock()
	archive.err = nil
	archive.mu.Unlock()
	body, err := c.Get(ctx, rawcache.RouteBlock, 3)
	require.NoError(t, err)
	assert.JSONEq(t, want(rawcache.RouteBlock, 3), string(body))
	assert.Equal(t, 3, archive.count(rawcache.RouteBlock))
}

func TestFallsBackToTheRetryNode(t *testing.T) {
	archive, retry := newFakeNode(), newFakeNode()
	archive.err = errDown
	c := newCache(archive, retry, rawcache.Config{})

	body, err := c.Get(context.Background(), rawcache.RouteCommit, 8)
	require.NoError(t, err)
	assert.JSONEq(t, want(rawcache.RouteCommit, 8), string(body))
	assert.Equal(t, 1, archive.count(rawcache.RouteCommit))
	assert.Equal(t, 1, retry.count(rawcache.RouteCommit))

	retry.err = errDown
	_, err = c.Get(context.Background(), rawcache.RouteCommit, 9)
	assert.ErrorIs(t, err, rawcache.ErrUpstream)
}

func TestPutHeightIsServedWithoutTheArchive(t *testing.T) {
	archive := newFakeNode()
	c := newCache(archive, nil, rawcache.Config{})
	ctx := context.Background()

	c.PutHeight(12, []byte(`"b"`), []byte(`"r"`), []byte(`"c"`))
	// A height already cached keeps its entries.
	c.PutHeight(12, []byte(`"B"`), []byte(`"R"`), []byte(`"C"`))
	for r, w := range map[rawcache.Route]string{rawcache.RouteBlock: `"b"`, rawcache.RouteBlockResults: `"r"`, rawcache.RouteCommit: `"c"`} {
		body, err := c.Get(ctx, r, 12)
		require.NoError(t, err)
		assert.Equal(t, w, string(body))
		assert.Zero(t, archive.count(r))
	}
}

const txHeight = 24998316 // three transactions, the third failed with code 11

func fixtureCache(t *testing.T) (*rawcache.Cache, *fakenode.Node) {
	t.Helper()
	fn := fakenode.New(t)
	archive := newFakeNode()
	for r, route := range map[rawcache.Route]string{rawcache.RouteBlock: fakenode.RouteBlock, rawcache.RouteBlockResults: fakenode.RouteBlockResults} {
		b, err := fn.Fixture(route, txHeight)
		require.NoError(t, err)
		archive.bodies[key(r, txHeight)] = b
	}
	return newCache(archive, nil, rawcache.Config{}), fn
}

func TestTxSlicesTheBlockByIndex(t *testing.T) {
	c, fn := fixtureCache(t)
	b, _, err := node.New(fn.URL).Block(context.Background(), txHeight)
	require.NoError(t, err)

	for i := range 3 {
		tx, err := c.Tx(context.Background(), txHeight, i)
		require.NoError(t, err)
		assert.Equal(t, b.Txs[i], tx.Tx, "index %d", i)
		var res struct {
			Code   uint32            `json:"code"`
			Events []json.RawMessage `json:"events"`
		}
		require.NoError(t, json.Unmarshal(tx.Result, &res))
		assert.NotEmpty(t, res.Events)
		if i == 2 {
			assert.Equal(t, uint32(11), res.Code)
		} else {
			assert.Zero(t, res.Code)
		}
	}
}

func TestTxIndexOutsideTheBlock(t *testing.T) {
	c, _ := fixtureCache(t)
	for _, i := range []int{-1, 3} {
		_, err := c.Tx(context.Background(), txHeight, i)
		assert.ErrorIs(t, err, rawcache.ErrTxNotInBlock, "index %d", i)
	}
}

func TestTxPropagatesTheUpstreamError(t *testing.T) {
	archive := newFakeNode()
	archive.err = errDown
	_, err := newCache(archive, nil, rawcache.Config{}).Tx(context.Background(), 5, 0)
	assert.ErrorIs(t, err, rawcache.ErrUpstream)
}
