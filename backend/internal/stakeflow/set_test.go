package stakeflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/stakeflow"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

type fakeStore struct {
	mu     sync.Mutex
	snap   *stakeflow.Snapshot
	errs   []error
	calls  int
	window time.Duration
}

func (f *fakeStore) Load(_ context.Context, window time.Duration) (*stakeflow.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.window = window
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return nil, err
	}
	if f.snap == nil {
		return &stakeflow.Snapshot{}, nil
	}
	return f.snap, nil
}

// blockTime is the time of height h: one block every 6 s from height 1000
// at t0.
func blockTime(h int64) time.Time {
	return t0.Add(time.Duration(h-1000) * 6 * time.Second)
}

// heightAt is the first height at or after d past t0.
func heightAt(d time.Duration) int64 {
	return 1000 + int64((d+6*time.Second-1)/(6*time.Second))
}

func storedEvents(t *testing.T, events ...transform.Event) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(events)
	require.NoError(t, err)
	return b
}

// flush is a flush of one block at h whose single message carries events.
func flush(h int64, events ...transform.Event) *transform.Entities {
	ents := &transform.Entities{Blocks: []transform.Block{{Height: h, Time: blockTime(h)}}}
	if len(events) > 0 {
		ents.Transactions = []transform.Transaction{{Height: h, Hash: "TX", Time: blockTime(h), Success: true}}
		ents.Messages = []transform.Message{{Height: h, Events: events}}
	}
	return ents
}

func delegate(val, amount string) transform.Event {
	return ev("delegate", "validator", val, "delegator", del1, "amount", amount+"ubze")
}

func undelegate(val, amount string) transform.Event {
	return ev("unbond", "validator", val, "delegator", del1, "amount", amount+"ubze")
}

func loaded(t *testing.T, store *fakeStore) *stakeflow.Set {
	t.Helper()
	s := stakeflow.New(stakeflow.Config{}, stakeflow.Deps{Store: store})
	require.NoError(t, s.Load(context.Background()))
	return s
}

func TestEmptyBeforeTheLoad(t *testing.T) {
	s := stakeflow.New(stakeflow.Config{}, stakeflow.Deps{Store: &fakeStore{}})
	s.Observe(flush(1000, delegate(valA, "5")))

	assert.Equal(t, stakeflow.Changes{Items: map[string]stakeflow.Change{}}, s.Changes())
	assert.Equal(t, stakeflow.Page{Items: []stakeflow.Movement{}}, s.Movements(valA, 0, 10))
}

func TestLoadReadsTheSnapshotThenThePendingFlushes(t *testing.T) {
	store := &fakeStore{snap: &stakeflow.Snapshot{
		// Indexed from well before the window up to 1010.
		Spans: []stakeflow.Span{{Lo: -200000, Hi: 1010, LoTime: blockTime(-200000), HiTime: blockTime(1010)}},
		Messages: []stakeflow.StoredMessage{
			{Ref: stakeflow.Ref{Height: 1005, TxHash: "LOADED", Time: blockTime(1005)},
				Events: storedEvents(t, delegate(valA, "100"))},
		},
	}}
	s := stakeflow.New(stakeflow.Config{}, stakeflow.Deps{Store: store})
	// Seen while loading: 1010 is in the snapshot too and counts once, 1011
	// is new.
	s.Observe(flush(1010, delegate(valA, "1000")))
	s.Observe(flush(1011, undelegate(valA, "30")))

	require.NoError(t, s.Load(context.Background()))

	assert.Equal(t, stakeflow.Window, store.window)
	ch := s.Changes()
	assert.True(t, ch.Complete)
	assert.Equal(t, int64(1011), ch.AsOfHeight)
	assert.Equal(t, map[string]stakeflow.Change{valA: {Day: 70, Week: 70}}, ch.Items)
	page := s.Movements(valA, 0, 10)
	assert.Equal(t, 2, page.Total)
	assert.Equal(t, []string{stakeflow.KindUndelegate, stakeflow.KindDelegate},
		[]string{page.Items[0].Kind, page.Items[1].Kind}, "newest first")
	assert.Equal(t, "LOADED", page.Items[1].TxHash)
	assert.Equal(t, blockTime(1011).Add(-stakeflow.Window), page.WindowFrom)
}

func TestLoadFailureIsReturnedAndRunRetries(t *testing.T) {
	store := &fakeStore{errs: []error{errors.New("db down")}}
	s := stakeflow.New(stakeflow.Config{LoadRetry: time.Millisecond}, stakeflow.Deps{Store: store})
	require.Error(t, s.Load(context.Background()))

	store.errs = []error{errors.New("db down")}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.calls == 3
	}, time.Second, time.Millisecond)
	cancel()
	assert.NoError(t, <-done)
}

func TestMovementsLeaveTheDayThenTheWindow(t *testing.T) {
	s := loaded(t, &fakeStore{})
	s.Observe(flush(1000, delegate(valA, "100")))
	s.Observe(flush(1001, undelegate(valB, "7")))
	s.Observe(flush(1002, delegate(valA, "10")))
	assert.Equal(t, map[string]stakeflow.Change{valA: {Day: 110, Week: 110}, valB: {Day: -7, Week: -7}}, s.Changes().Items)

	// More than 24 h after 1000 (but not after 1001; the window keeps its
	// start): 1000 leaves the day only.
	s.Observe(flush(heightAt(stakeflow.Day) + 1))
	assert.Equal(t, map[string]stakeflow.Change{valA: {Day: 10, Week: 110}, valB: {Day: -7, Week: -7}}, s.Changes().Items)

	// Past 24 h of 1002: every day total is 0, the week keeps them.
	s.Observe(flush(heightAt(stakeflow.Day) + 3))
	assert.Equal(t, map[string]stakeflow.Change{valA: {Day: 0, Week: 110}, valB: {Day: 0, Week: -7}}, s.Changes().Items)
	assert.Equal(t, 2, s.Movements(valA, 0, 10).Total)

	// 7 days after 1001: 1000 and 1001 are gone, valB with them.
	s.Observe(flush(heightAt(stakeflow.Window) + 2))
	assert.Equal(t, map[string]stakeflow.Change{valA: {Day: 0, Week: 10}}, s.Changes().Items)
	page := s.Movements(valA, 0, 10)
	require.Equal(t, 1, page.Total)
	assert.Equal(t, int64(1002), page.Items[0].Height)
	assert.Equal(t, 0, s.Movements(valB, 0, 10).Total)

	// And past 7 days of 1002 the set is empty.
	s.Observe(flush(heightAt(stakeflow.Window) + 3))
	assert.Empty(t, s.Changes().Items)
}

func TestOlderFlushesLandInPlace(t *testing.T) {
	s := loaded(t, &fakeStore{})
	day := heightAt(stakeflow.Day)
	s.Observe(flush(day+10, delegate(valA, "1")))
	// A catch-up writes older heights after the newest: one inside the day,
	// one inside the window only, one older than the window.
	s.Observe(flush(day+5, delegate(valA, "20")))
	s.Observe(flush(1005, delegate(valA, "300")))
	s.Observe(flush(heightAt(stakeflow.Day-stakeflow.Window-time.Hour), delegate(valA, "4000")))

	assert.Equal(t, stakeflow.Change{Day: 21, Week: 321}, s.Changes().Items[valA])
	page := s.Movements(valA, 0, 10)
	assert.Equal(t, []int64{day + 10, day + 5, 1005}, []int64{page.Items[0].Height, page.Items[1].Height, page.Items[2].Height})

	// The day index stays right: 1005 never re-enters the day total.
	s.Observe(flush(day + 20))
	assert.Equal(t, stakeflow.Change{Day: 21, Week: 321}, s.Changes().Items[valA])
}

func TestAFlushSeenTwiceCountsOnce(t *testing.T) {
	s := loaded(t, &fakeStore{})
	f := flush(1000, delegate(valA, "5"))
	s.Observe(f)
	// A rolled-back write runs its hooks again on the retry.
	require.NoError(t, s.PostFlush(context.Background(), nil, &writer.Flush{Entities: f}))

	assert.Equal(t, stakeflow.Change{Day: 5, Week: 5}, s.Changes().Items[valA])
	assert.Equal(t, 1, s.Movements(valA, 0, 10).Total)
}

func TestCompleteOnceTheWindowIsIndexed(t *testing.T) {
	// A fresh start: the explorer begins at 1000, younger than the window.
	s := loaded(t, &fakeStore{snap: &stakeflow.Snapshot{
		Spans: []stakeflow.Span{{Lo: 1000, Hi: 1100, LoTime: blockTime(1000), HiTime: blockTime(1100)}},
	}})
	assert.False(t, s.Changes().Complete)

	// The backfill reaches past the window's start, but leaves a hole.
	start := 1100 - int64(stakeflow.Window/(6*time.Second))
	s.Observe(&transform.Entities{Blocks: []transform.Block{
		{Height: start - 2, Time: blockTime(start - 2)},
		{Height: start - 1, Time: blockTime(start - 1)},
	}})
	for h := start + 1; h < 1000; h++ {
		s.Observe(flush(h))
	}
	assert.False(t, s.Changes().Complete, "height %d is missing", start)
	assert.False(t, s.Movements(valA, 0, 1).Complete)

	s.Observe(flush(start))
	assert.True(t, s.Changes().Complete)
	assert.True(t, s.Movements(valA, 0, 1).Complete)

	// A gap at the top, while the catch-up is behind, is incomplete too.
	s.Observe(flush(1102))
	assert.False(t, s.Changes().Complete)
	s.Observe(flush(1101))
	assert.True(t, s.Changes().Complete)
}

func TestMovementsPages(t *testing.T) {
	s := loaded(t, &fakeStore{})
	for h := int64(1000); h < 1025; h++ {
		s.Observe(flush(h, delegate(valA, "1")))
	}

	page := s.Movements(valA, 10, 10)
	assert.Equal(t, 25, page.Total)
	require.Len(t, page.Items, 10)
	assert.Equal(t, int64(1014), page.Items[0].Height)
	assert.Equal(t, int64(1005), page.Items[9].Height)

	last := s.Movements(valA, 20, 10)
	assert.Len(t, last.Items, 5)
	assert.Empty(t, s.Movements(valA, 30, 10).Items)
	assert.Empty(t, s.Movements(valB, 0, 10).Items)
}

func TestConcurrentReadsAndWrites(t *testing.T) {
	s := loaded(t, &fakeStore{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for h := int64(1000); h < 1500; h++ {
			s.Observe(flush(h, delegate(valA, "1")))
		}
	}()
	go func() {
		defer wg.Done()
		for range 500 {
			_ = s.Changes()
			_ = s.Movements(valA, 0, 10)
		}
	}()
	wg.Wait()
	assert.Equal(t, int64(500), s.Changes().Items[valA].Week)
}

func TestLoadedSpansJoinTheFlushesBetweenThem(t *testing.T) {
	old := 1000 - int64(stakeflow.Window/(6*time.Second)) - 10
	s := loaded(t, &fakeStore{snap: &stakeflow.Snapshot{Spans: []stakeflow.Span{
		{Lo: old, Hi: 999, LoTime: blockTime(old), HiTime: blockTime(999)},
		{Lo: 1001, Hi: 1050, LoTime: blockTime(1001), HiTime: blockTime(1050)},
	}}})
	assert.False(t, s.Changes().Complete)

	s.Observe(flush(1000))
	assert.True(t, s.Changes().Complete)
	assert.Equal(t, int64(1050), s.Changes().AsOfHeight)
}
