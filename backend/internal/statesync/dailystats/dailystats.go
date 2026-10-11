// Package dailystats is the daily_stats set of the state sync: the per-day
// numbers of the home charts, aggregated from the indexed tables once a
// day is closed. The indexer never touches daily_stats (one hot row per day
// would serialise every flush and break idempotency); only this set writes
// it, and every write recomputes a whole day, so any run can be repeated.
//
// A run, at start and every night at 00:10 UTC, recomputes:
//   - every closed day from the oldest indexed block's day to yesterday
//     that has no row yet;
//   - the last three closed days, always (the live indexer may still have
//     been catching up when a day closed);
//   - the days of the heights the backfill wrote since the previous run:
//     the backfill walks downward, so they lie between the main job's
//     lowest_dispatched now and the one the previous run saw (the set's
//     cursor), plus the day above to cover the heights still in flight.
//
// Days are UTC, bounded on blocks.time. A day with no indexed block gets no
// row: the series shows a gap, never a zero.
package dailystats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// RunAt is the time of day (UTC) of the nightly run.
const RunAt = 10 * time.Minute

// Corrected is how many of the latest closed days every run recomputes.
const Corrected = 3

// day is the length of a UTC day.
const day = 24 * time.Hour

// Clock is the set's time source; tests swap it.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Store reads the indexed tables and writes daily_stats.
type Store interface {
	// OldestDay returns the UTC day of the oldest indexed block; false when
	// no block is indexed.
	OldestDay(ctx context.Context) (time.Time, bool, error)
	// StoredDays returns the days in [from, to] that have a row.
	StoredDays(ctx context.Context, from, to time.Time) ([]time.Time, error)
	// LowestDispatched returns the main backfill job's lowest_dispatched;
	// false when the backfill never ran.
	LowestDispatched(ctx context.Context) (int64, bool, error)
	// HeightDays returns the UTC days of the lowest indexed block at or
	// above lo and of the highest at or below hi; false when no block in
	// [lo, hi] is indexed.
	HeightDays(ctx context.Context, lo, hi int64) (first, last time.Time, ok bool, err error)
	// Recompute aggregates the day and upserts its row; false when the day
	// has no indexed block, and no row was written.
	Recompute(ctx context.Context, day time.Time) (bool, error)
	// Cursor returns the set's stored cursor; nil when there is none.
	Cursor(ctx context.Context) (json.RawMessage, error)
}

// Deps of the set.
type Deps struct {
	Store Store
	// Clock defaults to the wall clock.
	Clock Clock
	// Log defaults to the standard logger.
	Log log.FieldLogger
}

// cursor is the backfill position the previous successful run saw.
type cursor struct {
	LowestDispatched int64 `json:"lowest_dispatched"`
}

// Set is the daily_stats set.
type Set struct {
	deps Deps
	mu   sync.Mutex

	curMu  sync.Mutex
	cur    *cursor
	loaded bool
}

// New returns the set.
func New(deps Deps) *Set {
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	return &Set{deps: deps}
}

var (
	_ statesync.Set      = (*Set)(nil)
	_ statesync.Cursorer = (*Set)(nil)
)

// Name is statesync.DailyStats.
func (s *Set) Name() string { return statesync.DailyStats }

// Interval is the time left until the next 00:10 UTC: the Syncer asks again
// after every run, so the set runs nightly whatever time it started at.
func (s *Set) Interval() time.Duration {
	now := s.deps.Clock.Now().UTC()
	next := Day(now).Add(RunAt)
	if !now.Before(next) {
		next = next.Add(day)
	}
	return next.Sub(now)
}

// Cursor is the backfill position the last successful run saw; nil before
// one saw the backfill, which keeps the stored one.
func (s *Set) Cursor() json.RawMessage {
	s.curMu.Lock()
	defer s.curMu.Unlock()
	if s.cur == nil {
		return nil
	}
	raw, _ := json.Marshal(s.cur) // an int64 always marshals
	return raw
}

// ResyncOne runs the nightly selection: no event marks the set.
func (s *Set) ResyncOne(ctx context.Context, _ string) error { return s.FullResync(ctx) }

// FullResync recomputes the days the nightly selection names (see the
// package comment). The cursor moves only when every day was written, so a
// failed run retries the same backfill window.
func (s *Set) FullResync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(ctx); err != nil {
		return err
	}
	days, lowest, err := s.selection(ctx)
	if err != nil {
		return err
	}
	if _, err := s.recompute(ctx, days); err != nil {
		return err
	}
	if lowest != nil {
		s.curMu.Lock()
		s.cur = &cursor{LowestDispatched: *lowest}
		s.curMu.Unlock()
	}
	return nil
}

// Range recomputes every day from from to to, both included (sync-state
// --from --to, after a reindex for example), and returns how many rows it
// wrote. Both must be closed days.
func (s *Set) Range(ctx context.Context, from, to time.Time) (int, error) {
	from, to = Day(from), Day(to)
	if to.Before(from) {
		return 0, fmt.Errorf("daily stats: %s is after %s", from.Format(time.DateOnly), to.Format(time.DateOnly))
	}
	if today := Day(s.deps.Clock.Now()); !to.Before(today) {
		return 0, fmt.Errorf("daily stats: %s is not a closed day (today is %s)",
			to.Format(time.DateOnly), today.Format(time.DateOnly))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recompute(ctx, span(from, to))
}

// selection returns the days to recompute, oldest first, and the backfill
// position to store once they are written (nil when the backfill never
// ran).
func (s *Set) selection(ctx context.Context) ([]time.Time, *int64, error) {
	yesterday := Day(s.deps.Clock.Now()).Add(-day)
	oldest, ok, err := s.deps.Store.OldestDay(ctx)
	if err != nil || !ok || oldest.After(yesterday) {
		return nil, nil, err
	}
	want := map[time.Time]bool{}

	// Missing days.
	stored, err := s.deps.Store.StoredDays(ctx, oldest, yesterday)
	if err != nil {
		return nil, nil, err
	}
	have := map[time.Time]bool{}
	for _, d := range stored {
		have[Day(d)] = true
	}
	for _, d := range span(oldest, yesterday) {
		if !have[d] {
			want[d] = true
		}
	}

	// The last closed days.
	for i := range Corrected {
		want[yesterday.Add(-time.Duration(i)*day)] = true
	}

	// The backfill window.
	lowest, ok, err := s.deps.Store.LowestDispatched(ctx)
	if err != nil {
		return nil, nil, err
	}
	var seen *int64
	if ok {
		seen = &lowest
		s.curMu.Lock()
		prev := s.cur
		s.curMu.Unlock()
		if prev != nil && lowest < prev.LowestDispatched {
			first, last, found, err := s.deps.Store.HeightDays(ctx, lowest, prev.LowestDispatched)
			if err != nil {
				return nil, nil, err
			}
			if found {
				for _, d := range span(first, last.Add(day)) {
					want[d] = true
				}
			}
		}
	}

	out := make([]time.Time, 0, len(want))
	for d := range want {
		if !d.Before(oldest) && !d.After(yesterday) {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return out, seen, nil
}

// recompute writes the days in order and returns how many got a row.
func (s *Set) recompute(ctx context.Context, days []time.Time) (int, error) {
	written := 0
	for _, d := range days {
		ok, err := s.deps.Store.Recompute(ctx, d)
		if err != nil {
			return written, fmt.Errorf("daily stats %s: %w", d.Format(time.DateOnly), err)
		}
		if ok {
			written++
		}
	}
	if written > 0 {
		s.deps.Log.WithFields(log.Fields{"days": written, "from": days[0].Format(time.DateOnly),
			"to": days[len(days)-1].Format(time.DateOnly)}).Debug("daily stats: recomputed")
	}
	return written, nil
}

// load reads the stored cursor once. A cursor that does not parse is
// dropped: the next run recomputes no backfill window, the missing days
// still cover the days never written.
func (s *Set) load(ctx context.Context) error {
	if s.loaded {
		return nil
	}
	raw, err := s.deps.Store.Cursor(ctx)
	if err != nil {
		return err
	}
	var c cursor
	s.curMu.Lock()
	if len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, &c) == nil && c.LowestDispatched > 0 {
		s.cur = &c
	}
	s.curMu.Unlock()
	s.loaded = true
	return nil
}

// Day is the UTC day holding t, as its midnight.
func Day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// span returns every day from from to to, both included.
func span(from, to time.Time) []time.Time {
	var out []time.Time
	for d := Day(from); !d.After(to); d = d.Add(day) {
		out = append(out, d)
	}
	return out
}

// ErrNoDay is returned by ParseDay for a value that is not YYYY-MM-DD.
var ErrNoDay = errors.New("not a day (YYYY-MM-DD)")

// ParseDay parses a YYYY-MM-DD day as UTC.
func ParseDay(s string) (time.Time, error) {
	t, err := time.ParseInLocation(time.DateOnly, s, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q: %w", s, ErrNoDay)
	}
	return t, nil
}
