package serve

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/sink"
)

// NewSinkCleaner builds the sink cleanup over the database with the
// SINK_* settings; retention, when positive, overrides SINK_RETENTION (the
// sink-cleanup command's --retention). Call close when done.
func NewSinkCleaner(ctx context.Context, cfg *config.Config, retention time.Duration) (cleaner *sink.Cleaner, closeFn func(), err error) {
	if err := cfg.RequireDatabase(); err != nil {
		return nil, nil, err
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("sink cleanup pool: %w", err)
	}
	return newSinkCleaner(cfg, retention, pool), pool.Close, nil
}

func newSinkCleaner(cfg *config.Config, retention time.Duration, db sink.DB) *sink.Cleaner {
	if retention <= 0 {
		retention = cfg.SinkRetention
	}
	return sink.New(sink.Config{
		Retention: retention,
		Interval:  cfg.SinkCleanupInterval,
		Batch:     int64(cfg.SinkCleanupBatch),
		Log:       log.StandardLogger(),
	}, sink.NewPGStore(db))
}
