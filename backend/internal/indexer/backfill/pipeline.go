// Package backfill indexes history from the archive nodes through a
// fetch-parallel, write-serial pipeline: a dispatcher hands heights to a
// semaphore of workers, each worker fetches and transforms one height under a
// global rate limit, and one batching writer flushes the results as bulk
// inserts. Failures never stall a run: a height that keeps failing is
// recorded in index_failures and skipped. An unreachable archive is not a
// failure of its heights: the workers wait for it, which pauses the run.
//
// The main job walks down from the live floor to a configured floor and
// checkpoints its progress; the catch-up job walks up from the live cursor
// through heights the local node has pruned. Both are the same pipeline.
package backfill

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// Node is the part of the node client a worker fetches through, always by
// height, and asks /status when a fetch finds it unavailable; *node.Client
// satisfies it.
type Node interface {
	Status(ctx context.Context) (*node.Status, []byte, error)
	Block(ctx context.Context, height int64) (*node.Block, []byte, error)
	BlockResults(ctx context.Context, height int64) (*node.BlockResults, []byte, error)
	Commit(ctx context.Context, height int64) (*node.Commit, []byte, error)
}

// Limiter paces the archive requests of every worker; *rate.Limiter
// satisfies it. Wait is called once before each request.
type Limiter interface {
	Wait(ctx context.Context) error
}

// Adapter normalises a height's input before the transformer sees it (the
// archive's older event formats). Nil is the identity.
type Adapter interface {
	Adapt(height int64, in *transform.Input) error
}

// Transformer turns one height's input into entities.
type Transformer interface {
	Transform(in transform.Input) (*transform.Entities, error)
}

// Writer persists the pipeline's flushes and failures; *writer.BatchWriter
// satisfies it.
type Writer interface {
	Write(ctx context.Context, batch []*transform.Entities, mode writer.Mode) error
	RecordFailure(ctx context.Context, height int64, source string, attempts int, cause error) error
}

// HeightSource yields the heights to dispatch, in dispatch order. ok false
// means the source is exhausted.
type HeightSource interface {
	Next(ctx context.Context) (height int64, ok bool, err error)
}

// Defaults applied to the zero fields of Config.
const (
	DefaultWorkers      = 10
	DefaultBatch        = 50
	DefaultQuiet        = 2 * time.Second
	DefaultFlushTimeout = 2 * time.Minute
	// DefaultUnavailableMin and DefaultUnavailableMax bound the waits of a
	// worker while the archive is unreachable.
	DefaultUnavailableMin = time.Second
	DefaultUnavailableMax = time.Minute
)

// DefaultRetryDelays are the waits before the four retries of a height,
// which go to the retry endpoint: a backoff of about 12.5 minutes in all, so
// an archive that answers /status but struggles for a while (timeouts,
// errors from the proxy in front of it) does not leave failed heights.
var DefaultRetryDelays = []time.Duration{5 * time.Second, 25 * time.Second, 125 * time.Second, 10 * time.Minute}

// Config tunes a pipeline; zero fields take the defaults.
type Config struct {
	// Workers (X) bounds the heights in flight; the writer's channel holds
	// as many.
	Workers int
	// Batch (M) is the number of heights a flush holds at most.
	Batch int
	// Quiet flushes what the writer holds after this long without a result.
	Quiet time.Duration
	// RetryDelays are the waits before each retry; the attempts at a height
	// are len(RetryDelays)+1, the first against Deps.Archive and the retries
	// against Deps.ArchiveRetry.
	RetryDelays []time.Duration
	// UnavailableMin is a worker's first wait while the archive is
	// unreachable; each wait doubles, up to UnavailableMax. These waits
	// spend no attempt and last as long as the outage.
	UnavailableMin time.Duration
	UnavailableMax time.Duration
	// FailureSource is index_failures.source; empty is "backfill".
	FailureSource string
	// FlushTimeout bounds one flush or failure record. Flushes run on even
	// after ctx is cancelled, so nothing fetched is left unwritten.
	FlushTimeout time.Duration
	// OnFlush is called after every successful flush, from the writer
	// goroutine.
	OnFlush func(Progress)
	// Sleep waits for d or until ctx is done; nil is a timer.
	Sleep func(ctx context.Context, d time.Duration) error
	// After is the quiet-period clock; nil is time.After.
	After func(d time.Duration) <-chan time.Time
}

// Deps are the pipeline's collaborators, built by the composition root.
type Deps struct {
	// Archive answers first; ArchiveRetry answers the retries (nil means
	// Archive again).
	Archive      Node
	ArchiveRetry Node
	// Limiter paces every request of every worker; nil does not wait.
	Limiter     Limiter
	Adapter     Adapter
	Transformer Transformer
	Writer      Writer
	// Log receives the failures; nil is the standard logger.
	Log log.FieldLogger
}

// Progress is a run's progress so far.
type Progress struct {
	// Lowest and Highest are the extreme heights dispatched; 0 before the
	// first dispatch.
	Lowest, Highest int64
	// Dispatched heights, Written by a flush, Failed and recorded.
	Dispatched, Written, Failed int64
	// Resume is where a descending walk resumes: every height above it was
	// written or recorded as failed. It is the highest height still pending
	// (in flight, queued or held unflushed), or Lowest when none is; 0
	// before the first dispatch. A height held up by retries can lie any
	// number of heights above Lowest, and a stop drops the heights in
	// flight, so Lowest plus a margin is not a safe resume point.
	Resume int64
}

// Result is the outcome of a run.
type Result struct {
	Progress
	// Complete: the source was exhausted and every height was written or
	// recorded as failed.
	Complete bool
	// Err is a fatal error: the source or the writer failed (the database
	// is unavailable). A cancelled ctx is not an error.
	Err error
}

// Pipeline runs one walk over a height source. It is not reusable.
type Pipeline struct {
	cfg  Config
	deps Deps

	mu       sync.Mutex
	progress Progress
	// pending are the heights dispatched and neither written nor recorded.
	pending map[int64]struct{}
}

// New returns a pipeline.
func New(cfg Config, deps Deps) *Pipeline {
	if cfg.Workers <= 0 {
		cfg.Workers = DefaultWorkers
	}
	if cfg.Batch <= 0 {
		cfg.Batch = DefaultBatch
	}
	if cfg.Quiet <= 0 {
		cfg.Quiet = DefaultQuiet
	}
	if cfg.RetryDelays == nil {
		cfg.RetryDelays = DefaultRetryDelays
	}
	if cfg.UnavailableMin <= 0 {
		cfg.UnavailableMin = DefaultUnavailableMin
	}
	if cfg.UnavailableMax <= 0 {
		cfg.UnavailableMax = DefaultUnavailableMax
	}
	if cfg.FailureSource == "" {
		cfg.FailureSource = writer.FailureSourceBackfill
	}
	if cfg.FlushTimeout <= 0 {
		cfg.FlushTimeout = DefaultFlushTimeout
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleep
	}
	if cfg.After == nil {
		cfg.After = time.After
	}
	if deps.ArchiveRetry == nil {
		deps.ArchiveRetry = deps.Archive
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	return &Pipeline{cfg: cfg, deps: deps, pending: map[int64]struct{}{}}
}

// Progress returns the progress so far; safe to call during Run.
func (p *Pipeline) Progress() Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshot()
}

// outcome is one worker's result: entities, or the error that failed the
// height after attempts.
type outcome struct {
	height   int64
	ents     *transform.Entities
	attempts int
	err      error
}

// Run dispatches every height of heights to the workers and writes the
// results in mode, until the source is exhausted, ctx is cancelled or a fatal
// error occurs. A worker slot is released once its result is in the writer's
// channel, so the heights in memory are bounded by X in flight, X queued and
// M held. The backfill and the catch-up write in writer.ModeInsert, the
// reindex in writer.ModeUpdate.
func (p *Pipeline) Run(ctx context.Context, heights HeightSource, mode writer.Mode) Result {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	results := make(chan outcome, p.cfg.Workers)
	writeDone := make(chan error, 1)
	go func() { writeDone <- p.write(ctx, cancel, results, mode) }()

	sem := make(chan struct{}, p.cfg.Workers)
	var wg sync.WaitGroup
	var srcErr error
	exhausted := false
dispatch:
	for {
		h, ok, err := heights.Next(ctx)
		if err != nil {
			if ctx.Err() == nil {
				srcErr = fmt.Errorf("height source: %w", err)
			}
			break
		}
		if !ok {
			exhausted = true
			break
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break dispatch
		}
		p.dispatched(h)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if o, ok := p.work(ctx, h); ok {
				// The writer drains the channel until it is closed, fatal
				// error or not, so this send never blocks for good.
				results <- o
			}
		}()
	}
	wg.Wait()
	close(results)
	writeErr := <-writeDone

	res := Result{Progress: p.Progress()}
	switch {
	case writeErr != nil:
		res.Err = writeErr
	case srcErr != nil:
		res.Err = srcErr
	default:
		res.Complete = exhausted && ctx.Err() == nil
	}
	return res
}

func (p *Pipeline) dispatched(h int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.progress.Dispatched == 0 {
		p.progress.Lowest, p.progress.Highest = h, h
	}
	p.progress.Lowest = min(p.progress.Lowest, h)
	p.progress.Highest = max(p.progress.Highest, h)
	p.progress.Dispatched++
	p.pending[h] = struct{}{}
}

// snapshot returns the progress with Resume set; p.mu must be held.
func (p *Pipeline) snapshot() Progress {
	pr := p.progress
	if pr.Dispatched == 0 {
		return pr
	}
	pr.Resume = pr.Lowest
	for h := range p.pending {
		pr.Resume = max(pr.Resume, h)
	}
	return pr
}

// work fetches, adapts and transforms one height. ok false means ctx was
// cancelled: the height is neither written nor recorded.
func (p *Pipeline) work(ctx context.Context, h int64) (outcome, bool) {
	attempts := len(p.cfg.RetryDelays) + 1
	var in transform.Input
	var err error
	wait := p.cfg.UnavailableMin
	attempt := 1
	for {
		n := p.deps.Archive
		if attempt > 1 {
			n = p.deps.ArchiveRetry
		}
		in, err = p.fetch(ctx, n, h)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return outcome{}, false
		}
		if p.down(ctx, n, err) {
			// An outage, not a bad height: wait for the archive without
			// spending the attempt.
			p.deps.Log.WithError(err).WithFields(log.Fields{"height": h, "retry_in": wait.String()}).
				Warn("backfill: archive unavailable, waiting")
			if p.cfg.Sleep(ctx, wait) != nil {
				return outcome{}, false
			}
			wait = min(wait*2, p.cfg.UnavailableMax)
			continue
		}
		wait = p.cfg.UnavailableMin
		p.deps.Log.WithError(err).WithFields(log.Fields{"height": h, "attempt": attempt}).Warn("backfill: fetch failed")
		if attempt == attempts {
			return outcome{height: h, attempts: attempts, err: err}, true
		}
		if p.cfg.Sleep(ctx, p.cfg.RetryDelays[attempt-1]) != nil {
			return outcome{}, false
		}
		attempt++
	}
	if p.deps.Adapter != nil {
		if err := p.deps.Adapter.Adapt(h, &in); err != nil {
			return outcome{height: h, attempts: attempt, err: fmt.Errorf("adapt: %w", err)}, true
		}
	}
	ents, err := p.deps.Transformer.Transform(in)
	if err != nil {
		return outcome{height: h, attempts: attempt, err: fmt.Errorf("transform: %w", err)}, true
	}
	return outcome{height: h, ents: ents}, true
}

// down reports whether err is n being unreachable rather than a problem of
// the height: the fetch found it unavailable and /status does not answer
// either. A node that answers /status had trouble with this height only (a
// slow block timing out, say), which is an ordinary failed attempt.
func (p *Pipeline) down(ctx context.Context, n Node, err error) bool {
	if !errors.Is(err, node.ErrUnavailable) {
		return false
	}
	if p.wait(ctx) != nil {
		return false
	}
	_, _, serr := n.Status(ctx)
	return serr != nil && ctx.Err() == nil
}

func (p *Pipeline) fetch(ctx context.Context, n Node, h int64) (transform.Input, error) {
	var in transform.Input
	var err error
	if err = p.wait(ctx); err != nil {
		return in, err
	}
	if in.Block, _, err = n.Block(ctx, h); err != nil {
		return in, err
	}
	if err = p.wait(ctx); err != nil {
		return in, err
	}
	if in.Results, _, err = n.BlockResults(ctx, h); err != nil {
		return in, err
	}
	if err = p.wait(ctx); err != nil {
		return in, err
	}
	in.Commit, _, err = n.Commit(ctx, h)
	return in, err
}

func (p *Pipeline) wait(ctx context.Context) error {
	if p.deps.Limiter == nil {
		return nil
	}
	return p.deps.Limiter.Wait(ctx)
}

// write is the batching writer: it holds results in arrival order and
// flushes them when it holds Batch heights, after Quiet without a result, and
// when the channel closes. A failed height is recorded at once. A write error
// is fatal: it cancels the run, and the rest of the channel is drained
// without writing.
func (p *Pipeline) write(ctx context.Context, cancel context.CancelCauseFunc, results <-chan outcome, mode writer.Mode) error {
	wctx := context.WithoutCancel(ctx)
	var held []*transform.Entities
	var heldHeights []int64
	var quiet <-chan time.Time
	var fatal error

	flush := func() {
		batch, heights := held, heldHeights
		held, heldHeights, quiet = nil, nil, nil
		if fatal != nil || len(batch) == 0 {
			return
		}
		fctx, done := context.WithTimeout(wctx, p.cfg.FlushTimeout)
		defer done()
		if err := p.deps.Writer.Write(fctx, batch, mode); err != nil {
			fatal = err
			cancel(err)
			return
		}
		p.mu.Lock()
		p.progress.Written += int64(len(batch))
		for _, h := range heights {
			delete(p.pending, h)
		}
		pr := p.snapshot()
		p.mu.Unlock()
		if p.cfg.OnFlush != nil {
			p.cfg.OnFlush(pr)
		}
	}

	for {
		select {
		case o, ok := <-results:
			if !ok {
				flush()
				return fatal
			}
			if fatal != nil {
				continue
			}
			if o.err != nil {
				p.recordFailure(wctx, o, cancel, &fatal)
				continue
			}
			held, heldHeights = append(held, o.ents), append(heldHeights, o.height)
			if len(held) >= p.cfg.Batch {
				flush()
			} else {
				quiet = p.cfg.After(p.cfg.Quiet)
			}
		case <-quiet:
			flush()
		}
	}
}

func (p *Pipeline) recordFailure(ctx context.Context, o outcome, cancel context.CancelCauseFunc, fatal *error) {
	p.deps.Log.WithError(o.err).WithFields(log.Fields{"height": o.height, "attempts": o.attempts}).
		Error("backfill: giving up on height, recorded in index_failures")
	fctx, done := context.WithTimeout(ctx, p.cfg.FlushTimeout)
	defer done()
	if err := p.deps.Writer.RecordFailure(fctx, o.height, p.cfg.FailureSource, o.attempts, o.err); err != nil {
		*fatal = err
		cancel(err)
		return
	}
	p.mu.Lock()
	p.progress.Failed++
	delete(p.pending, o.height)
	p.mu.Unlock()
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
