package statesync_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// recorder is shared by the fake sets: it logs every call in order.
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, s)
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func (r *recorder) count(s string) int {
	n := 0
	for _, c := range r.list() {
		if c == s {
			n++
		}
	}
	return n
}

// fakeSet records its runs; a key in gate blocks until the channel closes.
type fakeSet struct {
	name     string
	interval time.Duration
	rec      *recorder
	gate     map[string]chan struct{}
	started  chan string
	err      error
	cursor   json.RawMessage
}

func (s *fakeSet) Name() string            { return s.name }
func (s *fakeSet) Interval() time.Duration { return s.interval }

func (s *fakeSet) FullResync(ctx context.Context) error {
	s.rec.add(s.name + ":" + statesync.All)
	return s.err
}

func (s *fakeSet) ResyncOne(ctx context.Context, key string) error {
	if s.started != nil {
		s.started <- key
	}
	if g, ok := s.gate[key]; ok {
		<-g
	}
	s.rec.add(s.name + ":" + key)
	return s.err
}

type cursorSet struct{ *fakeSet }

func (s cursorSet) Cursor() json.RawMessage { return s.cursor }

// fakeJobs records the runs.
type fakeJobs struct {
	mu   sync.Mutex
	runs []statesync.Run
	err  error
}

func (j *fakeJobs) RecordRun(_ context.Context, r statesync.Run) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.runs = append(j.runs, r)
	return j.err
}

func (j *fakeJobs) list() []statesync.Run {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]statesync.Run(nil), j.runs...)
}

// fakeClock hands out the timers it creates through afters.
type fakeClock struct {
	now    time.Time
	afters chan chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC), afters: make(chan chan time.Time, 100)}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) After(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	c.afters <- ch
	return ch
}

// latest returns the last timer created once no new one appears for a while.
func (c *fakeClock) latest(t *testing.T) chan time.Time {
	t.Helper()
	var last chan time.Time
	for {
		select {
		case ch := <-c.afters:
			last = ch
		case <-time.After(100 * time.Millisecond):
			require.NotNil(t, last, "no timer was created")
			return last
		}
	}
}

func quietLog() logrus.FieldLogger {
	l, _ := logtest.NewNullLogger()
	return l
}

func eventually(t *testing.T, cond func() bool, what string) {
	t.Helper()
	require.Eventually(t, cond, 5*time.Second, 5*time.Millisecond, what)
}

// start runs s until the test ends.
func start(t *testing.T, s *statesync.Syncer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("syncer did not stop")
		}
	})
}

func TestDirty(t *testing.T) {
	var d statesync.Dirty
	assert.True(t, d.Empty())
	d.Mark(statesync.Validators, "b")
	d.Mark(statesync.Validators, "a")
	d.Mark(statesync.Validators, "b")
	d.Mark(statesync.Validators, "")
	d.Mark(statesync.Denoms, "ubze")
	assert.False(t, d.Empty())
	assert.Equal(t, []string{"a", "b"}, d.Keys(statesync.Validators))
	assert.Equal(t, []string{"ubze"}, d.Keys(statesync.Denoms))
	assert.Empty(t, d.Keys(statesync.Proposals))
}

func TestFullResyncOfEverySetAtStartComesFirstAndInOrder(t *testing.T) {
	rec := &recorder{}
	a := &fakeSet{name: "a", interval: time.Hour, rec: rec}
	b := &fakeSet{name: "b", interval: time.Hour, rec: rec}
	c := &fakeSet{name: "c", interval: time.Hour, rec: rec}
	s := statesync.New(statesync.Config{Log: quietLog(), Clock: newFakeClock()}, &fakeJobs{}, a, b, c)

	// Published before the start: served after the full resyncs.
	var d statesync.Dirty
	d.Mark("b", "k1")
	d.Mark("unknown", "k2")
	s.Publish(d)
	start(t, s)

	eventually(t, func() bool { return len(rec.list()) == 4 }, "four runs")
	assert.Equal(t, []string{"a:*", "b:*", "c:*", "b:k1"}, rec.list(), "keys of unregistered sets are dropped")
}

func TestQueueCoalescesKeys(t *testing.T) {
	rec := &recorder{}
	gate := make(chan struct{})
	set := &fakeSet{name: "v", interval: time.Hour, rec: rec, gate: map[string]chan struct{}{"busy": gate}, started: make(chan string, 100)}
	s := statesync.New(statesync.Config{Workers: 1, Log: quietLog(), Clock: newFakeClock()}, &fakeJobs{}, set)
	start(t, s)
	eventually(t, func() bool { return rec.count("v:*") == 1 }, "the start resync")

	// The only worker is held by "busy" while the keys queue up.
	var busy statesync.Dirty
	busy.Mark("v", "busy")
	s.Publish(busy)
	require.Equal(t, "busy", <-set.started)

	var d statesync.Dirty
	for _, k := range []string{"a", "b", "c"} {
		d.Mark("v", k)
	}
	s.Publish(d)
	s.Publish(d) // every key is already queued
	var again statesync.Dirty
	again.Mark("v", "a")
	s.Publish(again)
	close(gate)

	eventually(t, func() bool { return len(rec.list()) == 5 }, "busy, a, b and c")
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, []string{"v:*", "v:busy", "v:a", "v:b", "v:c"}, rec.list(), "one run per key")
}

func TestQueuedFullResyncAbsorbsKeys(t *testing.T) {
	rec := &recorder{}
	gate := make(chan struct{})
	set := &fakeSet{name: "v", interval: time.Hour, rec: rec, gate: map[string]chan struct{}{"busy": gate}, started: make(chan string, 100)}
	s := statesync.New(statesync.Config{Workers: 1, Log: quietLog(), Clock: newFakeClock()}, &fakeJobs{}, set)
	start(t, s)
	eventually(t, func() bool { return rec.count("v:*") == 1 }, "the start resync")

	var busy statesync.Dirty
	busy.Mark("v", "busy")
	s.Publish(busy)
	<-set.started

	var all statesync.Dirty
	all.Mark("v", statesync.All)
	s.Publish(all)
	var key statesync.Dirty
	key.Mark("v", "a")
	s.Publish(key)
	close(gate)

	eventually(t, func() bool { return rec.count("v:*") == 2 }, "the queued full resync")
	time.Sleep(50 * time.Millisecond)
	assert.Zero(t, rec.count("v:a"), "a full resync was queued: the key needs no run of its own")
}

func TestTimerIsDebouncedByFullResyncs(t *testing.T) {
	rec := &recorder{}
	clock := newFakeClock()
	set := &fakeSet{name: "v", interval: time.Minute, rec: rec}
	s := statesync.New(statesync.Config{Log: quietLog(), Clock: clock}, &fakeJobs{}, set)
	start(t, s)
	eventually(t, func() bool { return rec.count("v:*") == 1 }, "the start resync")
	stale := clock.latest(t)

	// A full resync from a block restarts the wait: the timer created
	// before it no longer fires anything.
	var all statesync.Dirty
	all.Mark("v", statesync.All)
	s.Publish(all)
	eventually(t, func() bool { return rec.count("v:*") == 2 }, "the published full resync")
	fresh := clock.latest(t)
	require.NotEqual(t, stale, fresh)
	stale <- time.Now()
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 2, rec.count("v:*"), "the stale timer is ignored")

	// A single-key resync does not restart it; the timer fires a full one.
	var key statesync.Dirty
	key.Mark("v", "a")
	s.Publish(key)
	eventually(t, func() bool { return rec.count("v:a") == 1 }, "the key")
	fresh <- time.Now()
	eventually(t, func() bool { return rec.count("v:*") == 3 }, "the timer's full resync")
}

func TestRunsAreRecordedInSyncJobs(t *testing.T) {
	rec := &recorder{}
	jobs := &fakeJobs{err: errors.New("db down")} // a bookkeeping failure is only logged
	ok := &fakeSet{name: "ok", interval: time.Hour, rec: rec}
	failing := cursorSet{&fakeSet{name: "failing", interval: time.Hour, rec: rec,
		err: errors.New("node down"), cursor: json.RawMessage(`{"page":2}`)}}
	clock := newFakeClock()
	s := statesync.New(statesync.Config{Log: quietLog(), Clock: clock}, jobs, ok, failing)
	start(t, s)
	eventually(t, func() bool { return len(jobs.list()) == 2 }, "two runs recorded")

	runs := jobs.list()
	assert.Equal(t, statesync.Run{Job: "ok", Started: clock.now, Finished: clock.now}, runs[0])
	assert.Equal(t, "failing", runs[1].Job)
	assert.EqualError(t, runs[1].Err, "node down")
	assert.JSONEq(t, `{"page":2}`, string(runs[1].Cursor))

	// The failure is retried on the next trigger.
	var d statesync.Dirty
	d.Mark("failing", "k")
	s.Publish(d)
	eventually(t, func() bool { return rec.count("failing:k") == 1 }, "the retry")
}

func TestRunOnceRunsEverySetAndJoinsTheFailures(t *testing.T) {
	rec := &recorder{}
	jobs := &fakeJobs{}
	a := &fakeSet{name: "a", interval: time.Hour, rec: rec, err: errors.New("node down")}
	b := &fakeSet{name: "b", interval: time.Hour, rec: rec}
	s := statesync.New(statesync.Config{Log: quietLog()}, jobs, a, b)

	err := s.RunOnce(context.Background())
	require.EqualError(t, err, "a: node down")
	assert.Equal(t, []string{"a:*", "b:*"}, rec.list(), "a failure does not stop the others")
	require.Len(t, jobs.list(), 2)

	a.err = nil
	require.NoError(t, s.RunOnce(context.Background()))
}
