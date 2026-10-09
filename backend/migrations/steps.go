package migrations

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bze-alphateam/bze-scan/backend/internal/classify"
	"github.com/bze-alphateam/bze-scan/backend/internal/labels"
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

// postMigrate lists the post-migration steps.
var postMigrate = []Step{
	{Name: "partitions", Run: ensurePartitions},
	{Name: "classification", Run: mirrorClassification},
	{Name: "labels", Run: seedLabels},
}

// ensurePartitions creates the history-table partitions for
// [0, PartitionsUpTo].
func ensurePartitions(ctx context.Context, db Execer) error {
	_, err := db.Exec(ctx, `SELECT explorer.ensure_partitions($1, $2)`, int64(0), PartitionsUpTo)
	return err
}

// mirrorClassification rewrites the classification tables from the Go
// classification and reclassifies the activity stored as "other".
func mirrorClassification(ctx context.Context, db Execer) error {
	return classify.Mirror(ctx, db)
}

// seedLabels writes the module and known account labels.
func seedLabels(ctx context.Context, db Execer) error {
	return labels.Seed(ctx, db)
}
