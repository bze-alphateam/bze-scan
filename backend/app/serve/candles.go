package serve

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
)

// NewCandleRebuilder builds `bze-scan candles rebuild`: the candles
// recomputed from the stored trades, nothing read from the node. Call
// closeFn when done.
func NewCandleRebuilder(ctx context.Context, cfg *config.Config) (r *candles.Rebuilder, closeFn func(), err error) {
	if err := cfg.RequireDatabase(); err != nil {
		return nil, nil, err
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("candles pool: %w", err)
	}
	return candles.NewRebuilder(pool, candles.Default, log.StandardLogger()), pool.Close, nil
}
