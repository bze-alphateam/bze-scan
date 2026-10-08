package status_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/status"
)

var errDown = errors.New("connection refused")

// fakeStore answers the heights it holds; a nil height is "not written yet".
type fakeStore struct {
	mu     sync.Mutex
	db     *int64
	oldest *int64
	err    error
}

func (s *fakeStore) set(db int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = &db
}

func (s *fakeStore) LastIndexedHeight(context.Context) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil || s.db == nil {
		return 0, false, s.err
	}
	return *s.db, true, nil
}

func (s *fakeStore) OldestHeight(context.Context) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil || s.oldest == nil {
		return 0, false, s.err
	}
	return *s.oldest, true, nil
}

// fakeNode answers its height, or err; calls counts the requests.
type fakeNode struct {
	mu     sync.Mutex
	height int64
	err    error
	calls  int
	block  bool // wait for the context instead of answering
}

func (n *fakeNode) Status(ctx context.Context) (*node.Status, []byte, error) {
	n.mu.Lock()
	n.calls++
	height, err, block := n.height, n.err, n.block
	n.mu.Unlock()
	if block {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	if err != nil {
		return nil, nil, err
	}
	return &node.Status{Network: "beezee-1", LatestBlockHeight: height}, nil, nil
}

func quiet() log.FieldLogger {
	l := log.New()
	l.SetOutput(io.Discard)
	return l
}

func ptr(v int64) *int64 { return &v }

var fixedNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func newChecker(store *fakeStore, local, archive, retry *fakeNode, tolerance int64) *status.Checker {
	deps := status.Deps{Store: store, Local: local, Archive: archive, Log: quiet()}
	if retry != nil {
		deps.ArchiveRetry = retry
	}
	return status.New(status.Config{Tolerance: tolerance, Now: func() time.Time { return fixedNow }}, deps)
}

// TestHealthy runs two ticks per case: the first after a start (spread only),
// the second with movement.
func TestHealthy(t *testing.T) {
	type tick struct {
		db, local, archive int64
		localErr           error
		archiveErr         error
	}
	cases := []struct {
		name      string
		tolerance int64
		first     tick
		wantFirst bool
		second    tick
		want      bool
	}{
		{name: "moved, spread inside", tolerance: 5,
			first: tick{db: 100, local: 101, archive: 102}, wantFirst: true,
			second: tick{db: 103, local: 104, archive: 105}, want: true},
		{name: "spread exactly the tolerance", tolerance: 5,
			first: tick{db: 100, local: 105, archive: 103}, wantFirst: true,
			second: tick{db: 101, local: 106, archive: 106}, want: true},
		{name: "stood still", tolerance: 5,
			first: tick{db: 100, local: 100, archive: 100}, wantFirst: true,
			second: tick{db: 100, local: 100, archive: 100}, want: false},
		{name: "moved but spread beyond", tolerance: 5,
			first: tick{db: 100, local: 100, archive: 100}, wantFirst: true,
			second: tick{db: 101, local: 107, archive: 107}, want: false},
		{name: "first tick beyond the tolerance", tolerance: 5,
			first: tick{db: 100, local: 106, archive: 106}, wantFirst: false,
			second: tick{db: 106, local: 107, archive: 107}, want: true},
		{name: "zero tolerance", tolerance: 0,
			first: tick{db: 100, local: 100, archive: 101}, wantFirst: false,
			second: tick{db: 101, local: 101, archive: 101}, want: true},
		{name: "local node unreachable", tolerance: 5,
			first: tick{db: 100, local: 100, archive: 100}, wantFirst: true,
			second: tick{db: 101, localErr: errDown, archive: 101}, want: false},
		{name: "archive unreachable", tolerance: 5,
			first: tick{db: 100, local: 100, archive: 100, archiveErr: errDown}, wantFirst: false,
			second: tick{db: 101, local: 101, archive: 101}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, local, archive := &fakeStore{}, &fakeNode{}, &fakeNode{}
			c := newChecker(store, local, archive, nil, tc.tolerance)
			for i, step := range []struct {
				tick
				want bool
			}{{tc.first, tc.wantFirst}, {tc.second, tc.want}} {
				store.set(step.db)
				local.height, local.err = step.local, step.localErr
				archive.height, archive.err = step.archive, step.archiveErr
				assert.Equal(t, step.want, c.Check(context.Background()).Healthy, "tick %d", i+1)
			}
		})
	}
}

func TestSnapshotBeforeTheFirstTick(t *testing.T) {
	c := newChecker(&fakeStore{}, &fakeNode{}, &fakeNode{}, nil, 5)

	assert.Equal(t, status.Snapshot{BackFill: status.BackFillFinished}, c.Snapshot())
}

func TestCheckStoresTheSnapshot(t *testing.T) {
	store := &fakeStore{oldest: ptr(90)}
	store.set(100)
	c := newChecker(store, &fakeNode{height: 101}, &fakeNode{height: 102}, nil, 5)

	got := c.Check(context.Background())
	want := status.Snapshot{CheckedAt: fixedNow, Healthy: true, DBHeight: ptr(100), NodeHeight: ptr(101),
		ArchiveHeight: ptr(102), BackFill: status.BackFillFinished, OldestHeight: ptr(90)}
	assert.Equal(t, want, got)
	assert.Equal(t, want, c.Snapshot())
}

func TestNothingIndexedYet(t *testing.T) {
	c := newChecker(&fakeStore{}, &fakeNode{height: 101}, &fakeNode{height: 101}, nil, 5)

	got := c.Check(context.Background())
	assert.False(t, got.Healthy)
	assert.Nil(t, got.DBHeight)
	assert.Nil(t, got.OldestHeight)
	assert.Equal(t, fixedNow, got.CheckedAt)
}

func TestDatabaseUnreachable(t *testing.T) {
	c := newChecker(&fakeStore{err: errDown}, &fakeNode{height: 101}, &fakeNode{height: 101}, nil, 5)

	got := c.Check(context.Background())
	assert.False(t, got.Healthy)
	assert.Nil(t, got.DBHeight)
	assert.Equal(t, ptr(101), got.NodeHeight)
}

func TestArchiveFallsBackToTheRetryNode(t *testing.T) {
	store := &fakeStore{}
	store.set(100)
	archive, retry := &fakeNode{err: errDown}, &fakeNode{height: 100}
	c := newChecker(store, &fakeNode{height: 100}, archive, retry, 5)

	got := c.Check(context.Background())
	assert.True(t, got.Healthy)
	assert.Equal(t, ptr(100), got.ArchiveHeight)
	assert.Equal(t, 1, archive.calls)
	assert.Equal(t, 1, retry.calls)
}

// Without a retry node the archive is asked a second time.
func TestArchiveRetriesItselfWithoutARetryNode(t *testing.T) {
	store := &fakeStore{}
	store.set(100)
	archive := &fakeNode{err: errDown}
	c := newChecker(store, &fakeNode{height: 100}, archive, nil, 5)

	got := c.Check(context.Background())
	assert.False(t, got.Healthy)
	assert.Nil(t, got.ArchiveHeight)
	assert.Equal(t, 2, archive.calls)
}

func TestNodeCallsAreBoundedByTheRequestTimeout(t *testing.T) {
	store := &fakeStore{}
	store.set(100)
	c := status.New(status.Config{RequestTimeout: 20 * time.Millisecond}, status.Deps{
		Store: store, Local: &fakeNode{block: true}, Archive: &fakeNode{height: 100}, Log: quiet(),
	})

	start := time.Now()
	got := c.Check(context.Background())
	assert.Less(t, time.Since(start), time.Second)
	assert.Nil(t, got.NodeHeight)
	assert.False(t, got.CheckedAt.IsZero())
}

func TestRunTicksAndStopsInsideATick(t *testing.T) {
	store := &fakeStore{}
	store.set(100)
	local := &fakeNode{height: 100}
	c := status.New(status.Config{Interval: 5 * time.Millisecond}, status.Deps{
		Store: store, Local: local, Archive: &fakeNode{height: 100}, Log: quiet(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.Eventually(t, func() bool {
		local.mu.Lock()
		defer local.mu.Unlock()
		return local.calls >= 3
	}, 2*time.Second, time.Millisecond, "ticks keep coming")
	assert.False(t, c.Snapshot().CheckedAt.IsZero())

	// A tick blocked on a node still stops with the context.
	local.mu.Lock()
	local.block = true
	local.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}
