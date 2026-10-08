package migrations

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
)

// Execer runs a statement; *pgxpool.Pool and pgx.Tx satisfy it.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Step is a post-migration Go step: it runs after every `migrate up`, in
// order, and must be idempotent.
type Step struct {
	Name string
	Run  func(ctx context.Context, db Execer) error
}

// postMigrate lists the post-migration steps. Later work appends its own (the
// classification mirror, the labels seed).
var postMigrate = []Step{
	{Name: "partitions", Run: ensurePartitions},
}

// ensurePartitions creates the history-table partitions for
// [0, PartitionsUpTo].
func ensurePartitions(ctx context.Context, db Execer) error {
	_, err := db.Exec(ctx, `SELECT explorer.ensure_partitions($1, $2)`, int64(0), PartitionsUpTo)
	return err
}
