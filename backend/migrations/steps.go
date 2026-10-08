package migrations

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Step is a post-migration Go step: it runs after every `migrate up`, in
// order, and must be idempotent.
type Step struct {
	Name string
	Run  func(ctx context.Context, pool *pgxpool.Pool) error
}

// postMigrate lists the post-migration steps. Later work appends its own (the
// classification mirror, the labels seed).
var postMigrate = []Step{
	{Name: "partitions", Run: ensurePartitions},
}

// ensurePartitions creates the history-table partitions for
// [0, PartitionsUpTo].
func ensurePartitions(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `SELECT explorer.ensure_partitions($1, $2)`, int64(0), PartitionsUpTo)
	return err
}
