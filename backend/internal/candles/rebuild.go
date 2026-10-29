package candles

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/migrations"
)

const (
	// pendingRow is migration 000024's explorer.pending_rewrites row: the
	// whole history has still to be built, its markets not listed yet.
	pendingRow = "candles"
	// marketRowPrefix + a market id is a market whose history has still
	// to be built.
	marketRowPrefix = "candles:"
)

// DB is what the rebuilds need; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Rebuilder recomputes whole markets, one transaction per chunk.
type Rebuilder struct {
	db     DB
	upkeep *Upkeep
	log    log.FieldLogger
}

// NewRebuilder returns a rebuilder over db whose stores come from upkeep
// (Default when nil). A nil lg is the standard logger.
func NewRebuilder(db DB, upkeep *Upkeep, lg log.FieldLogger) *Rebuilder {
	if upkeep == nil {
		upkeep = Default
	}
	if lg == nil {
		lg = log.StandardLogger()
	}
	return &Rebuilder{db: db, upkeep: upkeep, log: lg}
}

// Markets returns every market and pool with a stored trade, sorted.
func (r *Rebuilder) Markets(ctx context.Context) ([]string, error) {
	rows, err := r.db.Query(ctx, `SELECT market_id FROM explorer.order_fills
		UNION SELECT pool_id FROM explorer.swaps ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("candles: markets: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("candles: markets: %w", err)
	}
	return out, nil
}

// Market rebuilds market's candles (Rebuild, one transaction per chunk)
// and returns the number of trades read.
func (r *Rebuilder) Market(ctx context.Context, market string) (int, error) {
	return Rebuild(ctx, r.within, market)
}

// within implements Within over the pool.
func (r *Rebuilder) within(ctx context.Context, fn func(s Store) error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("candles: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(r.upkeep.open(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("candles: commit: %w", err)
	}
	return nil
}

// rebuild is Market, then the deletion of the pending row named done: an
// interruption rebuilds the whole market again.
func (r *Rebuilder) rebuild(ctx context.Context, market, done string) (int, error) {
	n, err := r.Market(ctx, market)
	if err != nil {
		return n, err
	}
	if _, err := r.db.Exec(ctx, `DELETE FROM explorer.pending_rewrites WHERE name = $1`, done); err != nil {
		return n, fmt.Errorf("candles of %s: pending row: %w", market, err)
	}
	return n, nil
}

// All rebuilds every market with a stored trade, or only market when not
// empty. It returns the markets and the trades it read.
func (r *Rebuilder) All(ctx context.Context, market string) (markets, trades int, err error) {
	list := []string{market}
	if market == "" {
		if list, err = r.Markets(ctx); err != nil {
			return 0, 0, err
		}
	}
	for _, m := range list {
		n, err := r.Market(ctx, m)
		trades += n
		if err != nil {
			return markets, trades, err
		}
		markets++
	}
	return markets, trades, nil
}

// Backfill builds the candles of the whole history once, per market and
// resumable: while the row 'candles' exists it is replaced, in one
// transaction, by a row 'candles:<market>' per market with a stored trade;
// then every such row's market is rebuilt (Rebuild) and its row deleted. An
// interrupted run resumes at the markets left.
func (r *Rebuilder) Backfill(ctx context.Context) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("candles: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `DELETE FROM explorer.pending_rewrites WHERE name = $1`, pendingRow)
	if err != nil {
		return fmt.Errorf("candles: pending build: %w", err)
	}
	if tag.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO explorer.pending_rewrites (name)
			SELECT $1 || m FROM (SELECT market_id FROM explorer.order_fills UNION SELECT pool_id FROM explorer.swaps) t(m)
			ON CONFLICT (name) DO NOTHING`, marketRowPrefix); err != nil {
			return fmt.Errorf("candles: list the markets: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("candles: commit: %w", err)
	}

	rows, err := r.db.Query(ctx, `SELECT name FROM explorer.pending_rewrites WHERE starts_with(name, $1) ORDER BY name`,
		marketRowPrefix)
	if err != nil {
		return fmt.Errorf("candles: pending markets: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("candles: pending markets: %w", err)
	}
	trades := 0
	for _, name := range names {
		n, err := r.rebuild(ctx, strings.TrimPrefix(name, marketRowPrefix), name)
		trades += n
		if err != nil {
			return err
		}
	}
	if len(names) > 0 {
		r.log.WithFields(log.Fields{"markets": len(names), "trades": trades}).Info("candles: history built")
	}
	return nil
}

// MigrationStep is the post-migration step "candles": Backfill at every
// `migrate up`. It runs after internal/trades' "swaps", which numbers the
// trades first.
func MigrationStep() migrations.Step {
	return migrations.Step{Name: "candles", Run: func(ctx context.Context, db migrations.DB) error {
		return NewRebuilder(db, nil, nil).Backfill(ctx)
	}}
}
