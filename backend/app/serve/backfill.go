package serve

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/backfill"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// history is the backfill side of the process: a pool of its own (never the
// live writer's), the batch writer, the checkpoint store and the pipeline's
// collaborators, shared by the main job and the catch-up.
type history struct {
	pool   *pgxpool.Pool
	store  *backfill.PGStore
	writer *writer.BatchWriter
	cfg    backfill.Config
	deps   backfill.Deps
}

// newHistory builds the backfill side. Close its pool when done.
func newHistory(ctx context.Context, cfg *config.Config, opts Options, codec *chain.Codec) (*history, error) {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("backfill pool: %w", err)
	}
	// Nil interface when there is no retry node, never a typed nil.
	var retry backfill.Node
	if cfg.ArchiveRPCRetryURL != "" {
		retry = node.New(cfg.ArchiveRPCRetryURL)
	}
	w := writer.NewBatchWriter(pool)
	limit := rate.Limit(cfg.BackfillRateLimit)
	if limit <= 0 {
		limit = rate.Inf // config.Load never yields 0; a hand-built config means no limit
	}
	return &history{
		pool:   pool,
		store:  backfill.NewPGStore(pool),
		writer: w,
		cfg: backfill.Config{
			Workers:     cfg.BackfillWorkers,
			Batch:       cfg.BackfillBatch,
			Quiet:       cfg.BackfillQuiet,
			RetryDelays: opts.BackfillRetryDelays,
		},
		deps: backfill.Deps{
			Archive:      node.New(cfg.ArchiveRPCURL),
			ArchiveRetry: retry,
			// One limiter for every worker of every job: the archive sees
			// one polite client.
			Limiter:     rate.NewLimiter(limit, 1),
			Transformer: transform.New(codec, log.StandardLogger()),
			Writer:      w,
		},
	}, nil
}

func (h *history) mainJob(cfg *config.Config) *backfill.MainJob {
	return backfill.NewMainJob(backfill.JobConfig{
		Floor:    backfill.Floor{Height: cfg.BackfillFloorHeight, Date: cfg.BackfillFloorDate},
		Pipeline: h.cfg,
	}, backfill.JobDeps{
		Store:    h.store,
		Locker:   backfill.NewPGLocker(cfg.DatabaseURL),
		Pipeline: h.deps,
	})
}

func (h *history) catchUp() *backfill.CatchUp {
	return backfill.NewCatchUp(h.cfg, h.deps, h.store, h.writer)
}

// ErrBackfillNotDone is returned by RunBackfill when the main job stopped
// before reaching its floor.
var ErrBackfillNotDone = errors.New("backfill not done")

// RunBackfill runs the main backfill job standalone, as the backfill command
// does: nil once the floor is reached, an error otherwise (another process
// holds the lock, a fatal error, or ctx cancelled before the end).
func RunBackfill(ctx context.Context, cfg *config.Config, opts Options) error {
	if err := cfg.RequireDatabase(); err != nil {
		return err
	}
	codec, err := chain.NewCodec()
	if err != nil {
		return err
	}
	h, err := newHistory(ctx, cfg, opts, codec)
	if err != nil {
		return err
	}
	defer h.pool.Close()

	status, err := h.mainJob(cfg).Run(ctx)
	if err != nil {
		return err
	}
	if status != backfill.StatusDone {
		return fmt.Errorf("%w: %s", ErrBackfillNotDone, status)
	}
	return nil
}

// backfillComponent runs the main job inside serve. It never stops the
// process: a job that is locked elsewhere, fails or finishes just ends, and
// the next start resumes it.
func backfillComponent(h *history, cfg *config.Config) component {
	return component{name: "backfill", run: func(ctx context.Context) error {
		_, err := h.mainJob(cfg).Run(ctx)
		switch {
		case errors.Is(err, backfill.ErrLocked):
			log.Info("backfill: another process runs it, skipping")
		case err != nil && ctx.Err() == nil:
			log.WithError(err).Error("backfill stopped; it resumes at the next start")
		}
		return nil
	}}
}
