package live_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/live"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// mockListener hands out one mockSubscription per successful Listen call.
// listenErrs are returned, in order, by the first Listen calls.
type mockListener struct {
	mu         sync.Mutex
	listenErrs []error
	subs       chan *mockSubscription
}

func newMockListener(listenErrs ...error) *mockListener {
	return &mockListener{listenErrs: listenErrs, subs: make(chan *mockSubscription, 16)}
}

func (l *mockListener) Listen(context.Context) (live.Subscription, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.listenErrs) > 0 {
		err := l.listenErrs[0]
		l.listenErrs = l.listenErrs[1:]
		return nil, err
	}
	s := &mockSubscription{notes: make(chan note, 16)}
	l.subs <- s
	return s, nil
}

// next returns the subscription of the next successful Listen.
func (l *mockListener) next(t *testing.T) *mockSubscription {
	t.Helper()
	select {
	case s := <-l.subs:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("the indexer did not listen")
		return nil
	}
}

type note struct {
	height int64
	err    error
}

type mockSubscription struct {
	notes chan note
}

func (s *mockSubscription) notify(h int64) { s.notes <- note{height: h} }

// drop makes the connection fail, as a terminated backend does.
func (s *mockSubscription) drop() { s.notes <- note{err: errors.New("connection reset")} }

func (s *mockSubscription) Wait(ctx context.Context) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case n := <-s.notes:
		return n.height, n.err
	}
}

func (s *mockSubscription) Close() {}

// mockNode answers every height with a minimal block. failBlock queues
// errors for Block at a height; statusErr fails /status.
type mockNode struct {
	mu        sync.Mutex
	tip       int64
	statusErr error
	blockErrs map[int64][]error
	badBlocks map[int64]bool
	calls     map[int64]int
}

func newMockNode(tip int64) *mockNode {
	return &mockNode{tip: tip, blockErrs: map[int64][]error{}, badBlocks: map[int64]bool{}, calls: map[int64]int{}}
}

func (n *mockNode) setTip(h int64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.tip = h
}

func (n *mockNode) failBlock(h int64, errs ...error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.blockErrs[h] = append(n.blockErrs[h], errs...)
}

func (n *mockNode) callsAt(h int64) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls[h]
}

func (n *mockNode) Status(context.Context) (*node.Status, []byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.statusErr != nil {
		return nil, nil, n.statusErr
	}
	return &node.Status{Network: "beezee-1", LatestBlockHeight: n.tip}, nil, nil
}

func (n *mockNode) Block(_ context.Context, h int64) (*node.Block, []byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls[h]++
	if errs := n.blockErrs[h]; len(errs) > 0 {
		n.blockErrs[h] = errs[1:]
		return nil, nil, errs[0]
	}
	hash := fmt.Sprintf("%064X", h)
	if n.badBlocks[h] {
		hash = "bad"
	}
	return &node.Block{Height: h, Time: time.Unix(h, 0), Hash: hash}, rawBody("block", h), nil
}

func (n *mockNode) BlockResults(_ context.Context, h int64) (*node.BlockResults, []byte, error) {
	return &node.BlockResults{Height: h}, rawBody("block_results", h), nil
}

func (n *mockNode) Commit(_ context.Context, h int64) (*node.Commit, []byte, error) {
	return &node.Commit{Height: h}, rawBody("commit", h), nil
}

// rawBody is the response body mockNode answers for route at h.
func rawBody(route string, h int64) []byte {
	return []byte(fmt.Sprintf("%s %d", route, h))
}

// mockRaw records the bodies the indexer puts, by height.
type mockRaw struct {
	mu   sync.Mutex
	puts map[int64][3]string
}

func (r *mockRaw) PutHeight(h int64, block, blockResults, commit []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.puts[h] = [3]string{string(block), string(blockResults), string(commit)}
}

func (r *mockRaw) put(h int64) ([3]string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.puts[h]
	return p, ok
}

// mockTransformer turns a height into a bare blocks row; a block whose hash
// is "bad" fails.
type mockTransformer struct{}

func (mockTransformer) Transform(in transform.Input) (*transform.Entities, error) {
	if in.Block.Hash == "bad" {
		return nil, errors.New("cannot decode")
	}
	return &transform.Entities{Blocks: []transform.Block{{Height: in.Block.Height}}}, nil
}

// mockStore keeps the indexer state in memory, with the writer's rules: the
// floor is the first height written, the cursor only moves up.
type mockStore struct {
	mu       sync.Mutex
	cursor   int64
	floor    int64
	written  []int64
	failures map[int64]failure
}

type failure struct {
	attempts int
	err      error
}

func (s *mockStore) Cursor(context.Context) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursor, s.cursor > 0, nil
}

func (s *mockStore) LiveFloor(context.Context) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.floor, s.floor > 0, nil
}

func (s *mockStore) WriteBlock(_ context.Context, ents *transform.Entities) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range ents.Blocks {
		s.written = append(s.written, b.Height)
		if s.floor == 0 {
			s.floor = b.Height
		}
		s.cursor = max(s.cursor, b.Height)
	}
	return nil
}

func (s *mockStore) RecordFailure(_ context.Context, h int64, attempts int, cause error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[h] = failure{attempts, cause}
	s.cursor = max(s.cursor, h)
	return nil
}

func (s *mockStore) writtenHeights() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.written)
}

func (s *mockStore) liveFloor() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.floor
}

func (s *mockStore) failure(h int64) (failure, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.failures[h]
	return f, ok
}

// fakeClock records the waits instead of sleeping. A blocking clock waits
// until the indexer is stopped instead of returning at once.
type fakeClock struct {
	mu       sync.Mutex
	waits    []time.Duration
	blocking bool
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.waits = append(c.waits, d)
	blocking := c.blocking
	c.mu.Unlock()
	if blocking {
		<-ctx.Done()
	}
	return ctx.Err()
}

func (c *fakeClock) recorded() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.waits)
}

type harness struct {
	listener *mockListener
	node     *mockNode
	store    *mockStore
	raw      *mockRaw
	clock    *fakeClock
	cancel   context.CancelFunc
	done     chan error
	stopOnce sync.Once
}

// run starts the indexer with the store at cursor and floor (0: not set) and
// stops it when the test ends.
func run(t *testing.T, l *mockListener, n *mockNode, clock *fakeClock, cursor, floor int64) *harness {
	t.Helper()
	h := &harness{
		listener: l, node: n, clock: clock, done: make(chan error, 1),
		store: &mockStore{cursor: cursor, floor: floor, failures: map[int64]failure{}},
		raw:   &mockRaw{puts: map[int64][3]string{}},
	}
	ix := live.New(live.Config{Sleep: clock.Sleep}, live.Deps{
		Listener: l, Node: n, Store: h.store, Transformer: mockTransformer{}, Raw: h.raw,
	})
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() { h.done <- ix.Run(ctx) }()
	t.Cleanup(func() { h.stop(t) })
	return h
}

func (h *harness) stop(t *testing.T) {
	t.Helper()
	h.stopOnce.Do(func() {
		h.cancel()
		select {
		case err := <-h.done:
			assert.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("the indexer did not stop")
		}
	})
}

func (h *harness) waitWritten(t *testing.T, want ...int64) {
	t.Helper()
	require.Eventually(t, func() bool { return slices.Equal(h.store.writtenHeights(), want) },
		5*time.Second, 5*time.Millisecond, "want %v written", want)
}

func TestFirstStartBeginsAtTheHead(t *testing.T) {
	h := run(t, newMockListener(), newMockNode(100), &fakeClock{}, 0, 0)

	h.waitWritten(t, 100)
	assert.Equal(t, int64(100), h.store.liveFloor())
}

func TestNotificationWithoutGap(t *testing.T) {
	h := run(t, newMockListener(), newMockNode(99), &fakeClock{}, 99, 50)
	sub := h.listener.next(t)

	sub.notify(100)
	h.waitWritten(t, 100)
}

func TestNotificationAfterAGapIndexesTheGapInOrder(t *testing.T) {
	h := run(t, newMockListener(), newMockNode(95), &fakeClock{}, 95, 50)
	sub := h.listener.next(t)

	sub.notify(100)
	h.waitWritten(t, 96, 97, 98, 99, 100)
}

func TestNotifiedHeightBehindTheNode(t *testing.T) {
	n := newMockNode(99)
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)
	sub := h.listener.next(t)

	n.setTip(103)
	sub.notify(100)
	h.waitWritten(t, 100, 101, 102, 103)
}

func TestStaleNotificationChangesNothing(t *testing.T) {
	n := newMockNode(100)
	h := run(t, newMockListener(), n, &fakeClock{}, 100, 50)
	sub := h.listener.next(t)

	sub.notify(97)
	n.setTip(101)
	sub.notify(101) // handled after the stale one
	h.waitWritten(t, 101)
}

func TestNeverIndexesBelowTheFloor(t *testing.T) {
	h := run(t, newMockListener(), newMockNode(52), &fakeClock{}, 10, 50)

	h.waitWritten(t, 50, 51, 52)
}

func TestReconnectCatchesUpWithoutANotification(t *testing.T) {
	n := newMockNode(100)
	h := run(t, newMockListener(), n, &fakeClock{}, 100, 50)
	sub := h.listener.next(t)

	n.setTip(103)
	sub.drop()
	h.listener.next(t)
	h.waitWritten(t, 101, 102, 103)
	assert.Equal(t, []time.Duration{time.Second}, h.clock.recorded())
}

func TestReconnectBackoffDoublesUpToTheCapAndResets(t *testing.T) {
	refused := errors.New("connection refused")
	l := newMockListener()
	h := run(t, l, newMockNode(100), &fakeClock{}, 100, 50)
	sub := l.next(t)

	l.mu.Lock()
	l.listenErrs = []error{refused, refused, refused, refused, refused, refused}
	l.mu.Unlock()
	sub.drop()
	sub = l.next(t)
	sub.drop()
	l.next(t)

	assert.Equal(t, []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second,
		30 * time.Second, 30 * time.Second, // capped
		time.Second, // reset after a successful connect
	}, h.clock.recorded())
}

func TestRetriesAHeightThenSucceeds(t *testing.T) {
	n := newMockNode(99)
	n.failBlock(100, errors.New("timeout"), errors.New("timeout"))
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)
	sub := h.listener.next(t)

	sub.notify(100)
	h.waitWritten(t, 100)
	assert.Equal(t, 3, n.callsAt(100))
	assert.Equal(t, []time.Duration{500 * time.Millisecond, 2 * time.Second}, h.clock.recorded())
	_, failed := h.store.failure(100)
	assert.False(t, failed)
}

func TestGivesUpAfterThreeAttemptsAndMovesOn(t *testing.T) {
	n := newMockNode(99)
	aboveTip := fmt.Errorf("block: %w", node.ErrAboveTip)
	n.failBlock(100, aboveTip, aboveTip, aboveTip)
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)
	sub := h.listener.next(t)

	n.setTip(101)
	sub.notify(101)
	h.waitWritten(t, 101)
	f, ok := h.store.failure(100)
	require.True(t, ok)
	assert.Equal(t, 3, f.attempts)
	assert.ErrorIs(t, f.err, node.ErrAboveTip)
	assert.Equal(t, 3, n.callsAt(100))
	assert.Equal(t, []time.Duration{500 * time.Millisecond, 2 * time.Second}, h.clock.recorded())
}

func TestPrunedHeightIsRecordedWithoutRetry(t *testing.T) {
	n := newMockNode(99)
	n.failBlock(100, fmt.Errorf("block: %w", node.ErrPruned))
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)
	sub := h.listener.next(t)

	n.setTip(101)
	sub.notify(101)
	h.waitWritten(t, 101)
	f, ok := h.store.failure(100)
	require.True(t, ok)
	assert.Equal(t, 1, f.attempts)
	assert.ErrorIs(t, f.err, node.ErrPruned)
	assert.Equal(t, 1, n.callsAt(100))
	assert.Empty(t, h.clock.recorded())
}

func TestNodeDownRecordsNoFailure(t *testing.T) {
	n := newMockNode(100)
	n.statusErr = errors.New("connection refused")
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)

	h.listener.next(t)
	h.listener.next(t) // the pass failed and the indexer reconnected
	assert.Empty(t, h.store.writtenHeights())
	assert.Zero(t, n.callsAt(100))
	_, failed := h.store.failure(100)
	assert.False(t, failed)
}

func TestShutdownBetweenAttemptsRecordsNoFailure(t *testing.T) {
	n := newMockNode(99)
	n.failBlock(100, errors.New("timeout"), errors.New("timeout"), errors.New("timeout"))
	h := run(t, newMockListener(), n, &fakeClock{blocking: true}, 99, 50)
	sub := h.listener.next(t)

	sub.notify(100)
	require.Eventually(t, func() bool { return len(h.clock.recorded()) == 1 }, 5*time.Second, time.Millisecond,
		"the first attempt failed and the indexer waits")
	h.stop(t)
	_, failed := h.store.failure(100)
	assert.False(t, failed, "a height interrupted by shutdown is not a failure")
	assert.Equal(t, 1, n.callsAt(100))
}

func TestTransformErrorIsAFailedAttempt(t *testing.T) {
	n := newMockNode(99)
	n.badBlocks[100] = true
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)
	sub := h.listener.next(t)

	n.setTip(101)
	sub.notify(101)
	h.waitWritten(t, 101)
	f, ok := h.store.failure(100)
	require.True(t, ok)
	assert.Equal(t, 3, f.attempts)
	assert.EqualError(t, f.err, "cannot decode")
}

// A written height's three bodies reach the raw cache; a failed one's do not.
func TestWrittenHeightsFillTheRawCache(t *testing.T) {
	n := newMockNode(99)
	n.badBlocks[100] = true
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)
	sub := h.listener.next(t)

	n.setTip(101)
	sub.notify(101)
	h.waitWritten(t, 101)
	require.Eventually(t, func() bool { _, ok := h.raw.put(101); return ok }, 5*time.Second, time.Millisecond)
	got, _ := h.raw.put(101)
	assert.Equal(t, [3]string{"block 101", "block_results 101", "commit 101"}, got)
	_, ok := h.raw.put(100)
	assert.False(t, ok, "a failed height puts nothing")
}
