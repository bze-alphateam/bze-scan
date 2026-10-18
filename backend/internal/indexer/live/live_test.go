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
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// mockListener hands out one mockSubscription per successful Listen call.
// listenErrs are returned, in order, by the first Listen calls.
type mockListener struct {
	mu         sync.Mutex
	listenErrs []error
	calls      int
	subs       chan *mockSubscription
}

func newMockListener(listenErrs ...error) *mockListener {
	return &mockListener{listenErrs: listenErrs, subs: make(chan *mockSubscription, 16)}
}

// Listen waits, like a real connect, until the test takes the subscription
// (16 may wait unread) or ctx is cancelled: a node that stays down makes the
// indexer reconnect in a loop the test never reads.
func (l *mockListener) Listen(ctx context.Context) (live.Subscription, error) {
	l.mu.Lock()
	l.calls++
	if len(l.listenErrs) > 0 {
		err := l.listenErrs[0]
		l.listenErrs = l.listenErrs[1:]
		l.mu.Unlock()
		return nil, err
	}
	l.mu.Unlock()
	s := &mockSubscription{notes: make(chan note, 16)}
	select {
	case l.subs <- s:
		return s, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *mockListener) callCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
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
// errors for Block at a height; statusErr fails /status; goDown makes the
// node go down at a height's Block call for the next /status calls.
type mockNode struct {
	mu        sync.Mutex
	tip       int64
	earliest  int64
	statusErr error
	downAt    map[int64]int
	statusOff int
	blockErrs map[int64][]error
	badBlocks map[int64]bool
	calls     map[int64]int
	// seen lists the commits the node answers next for a height, oldest
	// first; the last one repeats. A height without one is canonical with
	// three signers.
	seen        map[int64][]commitAnswer
	commitCalls map[int64]int
}

// commitAnswer is one /commit answer of mockNode.
type commitAnswer struct {
	canonical bool
	signers   int
	err       error
}

func newMockNode(tip int64) *mockNode {
	return &mockNode{
		tip: tip, blockErrs: map[int64][]error{}, badBlocks: map[int64]bool{}, calls: map[int64]int{}, downAt: map[int64]int{},
		seen: map[int64][]commitAnswer{}, commitCalls: map[int64]int{},
	}
}

// answerCommits queues the commits the node answers for h.
func (n *mockNode) answerCommits(h int64, answers ...commitAnswer) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.seen[h] = answers
}

func (n *mockNode) commitCallsAt(h int64) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.commitCalls[h]
}

// goDown makes the first Block call at h find the node unreachable, and the
// next statuses /status calls fail too.
func (n *mockNode) goDown(h int64, statuses int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.downAt[h] = statuses
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
	if n.statusOff > 0 {
		n.statusOff--
		return nil, nil, fmt.Errorf("status: %w: connection refused", node.ErrUnavailable)
	}
	return &node.Status{Network: "beezee-1", LatestBlockHeight: n.tip, EarliestBlockHeight: n.earliest}, nil, nil
}

func (n *mockNode) Block(_ context.Context, h int64) (*node.Block, []byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls[h]++
	if statuses, ok := n.downAt[h]; ok {
		delete(n.downAt, h)
		n.statusOff = statuses
		return nil, nil, fmt.Errorf("block: %w: connection refused", node.ErrUnavailable)
	}
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
	n.mu.Lock()
	defer n.mu.Unlock()
	n.commitCalls[h]++
	a := commitAnswer{canonical: true, signers: 3}
	if answers := n.seen[h]; len(answers) > 0 {
		a = answers[0]
		if len(answers) > 1 {
			n.seen[h] = answers[1:]
		}
	}
	if a.err != nil {
		return nil, nil, a.err
	}
	c := &node.Commit{Height: h, Canonical: a.canonical}
	for i := range a.signers {
		c.Signatures = append(c.Signatures, node.CommitSig{BlockIDFlag: node.BlockIDFlagCommit, ValidatorAddress: fmt.Sprintf("v%d", i)})
	}
	body := rawBody("commit", h)
	if !a.canonical {
		body = []byte(fmt.Sprintf("seen commit %d", h))
	}
	return c, body, nil
}

// rawBody is the response body mockNode answers for route at h.
func rawBody(route string, h int64) []byte {
	return []byte(fmt.Sprintf("%s %d", route, h))
}

// mockRaw records the bodies the indexer puts and the commits it replaces,
// by height.
type mockRaw struct {
	mu       sync.Mutex
	puts     map[int64][3]string
	replaced map[int64]string
}

func (r *mockRaw) ReplaceCommit(h int64, commit []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replaced[h] = string(commit)
}

func (r *mockRaw) replacedAt(h int64) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.replaced[h]
	return c, ok
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
// is "bad" fails. An even height marks the validator "v<height>" dirty.
type mockTransformer struct{}

func (mockTransformer) Transform(in transform.Input) (*transform.Entities, error) {
	if in.Block.Hash == "bad" {
		return nil, errors.New("cannot decode")
	}
	ents := &transform.Entities{Blocks: []transform.Block{{Height: in.Block.Height}}}
	if in.Block.Height%2 == 0 {
		ents.Dirty.Mark(statesync.Validators, fmt.Sprintf("v%d", in.Block.Height))
	}
	return ents, nil
}

// mockPublisher records the published dirty sets with the heights written
// at the time.
type mockPublisher struct {
	store *mockStore

	mu        sync.Mutex
	published []string
}

func (p *mockPublisher) Publish(d statesync.Dirty) {
	written := p.store.writtenHeights()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, k := range d.Keys(statesync.Validators) {
		p.published = append(p.published, fmt.Sprintf("%s after %v", k, written))
	}
}

func (p *mockPublisher) list() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.published)
}

// mockStore keeps the indexer state in memory, with the writer's rules: the
// floor is the first height written, the cursor only moves up.
type mockStore struct {
	mu       sync.Mutex
	cursor   int64
	floor    int64
	written  []int64
	failures map[int64]failure
	// signers are the UpdateSignatures calls, by height.
	signers   map[int64][]string
	updateErr error
}

func (s *mockStore) UpdateSignatures(_ context.Context, h int64, signers []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updateErr != nil {
		return s.updateErr
	}
	s.signers[h] = signers
	return nil
}

func (s *mockStore) updatedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.signers)
}

func (s *mockStore) updated(h int64) ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.signers[h]
	return v, ok
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
	dirty    *mockPublisher
	clock    *fakeClock
	cancel   context.CancelFunc
	done     chan error
	stopOnce sync.Once
}

// run starts the indexer with the store at cursor and floor (0: not set) and
// stops it when the test ends.
func run(t *testing.T, l *mockListener, n *mockNode, clock *fakeClock, cursor, floor int64) *harness {
	t.Helper()
	return runWithCatchUp(t, l, n, clock, cursor, floor, nil)
}

// runWithCatchUp is run with a catch-up job (nil: none).
func runWithCatchUp(t *testing.T, l *mockListener, n *mockNode, clock *fakeClock, cursor, floor int64, cu *mockCatchUp) *harness {
	t.Helper()
	h := &harness{
		listener: l, node: n, clock: clock, done: make(chan error, 1),
		store: &mockStore{cursor: cursor, floor: floor, failures: map[int64]failure{}, signers: map[int64][]string{}},
		raw:   &mockRaw{puts: map[int64][3]string{}, replaced: map[int64]string{}},
	}
	h.dirty = &mockPublisher{store: h.store}
	deps := live.Deps{Listener: l, Node: n, Store: h.store, Transformer: mockTransformer{}, Raw: h.raw, Dirty: h.dirty}
	if cu != nil {
		cu.store = h.store
		deps.CatchUp = cu
	}
	ix := live.New(live.Config{Sleep: clock.Sleep}, deps)
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

func TestDirtySetIsPublishedAfterTheWrite(t *testing.T) {
	h := run(t, newMockListener(), newMockNode(99), &fakeClock{}, 98, 50)
	sub := h.listener.next(t)
	h.waitWritten(t, 99)

	sub.notify(101)
	h.waitWritten(t, 99, 100, 101)
	require.Eventually(t, func() bool { return len(h.dirty.list()) == 1 }, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"v100 after [99 100]"}, h.dirty.list(), "clean heights publish nothing")
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

// A node that stays down keeps the indexer reconnecting; cancelling stops it
// even when nobody reads the new connections.
func TestStopsWhileTheNodeStaysDown(t *testing.T) {
	n := newMockNode(100)
	n.statusErr = errors.New("connection refused")
	l := newMockListener()
	h := run(t, l, n, &fakeClock{}, 99, 50)

	// The connection after the 16 unread ones waits until the indexer stops.
	require.Eventually(t, func() bool { return l.callCount() > cap(l.subs) }, 5*time.Second, time.Millisecond,
		"the indexer reconnects past the unread connections")
	h.stop(t)
}

func TestNodeGoingDownMidPassRecordsNoFailure(t *testing.T) {
	n := newMockNode(99)
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)
	sub := h.listener.next(t)

	// The node restarts while the pass is at 102: the probe and the next
	// pass's /status find it down.
	n.goDown(102, 2)
	n.setTip(104)
	sub.notify(104)
	h.listener.next(t)
	h.listener.next(t)
	h.waitWritten(t, 100, 101, 102, 103, 104)
	for hgt := int64(100); hgt <= 104; hgt++ {
		_, failed := h.store.failure(hgt)
		assert.False(t, failed, "height %d", hgt)
	}
	assert.Equal(t, 2, n.callsAt(102), "the same height again after the reconnect")
	assert.Equal(t, []time.Duration{time.Second, time.Second}, h.clock.recorded(), "two reconnects, no height retries")
}

func TestUnavailableHeightOfAnAnsweringNodeIsRecorded(t *testing.T) {
	n := newMockNode(99)
	slow := fmt.Errorf("block: %w: context deadline exceeded", node.ErrUnavailable)
	n.failBlock(100, slow, slow, slow)
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)
	sub := h.listener.next(t)

	n.setTip(101)
	sub.notify(101)
	h.waitWritten(t, 101)
	f, ok := h.store.failure(100)
	require.True(t, ok, "the node answers /status: the height itself fails")
	assert.Equal(t, 3, f.attempts)
	assert.ErrorIs(t, f.err, node.ErrUnavailable)
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

// mockCatchUp records its ranges and moves the store's cursor as the real
// job does; err fails the next call.
type mockCatchUp struct {
	mu     sync.Mutex
	store  *mockStore
	ranges [][2]int64
	err    error
}

func (c *mockCatchUp) CatchUp(_ context.Context, from, to int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ranges = append(c.ranges, [2]int64{from, to})
	if err := c.err; err != nil {
		c.err = nil
		return err
	}
	c.store.mu.Lock()
	c.store.cursor = max(c.store.cursor, to)
	c.store.mu.Unlock()
	return nil
}

func (c *mockCatchUp) calls() [][2]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.ranges)
}

func TestHeightsBelowTheNodesEarliestGoToTheCatchUp(t *testing.T) {
	n := newMockNode(110)
	n.earliest = 105
	cu := &mockCatchUp{}
	h := runWithCatchUp(t, newMockListener(), n, &fakeClock{}, 99, 50, cu)

	h.waitWritten(t, 105, 106, 107, 108, 109, 110)
	assert.Equal(t, [][2]int64{{100, 104}}, cu.calls())
	for hh := int64(100); hh < 105; hh++ {
		assert.Zero(t, n.callsAt(hh), "the local node is not asked for pruned height %d", hh)
	}
}

func TestCatchUpCoversTheWholeRangeWhenTheTipIsPruned(t *testing.T) {
	n := newMockNode(103)
	n.earliest = 200 // a node restored from a snapshot ahead of the tip it reports
	cu := &mockCatchUp{}
	h := runWithCatchUp(t, newMockListener(), n, &fakeClock{}, 99, 50, cu)

	require.Eventually(t, func() bool { return len(cu.calls()) == 1 }, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, [][2]int64{{100, 103}}, cu.calls())
	assert.Empty(t, h.store.writtenHeights())
}

func TestNoCatchUpWithinTheNodesWindow(t *testing.T) {
	n := newMockNode(103)
	n.earliest = 100
	cu := &mockCatchUp{}
	h := runWithCatchUp(t, newMockListener(), n, &fakeClock{}, 99, 50, cu)

	h.waitWritten(t, 100, 101, 102, 103)
	assert.Empty(t, cu.calls())
}

func TestAFailedCatchUpReconnectsAndRetries(t *testing.T) {
	n := newMockNode(106)
	n.earliest = 105
	cu := &mockCatchUp{err: errors.New("database unavailable")}
	clock := &fakeClock{}
	h := runWithCatchUp(t, newMockListener(), n, clock, 99, 50, cu)

	h.waitWritten(t, 105, 106)
	assert.Equal(t, [][2]int64{{100, 104}, {100, 104}}, cu.calls())
	assert.Equal(t, []time.Duration{live.DefaultReconnectMin}, clock.recorded())
}

// A height written at the tip carries the node's seen commit: once the next
// height is written, the indexer re-reads it, canonical by now, and corrects
// its signatures and its cached commit.
func TestASeenCommitIsCorrectedOnceTheNextHeightIsWritten(t *testing.T) {
	n := newMockNode(99)
	n.answerCommits(100, commitAnswer{canonical: false, signers: 2}, commitAnswer{canonical: true, signers: 3})
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)
	sub := h.listener.next(t)

	sub.notify(100)
	h.waitWritten(t, 100)
	got, _ := h.raw.put(100)
	assert.Equal(t, "seen commit 100", got[2], "the seen commit is what the node answered")
	_, ok := h.store.updated(100)
	assert.False(t, ok, "nothing to correct before 101")

	sub.notify(101)
	h.waitWritten(t, 100, 101)
	require.Eventually(t, func() bool { _, ok := h.raw.replacedAt(100); return ok }, 5*time.Second, time.Millisecond)
	signers, ok := h.store.updated(100)
	require.True(t, ok)
	assert.Equal(t, []string{"V0", "V1", "V2"}, signers, "the canonical commit's three signers")
	commit, _ := h.raw.replacedAt(100)
	assert.Equal(t, "commit 100", commit, "the canonical body replaces the seen one")
	assert.Equal(t, 2, n.commitCallsAt(100))
	_, ok = h.store.updated(101)
	assert.False(t, ok, "101 was canonical")
}

func TestACanonicalCommitIsNotReRead(t *testing.T) {
	n := newMockNode(99)
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)
	sub := h.listener.next(t)

	sub.notify(100)
	sub.notify(101)
	h.waitWritten(t, 100, 101)
	assert.Equal(t, 1, n.commitCallsAt(100))
	_, updated := h.store.updated(100)
	assert.False(t, updated)
	_, replaced := h.raw.replacedAt(100)
	assert.False(t, replaced)
}

// A failed re-read only logs: the session goes on, nothing is recorded, and
// the height keeps its seen figures.
func TestAFailedReReadDoesNotStopTheSession(t *testing.T) {
	n := newMockNode(99)
	n.answerCommits(100, commitAnswer{canonical: false, signers: 2}, commitAnswer{err: errors.New("connection reset")})
	n.answerCommits(101, commitAnswer{canonical: false, signers: 2}, commitAnswer{canonical: false, signers: 2})
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)
	sub := h.listener.next(t)

	sub.notify(100)
	sub.notify(101)
	sub.notify(102)
	h.waitWritten(t, 100, 101, 102)
	require.Eventually(t, func() bool { return n.commitCallsAt(101) == 2 }, 5*time.Second, time.Millisecond)
	assert.Equal(t, 2, n.commitCallsAt(100), "one re-read, no retry")
	for _, seen := range []int64{100, 101} {
		_, updated := h.store.updated(seen)
		assert.False(t, updated, "a failed or still seen re-read corrects nothing at %d", seen)
		_, replaced := h.raw.replacedAt(seen)
		assert.False(t, replaced, "%d", seen)
	}
	_, failed := h.store.failure(100)
	assert.False(t, failed, "a re-read never records a failure")

	sub.notify(103)
	h.waitWritten(t, 100, 101, 102, 103)
}

func TestAFailedSignaturesUpdateKeepsTheSessionAndTheCache(t *testing.T) {
	n := newMockNode(99)
	n.answerCommits(100, commitAnswer{canonical: false, signers: 2}, commitAnswer{canonical: true, signers: 3})
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)
	h.store.mu.Lock()
	h.store.updateErr = errors.New("database gone")
	h.store.mu.Unlock()
	sub := h.listener.next(t)

	sub.notify(100)
	sub.notify(101)
	sub.notify(102)
	h.waitWritten(t, 100, 101, 102)
	assert.Equal(t, 2, n.commitCallsAt(100))
	_, ok := h.raw.replacedAt(100)
	assert.False(t, ok, "the cache keeps the seen body the table still matches")
}

// The last height written before a reconnect or a restart may carry a seen
// commit the indexer no longer remembers: a session starts by treating the
// cursor as seen, and re-reads it once the next height is written.
func TestASessionCorrectsTheHeightWrittenBeforeIt(t *testing.T) {
	n := newMockNode(99)
	n.answerCommits(100, commitAnswer{canonical: false, signers: 2}, commitAnswer{canonical: true, signers: 3})
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)
	sub := h.listener.next(t)

	sub.notify(100)
	h.waitWritten(t, 100)
	sub.drop()
	sub = h.listener.next(t)
	sub.notify(101)
	h.waitWritten(t, 100, 101)
	require.Eventually(t, func() bool { _, ok := h.raw.replacedAt(100); return ok }, 5*time.Second, time.Millisecond)
	signers, ok := h.store.updated(100)
	require.True(t, ok)
	assert.Equal(t, []string{"V0", "V1", "V2"}, signers)
	assert.Equal(t, 2, n.commitCallsAt(100))
}

// A restart (a fresh indexer over a store that has a cursor) re-reads the
// cursor's commit once; a first start, without a cursor, re-reads nothing.
func TestARestartReReadsTheCursorOnce(t *testing.T) {
	n := newMockNode(99)
	h := run(t, newMockListener(), n, &fakeClock{}, 99, 50)
	sub := h.listener.next(t)

	sub.notify(100)
	sub.notify(101)
	h.waitWritten(t, 100, 101)
	require.Eventually(t, func() bool { _, ok := h.store.updated(99); return ok }, 5*time.Second, time.Millisecond)
	assert.Equal(t, 1, n.commitCallsAt(99), "never written by this indexer: only the re-read")
	assert.Equal(t, 1, n.commitCallsAt(100), "canonical when written")
	assert.Equal(t, 1, h.store.updatedCount())
}

func TestAFirstStartReReadsNothing(t *testing.T) {
	n := newMockNode(100)
	h := run(t, newMockListener(), n, &fakeClock{}, 0, 0)
	sub := h.listener.next(t)
	h.waitWritten(t, 100)

	sub.notify(101)
	h.waitWritten(t, 100, 101)
	assert.Equal(t, 1, n.commitCallsAt(100))
	assert.Zero(t, h.store.updatedCount())
}
