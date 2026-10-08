package backfill_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/backfill"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

type env struct {
	archive, retry *mockNode
	writer         *mockWriter
	limiter        *countingLimiter
	clock          *fakeClock
}

func newEnv() *env {
	return &env{archive: newMockNode(), retry: newMockNode(), writer: &mockWriter{},
		limiter: &countingLimiter{}, clock: &fakeClock{}}
}

func (e *env) pipeline(workers, batch int) *backfill.Pipeline {
	return backfill.New(e.config(workers, batch), e.deps())
}

func (e *env) config(workers, batch int) backfill.Config {
	return backfill.Config{Workers: workers, Batch: batch, Sleep: e.clock.Sleep, After: e.clock.After}
}

func (e *env) deps() backfill.Deps {
	return backfill.Deps{Archive: e.archive, ArchiveRetry: e.retry, Limiter: e.limiter,
		Transformer: mockTransformer{}, Writer: e.writer}
}

func TestDispatchFollowsTheSourceOrder(t *testing.T) {
	e := newEnv()
	res := e.pipeline(1, 100).Run(context.Background(), backfill.Descending(10, 1, nil), writer.ModeInsert)

	require.NoError(t, res.Err)
	assert.True(t, res.Complete)
	assert.Equal(t, heights(10, 1), e.archive.blockCalls())
	assert.Equal(t, [][]int64{heights(10, 1)}, e.writer.flushes(), "arrival order, one flush on close")
	assert.Equal(t, backfill.Progress{Lowest: 1, Highest: 10, Dispatched: 10, Written: 10}, res.Progress)
}

func TestSemaphoreBoundsTheHeightsInFlight(t *testing.T) {
	e := newEnv()
	e.archive.gate = make(chan struct{})
	done := make(chan backfill.Result, 1)
	go func() {
		done <- e.pipeline(3, 100).Run(context.Background(), backfill.Descending(20, 1, nil), writer.ModeInsert)
	}()

	require.Eventually(t, func() bool { _, in, _ := e.archive.stats(); return in == 3 }, 5*time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	_, in, maxIn := e.archive.stats()
	assert.Equal(t, 3, in, "no fourth worker while three are busy")
	assert.Equal(t, 3, maxIn)
	close(e.archive.gate)

	res := <-done
	require.True(t, res.Complete)
	_, _, maxIn = e.archive.stats()
	assert.Equal(t, 3, maxIn)
	assert.ElementsMatch(t, heights(20, 1), e.writer.written())
}

func TestFlushWhenTheBatchIsFull(t *testing.T) {
	e := newEnv()
	res := e.pipeline(1, 2).Run(context.Background(), backfill.Descending(5, 1, nil), writer.ModeInsert)

	require.True(t, res.Complete)
	assert.Equal(t, [][]int64{{5, 4}, {3, 2}, {1}}, e.writer.flushes(), "the last, partial batch is flushed on close")
}

func TestFlushAfterTheQuietPeriod(t *testing.T) {
	e := newEnv()
	e.writer.flushed = make(chan struct{}, 1)
	src := make(chanSource)
	done := make(chan backfill.Result, 1)
	go func() { done <- e.pipeline(2, 10).Run(context.Background(), src, writer.ModeInsert) }()

	src <- 7
	src <- 6
	require.Eventually(t, func() bool { return e.clock.armed() == 2 }, 5*time.Second, time.Millisecond)
	assert.Empty(t, e.writer.flushes(), "nothing flushes before the quiet period")
	require.True(t, e.clock.fireLatest())
	<-e.writer.flushed
	require.Len(t, e.writer.flushes(), 1)
	assert.ElementsMatch(t, []int64{7, 6}, e.writer.flushes()[0])

	src <- 5
	close(src)
	res := <-done
	require.True(t, res.Complete)
	require.Len(t, e.writer.flushes(), 2)
	assert.Equal(t, []int64{5}, e.writer.flushes()[1])
}

func TestAStaleQuietTimerDoesNotFlushEarly(t *testing.T) {
	e := newEnv()
	e.writer.flushed = make(chan struct{}, 1)
	src := make(chanSource)
	done := make(chan backfill.Result, 1)
	go func() { done <- e.pipeline(1, 10).Run(context.Background(), src, writer.ModeInsert) }()

	src <- 3
	require.Eventually(t, func() bool { return e.clock.armed() == 1 }, 5*time.Second, time.Millisecond)
	src <- 2
	require.Eventually(t, func() bool { return e.clock.armed() == 2 }, 5*time.Second, time.Millisecond)

	// The first timer was replaced by the second arrival.
	e.clock.mu.Lock()
	e.clock.timers[0] <- time.Now()
	e.clock.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	assert.Empty(t, e.writer.flushes())

	close(src)
	<-done
	assert.Equal(t, [][]int64{{3, 2}}, e.writer.flushes())
}

func TestCancelledRunStillWritesWhatItFetched(t *testing.T) {
	e := newEnv()
	src := make(chanSource)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan backfill.Result, 1)
	go func() { done <- e.pipeline(2, 10).Run(ctx, src, writer.ModeInsert) }()

	src <- 9
	src <- 8
	require.Eventually(t, func() bool { return e.clock.armed() == 2 }, 5*time.Second, time.Millisecond)
	cancel()

	res := <-done
	require.NoError(t, res.Err)
	assert.False(t, res.Complete)
	require.Len(t, e.writer.flushes(), 1)
	assert.ElementsMatch(t, []int64{9, 8}, e.writer.flushes()[0])
}

func TestAFailingHeightIsRetriedOnTheRetryNodeThenRecorded(t *testing.T) {
	e := newEnv()
	e.archive.fail(3, errUnavailable)
	e.retry.fail(3, errUnavailable, errUnavailable, errUnavailable)

	res := e.pipeline(2, 2).Run(context.Background(), backfill.Descending(5, 1, nil), writer.ModeInsert)

	require.NoError(t, res.Err)
	assert.True(t, res.Complete)
	assert.ElementsMatch(t, []int64{5, 4, 2, 1}, e.writer.written())
	require.Len(t, e.writer.recorded(), 1)
	f := e.writer.recorded()[0]
	assert.Equal(t, "3/"+writer.FailureSourceBackfill+"/4", describe(f))
	assert.ErrorIs(t, f.err, errUnavailable)
	assert.Equal(t, int64(1), res.Failed)
	assert.Equal(t, int64(4), res.Written)
	assert.Equal(t, backfill.DefaultRetryDelays, e.clock.recordedSleeps())
	assert.Equal(t, []int64{3, 3, 3}, e.retry.blockCalls(), "three retries, all on the retry node")
}

func TestARetryOnTheRetryNodeSucceeds(t *testing.T) {
	e := newEnv()
	e.archive.fail(2, errUnavailable)

	res := e.pipeline(1, 10).Run(context.Background(), backfill.Descending(2, 1, nil), writer.ModeInsert)

	require.True(t, res.Complete)
	assert.Equal(t, []int64{2, 1}, e.writer.written())
	assert.Empty(t, e.writer.recorded())
	assert.Equal(t, []int64{2}, e.retry.blockCalls())
}

func TestTheRateLimiterPacesEveryRequest(t *testing.T) {
	e := newEnv()
	e.archive.fail(4, errUnavailable)

	res := e.pipeline(3, 10).Run(context.Background(), backfill.Descending(5, 1, nil), writer.ModeInsert)

	require.True(t, res.Complete)
	archive, _, _ := e.archive.stats()
	retry, _, _ := e.retry.stats()
	assert.Equal(t, 5*3+1, archive+retry, "three routes per height plus the failed block call")
	assert.Equal(t, archive+retry, e.limiter.count(), "one wait per request")
}

func TestAWriteErrorIsFatal(t *testing.T) {
	e := newEnv()
	e.writer.writeErr = errors.New("database unavailable")

	res := e.pipeline(1, 2).Run(context.Background(), backfill.Descending(100, 1, nil), writer.ModeInsert)

	require.ErrorContains(t, res.Err, "database unavailable")
	assert.False(t, res.Complete)
	assert.Less(t, res.Dispatched, int64(100), "dispatching stops")
	assert.Zero(t, res.Written)
}

func TestAPresenceErrorIsFatal(t *testing.T) {
	e := newEnv()
	store := newMockStore(0)
	store.presenceErr = errors.New("database unavailable")

	res := e.pipeline(1, 2).Run(context.Background(), backfill.Descending(10, 1, store), writer.ModeInsert)

	require.ErrorContains(t, res.Err, "database unavailable")
	assert.False(t, res.Complete)
}

func TestEveryFlushIsWrittenInTheRunsMode(t *testing.T) {
	for _, mode := range []writer.Mode{writer.ModeInsert, writer.ModeUpdate} {
		e := newEnv()
		res := e.pipeline(1, 2).Run(context.Background(), backfill.Descending(5, 1, nil), mode)
		require.NoError(t, res.Err)
		e.writer.mu.Lock()
		assert.Equal(t, []writer.Mode{mode, mode, mode}, e.writer.modes, "%s", mode)
		e.writer.mu.Unlock()
	}
}
