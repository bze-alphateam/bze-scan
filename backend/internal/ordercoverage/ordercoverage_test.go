package ordercoverage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/ordercoverage"
)

const start = int64(12_940_553)

// fakeStore indexes the heights in blocks, with the failures in failed.
type fakeStore struct {
	last    int64
	hasLast bool
	missing map[int64]bool
	failed  map[int64]bool
	err     error

	counts [][2]int64 // every CountBlocks range
}

func (s *fakeStore) LastIndexed(context.Context) (int64, bool, error) {
	return s.last, s.hasLast, s.err
}

func (s *fakeStore) CountBlocks(_ context.Context, lo, hi int64) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	s.counts = append(s.counts, [2]int64{lo, hi})
	n := hi - lo + 1
	for h := range s.missing {
		if h >= lo && h <= hi {
			n--
		}
	}
	return n, nil
}

func (s *fakeStore) OpenFailures(_ context.Context, lo, hi int64) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	for h := range s.failed {
		if h >= lo && h <= hi {
			return true, nil
		}
	}
	return false, nil
}

func newCoverage(store *fakeStore) *ordercoverage.Coverage {
	return ordercoverage.New(ordercoverage.Config{ChainID: "beezee-1"}, store)
}

func TestMainnetStartsAtTheFirstMarket(t *testing.T) {
	assert.Equal(t, start, ordercoverage.DEXStart["beezee-1"])
}

func TestIncompleteUntilTheFirstCheck(t *testing.T) {
	store := &fakeStore{last: start + 10, hasLast: true}
	c := newCoverage(store)
	assert.False(t, c.Complete())
	require.NoError(t, c.Check(context.Background()))
	assert.True(t, c.Complete())
	assert.Equal(t, [][2]int64{{start, start + 10}}, store.counts, "the first check counts the whole range")
}

func TestLaterChecksCountOnlyAboveTheWatermark(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{last: start + 10, hasLast: true}
	c := newCoverage(store)
	require.NoError(t, c.Check(ctx))

	store.last = start + 15
	require.NoError(t, c.Check(ctx))
	assert.True(t, c.Complete())
	require.NoError(t, c.Check(ctx)) // nothing new: no count at all
	assert.Equal(t, [][2]int64{{start, start + 10}, {start + 11, start + 15}}, store.counts)
}

func TestAGapMakesItIncompleteAndTheNextCheckStartsOver(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{last: start + 10, hasLast: true}
	c := newCoverage(store)
	require.NoError(t, c.Check(ctx))
	require.True(t, c.Complete())

	store.last, store.missing = start+20, map[int64]bool{start + 12: true}
	require.NoError(t, c.Check(ctx))
	assert.False(t, c.Complete(), "complete → gap found")

	delete(store.missing, start+12) // the gap is filled
	store.counts = nil
	require.NoError(t, c.Check(ctx))
	assert.True(t, c.Complete())
	assert.Equal(t, [][2]int64{{start, start + 20}}, store.counts, "the recheck starts from the DEX start")
}

func TestAGapBelowTheFirstCheckIsIncomplete(t *testing.T) {
	store := &fakeStore{last: start + 10, hasLast: true, missing: map[int64]bool{start: true}}
	c := newCoverage(store)
	require.NoError(t, c.Check(context.Background()))
	assert.False(t, c.Complete())
}

func TestAnOpenFailureInRangeIsIncomplete(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{last: start + 10, hasLast: true, failed: map[int64]bool{start + 3: true}}
	c := newCoverage(store)
	require.NoError(t, c.Check(ctx))
	assert.False(t, c.Complete())

	store.failed = map[int64]bool{start - 1: true} // below the DEX: irrelevant
	require.NoError(t, c.Check(ctx))
	assert.True(t, c.Complete())
}

func TestNotYetIndexedOrBelowTheStartIsIncomplete(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{}
	c := newCoverage(store)
	require.NoError(t, c.Check(ctx))
	assert.False(t, c.Complete(), "the live indexer has not written")

	store.last, store.hasLast = start-1, true
	require.NoError(t, c.Check(ctx))
	assert.False(t, c.Complete(), "the cursor is below the DEX start")
}

func TestADatabaseErrorIsIncompleteAndKeepsTheWatermark(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{last: start + 10, hasLast: true}
	c := newCoverage(store)
	require.NoError(t, c.Check(ctx))

	store.err = errors.New("connection refused")
	require.Error(t, c.Check(ctx))
	assert.False(t, c.Complete())

	store.err, store.last, store.counts = nil, start+12, nil
	require.NoError(t, c.Check(ctx))
	assert.True(t, c.Complete())
	assert.Equal(t, [][2]int64{{start + 11, start + 12}}, store.counts)
}

func TestAChainWithoutAStartIsNeverComplete(t *testing.T) {
	store := &fakeStore{last: 100, hasLast: true}
	c := ordercoverage.New(ordercoverage.Config{ChainID: "beezee-testnet"}, store)
	require.NoError(t, c.Check(context.Background()))
	assert.False(t, c.Complete())
	require.NoError(t, c.Run(context.Background()), "Run returns at once")
	assert.Empty(t, store.counts)
}

func TestRunChecksAtOnceThenEveryInterval(t *testing.T) {
	store := &fakeStore{last: start, hasLast: true}
	c := ordercoverage.New(ordercoverage.Config{ChainID: "beezee-1", Interval: 10 * time.Millisecond}, store)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, c.Complete, time.Second, time.Millisecond)
	cancel()
	require.NoError(t, <-done)
}
