package migrations

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bze-alphateam/bze-scan/backend/internal/classify"
	"github.com/bze-alphateam/bze-scan/backend/internal/labels"
	"github.com/bze-alphateam/bze-scan/backend/internal/params"
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
	{Name: "params_v830", Run: rewriteParamSnapshotsV830},
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

// rewriteParamSnapshotsV830 rewrites the latest stored tradebin and burner
// parameter snapshots into the shape the v8.3.0 types render (see
// params.UpgradeStoredToV830), so the params set does not record the
// explorer's own change of types as a parameter change. Only the latest row
// of each module is rewritten (no row is added); a row already in the new
// shape is left alone.
func rewriteParamSnapshotsV830(ctx context.Context, db DB) error {
	rows, err := db.Query(ctx, `SELECT DISTINCT ON (module) module, height, params::text
		FROM explorer.param_snapshots WHERE module IN ('tradebin', 'burner')
		ORDER BY module, height DESC`)
	if err != nil {
		return err
	}
	type latest struct {
		module string
		height int64
		params string
	}
	var found []latest
	for rows.Next() {
		var l latest
		if err := rows.Scan(&l.module, &l.height, &l.params); err != nil {
			rows.Close()
			return err
		}
		found = append(found, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, l := range found {
		out, changed, err := params.UpgradeStoredToV830(l.module, json.RawMessage(l.params))
		if err != nil {
			return err
		}
		if !changed {
			continue
		}
		if _, err := db.Exec(ctx, `UPDATE explorer.param_snapshots SET params = $3::jsonb
			WHERE module = $1 AND height = $2`, l.module, l.height, string(out)); err != nil {
			return fmt.Errorf("%s params snapshot at %d: %w", l.module, l.height, err)
		}
	}
	return nil
}
