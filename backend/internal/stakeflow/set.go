package stakeflow

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// StoredMessage is a message whose stored events carry a movement.
type StoredMessage struct {
	Ref    Ref
	Events json.RawMessage
}

// Snapshot is what the load reads, in one consistent view: the indexed
// spans from the last height before the window up to the newest, and the
// messages of the window that carry a movement.
type Snapshot struct {
	Spans    []Span
	Messages []StoredMessage
}

// Store reads the snapshot the set starts from.
type Store interface {
	// Load returns the spans and messages of window back from the newest
	// indexed block; an empty snapshot when nothing is indexed.
	Load(ctx context.Context, window time.Duration) (*Snapshot, error)
}

// Change is a validator's net stake change (bond denom, signed) over the
// last Day and the last Window.
type Change struct {
	Day  int64
	Week int64
}

// Changes are every validator's net changes. A validator with no movement in
// the window is left out.
type Changes struct {
	// Complete: every height of the window is indexed.
	Complete bool
	// AsOfHeight is the newest indexed height, 0 before the load.
	AsOfHeight int64
	Items      map[string]Change
}

// Page is one page of a validator's movements, newest first.
type Page struct {
	Complete bool
	// WindowFrom is the window's start; zero before the load.
	WindowFrom time.Time
	// Total is the validator's movements in the window.
	Total int
	Items []Movement
}

// Config tunes the set; the zero value is production.
type Config struct {
	// LoadRetry is the wait between failed loads; zero means 5 s.
	LoadRetry time.Duration
}

// Deps are the set's collaborators.
type Deps struct {
	Store Store
	// Log receives the load failures; nil is the standard logger.
	Log log.FieldLogger
}

// flow is one validator's movements, oldest first, with its running totals.
type flow struct {
	moves []Movement
	// day is the index of the first movement inside the last Day.
	day       int
	dayNet    int64
	windowNet int64
}

// Set is the in-memory movement set. It is safe for concurrent use: the
// writers feed it from their hooks while the API reads it.
type Set struct {
	cfg  Config
	deps Deps

	mu     sync.RWMutex
	loaded bool
	// pending holds the flushes seen before the load finished.
	pending []flushed
	cov     coverage
	// anchor is the newest indexed block's time.
	anchor time.Time
	flows  map[string]*flow
}

type flushed struct {
	blocks    []Block
	movements []Movement
}

// New returns an empty set; Load (or Run) fills it.
func New(cfg Config, deps Deps) *Set {
	if cfg.LoadRetry <= 0 {
		cfg.LoadRetry = 5 * time.Second
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	return &Set{cfg: cfg, deps: deps, flows: map[string]*flow{}}
}

// Run loads the set, retrying until it succeeds, and then waits for ctx; it
// always returns nil. The hooks keep it current meanwhile.
func (s *Set) Run(ctx context.Context) error {
	for {
		err := s.Load(ctx)
		if err == nil || ctx.Err() != nil {
			break
		}
		s.deps.Log.WithError(err).Warn("stake movements: load failed, retrying")
		t := time.NewTimer(s.cfg.LoadRetry)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-t.C:
		}
	}
	<-ctx.Done()
	return nil
}

// Load reads the window from the store and then applies every flush seen
// since the set was built. Heights both in the snapshot and in a flush count
// once.
func (s *Set) Load(ctx context.Context) error {
	snap, err := s.deps.Store.Load(ctx, Window)
	if err != nil {
		return fmt.Errorf("stake movements: %w", err)
	}
	var moves []Movement
	for _, m := range snap.Messages {
		var events []transform.Event
		if err := json.Unmarshal(m.Events, &events); err != nil {
			return fmt.Errorf("stake movements: events of %d/%d/%d: %w", m.Ref.Height, m.Ref.TxIndex, m.Ref.MsgIndex, err)
		}
		moves = append(moves, FromEvents(m.Ref, events)...)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}
	for _, sp := range snap.Spans {
		s.cov.addSpan(sp)
	}
	if top, ok := s.cov.top(); ok {
		s.advance(top.HiTime)
	}
	for _, m := range moves {
		s.insert(m)
	}
	s.loaded = true
	for _, f := range s.pending {
		s.apply(f.blocks, f.movements)
	}
	s.pending = nil
	return nil
}

// Observe applies the blocks and movements of one flush. Heights already
// applied are skipped, so a flush retried after a rollback counts once.
func (s *Set) Observe(ents *transform.Entities) {
	blocks, moves := FromEntities(ents)
	if len(blocks) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		s.pending = append(s.pending, flushed{blocks: blocks, movements: moves})
		return
	}
	s.apply(blocks, moves)
}

// PostFlush is the writers' hook: it hands every flush to Observe. It runs
// inside the flush's transaction and never fails it.
func (s *Set) PostFlush(_ context.Context, _ pgx.Tx, f *writer.Flush) error {
	s.Observe(f.Entities)
	return nil
}

func (s *Set) apply(blocks []Block, moves []Movement) {
	fresh := make(map[int64]bool, len(blocks))
	newest := s.anchor
	for _, b := range blocks {
		if s.cov.has(b.Height) {
			continue
		}
		fresh[b.Height] = true
		s.cov.add(b.Height, b.Time)
		if b.Time.After(newest) {
			newest = b.Time
		}
	}
	s.advance(newest)
	for _, m := range moves {
		if fresh[m.Height] {
			s.insert(m)
		}
	}
}

// advance moves the anchor to t (never back) and ages the movements out:
// out of the day total first, out of the set once older than Window.
func (s *Set) advance(t time.Time) {
	if !t.After(s.anchor) {
		return
	}
	s.anchor = t
	dayCut, windowCut := t.Add(-Day), t.Add(-Window)
	for v, f := range s.flows {
		for f.day < len(f.moves) && f.moves[f.day].Time.Before(dayCut) {
			f.dayNet -= f.moves[f.day].Delta()
			f.day++
		}
		n := 0
		for n < f.day && f.moves[n].Time.Before(windowCut) {
			f.windowNet -= f.moves[n].Delta()
			n++
		}
		if n > 0 {
			f.moves = slices.Delete(f.moves, 0, n)
			f.day -= n
		}
		if len(f.moves) == 0 {
			delete(s.flows, v)
		}
	}
	s.cov.prune(windowCut)
}

// insert adds a movement in key order; one older than the window is
// dropped. Block times never decrease with height, so a movement before the
// day cut always lands before the day index.
func (s *Set) insert(m Movement) {
	if m.Time.Before(s.anchor.Add(-Window)) {
		return
	}
	f := s.flows[m.Validator]
	if f == nil {
		f = &flow{}
		s.flows[m.Validator] = f
	}
	i, _ := slices.BinarySearchFunc(f.moves, m, compare)
	f.moves = slices.Insert(f.moves, i, m)
	f.windowNet += m.Delta()
	if m.Time.Before(s.anchor.Add(-Day)) {
		f.day++
	} else {
		f.dayNet += m.Delta()
	}
}

func compare(a, b Movement) int {
	return cmp.Or(
		cmp.Compare(a.Height, b.Height),
		cmp.Compare(a.TxIndex, b.TxIndex),
		cmp.Compare(a.MsgIndex, b.MsgIndex),
		cmp.Compare(a.Seq, b.Seq),
		cmp.Compare(a.Kind, b.Kind),
	)
}

func (s *Set) complete() bool {
	return s.loaded && s.cov.complete(s.anchor.Add(-Window))
}

// Changes returns every validator's net changes.
func (s *Set) Changes() Changes {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := Changes{Complete: s.complete(), Items: make(map[string]Change, len(s.flows))}
	if !s.loaded {
		return out
	}
	if top, ok := s.cov.top(); ok {
		out.AsOfHeight = top.Hi
	}
	for v, f := range s.flows {
		out.Items[v] = Change{Day: f.dayNet, Week: f.windowNet}
	}
	return out
}

// Movements returns limit movements of validator, newest first, after
// skipping offset of them.
func (s *Set) Movements(validator string, offset, limit int) Page {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := Page{Complete: s.complete(), Items: []Movement{}}
	if !s.loaded {
		return out
	}
	if !s.anchor.IsZero() {
		out.WindowFrom = s.anchor.Add(-Window)
	}
	f := s.flows[validator]
	if f == nil {
		return out
	}
	out.Total = len(f.moves)
	for i := len(f.moves) - 1 - offset; i >= 0 && len(out.Items) < limit; i-- {
		out.Items = append(out.Items, f.moves[i])
	}
	return out
}
