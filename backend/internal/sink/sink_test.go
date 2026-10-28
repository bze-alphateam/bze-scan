package sink_test

import (
	"context"
	"errors"
	"io"
	"sort"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/sink"
)

var now = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

// fakeStore is a sink of heights, each logged at its time, with one row in
// every child table per height.
type fakeStore struct {
	logged    map[int64]time.Time
	cursor    int64
	hasCursor bool
	err       error
	// failDeleteAt fails the Delete whose range starts there.
	failDeleteAt int64

	calls   int
	deletes [][2]int64
	counts  [][2]int64
}

// newFakeStore stores heights lo..hi, height h logged (hi-h) minutes
// before now.
func newFakeStore(lo, hi int64) *fakeStore {
	s := &fakeStore{logged: map[int64]time.Time{}}
	for h := lo; h <= hi; h++ {
		s.logged[h] = now.Add(-time.Duration(hi-h) * time.Minute)
	}
	return s
}

func (s *fakeStore) heights() []int64 {
	out := make([]int64, 0, len(s.logged))
	for h := range s.logged {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (s *fakeStore) Oldest(context.Context) (int64, bool, error) {
	s.calls++
	if s.err != nil {
		return 0, false, s.err
	}
	hs := s.heights()
	if len(hs) == 0 {
		return 0, false, nil
	}
	return hs[0], true, nil
}

func (s *fakeStore) NewestBefore(_ context.Context, t time.Time) (int64, bool, error) {
	s.calls++
	var best int64
	found := false
	for h, at := range s.logged {
		if at.Before(t) && (!found || h > best) {
			best, found = h, true
		}
	}
	return best, found, nil
}

func (s *fakeStore) LastIndexed(context.Context) (int64, bool, error) {
	s.calls++
	return s.cursor, s.hasCursor, nil
}

func (s *fakeStore) rows(from, to int64) sink.Counts {
	var n int64
	for h := range s.logged {
		if h >= from && h < to {
			n++
		}
	}
	return sink.Counts{Attributes: 2 * n, Events: n, TxResults: n, Blocks: n}
}

func (s *fakeStore) Count(_ context.Context, from, to int64) (sink.Counts, error) {
	s.calls++
	s.counts = append(s.counts, [2]int64{from, to})
	return s.rows(from, to), nil
}

func (s *fakeStore) Delete(_ context.Context, from, to int64) (sink.Counts, error) {
	s.calls++
	if s.failDeleteAt != 0 && from == s.failDeleteAt {
		return sink.Counts{}, errors.New("boom")
	}
	s.deletes = append(s.deletes, [2]int64{from, to})
	n := s.rows(from, to)
	for h := range s.logged {
		if h >= from && h < to {
			delete(s.logged, h)
		}
	}
	return n, nil
}

func quiet() log.FieldLogger {
	l := log.New()
	l.SetOutput(io.Discard)
	return l
}

func newCleaner(s *fakeStore, retention time.Duration, batch int64) *sink.Cleaner {
	return sink.New(sink.Config{
		Retention: retention,
		Batch:     batch,
		Now:       func() time.Time { return now },
		Log:       quiet(),
	}, s)
}

func TestPlanByAgeOnly(t *testing.T) {
	// Heights 1..5000, one a minute: 4952 is the newest logged more than
	// 47.5 minutes ago. The cursor is far ahead.
	s := newFakeStore(1, 5000)
	s.cursor, s.hasCursor = 10_000, true

	p, err := newCleaner(s, 47*time.Minute+30*time.Second, 0).Plan(context.Background())
	require.NoError(t, err)
	assert.Equal(t, sink.Plan{Oldest: 1, Cutoff: 4952, ByAge: 4952, ByCursor: 9000}, p)
	assert.False(t, p.Empty())
}

func TestPlanAgeLimitedByTheCursor(t *testing.T) {
	// Everything is old enough, but the live indexer is at 3000: nothing
	// from 2000 up goes.
	s := newFakeStore(1, 5000)
	s.cursor, s.hasCursor = 3000, true

	p, err := newCleaner(s, time.Minute, 0).Plan(context.Background())
	require.NoError(t, err)
	assert.Equal(t, sink.Plan{Oldest: 1, Cutoff: 2000, ByAge: 4998, ByCursor: 2000}, p)
}

func TestPlanEmptySink(t *testing.T) {
	s := newFakeStore(1, 0)
	s.cursor, s.hasCursor = 3000, true

	p, err := newCleaner(s, time.Minute, 0).Plan(context.Background())
	require.NoError(t, err)
	assert.True(t, p.Empty())
	assert.Equal(t, sink.Plan{}, p)
}

func TestPlanNothingToDelete(t *testing.T) {
	for name, tc := range map[string]struct {
		lo, hi    int64
		retention time.Duration
		cursor    int64
		hasCursor bool
	}{
		// No block is older than the retention.
		"fresh sink": {lo: 1, hi: 100, retention: 24 * time.Hour, cursor: 10_000, hasCursor: true},
		// The live indexer has never written.
		"no cursor": {lo: 1, hi: 5000, retention: time.Minute},
		// The cursor margin reaches below the oldest height.
		"cursor at the bottom": {lo: 4000, hi: 5000, retention: time.Minute, cursor: 4500, hasCursor: true},
		// The newest old block is the oldest one: nothing lies below it.
		"cut-off at the oldest": {lo: 4000, hi: 5000, retention: 999*time.Minute + 30*time.Second, cursor: 10_000, hasCursor: true},
	} {
		t.Run(name, func(t *testing.T) {
			s := newFakeStore(tc.lo, tc.hi)
			s.cursor, s.hasCursor = tc.cursor, tc.hasCursor
			c := newCleaner(s, tc.retention, 0)

			p, n, err := c.Once(context.Background())
			require.NoError(t, err)
			assert.True(t, p.Empty(), p.String())
			assert.Zero(t, p.Cutoff)
			assert.Equal(t, sink.Counts{}, n)
			assert.Empty(t, s.deletes)
			assert.Len(t, s.logged, int(tc.hi-tc.lo+1))
		})
	}
}

func TestCleanWalksBatchesUpToTheCutOff(t *testing.T) {
	s := newFakeStore(1, 5000)
	s.cursor, s.hasCursor = 3500, true // cut-off 2500
	c := newCleaner(s, time.Minute, 1000)

	p, n, err := c.Once(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(2500), p.Cutoff)
	assert.Equal(t, [][2]int64{{1, 1001}, {1001, 2001}, {2001, 2500}}, s.deletes)
	assert.Equal(t, sink.Counts{Attributes: 4998, Events: 2499, TxResults: 2499, Blocks: 2499}, n)
	oldest, _, _ := s.Oldest(context.Background())
	assert.Equal(t, int64(2500), oldest, "the cut-off height is kept")
}

func TestCleanSkipsGaps(t *testing.T) {
	// Heights 1..10 and 5000..6000: the batch after the first starts at the
	// next stored height, not at 11.
	s := newFakeStore(5000, 6000)
	for h := int64(1); h <= 10; h++ {
		s.logged[h] = now.Add(-24 * time.Hour)
	}
	s.cursor, s.hasCursor = 6500, true // cut-off 5500
	c := newCleaner(s, time.Minute, 300)

	_, _, err := c.Once(context.Background())
	require.NoError(t, err)
	assert.Equal(t, [][2]int64{{1, 301}, {5000, 5300}, {5300, 5500}}, s.deletes)
}

func TestCleanStopsOnAnError(t *testing.T) {
	s := newFakeStore(1, 5000)
	s.cursor, s.hasCursor = 4000, true
	s.failDeleteAt = 101
	c := newCleaner(s, time.Minute, 100)

	_, n, err := c.Once(context.Background())
	require.ErrorContains(t, err, "boom")
	assert.Equal(t, int64(100), n.Blocks, "the first batch is reported")
	assert.Equal(t, [][2]int64{{1, 101}}, s.deletes)
}

func TestCleanStopsWhenCancelled(t *testing.T) {
	s := newFakeStore(1, 5000)
	s.cursor, s.hasCursor = 4000, true
	c := newCleaner(s, time.Minute, 100)
	p, err := c.Plan(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Clean(ctx, p)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, s.deletes)
}

func TestDryRunCountsAndDeletesNothing(t *testing.T) {
	s := newFakeStore(1, 5000)
	s.cursor, s.hasCursor = 3500, true
	c := newCleaner(s, time.Minute, 1000)

	p, err := c.Plan(context.Background())
	require.NoError(t, err)
	n, err := c.Count(context.Background(), p)
	require.NoError(t, err)
	assert.Equal(t, sink.Counts{Attributes: 4998, Events: 2499, TxResults: 2499, Blocks: 2499}, n)
	assert.Equal(t, [][2]int64{{1, 2500}}, s.counts)
	assert.Empty(t, s.deletes)
	assert.Len(t, s.logged, 5000)
	assert.Equal(t, "cut-off 2500: heights 1..2499 (by age 4998, by cursor 2500)", p.String())
}

func TestDryRunOfAnEmptyPlanCountsNothing(t *testing.T) {
	s := newFakeStore(1, 0)
	c := newCleaner(s, time.Minute, 0)
	n, err := c.Count(context.Background(), sink.Plan{})
	require.NoError(t, err)
	assert.Equal(t, sink.Counts{}, n)
	assert.Empty(t, s.counts)
}

func TestPlanWithoutRetentionIsOff(t *testing.T) {
	s := newFakeStore(1, 5000)
	_, err := newCleaner(s, 0, 0).Plan(context.Background())
	require.ErrorIs(t, err, sink.ErrOff)
	assert.Zero(t, s.calls)
}

func TestRunWithoutRetentionDoesNothing(t *testing.T) {
	s := newFakeStore(1, 5000)
	s.cursor, s.hasCursor = 4000, true

	done := make(chan error, 1)
	go func() { done <- newCleaner(s, 0, 0).Run(context.Background()) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return without a retention")
	}
	assert.Zero(t, s.calls, "the store is never touched")
	assert.Len(t, s.logged, 5000)
}

func TestRunCleansAtOnceAndKeepsGoingAfterErrors(t *testing.T) {
	s := newFakeStore(1, 5000)
	s.cursor, s.hasCursor = 3500, true
	s.err = errors.New("db down")
	c := sink.New(sink.Config{Retention: time.Minute, Interval: 10 * time.Millisecond, Now: func() time.Time { return now }, Log: quiet()}, s)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	require.NoError(t, <-done, "errors are logged, not returned")
	assert.Greater(t, s.calls, 1, "it tried more than once")
}
