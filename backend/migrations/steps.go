package migrations

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bze-alphateam/bze-scan/backend/internal/classify"
	"github.com/bze-alphateam/bze-scan/backend/internal/labels"
)

// Execer runs a statement; *pgxpool.Pool and pgx.Tx satisfy it.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// DB is what a post-migration step gets; *pgxpool.Pool satisfies it.
type DB interface {
	Execer
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Step is a post-migration Go step: it runs after every `migrate up`, in
// order, and must be idempotent. Steps that need packages importing this one
// (the writers) are given to Open by the composition root.
type Step struct {
	Name string
	Run  func(ctx context.Context, db DB) error
}

// postMigrate lists the post-migration steps.
var postMigrate = []Step{
	{Name: "partitions", Run: ensurePartitions},
	{Name: "classification", Run: mirrorClassification},
	{Name: "labels", Run: seedLabels},
	{Name: "message_events", Run: rewriteMessageEvents},
}

// ensurePartitions creates the history-table partitions for
// [0, PartitionsUpTo].
func ensurePartitions(ctx context.Context, db DB) error {
	_, err := db.Exec(ctx, `SELECT explorer.ensure_partitions($1, $2)`, int64(0), PartitionsUpTo)
	return err
}

// mirrorClassification rewrites the classification tables from the Go
// classification and reclassifies the activity stored as "other".
func mirrorClassification(ctx context.Context, db DB) error {
	return classify.Mirror(ctx, db)
}

// seedLabels writes the module and known account labels.
func seedLabels(ctx context.Context, db DB) error {
	return labels.Seed(ctx, db)
}

// leanEvents matches the stored message events explorer.kept_events changes.
const leanEvents = `'$[*] ? (@.type == "message" || @.type == "transfer" || @.type == "coin_spent" ||
  @.type == "coin_received" || (@.type == "update_client" && exists(@.attrs.header)))'`

// rewriteMessageEvents filters the stored explorer.messages events through
// explorer.kept_events (migration 000018) while its pending_rewrites row
// exists: one statement per partition, then one for every height above
// PartitionsUpTo, each its own transaction, then the row goes. Once it has
// gone every statement stops at its one-time filter without reading a row.
func rewriteMessageEvents(ctx context.Context, db DB) error {
	const pending = `EXISTS (SELECT 1 FROM explorer.pending_rewrites WHERE name = 'message_events')`
	rewrite := func(where string) error {
		_, err := db.Exec(ctx, `UPDATE explorer.messages SET events = explorer.kept_events(events)
			WHERE `+where+` AND `+pending+` AND events @? `+leanEvents)
		return err
	}
	for lo := int64(0); lo <= PartitionsUpTo; lo += PartitionSize {
		if err := rewrite(fmt.Sprintf("height >= %d AND height < %d", lo, lo+PartitionSize)); err != nil {
			return fmt.Errorf("partition %s: %w", PartitionName("messages", lo), err)
		}
	}
	if err := rewrite(fmt.Sprintf("height > %d", PartitionsUpTo)); err != nil {
		return fmt.Errorf("above the partitions: %w", err)
	}
	_, err := db.Exec(ctx, `DELETE FROM explorer.pending_rewrites WHERE name = 'message_events'`)
	return err
}
