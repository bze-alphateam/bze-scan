package backfill_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/backfill"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

var genesisTime = time.Date(2021, 3, 1, 0, 0, 0, 0, time.UTC)

// blockTime is the header time of height h on the mock chain: one block
// every 6 seconds from genesis.
func blockTime(h int64) time.Time {
	return genesisTime.Add(time.Duration(h-1) * 6 * time.Second)
}

// mockNode serves every height. fail makes the next len(errs) requests of a
// height fail; gate, when set, holds every Block call until it is closed.
type mockNode struct {
	mu       sync.Mutex
	fails    map[int64][]error
	blocks   []int64 // Block calls in order
	requests int
	inFlight int
	maxIn    int
	gate     chan struct{}
}

func newMockNode() *mockNode {
	return &mockNode{fails: map[int64][]error{}}
}

func (n *mockNode) fail(h int64, errs ...error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.fails[h] = append(n.fails[h], errs...)
}

func (n *mockNode) next(h int64) error {
	n.requests++
	if errs := n.fails[h]; len(errs) > 0 {
		n.fails[h] = errs[1:]
		return errs[0]
	}
	return nil
}

func (n *mockNode) Block(ctx context.Context, h int64) (*node.Block, []byte, error) {
	n.mu.Lock()
	n.blocks = append(n.blocks, h)
	n.inFlight++
	n.maxIn = max(n.maxIn, n.inFlight)
	gate := n.gate
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		n.inFlight--
		n.mu.Unlock()
	}()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := n.next(h); err != nil {
		return nil, nil, err
	}
	return &node.Block{Height: h, Time: blockTime(h)}, nil, nil
}

func (n *mockNode) BlockResults(_ context.Context, h int64) (*node.BlockResults, []byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.requests++
	return &node.BlockResults{Height: h}, nil, nil
}

func (n *mockNode) Commit(_ context.Context, h int64) (*node.Commit, []byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.requests++
	return &node.Commit{Height: h}, nil, nil
}

func (n *mockNode) blockCalls() []int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.blocks)
}

func (n *mockNode) stats() (requests, inFlight, maxInFlight int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.requests, n.inFlight, n.maxIn
}

type mockTransformer struct{}

func (mockTransformer) Transform(in transform.Input) (*transform.Entities, error) {
	return &transform.Entities{Blocks: []transform.Block{{Height: in.Block.Height, Time: in.Block.Time}}}, nil
}

type recordedFailure struct {
	height   int64
	source   string
	attempts int
	err      error
}

// mockWriter records the flushes (heights in batch order) and failures;
// writeErr fails every flush.
type mockWriter struct {
	mu       sync.Mutex
	batches  [][]int64
	failures []recordedFailure
	writeErr error
	flushed  chan struct{} // receives after every flush when set
}

func (w *mockWriter) Write(_ context.Context, batch []*transform.Entities) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.writeErr != nil {
		return w.writeErr
	}
	var hs []int64
	for _, e := range batch {
		for _, b := range e.Blocks {
			hs = append(hs, b.Height)
		}
	}
	w.batches = append(w.batches, hs)
	if w.flushed != nil {
		select {
		case w.flushed <- struct{}{}:
		default:
		}
	}
	return nil
}

func (w *mockWriter) RecordFailure(_ context.Context, h int64, source string, attempts int, cause error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failures = append(w.failures, recordedFailure{h, source, attempts, cause})
	return nil
}

func (w *mockWriter) flushes() [][]int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.batches)
}

func (w *mockWriter) recorded() []recordedFailure {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.failures)
}

func (w *mockWriter) written() []int64 {
	var all []int64
	for _, b := range w.flushes() {
		all = append(all, b...)
	}
	return all
}

// countingLimiter counts the waits and never blocks.
type countingLimiter struct {
	mu    sync.Mutex
	waits int
}

func (l *countingLimiter) Wait(context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.waits++
	return nil
}

func (l *countingLimiter) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.waits
}

// fakeClock replaces both the retry sleeps (recorded, instant) and the quiet
// timer (fires only when the test says so).
type fakeClock struct {
	mu     sync.Mutex
	sleeps []time.Duration
	timers []chan time.Time
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	c.mu.Unlock()
	return ctx.Err()
}

func (c *fakeClock) After(time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, ch)
	return ch
}

// fireLatest fires the quiet timer armed last; false when none was armed.
func (c *fakeClock) fireLatest() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.timers) == 0 {
		return false
	}
	c.timers[len(c.timers)-1] <- time.Now()
	return true
}

func (c *fakeClock) armed() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

func (c *fakeClock) recordedSleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.sleeps)
}

// chanSource dispatches the heights sent on its channel, until it is
// closed.
type chanSource chan int64

func (s chanSource) Next(ctx context.Context) (int64, bool, error) {
	select {
	case h, ok := <-s:
		return h, ok, nil
	case <-ctx.Done():
		return 0, false, ctx.Err()
	}
}

// mockStore is the job's store in memory.
type mockStore struct {
	mu          sync.Mutex
	liveFloor   int64
	floorAfter  int // LiveFloor answers not-set this many times first
	present     map[int64]bool
	checkpoint  *backfill.Checkpoint
	saves       []backfill.Checkpoint
	presenceErr error
}

func newMockStore(liveFloor int64) *mockStore {
	return &mockStore{liveFloor: liveFloor, present: map[int64]bool{}}
}

func (s *mockStore) LiveFloor(context.Context) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.floorAfter > 0 {
		s.floorAfter--
		return 0, false, nil
	}
	return s.liveFloor, s.liveFloor > 0, nil
}

func (s *mockStore) PresentHeights(_ context.Context, lo, hi int64) ([]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.presenceErr != nil {
		return nil, s.presenceErr
	}
	var out []int64
	for h := range s.present {
		if h >= lo && h <= hi {
			out = append(out, h)
		}
	}
	return out, nil
}

func (s *mockStore) Checkpoint(_ context.Context, job string) (backfill.Checkpoint, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checkpoint == nil || job != backfill.JobMain {
		return backfill.Checkpoint{}, false, nil
	}
	return *s.checkpoint, true, nil
}

func (s *mockStore) SaveCheckpoint(_ context.Context, cp backfill.Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoint = &cp
	s.saves = append(s.saves, cp)
	return nil
}

func (s *mockStore) last() backfill.Checkpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.checkpoint
}

// mockLocker grants the lock unless held.
type mockLocker struct {
	mu       sync.Mutex
	held     bool
	released int
}

func (l *mockLocker) TryLock(context.Context, string) (func(), bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held {
		return nil, false, nil
	}
	l.held = true
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.held = false
		l.released++
	}, true, nil
}

var errUnavailable = errors.New("archive unavailable")

func heights(from, to int64) []int64 {
	var out []int64
	step := int64(1)
	if to < from {
		step = -1
	}
	for h := from; ; h += step {
		out = append(out, h)
		if h == to {
			return out
		}
	}
}

func describe(f recordedFailure) string {
	return fmt.Sprintf("%d/%s/%d", f.height, f.source, f.attempts)
}
