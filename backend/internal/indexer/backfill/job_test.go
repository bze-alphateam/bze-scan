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

func (e *env) job(store *mockStore, locker *mockLocker, floor backfill.Floor, workers, batch int) *backfill.MainJob {
	return backfill.NewMainJob(backfill.JobConfig{Floor: floor, Pipeline: e.config(workers, batch)},
		backfill.JobDeps{Store: store, Locker: locker, Pipeline: e.deps()})
}

func TestResumeWindow(t *testing.T) {
	cases := []struct {
		name    string
		cp      backfill.Checkpoint
		has     bool
		ceiling int64
		want    int64
	}{
		{"first run starts at the ceiling", backfill.Checkpoint{}, false, 1000, 1000},
		{"X+M above the lowest dispatched", backfill.Checkpoint{LowestDispatched: 500}, true, 1000, 515},
		{"never above the ceiling", backfill.Checkpoint{LowestDispatched: 990}, true, 1000, 1000},
		{"nothing dispatched yet", backfill.Checkpoint{LowestDispatched: 1001}, true, 1000, 1000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, backfill.ResumeFrom(c.ceiling, c.cp, c.has, 10, 5))
		})
	}
}

func TestMainJobWalksFromTheCeilingToTheFloor(t *testing.T) {
	e := newEnv()
	store := newMockStore(101)
	locker := &mockLocker{}

	status, err := e.job(store, locker, backfill.Floor{Height: 90}, 1, 4).Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, backfill.StatusDone, status)
	assert.ElementsMatch(t, heights(100, 90), e.writer.written())
	assert.Equal(t, heights(100, 90), e.archive.blockCalls(), "the ceiling is live_floor-1")
	assert.NotContains(t, e.archive.blockCalls(), int64(101))
	assert.NotContains(t, e.archive.blockCalls(), int64(89))
	assert.Equal(t, backfill.Checkpoint{Job: backfill.JobMain, Ceiling: 100, Floor: 90, LowestDispatched: 90,
		BlocksDone: 11, Status: backfill.StatusDone}, store.last())
	assert.Equal(t, backfill.StatusRunning, store.saves[0].Status)
	assert.Equal(t, int64(101), store.saves[0].LowestDispatched, "nothing dispatched before the run")
	assert.Equal(t, 1, locker.released)
}

func TestMainJobWaitsForTheLiveFloor(t *testing.T) {
	e := newEnv()
	store := newMockStore(11)
	store.floorAfter = 2

	status, err := e.job(store, &mockLocker{}, backfill.Floor{Height: 1}, 2, 4).Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, backfill.StatusDone, status)
	assert.Equal(t, []time.Duration{backfill.DefaultPollInterval, backfill.DefaultPollInterval}, e.clock.recordedSleeps())
	assert.ElementsMatch(t, heights(10, 1), e.writer.written())
}

func TestMainJobResumesAboveTheCheckpointAndSkipsWhatIsPresent(t *testing.T) {
	e := newEnv()
	store := newMockStore(1001)
	store.checkpoint = &backfill.Checkpoint{Job: backfill.JobMain, Ceiling: 1000, Floor: 1,
		LowestDispatched: 500, BlocksDone: 495, Status: backfill.StatusRunning}
	for h := int64(503); h <= 1000; h++ {
		store.present[h] = true
	}

	status, err := e.job(store, &mockLocker{}, backfill.Floor{Height: 480}, 1, 4).Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, backfill.StatusDone, status)
	calls := e.archive.blockCalls()
	require.NotEmpty(t, calls)
	assert.Equal(t, int64(502), calls[0], "resume window 505..1000 (X=1, M=4) minus the present heights")
	assert.ElementsMatch(t, heights(502, 480), e.writer.written())
	assert.Equal(t, int64(495+23), store.last().BlocksDone)
}

func TestMainJobAlreadyDone(t *testing.T) {
	e := newEnv()
	store := newMockStore(1001)
	store.checkpoint = &backfill.Checkpoint{Job: backfill.JobMain, Ceiling: 1000, Floor: 1,
		LowestDispatched: 1, Status: backfill.StatusDone}

	status, err := e.job(store, &mockLocker{}, backfill.Floor{Height: 1}, 2, 3).Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, backfill.StatusDone, status)
	assert.Empty(t, e.archive.blockCalls())
}

func TestMainJobContinuesBelowAnEarlierFloor(t *testing.T) {
	e := newEnv()
	store := newMockStore(1001)
	store.checkpoint = &backfill.Checkpoint{Job: backfill.JobMain, Ceiling: 1000, Floor: 900,
		LowestDispatched: 900, Status: backfill.StatusDone}
	for h := int64(900); h <= 1000; h++ {
		store.present[h] = true
	}

	status, err := e.job(store, &mockLocker{}, backfill.Floor{Height: 895}, 1, 1).Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, backfill.StatusDone, status)
	assert.ElementsMatch(t, heights(899, 895), e.writer.written())
}

func TestMainJobWithTheFloorAboveTheCeilingIsDone(t *testing.T) {
	e := newEnv()
	store := newMockStore(100)

	status, err := e.job(store, &mockLocker{}, backfill.Floor{Height: 100}, 1, 1).Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, backfill.StatusDone, status)
	assert.Empty(t, e.archive.blockCalls())
	assert.Equal(t, backfill.StatusDone, store.last().Status)
}

func TestMainJobSkipsWhenLocked(t *testing.T) {
	e := newEnv()
	store := newMockStore(100)

	_, err := e.job(store, &mockLocker{held: true}, backfill.Floor{Height: 1}, 1, 1).Run(context.Background())

	require.ErrorIs(t, err, backfill.ErrLocked)
	assert.Empty(t, e.archive.blockCalls())
	assert.Nil(t, store.checkpoint)
}

func TestMainJobRecordsAFatalError(t *testing.T) {
	e := newEnv()
	e.writer.writeErr = errors.New("database unavailable")
	store := newMockStore(101)

	status, err := e.job(store, &mockLocker{}, backfill.Floor{Height: 1}, 1, 2).Run(context.Background())

	require.ErrorContains(t, err, "database unavailable")
	assert.Equal(t, backfill.StatusError, status)
	assert.Equal(t, backfill.StatusError, store.last().Status)
	assert.Contains(t, store.last().LastError, "database unavailable")
}

func TestMainJobStoppedByShutdownStaysRunning(t *testing.T) {
	e := newEnv()
	e.archive.gate = make(chan struct{})
	store := newMockStore(101)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var status string
	go func() {
		var err error
		status, err = e.job(store, &mockLocker{}, backfill.Floor{Height: 1}, 2, 2).Run(ctx)
		done <- err
	}()
	require.Eventually(t, func() bool { _, in, _ := e.archive.stats(); return in == 2 }, 5*time.Second, time.Millisecond)
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
	assert.Equal(t, backfill.StatusRunning, status)
	cp := store.last()
	assert.Equal(t, backfill.StatusRunning, cp.Status)
	assert.Equal(t, int64(99), cp.LowestDispatched)
}

func TestPause(t *testing.T) {
	store := newMockStore(0)
	require.NoError(t, backfill.Pause(context.Background(), store))
	assert.Nil(t, store.checkpoint, "no checkpoint, nothing to pause")

	store.checkpoint = &backfill.Checkpoint{Job: backfill.JobMain, Status: backfill.StatusRunning}
	require.NoError(t, backfill.Pause(context.Background(), store))
	assert.Equal(t, backfill.StatusPaused, store.last().Status)

	store.checkpoint = &backfill.Checkpoint{Job: backfill.JobMain, Status: backfill.StatusDone}
	require.NoError(t, backfill.Pause(context.Background(), store))
	assert.Equal(t, backfill.StatusDone, store.last().Status)
}

// mockCursor records the cursor moves.
type mockCursor struct{ moves []int64 }

func (c *mockCursor) AdvanceCursor(_ context.Context, h int64) error {
	c.moves = append(c.moves, h)
	return nil
}

func TestCatchUpWalksUpAndMovesTheCursor(t *testing.T) {
	e := newEnv()
	e.archive.fail(103, errUnavailable)
	e.retry.fail(103, errUnavailable, errUnavailable, errUnavailable)
	store := newMockStore(0)
	store.present[102] = true
	cursor := &mockCursor{}

	err := backfill.NewCatchUp(e.config(1, 10), e.deps(), store, cursor).CatchUp(context.Background(), 100, 105)

	require.NoError(t, err)
	assert.Equal(t, []int64{100, 101, 103, 104, 105}, e.archive.blockCalls(), "ascending, 102 skipped")
	assert.Equal(t, []int64{100, 101, 104, 105}, e.writer.written())
	require.Len(t, e.writer.recorded(), 1)
	assert.Equal(t, "103/"+writer.FailureSourceLive+"/4", describe(e.writer.recorded()[0]))
	assert.Equal(t, []int64{105}, cursor.moves)
}

func TestAFailedCatchUpLeavesTheCursor(t *testing.T) {
	e := newEnv()
	e.writer.writeErr = errors.New("database unavailable")
	cursor := &mockCursor{}

	err := backfill.NewCatchUp(e.config(1, 1), e.deps(), newMockStore(0), cursor).CatchUp(context.Background(), 100, 105)

	require.ErrorContains(t, err, "database unavailable")
	assert.Empty(t, cursor.moves)
}
