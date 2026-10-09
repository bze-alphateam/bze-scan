// Package reindex is the repair tool: it runs chosen heights through the
// backfill pipeline again in writer.ModeUpdate, so whatever is there is
// overwritten with what the transformer produces today. It fills the holes
// an outage or a failed height left, and re-runs a fixed transformer over a
// range.
//
// Each height is read from the local node when the node still has it (at or
// above its earliest_block_height), else from the archive. Progress is
// tracked in a backfill_checkpoints row of its own, failures go to
// index_failures under the reindex source, and the open failures of every
// height written are resolved.
package reindex

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/backfill"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// JobPrefix starts the backfill_checkpoints name of a reindex run, followed
// by its start time in RFC 3339.
const JobPrefix = "reindex-"

// Store is the reindex's database access.
type Store interface {
	// UnresolvedFailures lists the heights of the index_failures rows with
	// no resolved_at, highest first, narrowed to source unless it is empty.
	UnresolvedFailures(ctx context.Context, source string) ([]int64, error)
	// SaveCheckpoint upserts the run's backfill_checkpoints row.
	SaveCheckpoint(ctx context.Context, cp backfill.Checkpoint) error
}

// LocalNode is the local node: fetched by height like the archive, and
// asked once for the lowest height it still has. *node.Client satisfies it.
type LocalNode interface {
	backfill.Node
	Status(ctx context.Context) (*node.Status, []byte, error)
}

// Deps are the reindex's collaborators. Pipeline.Archive and
// Pipeline.ArchiveRetry are the archive endpoints; the reindex routes each
// height between them and Local.
type Deps struct {
	Store Store
	// Local is the local node; nil reads every height from the archive.
	Local    LocalNode
	Pipeline backfill.Deps
	// Now is the clock of the run's name and duration; nil is time.Now.
	Now func() time.Time
}

// Job reindexes plans. It is reusable.
type Job struct {
	cfg  backfill.Config
	deps Deps
}

// New returns a reindex job; cfg tunes its pipeline (its FailureSource and
// OnFlush are the reindex's own).
func New(cfg backfill.Config, deps Deps) *Job {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Pipeline.Log == nil {
		deps.Pipeline.Log = log.StandardLogger()
	}
	if deps.Pipeline.ArchiveRetry == nil {
		deps.Pipeline.ArchiveRetry = deps.Pipeline.Archive
	}
	return &Job{cfg: cfg, deps: deps}
}

// Plan resolves a selection to heights: --failed reads index_failures.
func (j *Job) Plan(ctx context.Context, sel Selection) (Plan, error) {
	switch sel.Kind {
	case KindHeights:
		return Plan{List: sel.Heights}, nil
	case KindRange:
		return Plan{From: sel.From, To: sel.To, isRange: true}, nil
	case KindFailed:
		hs, err := j.deps.Store.UnresolvedFailures(ctx, sel.Source)
		if err != nil {
			return Plan{}, err
		}
		return Plan{List: hs}, nil
	default:
		return Plan{}, fmt.Errorf("%w: no selector", ErrSelection)
	}
}

// Result is the outcome of a run.
type Result struct {
	// Job is the run's backfill_checkpoints name; empty when nothing was
	// planned.
	Job string
	// Heights planned, Written by a flush, and Failed (recorded in
	// index_failures), ascending.
	Heights, Written int64
	Failed           []int64
	Duration         time.Duration
}

// Summary is the run's final line.
func (r Result) Summary() string {
	return fmt.Sprintf("reindex done heights=%d failed=%d duration=%s",
		r.Heights, len(r.Failed), r.Duration.Round(time.Millisecond))
}

// ErrInterrupted: the run was cancelled before every height was handled.
var ErrInterrupted = errors.New("reindex interrupted")

// Run reindexes every height of p. A height that fails after its retries is
// recorded and listed in Result.Failed; that is not an error. The error is a
// fatal one (the database failed) or ErrInterrupted, and the checkpoint row
// then ends in error; otherwise it ends in done.
func (j *Job) Run(ctx context.Context, p Plan) (Result, error) {
	start := j.deps.Now()
	res := Result{Heights: p.Count()}
	if p.Count() == 0 {
		return res, nil
	}
	lg := j.deps.Pipeline.Log
	res.Job = JobPrefix + start.UTC().Format(time.RFC3339)

	earliest := j.earliest(ctx)
	failures := &failureRecorder{Writer: j.deps.Pipeline.Writer}
	deps := j.deps.Pipeline
	deps.Archive = &routed{local: j.deps.Local, remote: deps.Archive, earliest: earliest}
	deps.ArchiveRetry = &routed{local: j.deps.Local, remote: deps.ArchiveRetry, earliest: earliest}
	deps.Writer = failures

	cp := backfill.Checkpoint{
		Job: res.Job, Ceiling: p.Highest(), Floor: p.Lowest(),
		LowestDispatched: p.Highest() + 1, Status: backfill.StatusRunning,
	}
	if err := j.deps.Store.SaveCheckpoint(ctx, cp); err != nil {
		return res, err
	}
	lg.WithFields(log.Fields{"job": res.Job, "heights": p.Count(), "lowest": p.Lowest(), "highest": p.Highest(),
		"local_from": earliest}).Info("reindex started")

	// Saves come from the writer goroutine only; the mutex orders the final
	// save after the last of them.
	var mu sync.Mutex
	save := func(pr backfill.Progress) {
		mu.Lock()
		defer mu.Unlock()
		if pr.Dispatched > 0 {
			cp.LowestDispatched = min(cp.LowestDispatched, pr.Lowest)
		}
		cp.BlocksDone = pr.Written
		if err := j.deps.Store.SaveCheckpoint(context.WithoutCancel(ctx), cp); err != nil {
			lg.WithError(err).Warn("reindex: saving the checkpoint")
		}
	}
	cfg := j.cfg
	cfg.FailureSource = writer.FailureSourceReindex
	cfg.OnFlush = save

	out := backfill.New(cfg, deps).Run(ctx, p.source(), writer.ModeUpdate)
	save(out.Progress)
	res.Written = out.Written
	res.Failed = failures.heights()
	res.Duration = j.deps.Now().Sub(start)

	mu.Lock()
	defer mu.Unlock()
	var runErr error
	switch {
	case out.Err != nil:
		runErr = out.Err
	case !out.Complete:
		runErr = ErrInterrupted
		if cause := context.Cause(ctx); cause != nil {
			runErr = fmt.Errorf("%w: %w", ErrInterrupted, cause)
		}
	default:
		cp.Status, cp.LowestDispatched = backfill.StatusDone, p.Lowest()
	}
	if runErr != nil {
		cp.Status, cp.LastError = backfill.StatusError, runErr.Error()
	}
	if err := j.deps.Store.SaveCheckpoint(context.WithoutCancel(ctx), cp); err != nil {
		return res, errors.Join(runErr, err)
	}
	return res, runErr
}

// earliest is the lowest height the local node still has. Without a local
// node, or when it does not answer, every height goes to the archive.
func (j *Job) earliest(ctx context.Context) int64 {
	if j.deps.Local == nil {
		return math.MaxInt64
	}
	st, _, err := j.deps.Local.Status(ctx)
	if err != nil {
		j.deps.Pipeline.Log.WithError(err).Warn("reindex: the local node did not answer /status, reading every height from the archive")
		return math.MaxInt64
	}
	return max(st.EarliestBlockHeight, 1)
}

// routed reads a height from the local node when it has it, else from
// remote. A height the local node pruned meanwhile goes to remote too.
type routed struct {
	local    backfill.Node
	remote   backfill.Node
	earliest int64
}

func via[T any](r *routed, h int64, call func(backfill.Node) (T, []byte, error)) (T, []byte, error) {
	if r.local == nil || h < r.earliest {
		return call(r.remote)
	}
	v, raw, err := call(r.local)
	if errors.Is(err, node.ErrPruned) {
		return call(r.remote)
	}
	return v, raw, err
}

func (r *routed) Block(ctx context.Context, h int64) (*node.Block, []byte, error) {
	return via(r, h, func(n backfill.Node) (*node.Block, []byte, error) { return n.Block(ctx, h) })
}

func (r *routed) BlockResults(ctx context.Context, h int64) (*node.BlockResults, []byte, error) {
	return via(r, h, func(n backfill.Node) (*node.BlockResults, []byte, error) { return n.BlockResults(ctx, h) })
}

func (r *routed) Commit(ctx context.Context, h int64) (*node.Commit, []byte, error) {
	return via(r, h, func(n backfill.Node) (*node.Commit, []byte, error) { return n.Commit(ctx, h) })
}

// failureRecorder passes everything to the writer and remembers the heights
// recorded as failed.
type failureRecorder struct {
	backfill.Writer

	mu     sync.Mutex
	failed []int64
}

func (f *failureRecorder) RecordFailure(ctx context.Context, height int64, source string, attempts int, cause error) error {
	if err := f.Writer.RecordFailure(ctx, height, source, attempts, cause); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = append(f.failed, height)
	return nil
}

func (f *failureRecorder) heights() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := slices.Clone(f.failed)
	slices.Sort(out)
	return out
}
