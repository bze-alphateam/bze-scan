// Package snapshots reads state the chain no longer has, or that no event
// carries, from the archive node at the height it describes, and writes it
// over what the database holds: the turnout of a resolved proposal, the
// votes and deposits the gov module deleted once it tallied.
//
// The snapshots command runs it by hand, never serve. A kind of snapshot
// lists its targets and snapshots one target in one transaction; a snapshot
// never deletes a row and writes the same values again on every run, so
// after a fix one more run repairs history.
package snapshots

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	gogoproto "github.com/cosmos/gogoproto/proto"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultWorkers is how many targets are snapshotted at once.
const DefaultWorkers = 4

// JobPrefix prefixes the kind's name in sync_jobs (snapshot:proposals).
const JobPrefix = "snapshot:"

// Reader reads the archive node at past heights.
type Reader interface {
	// Query calls the gRPC method path (/<service>/<Method>) at height
	// through abci_query.
	Query(ctx context.Context, path string, req, resp gogoproto.Message, height int64) error
	// Status is the archive's /status.
	Status(ctx context.Context) (*node.Status, error)
	// Block is the block at height.
	Block(ctx context.Context, height int64) (*node.Block, error)
	// BlockResults are the results of the block at height.
	BlockResults(ctx context.Context, height int64) (*node.BlockResults, error)
}

// Target is one thing a kind snapshots.
type Target struct {
	// ID names the target in the logs and in --id (a proposal id).
	ID string
	// Value is the kind's own description of the target.
	Value any
}

// Kind is one kind of snapshot.
type Kind interface {
	// Name is the kind's name (--kind, sync_jobs snapshot:<name>).
	Name() string
	// Targets lists what to snapshot: every target, or those of ids.
	Targets(ctx context.Context, ids []string) ([]Target, error)
	// Snapshot reads target from r and writes it in one transaction; the
	// summary goes to the target's log line.
	Snapshot(ctx context.Context, r Reader, target Target) (summary string, err error)
}

// Selection narrows a run: no kinds means every kind, no ids every target.
type Selection struct {
	Kinds []string
	IDs   []string
}

// Result of a run.
type Result struct {
	// Targets is how many targets were snapshotted or tried.
	Targets int
	// Failed lists "<kind> <id>" of every failed target ("<kind>" when the
	// kind could not list its targets).
	Failed []string
}

// Summary is the run's one-line report.
func (r Result) Summary() string {
	return fmt.Sprintf("snapshots: %d targets, %d failed", r.Targets, len(r.Failed))
}

// Deps of the runner.
type Deps struct {
	Kinds  []Kind
	Reader Reader
	Jobs   statesync.JobStore
	Log    log.FieldLogger
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// Runner runs kinds of snapshot.
type Runner struct {
	deps    Deps
	workers int
}

// New returns a runner snapshotting workers targets at once (DefaultWorkers
// when not positive).
func New(deps Deps, workers int) *Runner {
	if workers <= 0 {
		workers = DefaultWorkers
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	return &Runner{deps: deps, workers: workers}
}

// Names lists the kinds' names.
func (r *Runner) Names() []string {
	names := make([]string, 0, len(r.deps.Kinds))
	for _, k := range r.deps.Kinds {
		names = append(names, k.Name())
	}
	return names
}

// Run snapshots the selection. A failed target is logged and counted, and
// the others go on; the error is for an unknown kind, a sync_jobs write or
// a cancelled ctx.
func (r *Runner) Run(ctx context.Context, sel Selection) (Result, error) {
	kinds := r.deps.Kinds
	if len(sel.Kinds) > 0 {
		kinds = nil
		for _, name := range sel.Kinds {
			i := slices.IndexFunc(r.deps.Kinds, func(k Kind) bool { return k.Name() == name })
			if i < 0 {
				return Result{}, fmt.Errorf("snapshots: unknown kind %q (kinds: %s)", name, strings.Join(r.Names(), ", "))
			}
			kinds = append(kinds, r.deps.Kinds[i])
		}
	}
	var res Result
	for _, k := range kinds {
		if err := r.runKind(ctx, k, sel.IDs, &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (r *Runner) runKind(ctx context.Context, k Kind, ids []string, res *Result) error {
	started := r.deps.Now()
	logger := r.deps.Log.WithField("kind", k.Name())
	targets, err := k.Targets(ctx, ids)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		logger.WithError(err).Error("snapshot: listing the targets failed")
		res.Failed = append(res.Failed, k.Name())
		return r.record(ctx, k, started, err)
	}

	var (
		mu     sync.Mutex
		failed []string
		wg     sync.WaitGroup
	)
	sem := make(chan struct{}, r.workers)
dispatch:
	for _, t := range targets {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break dispatch
		}
		wg.Go(func() {
			defer func() { <-sem }()
			summary, err := k.Snapshot(ctx, r.deps.Reader, t)
			entry := logger.WithField("id", t.ID)
			if err != nil {
				entry.WithError(err).Error("snapshot failed")
				mu.Lock()
				failed = append(failed, t.ID)
				mu.Unlock()
				return
			}
			entry.Info("snapshot: " + summary)
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	slices.Sort(failed)
	res.Targets += len(targets)
	for _, id := range failed {
		res.Failed = append(res.Failed, k.Name()+" "+id)
	}
	var runErr error
	if len(failed) > 0 {
		runErr = fmt.Errorf("%d of %d targets failed: %s", len(failed), len(targets), strings.Join(failed, ", "))
	}
	return r.record(ctx, k, started, runErr)
}

func (r *Runner) record(ctx context.Context, k Kind, started time.Time, runErr error) error {
	return r.deps.Jobs.RecordRun(ctx, statesync.Run{
		Job: JobPrefix + k.Name(), Started: started, Finished: r.deps.Now(), Err: runErr,
	})
}
