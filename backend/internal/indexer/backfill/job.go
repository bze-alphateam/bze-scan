package backfill

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// JobMain is the main job's name in backfill_checkpoints and its lock key.
const JobMain = "main"

// Checkpoint statuses.
const (
	StatusRunning = "running"
	StatusPaused  = "paused"
	StatusDone    = "done"
	StatusError   = "error"
)

// Defaults applied to the zero fields of JobConfig.
const (
	DefaultPollInterval       = 10 * time.Second
	DefaultCheckpointInterval = 5 * time.Second
)

// ErrLocked: another process runs this job against the database.
var ErrLocked = errors.New("another backfill holds the lock")

// Checkpoint is one backfill_checkpoints row.
type Checkpoint struct {
	Job     string
	Ceiling int64
	Floor   int64
	// LowestDispatched is the lowest height handed to a worker; Ceiling+1
	// before the first dispatch.
	LowestDispatched int64
	BlocksDone       int64
	Status           string
	LastError        string
}

// Store is the main job's database access.
type Store interface {
	Presence
	// LiveFloor is indexer_state.live_floor; false until the live indexer
	// has written its first block.
	LiveFloor(ctx context.Context) (int64, bool, error)
	// Checkpoint reads the job's row; false when there is none.
	Checkpoint(ctx context.Context, job string) (Checkpoint, bool, error)
	// SaveCheckpoint upserts the job's row.
	SaveCheckpoint(ctx context.Context, cp Checkpoint) error
}

// Locker takes the per-database lock of a job.
type Locker interface {
	// TryLock takes the job's lock without waiting: ok false when another
	// session holds it. release gives it back.
	TryLock(ctx context.Context, job string) (release func(), ok bool, err error)
}

// JobConfig tunes the main job.
type JobConfig struct {
	Floor    Floor
	Pipeline Config
	// PollInterval is the wait between reads of the live floor on a fresh
	// install.
	PollInterval time.Duration
	// CheckpointInterval is the period of checkpoint saves during a run
	// (each flush saves too).
	CheckpointInterval time.Duration
}

// JobDeps are the main job's collaborators.
type JobDeps struct {
	Store    Store
	Locker   Locker
	Pipeline Deps
}

// MainJob backfills from the live floor down to the configured floor.
type MainJob struct {
	cfg  JobConfig
	deps JobDeps
}

// NewMainJob returns the main job.
func NewMainJob(cfg JobConfig, deps JobDeps) *MainJob {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	if cfg.CheckpointInterval <= 0 {
		cfg.CheckpointInterval = DefaultCheckpointInterval
	}
	if cfg.Pipeline.Workers <= 0 {
		cfg.Pipeline.Workers = DefaultWorkers
	}
	if cfg.Pipeline.Batch <= 0 {
		cfg.Pipeline.Batch = DefaultBatch
	}
	if cfg.Pipeline.Sleep == nil {
		cfg.Pipeline.Sleep = sleep
	}
	if deps.Pipeline.Log == nil {
		deps.Pipeline.Log = log.StandardLogger()
	}
	return &MainJob{cfg: cfg, deps: deps}
}

// ResumeFrom is the height a run starts at: the ceiling on a first run,
// otherwise workers+batch heights above the checkpoint's lowest dispatched
// height (the heights that may have been in flight or held unflushed when
// the previous run stopped), never above the ceiling.
func ResumeFrom(ceiling int64, cp Checkpoint, hasCheckpoint bool, workers, batch int) int64 {
	if !hasCheckpoint {
		return ceiling
	}
	return min(ceiling, cp.LowestDispatched+int64(workers)+int64(batch))
}

// Run takes the lock, waits for the live floor, and walks down from the
// resume height to the floor, skipping the heights already present. It
// returns the checkpoint status it ended with: done (nil error), running when
// ctx was cancelled (ctx's error), error on a fatal error. ErrLocked means
// another process runs the job; nothing was done.
func (j *MainJob) Run(ctx context.Context) (string, error) {
	release, ok, err := j.deps.Locker.TryLock(ctx, JobMain)
	if err != nil {
		return StatusError, fmt.Errorf("backfill lock: %w", err)
	}
	if !ok {
		return "", ErrLocked
	}
	defer release()
	lg := j.deps.Pipeline.Log

	ceiling, err := j.waitCeiling(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return StatusRunning, ctx.Err()
		}
		return StatusError, err
	}
	floor, err := ResolveFloor(ctx, j.cfg.Floor, j.deps.Pipeline.Archive, j.deps.Pipeline.ArchiveRetry, ceiling)
	if err != nil {
		return j.fail(ctx, Checkpoint{Job: JobMain, Ceiling: ceiling, LowestDispatched: ceiling + 1}, err)
	}

	prev, has, err := j.deps.Store.Checkpoint(ctx, JobMain)
	if err != nil {
		return StatusError, err
	}
	cp := Checkpoint{Job: JobMain, Ceiling: ceiling, Floor: floor, LowestDispatched: ceiling + 1, Status: StatusRunning}
	if has {
		cp.LowestDispatched, cp.BlocksDone = prev.LowestDispatched, prev.BlocksDone
		if prev.Status == StatusDone && prev.Floor <= floor {
			lg.WithField("floor", prev.Floor).Info("backfill: already done")
			return StatusDone, nil
		}
	}
	if ceiling < floor {
		cp.Status, cp.LowestDispatched = StatusDone, floor
		return j.finish(ctx, cp)
	}
	start := ResumeFrom(ceiling, prev, has, j.cfg.Pipeline.Workers, j.cfg.Pipeline.Batch)
	if err := j.deps.Store.SaveCheckpoint(ctx, cp); err != nil {
		return StatusError, err
	}
	lg.WithFields(log.Fields{"from": start, "floor": floor, "ceiling": ceiling}).Info("backfill started")

	// Checkpoint saves come from the writer (each flush) and the ticker;
	// the mutex keeps them in order.
	var mu sync.Mutex
	baseDone := cp.BlocksDone
	save := func(p Progress) {
		mu.Lock()
		defer mu.Unlock()
		if p.Dispatched > 0 {
			cp.LowestDispatched = min(cp.LowestDispatched, p.Lowest)
		}
		cp.BlocksDone = baseDone + p.Written
		if err := j.deps.Store.SaveCheckpoint(context.WithoutCancel(ctx), cp); err != nil {
			lg.WithError(err).Warn("backfill: saving the checkpoint")
		}
	}
	pcfg := j.cfg.Pipeline
	pcfg.OnFlush = save
	p := New(pcfg, j.deps.Pipeline)

	tickCtx, stopTicks := context.WithCancel(ctx)
	ticks := make(chan struct{})
	go func() {
		defer close(ticks)
		t := time.NewTicker(j.cfg.CheckpointInterval)
		defer t.Stop()
		for {
			select {
			case <-tickCtx.Done():
				return
			case <-t.C:
				pr := p.Progress()
				save(pr)
				lg.WithFields(log.Fields{"lowest": pr.Lowest, "written": pr.Written, "failed": pr.Failed}).Debug("backfill progress")
			}
		}
	}()
	res := p.Run(ctx, Descending(start, floor, j.deps.Store), Insert)
	stopTicks()
	<-ticks
	save(res.Progress)

	switch {
	case res.Err != nil:
		return j.fail(ctx, cp, res.Err)
	case res.Complete:
		cp.Status, cp.LowestDispatched = StatusDone, floor
		lg.WithFields(log.Fields{"floor": floor, "written": res.Written, "failed": res.Failed}).Info("backfill done")
		return j.finish(ctx, cp)
	default:
		lg.WithField("lowest_dispatched", cp.LowestDispatched).Info("backfill stopped, resumes at the next start")
		return StatusRunning, ctx.Err()
	}
}

// waitCeiling returns live_floor-1, polling until the live indexer has
// recorded the floor.
func (j *MainJob) waitCeiling(ctx context.Context) (int64, error) {
	for {
		floor, ok, err := j.deps.Store.LiveFloor(ctx)
		if err != nil {
			return 0, err
		}
		if ok {
			return floor - 1, nil
		}
		j.deps.Pipeline.Log.Info("backfill: waiting for the live indexer to record the live floor")
		if err := j.cfg.Pipeline.Sleep(ctx, j.cfg.PollInterval); err != nil {
			return 0, err
		}
	}
}

func (j *MainJob) finish(ctx context.Context, cp Checkpoint) (string, error) {
	cp.LastError = ""
	if err := j.deps.Store.SaveCheckpoint(context.WithoutCancel(ctx), cp); err != nil {
		return StatusError, err
	}
	return cp.Status, nil
}

// fail records the error in the checkpoint (best effort: the database may be
// what failed) and returns it.
func (j *MainJob) fail(ctx context.Context, cp Checkpoint, cause error) (string, error) {
	cp.Status, cp.LastError = StatusError, cause.Error()
	if err := j.deps.Store.SaveCheckpoint(context.WithoutCancel(ctx), cp); err != nil {
		j.deps.Pipeline.Log.WithError(err).Warn("backfill: recording the error in the checkpoint")
	}
	j.deps.Pipeline.Log.WithError(cause).Error("backfill stopped on an error, resumes at the next start")
	return StatusError, cause
}

// Pause marks an unfinished main job paused: run at a start with the backfill
// disabled. A missing or done job is left alone.
func Pause(ctx context.Context, store Store) error {
	cp, ok, err := store.Checkpoint(ctx, JobMain)
	if err != nil || !ok || cp.Status == StatusDone || cp.Status == StatusPaused {
		return err
	}
	cp.Status = StatusPaused
	return store.SaveCheckpoint(ctx, cp)
}

// CursorWriter moves the live cursor; *writer.BatchWriter satisfies it.
type CursorWriter interface {
	AdvanceCursor(ctx context.Context, height int64) error
}

// CatchUp repairs an outage longer than the local node's retained window: it
// walks up through heights the node has pruned with the same pipeline, then
// hands the live indexer back a cursor past them.
type CatchUp struct {
	cfg      Config
	deps     Deps
	presence Presence
	cursor   CursorWriter
}

// NewCatchUp returns the catch-up job. Its failures are recorded with the
// live source: the heights are above the live floor.
func NewCatchUp(cfg Config, deps Deps, presence Presence, cursor CursorWriter) *CatchUp {
	cfg.FailureSource = writer.FailureSourceLive
	return &CatchUp{cfg: cfg, deps: deps, presence: presence, cursor: cursor}
}

// CatchUp indexes [from, to] in ascending order, skipping the heights
// present, and moves the live cursor to to. An error (or a cancelled ctx)
// leaves the cursor where it was: the next pass starts over and skips what
// was written.
func (c *CatchUp) CatchUp(ctx context.Context, from, to int64) error {
	if to < from {
		return nil
	}
	lg := c.deps.Log
	if lg == nil {
		lg = log.StandardLogger()
	}
	lg.WithFields(log.Fields{"from": from, "to": to}).Info("catch-up: reading heights the local node has pruned from the archive")
	res := New(c.cfg, c.deps).Run(ctx, Ascending(from, to, c.presence), Insert)
	if res.Err != nil {
		return fmt.Errorf("catch-up %d..%d: %w", from, to, res.Err)
	}
	if !res.Complete {
		return ctx.Err()
	}
	if err := c.cursor.AdvanceCursor(ctx, to); err != nil {
		return err
	}
	lg.WithFields(log.Fields{"to": to, "written": res.Written, "failed": res.Failed}).Info("catch-up done")
	return nil
}
