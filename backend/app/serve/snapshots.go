package serve

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/snapshots"
	"github.com/bze-alphateam/bze-scan/backend/internal/snapshots/proposals"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// NewSnapshots builds the snapshots runner: every kind over the database,
// reading the archive node (ARCHIVE_RPC_URL) paced by the backfill's rate
// limit (BACKFILL_RATE_LIMIT). workers, when positive, overrides
// snapshots.DefaultWorkers. Call close when done.
func NewSnapshots(ctx context.Context, cfg *config.Config, workers int) (runner *snapshots.Runner, closeFn func(), err error) {
	if err := cfg.RequireDatabase(); err != nil {
		return nil, nil, err
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshots pool: %w", err)
	}
	limit := rate.Limit(cfg.BackfillRateLimit)
	if limit <= 0 {
		limit = rate.Inf // config.Load never yields 0; a hand-built config means no limit
	}
	runner = snapshots.New(snapshots.Deps{
		Kinds:  []snapshots.Kind{proposals.New(proposals.NewPGStore(pool))},
		Reader: snapshots.NewArchive(node.New(cfg.ArchiveRPCURL), rate.NewLimiter(limit, 1)),
		Jobs:   statesync.NewPGJobStore(pool),
		Log:    log.StandardLogger(),
	}, workers)
	return runner, pool.Close, nil
}
