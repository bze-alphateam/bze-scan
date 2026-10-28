package migrations

import (
	"context"
	"fmt"
	"math"

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
	{Name: "message_events", Run: rewriteMessageEvents},
	{Name: "swaps", Run: indexStoredTrades},
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

// leanEvents matches the stored message events explorer.kept_events changes.
const leanEvents = `'$[*] ? (@.type == "message" || @.type == "transfer" || @.type == "coin_spent" ||
  @.type == "coin_received" || (@.type == "update_client" && exists(@.attrs.header)))'`

// rewriteMessageEvents filters the stored explorer.messages events through
// explorer.kept_events (migration 000018) while its pending_rewrites row
// exists: one statement per partition, then one for every height above
// PartitionsUpTo, each its own transaction, then the row goes. Once it has
// gone every statement stops at its one-time filter without reading a row.
func rewriteMessageEvents(ctx context.Context, db Execer) error {
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

// indexStoredTrades builds the swaps rows and the order_fills trade ids of
// what is already stored (migration 000023) while its pending_rewrites row
// exists: explorer.index_trades once per partition from the row's
// resume_from up, then once for every height above PartitionsUpTo, each its
// own transaction that also moves resume_from, then the row goes. An
// interrupted run resumes at the first partition not done; once the row has
// gone every statement selects nothing.
func indexStoredTrades(ctx context.Context, db Execer) error {
	run := func(lo, hi int64) error {
		_, err := db.Exec(ctx, `SELECT explorer.index_trades($1, $2) FROM explorer.pending_rewrites
			WHERE name = 'swaps' AND resume_from < $2`, lo, hi)
		return err
	}
	for lo := int64(0); lo <= PartitionsUpTo; lo += PartitionSize {
		if err := run(lo, lo+PartitionSize); err != nil {
			return fmt.Errorf("partition %s: %w", PartitionName("swaps", lo), err)
		}
	}
	if err := run(PartitionsUpTo+1, math.MaxInt64); err != nil {
		return fmt.Errorf("above the partitions: %w", err)
	}
	_, err := db.Exec(ctx, `DELETE FROM explorer.pending_rewrites WHERE name = 'swaps'`)
	return err
}
