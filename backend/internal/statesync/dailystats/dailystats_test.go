package dailystats_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/dailystats"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// fakeStore holds the days with indexed blocks, the rows written and the
// backfill checkpoint; heights map to days through blockDay.
type fakeStore struct {
	oldest    *time.Time
	blockDays map[string]bool
	rows      map[string]bool
	lowest    *int64
	heightDay func(h int64) time.Time
	cursor    json.RawMessage
	failOn    string

	recomputed []string
}

func d(s string) time.Time {
	t, err := dailystats.ParseDay(s)
	if err != nil {
		panic(err)
	}
	return t
}

func (f *fakeStore) OldestDay(context.Context) (time.Time, bool, error) {
	if f.oldest == nil {
		return time.Time{}, false, nil
	}
	return *f.oldest, true, nil
}

func (f *fakeStore) StoredDays(_ context.Context, from, to time.Time) ([]time.Time, error) {
	var out []time.Time
	for day := range f.rows {
		if t := d(day); !t.Before(from) && !t.After(to) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeStore) LowestDispatched(context.Context) (int64, bool, error) {
	if f.lowest == nil {
		return 0, false, nil
	}
	return *f.lowest, true, nil
}

func (f *fakeStore) HeightDays(_ context.Context, lo, hi int64) (time.Time, time.Time, bool, error) {
	return f.heightDay(lo), f.heightDay(hi), true, nil
}

func (f *fakeStore) Recompute(_ context.Context, day time.Time) (bool, error) {
	s := day.Format(time.DateOnly)
	if s == f.failOn {
		return false, errors.New("db down")
	}
	f.recomputed = append(f.recomputed, s)
	if !f.blockDays[s] {
		return false, nil
	}
	if f.rows == nil {
		f.rows = map[string]bool{}
	}
	f.rows[s] = true
	return true, nil
}

func (f *fakeStore) Cursor(context.Context) (json.RawMessage, error) { return f.cursor, nil }

func ptr[T any](v T) *T { return &v }

// now is 2026-10-11 at 00:10 UTC: yesterday is 2026-10-10.
var now = time.Date(2026, 10, 11, 0, 10, 0, 0, time.UTC)

func newSet(st *fakeStore) *dailystats.Set {
	return dailystats.New(dailystats.Deps{Store: st, Clock: fixedClock{now}})
}

func days(ss ...string) map[string]bool {
	out := map[string]bool{}
	for _, s := range ss {
		out[s] = true
	}
	return out
}

func TestTheSetIsNamedDailyStats(t *testing.T) {
	assert.Equal(t, statesync.DailyStats, newSet(&fakeStore{}).Name())
}

// The Syncer asks the interval after every run: always until the next
// 00:10 UTC.
func TestIntervalRunsAtTenPastMidnightUTC(t *testing.T) {
	for at, want := range map[time.Time]time.Duration{
		time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC):                       10 * time.Minute,
		time.Date(2026, 10, 11, 0, 10, 0, 0, time.UTC):                      24 * time.Hour,
		time.Date(2026, 10, 11, 18, 30, 0, 0, time.UTC):                     5*time.Hour + 40*time.Minute,
		time.Date(2026, 10, 11, 2, 0, 0, 0, time.FixedZone("CEST", 2*3600)): 10 * time.Minute,
	} {
		s := dailystats.New(dailystats.Deps{Store: &fakeStore{}, Clock: fixedClock{at}})
		assert.Equal(t, want, s.Interval(), at.String())
	}
}

func TestNothingIndexedWritesNothing(t *testing.T) {
	st := &fakeStore{}
	require.NoError(t, newSet(st).FullResync(context.Background()))
	assert.Empty(t, st.recomputed)

	// Only today indexed: no closed day yet.
	st = &fakeStore{oldest: ptr(d("2026-10-11"))}
	require.NoError(t, newSet(st).FullResync(context.Background()))
	assert.Empty(t, st.recomputed)
}

// Every closed day without a row since the oldest block, plus the last
// three closed days whatever they hold; a day without blocks writes no row.
func TestMissingDaysAndTheLastThree(t *testing.T) {
	st := &fakeStore{
		oldest:    ptr(d("2026-10-01")),
		blockDays: days("2026-10-01", "2026-10-02", "2026-10-03", "2026-10-06", "2026-10-07", "2026-10-08", "2026-10-09", "2026-10-10", "2026-10-11"),
		rows:      days("2026-10-01", "2026-10-02", "2026-10-03", "2026-10-07", "2026-10-08", "2026-10-09"),
	}
	s := newSet(st)
	require.NoError(t, s.FullResync(context.Background()))
	assert.Equal(t, []string{"2026-10-04", "2026-10-05", "2026-10-06", "2026-10-08", "2026-10-09", "2026-10-10"},
		st.recomputed, "missing days, the last three, never today")
	assert.False(t, st.rows["2026-10-04"], "a day without blocks stays absent")
	assert.Nil(t, s.Cursor(), "no backfill seen: the stored cursor is kept")

	// The next night: the gap days are tried again, the rest is only the
	// last three.
	st.recomputed = nil
	require.NoError(t, s.FullResync(context.Background()))
	assert.Equal(t, []string{"2026-10-04", "2026-10-05", "2026-10-08", "2026-10-09", "2026-10-10"}, st.recomputed)
}

// The backfill walks down: the days between the checkpoint the previous run
// saw and the current one are recomputed, with the day above for the
// heights still in flight; the cursor then moves.
func TestTheBackfillWindow(t *testing.T) {
	// One day per 1,000 heights: height 0 is 2026-09-01.
	heightDay := func(h int64) time.Time { return d("2026-09-01").AddDate(0, 0, int(h/1000)) }
	st := &fakeStore{
		oldest:    ptr(d("2026-09-01")),
		blockDays: map[string]bool{},
		lowest:    ptr(int64(12_500)),
		heightDay: heightDay,
		cursor:    json.RawMessage(`{"lowest_dispatched":15500}`),
	}
	for i := range 41 {
		day := d("2026-09-01").AddDate(0, 0, i).Format(time.DateOnly)
		st.blockDays[day] = true
		if day != "2026-10-10" {
			if st.rows == nil {
				st.rows = map[string]bool{}
			}
			st.rows[day] = true
		}
	}
	s := newSet(st)
	require.NoError(t, s.FullResync(context.Background()))
	assert.Equal(t, []string{"2026-09-13", "2026-09-14", "2026-09-15", "2026-09-16", "2026-09-17",
		"2026-10-08", "2026-10-09", "2026-10-10"}, st.recomputed,
		"heights 12,500..15,500 span 09-13..09-16, plus the day above")
	assert.JSONEq(t, `{"lowest_dispatched":12500}`, string(s.Cursor()))

	// The backfill did not move: no window.
	st.recomputed = nil
	require.NoError(t, s.FullResync(context.Background()))
	assert.Equal(t, []string{"2026-10-08", "2026-10-09", "2026-10-10"}, st.recomputed)
}

func TestTheFirstRunOnlyRecordsTheBackfillPosition(t *testing.T) {
	st := &fakeStore{
		oldest: ptr(d("2026-10-09")), blockDays: days("2026-10-09", "2026-10-10"),
		lowest: ptr(int64(7)), cursor: json.RawMessage(`null`),
	}
	s := newSet(st)
	require.NoError(t, s.FullResync(context.Background()))
	assert.Equal(t, []string{"2026-10-09", "2026-10-10"}, st.recomputed, "no window before a position is known")
	assert.JSONEq(t, `{"lowest_dispatched":7}`, string(s.Cursor()))
}

// A failed day fails the run and keeps the cursor, so the window is
// retried.
func TestAFailedDayKeepsTheCursor(t *testing.T) {
	st := &fakeStore{
		oldest: ptr(d("2026-10-08")), blockDays: days("2026-10-08", "2026-10-09", "2026-10-10"),
		lowest: ptr(int64(10)), heightDay: func(int64) time.Time { return d("2026-10-08") },
		cursor: json.RawMessage(`{"lowest_dispatched":20}`), failOn: "2026-10-09",
	}
	s := dailystats.New(dailystats.Deps{Store: st, Clock: fixedClock{now}})
	err := s.ResyncOne(context.Background(), "")
	require.ErrorContains(t, err, "daily stats 2026-10-09: db down")
	assert.JSONEq(t, `{"lowest_dispatched":20}`, string(s.Cursor()))
}

// Recomputing is idempotent: a row is rewritten whole, so a second range
// writes the same days again.
func TestRange(t *testing.T) {
	st := &fakeStore{blockDays: days("2026-10-01", "2026-10-03")}
	s := newSet(st)
	n, err := s.Range(context.Background(), d("2026-10-01"), d("2026-10-03"))
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, []string{"2026-10-01", "2026-10-02", "2026-10-03"}, st.recomputed)

	n, err = s.Range(context.Background(), d("2026-10-01"), d("2026-10-03"))
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	_, err = s.Range(context.Background(), d("2026-10-03"), d("2026-10-01"))
	require.ErrorContains(t, err, "2026-10-03 is after 2026-10-01")
	_, err = s.Range(context.Background(), d("2026-10-10"), d("2026-10-11"))
	require.ErrorContains(t, err, "2026-10-11 is not a closed day")
}

func TestParseDay(t *testing.T) {
	got, err := dailystats.ParseDay("2026-02-28")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC), got)
	for _, bad := range []string{"", "2026-02-30", "28.02.2026", "2026-2-28"} {
		_, err := dailystats.ParseDay(bad)
		require.ErrorIs(t, err, dailystats.ErrNoDay, bad)
	}
}

func TestDayIsTheUTCDay(t *testing.T) {
	late := time.Date(2026, 10, 11, 1, 30, 0, 0, time.FixedZone("CEST", 2*3600))
	assert.Equal(t, d("2026-10-10"), dailystats.Day(late))
}

// row is a pgx.Row over fixed values.
type row struct{ vals []any }

func (r row) Scan(dest ...any) error {
	for i, v := range r.vals {
		switch p := dest[i].(type) {
		case *int64:
			*p = v.(int64)
		case *[]byte:
			*p = v.([]byte)
		case **int64:
			*p = v.(*int64)
		}
	}
	return nil
}

type querier struct {
	r    row
	args []any
}

func (q *querier) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	q.args = args
	return q.r
}

// Aggregate reads the window in UTC and decodes the per-denom fees.
func TestAggregate(t *testing.T) {
	q := &querier{r: row{vals: []any{int64(17280), int64(40), int64(3), int64(25), int64(2), int64(90),
		[]byte(`[{"denom":"ibc/AB","amount":"7"},{"denom":"ubze","amount":"1200000"}]`), ptr(int64(5001))}}}
	from := time.Date(2026, 10, 11, 2, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	got, err := dailystats.Aggregate(context.Background(), q, from, from.Add(24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, []any{from.UTC(), from.Add(24 * time.Hour).UTC()}, q.args)
	assert.Equal(t, dailystats.Stats{
		Day: d("2026-10-11"), Blocks: 17280, Txs: 40, TxsFailed: 3, ActiveAddresses: 25, NewAddresses: 2, Transfers: 90,
		Fees:           []chain.Coin{{Denom: "ibc/AB", Amount: "7"}, {Denom: "ubze", Amount: "1200000"}},
		AvgBlockTimeMs: ptr(int64(5001)),
	}, got)
}
