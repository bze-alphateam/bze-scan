package live

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

func TestRange(t *testing.T) {
	cases := []struct {
		name             string
		cursor           int64
		hasCursor        bool
		floor            int64
		hasFloor         bool
		notified, tip    int64
		wantFrom, wantTo int64
		wantOK           bool
	}{
		{name: "no gap", cursor: 99, hasCursor: true, floor: 50, hasFloor: true, notified: 100, tip: 100, wantFrom: 100, wantTo: 100, wantOK: true},
		{name: "gap", cursor: 95, hasCursor: true, floor: 50, hasFloor: true, notified: 100, tip: 100, wantFrom: 96, wantTo: 100, wantOK: true},
		{name: "notified behind the node", cursor: 99, hasCursor: true, floor: 50, hasFloor: true, notified: 100, tip: 103, wantFrom: 100, wantTo: 103, wantOK: true},
		{name: "node behind the notification", cursor: 99, hasCursor: true, floor: 50, hasFloor: true, notified: 102, tip: 100, wantFrom: 100, wantTo: 102, wantOK: true},
		{name: "reconnect without notification", cursor: 90, hasCursor: true, floor: 50, hasFloor: true, notified: 0, tip: 100, wantFrom: 91, wantTo: 100, wantOK: true},
		{name: "already up to date", cursor: 100, hasCursor: true, floor: 50, hasFloor: true, notified: 100, tip: 100, wantOK: false},
		{name: "stale notification", cursor: 100, hasCursor: true, floor: 50, hasFloor: true, notified: 97, tip: 100, wantOK: false},
		{name: "first start begins at the head", notified: 0, tip: 100, wantFrom: 100, wantTo: 100, wantOK: true},
		{name: "first start on a notification", notified: 101, tip: 100, wantFrom: 101, wantTo: 101, wantOK: true},
		{name: "never below the floor", cursor: 10, hasCursor: true, floor: 50, hasFloor: true, notified: 0, tip: 52, wantFrom: 50, wantTo: 52, wantOK: true},
		{name: "no height known", notified: 0, tip: 0, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, to, ok := Range(tc.cursor, tc.hasCursor, tc.floor, tc.hasFloor, tc.notified, tc.tip)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantFrom, from)
				assert.Equal(t, tc.wantTo, to)
			}
		})
	}
}

// fakeStore records the indexer's writes in memory.
type fakeStore struct {
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

func (s *fakeStore) Cursor(context.Context) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursor, s.cursor > 0, nil
}

func (s *fakeStore) LiveFloor(context.Context) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.floor, s.floor > 0, nil
}

func (s *fakeStore) WriteBlock(_ context.Context, ents *transform.Entities) error {
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

func (s *fakeStore) RecordFailure(_ context.Context, h int64, attempts int, cause error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failures == nil {
		s.failures = map[int64]failure{}
	}
	s.failures[h] = failure{attempts, cause}
	s.cursor = max(s.cursor, h)
	return nil
}

// fakeClock records the waits instead of sleeping.
type fakeClock struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.waits = append(c.waits, d)
	c.mu.Unlock()
	return ctx.Err()
}

// flakyNode wraps the recorded node and fails Block for chosen heights a
// chosen number of times.
type flakyNode struct {
	*node.Client
	mu    sync.Mutex
	fails map[int64]int
	err   error
	calls map[int64]int
}

func (n *flakyNode) Block(ctx context.Context, h int64) (*node.Block, []byte, error) {
	n.mu.Lock()
	n.calls[h]++
	if n.fails[h] > 0 {
		n.fails[h]--
		n.mu.Unlock()
		return nil, nil, n.err
	}
	n.mu.Unlock()
	return n.Client.Block(ctx, h)
}

func newIndexer(t *testing.T, fn *fakenode.Node, fails map[int64]int, err error) (*Indexer, *fakeStore, *fakeClock, *flakyNode) {
	t.Helper()
	store := &fakeStore{}
	clock := &fakeClock{}
	n := &flakyNode{Client: node.New(fn.URL), fails: fails, err: err, calls: map[int64]int{}}
	return New(Config{Sleep: clock.Sleep}, n, store), store, clock, n
}

func TestRetriesThenSucceeds(t *testing.T) {
	ix, store, clock, n := newIndexer(t, fakenode.New(t), map[int64]int{24998316: 2}, errors.New("connection refused"))

	require.NoError(t, ix.indexHeight(context.Background(), 24998316))
	assert.Equal(t, []int64{24998316}, store.written)
	assert.Empty(t, store.failures)
	assert.Equal(t, 3, n.calls[24998316])
	assert.Equal(t, []time.Duration{500 * time.Millisecond, 2 * time.Second}, clock.waits)
}

func TestGivesUpAfterThreeAttempts(t *testing.T) {
	fn := fakenode.New(t)
	ix, store, clock, n := newIndexer(t, fn, nil, nil)

	// 24998319 has no fixture: the fake node answers the above-tip error.
	require.NoError(t, ix.indexHeight(context.Background(), 24998319))
	assert.Empty(t, store.written)
	require.Contains(t, store.failures, int64(24998319))
	f := store.failures[24998319]
	assert.Equal(t, 3, f.attempts)
	assert.ErrorIs(t, f.err, node.ErrAboveTip)
	assert.Equal(t, 3, n.calls[24998319])
	assert.Equal(t, []time.Duration{500 * time.Millisecond, 2 * time.Second}, clock.waits)
	assert.Equal(t, int64(24998319), store.cursor, "the cursor moves on")
}

func TestPrunedHeightIsRecordedWithoutRetry(t *testing.T) {
	ix, store, clock, n := newIndexer(t, fakenode.New(t), map[int64]int{24998316: 5}, fmt.Errorf("block: %w", node.ErrPruned))

	require.NoError(t, ix.indexHeight(context.Background(), 24998316))
	require.Contains(t, store.failures, int64(24998316))
	assert.Equal(t, 1, store.failures[24998316].attempts)
	assert.ErrorIs(t, store.failures[24998316].err, node.ErrPruned)
	assert.Equal(t, 1, n.calls[24998316])
	assert.Empty(t, clock.waits)
}

func TestPassIndexesTheGapInOrderAndSkipsFailures(t *testing.T) {
	fn := fakenode.New(t)
	fn.SetStatusHeight(24998320)
	ix, store, _, _ := newIndexer(t, fn, nil, nil)
	store.cursor, store.floor = 24998316, 24998316

	require.NoError(t, ix.pass(context.Background(), 24998318))
	assert.Equal(t, []int64{24998317, 24998318, 24998320}, store.written)
	assert.Contains(t, store.failures, int64(24998319))
	assert.Equal(t, int64(24998320), store.cursor)
}

func TestFirstPassStartsAtTheHead(t *testing.T) {
	fn := fakenode.New(t)
	fn.SetStatusHeight(24998317)
	ix, store, _, _ := newIndexer(t, fn, nil, nil)

	require.NoError(t, ix.pass(context.Background(), 0))
	assert.Equal(t, []int64{24998317}, store.written)
	assert.Equal(t, int64(24998317), store.floor)
}

func TestPassFailsWhenTheNodeIsDown(t *testing.T) {
	fn := fakenode.New(t)
	ix, store, _, _ := newIndexer(t, fn, nil, nil)
	fn.Close()

	assert.Error(t, ix.pass(context.Background(), 24998316))
	assert.Empty(t, store.written)
	assert.Empty(t, store.failures)
}

func TestShutdownStopsBetweenAttempts(t *testing.T) {
	ix, store, _, _ := newIndexer(t, fakenode.New(t), map[int64]int{24998316: 5}, errors.New("timeout"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.Error(t, ix.indexHeight(ctx, 24998316))
	assert.Empty(t, store.failures, "a height interrupted by shutdown is not a failure")
}
