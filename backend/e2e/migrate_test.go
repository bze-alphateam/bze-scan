//go:build e2e

package e2e

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/migrations"
)

// sinkSchemaFile is the vendored CometBFT psql sink schema the compose
// database is initialised with.
const sinkSchemaFile = "../../docker/postgres/initdb/01-cometbft-indexer-schema.sql"

// sinkTables are the tables of the vendored sink schema.
var sinkTables = []string{"attributes", "blocks", "events", "tx_results"}

// explorerTables are the tables of the database schema design, partition
// parents included, partitions excluded.
var explorerTables = []string{
	"account_activity", "accounts", "backfill_checkpoints", "block_event_kinds",
	"block_events", "blocks", "chain_state", "chains", "daily_stats", "denoms",
	"ibc_channels", "ibc_transfers", "index_failures", "indexer_state", "labels",
	"message_kinds", "messages", "order_fills", "order_messages", "orders",
	"param_snapshots", "proposal_deposits", "proposal_votes", "proposals",
	"registry_assets", "schema_migrations", "sync_jobs", "token_events",
	"token_holders", "transactions", "transfers", "validator_events", "validators",
}

// freshDatabase creates an empty database on the compose PostgreSQL, with the
// sink schema installed when withSink is set, drops it when the test ends and
// returns its URL.
func freshDatabase(t *testing.T, withSink bool) string {
	t.Helper()
	admin := openDB(t)

	b := make([]byte, 6)
	_, err := rand.Read(b)
	require.NoError(t, err)
	name := "e2e_" + hex.EncodeToString(b)
	_, err = admin.Exec(`CREATE DATABASE ` + name)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = admin.Exec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`) })

	base := os.Getenv("E2E_DATABASE_URL")
	if base == "" {
		base = defaultDatabaseURL
	}
	u, err := url.Parse(base)
	require.NoError(t, err)
	u.Path = "/" + name
	dbURL := u.String()

	if withSink {
		schema, err := os.ReadFile(sinkSchemaFile)
		require.NoError(t, err)
		db := connect(t, dbURL)
		_, err = db.Exec(string(schema))
		require.NoError(t, err)
	}
	return dbURL
}

func connect(t *testing.T, dbURL string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dbURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.Ping())
	return db
}

func insertSinkBlock(t *testing.T, db *sql.DB, height int64) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO public.blocks (height, chain_id, created_at) VALUES ($1, 'beezee-1', now())`, height)
	require.NoError(t, err)
}

func queryStrings(t *testing.T, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.Query(query, args...)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	require.NoError(t, rows.Err())
	sort.Strings(out)
	return out
}

// tablesIn lists the tables of schema that are not partitions.
func tablesIn(t *testing.T, db *sql.DB, schema string) []string {
	return queryStrings(t, db, `SELECT c.relname FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r', 'p') AND NOT c.relispartition`, schema)
}

func partitionsOf(t *testing.T, db *sql.DB, table string) []string {
	return queryStrings(t, db, `SELECT c.relname FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = ('explorer.' || $1)::regclass`, table)
}

func triggersOnSinkBlocks(t *testing.T, db *sql.DB) []string {
	return queryStrings(t, db, `SELECT tgname FROM pg_trigger
		WHERE tgrelid = 'public.blocks'::regclass AND NOT tgisinternal`)
}

func TestMigrateBuildsTheExplorerSchema(t *testing.T) {
	dbURL := freshDatabase(t, true)
	db := connect(t, dbURL)
	const sinkHead = 24_998_316
	insertSinkBlock(t, db, sinkHead)

	res, err := migrations.Run(context.Background(), dbURL)
	require.NoError(t, err)
	assert.Equal(t, migrations.Result{
		Version: 3, Applied: true, PartitionsFrom: 0, PartitionsTo: sinkHead + migrations.PartitionLookAhead,
	}, res)

	var version int64
	var dirty bool
	require.NoError(t, db.QueryRow(`SELECT version, dirty FROM explorer.schema_migrations`).Scan(&version, &dirty))
	assert.Equal(t, int64(3), version)
	assert.False(t, dirty)

	assert.Equal(t, explorerTables, tablesIn(t, db, "explorer"))
	assert.Equal(t, sinkTables, tablesIn(t, db, "public"), "public holds only the sink's tables")

	for _, table := range migrations.PartitionedTables {
		want := migrations.PartitionNames(table, res.PartitionsFrom, res.PartitionsTo)
		sort.Strings(want)
		assert.Equal(t, want, partitionsOf(t, db, table), table)
		assert.Equal(t, table+"_p000044", want[len(want)-1], "partitions end at the one holding head + look-ahead")
	}

	_, err = db.Exec(`INSERT INTO explorer.blocks (height, time, hash) VALUES ($1, now(), 'AB')`, sinkHead)
	require.NoError(t, err)
	var partition string
	require.NoError(t, db.QueryRow(`SELECT tableoid::regclass::text FROM explorer.blocks WHERE height = $1`, sinkHead).Scan(&partition))
	assert.Equal(t, "explorer."+migrations.PartitionName("blocks", sinkHead), partition)

	assert.Equal(t, []string{"trg_notify_block"}, triggersOnSinkBlocks(t, db))
}

func TestSinkBlockInsertNotifiesTheHeight(t *testing.T) {
	dbURL := freshDatabase(t, true)
	_, err := migrations.Run(context.Background(), dbURL)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	listener, err := pgx.Connect(ctx, dbURL)
	require.NoError(t, err)
	defer func() { _ = listener.Close(context.Background()) }()
	_, err = listener.Exec(ctx, `LISTEN `+migrations.NotifyChannel)
	require.NoError(t, err)

	const height = 24_998_317
	insertSinkBlock(t, connect(t, dbURL), height)

	n, err := listener.WaitForNotification(ctx)
	require.NoError(t, err)
	assert.Equal(t, migrations.NotifyChannel, n.Channel)
	assert.Equal(t, strconv.Itoa(height), n.Payload)
}

func TestMigrateTwiceIsANoOp(t *testing.T) {
	dbURL := freshDatabase(t, true)
	db := connect(t, dbURL)

	first, err := migrations.Run(context.Background(), dbURL)
	require.NoError(t, err)
	relations := func() []string {
		return queryStrings(t, db, `SELECT c.relname FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname IN ('explorer', 'public')`)
	}
	before := relations()

	second, err := migrations.Run(context.Background(), dbURL)
	require.NoError(t, err)
	assert.False(t, second.Applied)
	assert.Equal(t, first.Version, second.Version)
	assert.Equal(t, first.PartitionsTo, second.PartitionsTo)
	assert.Equal(t, before, relations(), "no relation created or dropped")
	assert.Equal(t, []string{"trg_notify_block"}, triggersOnSinkBlocks(t, db))
}

func TestMigrateRequiresTheSinkSchema(t *testing.T) {
	dbURL := freshDatabase(t, false)

	_, err := migrations.Run(context.Background(), dbURL)
	require.ErrorIs(t, err, migrations.ErrSinkSchemaMissing)
	assert.Contains(t, err.Error(), "public.blocks")

	var schema sql.NullString
	require.NoError(t, connect(t, dbURL).QueryRow(`SELECT to_regnamespace('explorer')::text`).Scan(&schema))
	assert.False(t, schema.Valid, "nothing is created")
}

// TestMigrateCommand runs the real binary's migrate subcommand twice.
func TestMigrateCommand(t *testing.T) {
	dbURL := freshDatabase(t, true)
	bin := filepath.Join(t.TempDir(), "bze-scan")
	build := exec.Command("go", "build", "-o", bin, "../cmd/bze-scan")
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))

	run := func() string {
		cmd := exec.Command(bin, "migrate")
		cmd.Dir = t.TempDir() // no stray .env
		cmd.Env = append(os.Environ(), "DATABASE_URL="+dbURL, "LOG_FORMAT=json", "LOG_LEVEL=info")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		assert.NotContains(t, string(out), dbURL, "the database URL is never logged")
		return string(out)
	}

	first := run()
	assert.Contains(t, first, `"msg":"database migrated"`)
	assert.Contains(t, first, `"applied":true`)
	assert.Contains(t, first, `"version":3`)
	assert.Contains(t, run(), `"applied":false`)
}
