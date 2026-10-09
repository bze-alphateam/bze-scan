// Package statesync keeps the explorer's current-state tables (validators,
// proposals, denoms, channels, parameters…) fresh from the local node's
// gRPC. Each domain registers a Set; the Syncer resyncs every set in full at
// start, then resyncs the keys the live indexer publishes after each block
// (the Dirty set the transformer fills) through a coalescing queue served by
// a few workers, with a per-set timer as the safety net. Every run is
// recorded in explorer.sync_jobs.
//
// Nothing here is on the user's critical path: a node that is down fails the
// runs, which are logged and recorded, the rows stay as they were, and the
// next trigger retries.
package statesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Set names: the keys of a Dirty and the sync_jobs rows of the sets.
const (
	Validators = "validators"
	Proposals  = "proposals"
	Denoms     = "denoms"
	Channels   = "channels"
	Params     = "params"
	// ChainRegistry is the Cosmos chain registry cache; its keys are chain
	// ids, or RegistryNameKey of a registry directory.
	ChainRegistry = "chain_registry"
	Holders       = "holders"
	Prices        = "prices"
)

// registryNamePrefix starts the chain_registry keys that name a chain by its
// registry directory (the chain_name of an asset's trace) instead of its
// chain id.
const registryNamePrefix = "name:"

// RegistryNameKey is the chain_registry key of the chain whose registry
// name (directory) is name.
func RegistryNameKey(name string) string {
	if name == "" {
		return ""
	}
	return registryNamePrefix + name
}

// RegistryName returns the name of a key made by RegistryNameKey, and false
// for a chain id.
func RegistryName(key string) (string, bool) {
	return strings.CutPrefix(key, registryNamePrefix)
}

// Publisher queues the keys of a Dirty; *Syncer satisfies it. Sets that ask
// other sets for a resync (a denom whose registry asset is missing) hold
// one.
type Publisher interface {
	Publish(d Dirty)
}

// Deferred is a Publisher bound after construction: the sets are built
// before the Syncer that runs them. Until Bind it drops what it gets.
type Deferred struct {
	mu sync.RWMutex
	to Publisher
}

// Bind forwards every later Publish to p.
func (d *Deferred) Bind(p Publisher) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.to = p
}

// Publish forwards dirty to the bound Publisher.
func (d *Deferred) Publish(dirty Dirty) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.to != nil {
		d.to.Publish(dirty)
	}
}

// loggedError is a failure its set already logged.
type loggedError struct{ err error }

func (e loggedError) Error() string { return e.err.Error() }
func (e loggedError) Unwrap() error { return e.err }

// Logged marks err as already logged by the set that returns it: the run is
// still recorded as failed in sync_jobs, but the Syncer does not log it
// again (a set polled every minute logs its failures once per change of
// state, not once per run).
func Logged(err error) error {
	if err == nil {
		return nil
	}
	return loggedError{err: err}
}

// All is the key that asks for a full resync of a set: what a block changed
// is known to touch the set but not which entry (a slash names a consensus
// address, a validator leaving the active set names a key).
const All = "*"

// consPrefix starts the keys of the validators set that name a validator by
// its consensus address: the slash and liveness events and the validator
// updates of a block carry no operator address, and the transformer has no
// table to look one up in.
const consPrefix = "cons:"

// ConsKey is the validators-set key of the validator whose consensus
// address is hexAddr (upper-case hex, as /block reports the proposer).
func ConsKey(hexAddr string) string {
	if hexAddr == "" {
		return ""
	}
	return consPrefix + strings.ToUpper(hexAddr)
}

// ConsAddress returns the consensus address of a key made by ConsKey, and
// false for any other key.
func ConsAddress(key string) (string, bool) {
	return strings.CutPrefix(key, consPrefix)
}

// Dirty is the current state one block changed: per set name, the keys to
// resync (a validator's operator address, a proposal id, a denom…). The zero
// value is empty and ready to use.
type Dirty map[string]map[string]struct{}

// Mark notes key of set.
func (d *Dirty) Mark(set, key string) {
	if key == "" {
		return
	}
	if *d == nil {
		*d = Dirty{}
	}
	if (*d)[set] == nil {
		(*d)[set] = map[string]struct{}{}
	}
	(*d)[set][key] = struct{}{}
}

// Keys returns the keys of set, sorted.
func (d Dirty) Keys(set string) []string {
	out := make([]string, 0, len(d[set]))
	for k := range d[set] {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Empty reports whether no key is marked.
func (d Dirty) Empty() bool {
	for _, keys := range d {
		if len(keys) > 0 {
			return false
		}
	}
	return true
}

// Set is one synced table (or group of tables).
type Set interface {
	// Name is the set's name: its Dirty key and its sync_jobs row.
	Name() string
	// Interval is the safety-net period: a full resync runs when none ran
	// for this long.
	Interval() time.Duration
	// FullResync rewrites the whole set from the node.
	FullResync(ctx context.Context) error
	// ResyncOne rewrites the entry key (never All, which is a FullResync).
	ResyncOne(ctx context.Context, key string) error
}

// Canonicaliser is implemented by sets whose entries have several keys (a
// validator: its operator address and its consensus address). Publish folds
// every key into the one Canonical returns before queueing, so one entry
// marked under two keys is resynced once. An empty key drops it: the set
// knows the entry needs no resync (a denom seen in a transfer that the set
// already holds).
type Canonicaliser interface {
	Canonical(key string) string
}

// Cursorer is implemented by sets that keep a position across runs (a denom
// being paginated, chain ids queued for a refetch); it is stored in
// sync_jobs.cursor after each run.
type Cursorer interface {
	Cursor() json.RawMessage
}

// Run is one finished run of a set, as sync_jobs records it.
type Run struct {
	Job      string
	Started  time.Time
	Finished time.Time
	// Err is nil on success.
	Err error
	// Cursor is nil when the set keeps none; the stored one stays then.
	Cursor json.RawMessage
}

// JobStore records runs in sync_jobs.
type JobStore interface {
	RecordRun(ctx context.Context, r Run) error
}

// Clock is the time source of the Syncer; tests swap it.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// DefaultWorkers serve the queue.
const DefaultWorkers = 2

// Config of a Syncer. Zero values take the defaults.
type Config struct {
	Workers int
	Clock   Clock
	Log     log.FieldLogger
}

// job is one queued resync: a key of a set, or All.
type job struct {
	set string
	key string
}

// Syncer runs the registered sets.
type Syncer struct {
	cfg  Config
	sets []Set
	byID map[string]Set
	jobs JobStore

	mu     sync.Mutex
	queue  []job
	queued map[job]bool
	wake   chan struct{}
	// reset restarts a set's safety-net timer after a full resync.
	reset map[string]chan struct{}
}

// New returns a syncer over sets, in registration order, recording runs in
// jobs.
func New(cfg Config, jobs JobStore, sets ...Set) *Syncer {
	if cfg.Workers <= 0 {
		cfg.Workers = DefaultWorkers
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	if cfg.Log == nil {
		cfg.Log = log.StandardLogger()
	}
	s := &Syncer{
		cfg: cfg, sets: sets, byID: map[string]Set{}, jobs: jobs,
		queued: map[job]bool{}, wake: make(chan struct{}, 1), reset: map[string]chan struct{}{},
	}
	for _, set := range sets {
		s.byID[set.Name()] = set
		s.reset[set.Name()] = make(chan struct{}, 1)
	}
	return s
}

// Publish queues the keys of d for the registered sets; keys of other sets
// are dropped, and so are keys a Canonicaliser folds to "". It never blocks:
// a key already queued is not queued again, and a set with a full resync
// queued needs none of its keys.
func (s *Syncer) Publish(d Dirty) {
	for _, set := range s.sets {
		for _, k := range d.Keys(set.Name()) {
			if j := s.canonical(job{set: set.Name(), key: k}); j.key != "" {
				s.enqueue(j)
			}
		}
	}
}

// canonical folds the key of j when its set is a Canonicaliser.
func (s *Syncer) canonical(j job) job {
	if c, ok := s.byID[j.set].(Canonicaliser); ok && j.key != All {
		j.key = c.Canonical(j.key)
	}
	return j
}

// refold folds the queued keys again and drops the duplicates: keys
// published before the start resyncs taught the sets their aliases.
func (s *Syncer) refold() {
	s.mu.Lock()
	defer s.mu.Unlock()
	queue := s.queue[:0:0]
	queued := map[job]bool{}
	for _, j := range s.queue {
		j = s.canonical(j)
		if j.key == "" || queued[j] || (j.key != All && queued[job{set: j.set, key: All}]) {
			continue
		}
		queued[j] = true
		queue = append(queue, j)
	}
	s.queue, s.queued = queue, queued
}

func (s *Syncer) enqueue(j job) {
	s.mu.Lock()
	if s.queued[j] || (j.key != All && s.queued[job{set: j.set, key: All}]) {
		s.mu.Unlock()
		return
	}
	s.queued[j] = true
	s.queue = append(s.queue, j)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// next pops the oldest queued job.
func (s *Syncer) next() (job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return job{}, false
	}
	j := s.queue[0]
	s.queue = s.queue[1:]
	delete(s.queued, j)
	return j, true
}

// Run resyncs every set in full, in registration order, then serves the
// queue and the safety-net timers until ctx is cancelled. Failures are
// logged and recorded, never returned: it returns nil on stop.
func (s *Syncer) Run(ctx context.Context) error {
	s.cfg.Log.WithField("sets", len(s.sets)).Info("state sync: full resync at start")
	for _, set := range s.sets {
		if ctx.Err() != nil {
			return nil
		}
		_ = s.run(ctx, job{set: set.Name(), key: All})
	}
	s.refold()

	var wg sync.WaitGroup
	for range s.cfg.Workers {
		wg.Go(func() { s.work(ctx) })
	}
	for _, set := range s.sets {
		wg.Go(func() { s.timer(ctx, set) })
	}
	wg.Wait()
	s.cfg.Log.Info("state sync stopped")
	return nil
}

// RunOnce resyncs every set in full once, in registration order (the
// sync-state command). It returns the failures joined.
func (s *Syncer) RunOnce(ctx context.Context) error {
	var errs []error
	for _, set := range s.sets {
		if err := s.run(ctx, job{set: set.Name(), key: All}); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", set.Name(), err))
		}
	}
	return errors.Join(errs...)
}

func (s *Syncer) work(ctx context.Context) {
	for {
		j, ok := s.next()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
				continue
			}
		}
		if ctx.Err() != nil {
			return
		}
		_ = s.run(ctx, j)
		// Another job may be waiting behind this one.
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

// timer queues a full resync of set when none finished for its interval.
func (s *Syncer) timer(ctx context.Context, set Set) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.reset[set.Name()]:
		case <-s.cfg.Clock.After(set.Interval()):
			s.enqueue(job{set: set.Name(), key: All})
		}
	}
}

// run runs one job and records it.
func (s *Syncer) run(ctx context.Context, j job) error {
	set := s.byID[j.set]
	started := s.cfg.Clock.Now()
	var err error
	if j.key == All {
		err = set.FullResync(ctx)
	} else {
		err = set.ResyncOne(ctx, j.key)
	}
	r := Run{Job: j.set, Started: started, Finished: s.cfg.Clock.Now(), Err: err}
	if c, ok := set.(Cursorer); ok {
		r.Cursor = c.Cursor()
	}
	lg := s.cfg.Log.WithFields(log.Fields{"set": j.set, "key": j.key, "took_ms": r.Finished.Sub(started).Milliseconds()})
	var logged loggedError
	switch {
	case errors.As(err, &logged):
		lg.WithError(err).Debug("state sync: resync failed, logged by the set")
	case err != nil:
		lg.WithError(err).Warn("state sync: resync failed, rows kept, retried on the next trigger")
	default:
		lg.Debug("state sync: resynced")
		if j.key == All {
			select {
			case s.reset[j.set] <- struct{}{}:
			default:
			}
		}
	}
	if rerr := s.jobs.RecordRun(context.WithoutCancel(ctx), r); rerr != nil {
		lg.WithError(rerr).Warn("state sync: recording the run in sync_jobs")
	}
	return err
}
