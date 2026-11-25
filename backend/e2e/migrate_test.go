//go:build e2e

package e2e_test

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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil"
	"github.com/bze-alphateam/bze-scan/backend/internal/trades"
	"github.com/bze-alphateam/bze-scan/backend/migrations"
)

// sinkSchemaFile is the vendored CometBFT psql sink schema the compose
// database is initialised with.
const sinkSchemaFile = "../../docker/postgres/initdb/01-cometbft-indexer-schema.sql"

// latestVersion is the number of migrations.
const latestVersion = 35

// migrationVersions are the versions of the up migrations, ascending.
func migrationVersions(t *testing.T) []int {
	t.Helper()
	files, err := filepath.Glob("../migrations/*.up.sql")
	require.NoError(t, err)
	var out []int
	for _, f := range files {
		v, err := strconv.Atoi(strings.SplitN(filepath.Base(f), "_", 2)[0])
		require.NoError(t, err, f)
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

// stepsBefore is the number of down steps from the latest migration back to
// just before migration version: pinned to the migration a test means, so a
// later migration (or a gap in the numbers) does not move it.
func stepsBefore(t *testing.T, version int) int {
	t.Helper()
	n := 0
	for _, v := range migrationVersions(t) {
		if v >= version {
			n++
		}
	}
	return n
}

// sinkTables are the tables of the vendored sink schema.
var sinkTables = []string{"attributes", "blocks", "events", "tx_results"}

// explorerTables are the tables of the database schema design, partition
// parents included, partitions excluded.
var explorerTables = []string{
	"account_activity", "accounts", "backfill_checkpoints", "block_event_kinds",
	"block_events", "blocks", "candles", "chain_state", "chains", "daily_stats", "dao_ballot_reductions", "dao_events", "dao_proposals", "dao_votes", "daos", "denoms",
	"ibc_channels", "ibc_transfers", "index_failures", "indexer_state", "labels",
	"message_kinds", "messages", "order_closures", "order_fills", "order_messages", "orders",
	"param_snapshots", "pending_rewrites", "proposal_deposits", "proposal_non_voters", "proposal_votes", "proposals",
	"registry_assets", "swaps", "sync_jobs", "token_events", "token_holders",
	"transactions", "transfers", "upgrade_recovery", "upgrades", "validator_events", "validators",
}

// explorerFunctions are the functions the migrations create.
var explorerFunctions = []string{"ensure_partitions", "kept_events", "notify_block", "reclassify_unknown"}

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

	u, err := url.Parse(testutil.DatabaseURL())
	require.NoError(t, err)
	u.Path = "/" + name
	dbURL := u.String()

	if withSink {
		schema, err := os.ReadFile(sinkSchemaFile)
		require.NoError(t, err)
		_, err = connect(t, dbURL).Exec(string(schema))
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

func openMigrator(t *testing.T, dbURL string) *migrations.Migrator {
	t.Helper()
	mg, err := migrations.Open(context.Background(), dbURL, trades.MigrationStep(candles.Default), candles.MigrationStep())
	require.NoError(t, err)
	t.Cleanup(func() { _ = mg.Close() })
	return mg
}

func migrateUp(t *testing.T, dbURL string) migrations.UpResult {
	t.Helper()
	res, err := openMigrator(t, dbURL).Up(context.Background())
	require.NoError(t, err)
	return res
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

// tablesIn lists the tables of schema from information_schema, partitions
// excluded.
func tablesIn(t *testing.T, db *sql.DB, schema string) []string {
	return queryStrings(t, db, `SELECT t.table_name FROM information_schema.tables t
		WHERE t.table_schema = $1 AND t.table_type = 'BASE TABLE'
		AND NOT EXISTS (SELECT 1 FROM pg_inherits i
			WHERE i.inhrelid = (quote_ident(t.table_schema) || '.' || quote_ident(t.table_name))::regclass)`, schema)
}

func functionsIn(t *testing.T, db *sql.DB, schema string) []string {
	return queryStrings(t, db, `SELECT routine_name FROM information_schema.routines
		WHERE routine_schema = $1`, schema)
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

func TestMigrateUpBuildsTheExplorerSchema(t *testing.T) {
	dbURL := freshDatabase(t, true)
	db := connect(t, dbURL)

	res := migrateUp(t, dbURL)
	assert.Equal(t, migrations.UpResult{Version: latestVersion, Applied: true, Steps: []string{"partitions", "classification", "labels", "message_events", "params_v830", "swaps", "candles"}}, res)

	var version int64
	var dirty bool
	require.NoError(t, db.QueryRow(`SELECT version, dirty FROM explorer_migrations.schema_migrations`).Scan(&version, &dirty))
	assert.Equal(t, int64(latestVersion), version)
	assert.False(t, dirty)

	assert.Equal(t, explorerTables, tablesIn(t, db, "explorer"))
	assert.Equal(t, explorerFunctions, functionsIn(t, db, "explorer"))
	assert.Equal(t, sinkTables, tablesIn(t, db, "public"), "public holds only the sink's tables")
	assert.Equal(t, []string{"trg_notify_block"}, triggersOnSinkBlocks(t, db))

	for _, table := range migrations.PartitionedTables {
		want := migrations.PartitionNames(table, 0, migrations.PartitionsUpTo)
		sort.Strings(want)
		assert.Equal(t, want, partitionsOf(t, db, table), table)
	}

	for _, height := range []int64{25_000_000, 49_999_999} {
		_, err := db.Exec(`INSERT INTO explorer.blocks (height, time, hash) VALUES ($1, now(), 'AB')`, height)
		require.NoError(t, err, "height %d has a partition", height)
		var partition string
		require.NoError(t, db.QueryRow(`SELECT tableoid::regclass::text FROM explorer.blocks WHERE height = $1`, height).Scan(&partition))
		assert.Equal(t, "explorer."+migrations.PartitionName("blocks", height), partition)
	}
	_, err := db.Exec(`INSERT INTO explorer.blocks (height, time, hash) VALUES (50000000, now(), 'AB')`)
	assert.Error(t, err, "no partition beyond 49,999,999 until the live indexer tops up")
}

// TestSinkBlockInsertNotifiesTheHeight runs on the compose database, migrated
// through the testutil helper.
func TestSinkBlockInsertNotifiesTheHeight(t *testing.T) {
	openDB(t) // waits for the database
	dbURL := testutil.Migrate(t)

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

func TestMigrateUpTwiceIsANoOp(t *testing.T) {
	dbURL := freshDatabase(t, true)
	db := connect(t, dbURL)

	first := migrateUp(t, dbURL)
	relations := func() []string {
		return queryStrings(t, db, `SELECT n.nspname || '.' || c.relname FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname IN ('explorer', 'explorer_migrations', 'public')`)
	}
	before := relations()

	second := migrateUp(t, dbURL)
	assert.False(t, second.Applied)
	assert.Equal(t, first.Version, second.Version)
	assert.Equal(t, before, relations(), "no relation created or dropped")
	assert.Equal(t, []string{"trg_notify_block"}, triggersOnSinkBlocks(t, db))
}

func TestMigrateDownToZeroLeavesTheSinkIntact(t *testing.T) {
	dbURL := freshDatabase(t, true)
	db := connect(t, dbURL)
	insertSinkBlock(t, db, 24_998_316)
	migrateUp(t, dbURL)
	insertSinkBlock(t, db, 24_998_317) // fires the trigger while it exists

	mg := openMigrator(t, dbURL)
	version, err := mg.Down(latestVersion)
	require.NoError(t, err)
	assert.Equal(t, uint(0), version)

	var schema sql.NullString
	require.NoError(t, db.QueryRow(`SELECT to_regnamespace('explorer')::text`).Scan(&schema))
	assert.False(t, schema.Valid, "the explorer schema is gone")
	assert.Empty(t, triggersOnSinkBlocks(t, db))
	assert.Equal(t, sinkTables, tablesIn(t, db, "public"))
	assert.Equal(t, []string{"24998316", "24998317"}, queryStrings(t, db, `SELECT height::text FROM public.blocks`))

	// And back up again from zero.
	res := migrateUp(t, dbURL)
	assert.Equal(t, uint(latestVersion), res.Version)
	assert.Equal(t, explorerTables, tablesIn(t, db, "explorer"))
}

func TestMigrateRequiresTheSinkSchema(t *testing.T) {
	dbURL := freshDatabase(t, false)

	_, err := migrations.Open(context.Background(), dbURL)
	require.ErrorIs(t, err, migrations.ErrSinkSchemaMissing)
	assert.Contains(t, err.Error(), "public.blocks")

	assert.Equal(t, []string{"public"}, queryStrings(t, connect(t, dbURL), `SELECT nspname FROM pg_namespace
		WHERE nspname NOT LIKE 'pg\_%' AND nspname <> 'information_schema'`), "nothing is created")
}

func TestReclassifyUnknown(t *testing.T) {
	dbURL := freshDatabase(t, true)
	db := connect(t, dbURL)
	migrateUp(t, dbURL)

	_, err := db.Exec(`INSERT INTO explorer.account_activity
		(address, height, tx_index, time, category, kind, is_signer, msg_types) VALUES
		('bze1sender',   100, 0, now(), 'other', 'other', true,  '{/cosmos.bank.v1beta1.MsgSend}'),
		('bze1receiver', 100, 0, now(), 'other', 'other', false, '{/cosmos.bank.v1beta1.MsgSend}'),
		('bze1someone',  101, 0, now(), 'other', 'other', true,  '{/bze.unknown.MsgFoo}'),
		('bze1known',    102, 0, now(), 'staking', 'delegate', true, '{/cosmos.bank.v1beta1.MsgSend}')`)
	require.NoError(t, err) // message_kinds holds MsgSend: `migrate up` mirrored the classification

	var updated int64
	require.NoError(t, db.QueryRow(`SELECT explorer.reclassify_unknown()`).Scan(&updated))
	assert.Equal(t, int64(2), updated)

	assert.Equal(t, []string{
		"bze1known staking delegate",
		"bze1receiver received send",
		"bze1sender sent send",
		"bze1someone other other",
	}, queryStrings(t, db, `SELECT address || ' ' || category || ' ' || kind FROM explorer.account_activity`))
}

// Activity stored as "other" before the v8.3.0 messages were classified (an
// explorer that indexed the upgrade ahead of this release) is reclassified
// by the next `migrate up`, which mirrors the classification first.
func TestMigrateUpReclassifiesTheV830MessagesStoredAsOther(t *testing.T) {
	dbURL := freshDatabase(t, true)
	db := connect(t, dbURL)
	migrateUp(t, dbURL)

	_, err := db.Exec(`DELETE FROM explorer.message_kinds WHERE type_url LIKE '/bze.dao.%' OR type_url LIKE '/bze.guardian.%'
		OR type_url IN ('/bze.tokenfactory.MsgDisableMinting', '/bze.tokenfactory.MsgSetDenomConfig')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO explorer.account_activity
		(address, height, tx_index, time, category, kind, is_signer, msg_types) VALUES
		('bze1creator', 100, 0, now(), 'other', 'other', true,  '{/bze.dao.MsgCreateDao}'),
		('bze1halter',  101, 0, now(), 'other', 'other', true,  '{/bze.guardian.v1.MsgHaltTradingDenoms}'),
		('bze1admin',   102, 0, now(), 'other', 'other', true,  '{/bze.tokenfactory.MsgDisableMinting}'),
		('bze1gov',     103, 0, now(), 'other', 'other', true,  '{/bze.tradebin.MsgUpdatePools}')`)
	require.NoError(t, err)

	res := migrateUp(t, dbURL)
	assert.False(t, res.Applied, "no schema migration is pending; the post-migration steps still run")
	assert.Contains(t, res.Steps, "classification")

	assert.Equal(t, []string{
		"bze1admin tokens token_admin",
		"bze1creator governance dao_create",
		"bze1gov other other",
		"bze1halter governance guardian",
	}, queryStrings(t, db, `SELECT address || ' ' || category || ' ' || kind FROM explorer.account_activity`),
		"gov-only messages stay other")
}

// TestMigrateCommand runs the real binary's migrate subcommands.
func TestMigrateCommand(t *testing.T) {
	dbURL := freshDatabase(t, true)
	bin := filepath.Join(t.TempDir(), "bze-scan")
	out, err := exec.Command("go", "build", "-o", bin, "../cmd/bze-scan").CombinedOutput()
	require.NoError(t, err, string(out))

	run := func(args ...string) string {
		cmd := exec.Command(bin, append([]string{"migrate"}, args...)...)
		cmd.Dir = t.TempDir() // no stray .env
		cmd.Env = append(os.Environ(), "DATABASE_URL="+dbURL, "LOG_FORMAT=json", "LOG_LEVEL=info")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		assert.NotContains(t, string(out), dbURL, "the database URL is never logged")
		return string(out)
	}

	first := run() // no subcommand: up
	assert.Contains(t, first, `"msg":"database migrated"`)
	assert.Contains(t, first, `"applied":true`)
	assert.Contains(t, run("up"), `"applied":false`)
	assert.Equal(t, strconv.Itoa(latestVersion), strings.TrimSpace(run("version")))
	versions := migrationVersions(t)
	twoDown := strconv.Itoa(versions[len(versions)-3])
	assert.Contains(t, run("down", "2"), `"version":`+twoDown)
	assert.Equal(t, twoDown, strings.TrimSpace(run("version")))
}

// Proposals dropped for lack of deposit were stored as rejected: the
// migration renames them, a proposal rejected by vote stays.
func TestMigrateDropsTheProposalsThatNeverReachedAVote(t *testing.T) {
	dbURL := freshDatabase(t, true)
	db := connect(t, dbURL)
	migrateUp(t, dbURL)
	mg := openMigrator(t, dbURL)
	_, err := mg.Down(stepsBefore(t, 13)) // back to before 000013
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO explorer.proposals (id, title, kind, status, submit_time, voting_start_time, updated_at)
		VALUES (1, 'spam', 'text', 'rejected', now(), NULL, now()),
		       (2, 'voted', 'text', 'rejected', now(), now(), now()),
		       (3, 'open', 'text', 'deposit_period', now(), NULL, now())`)
	require.NoError(t, err)

	migrateUp(t, dbURL)
	assert.Equal(t, []string{"1 dropped", "2 rejected", "3 deposit_period"}, queryStrings(t, db,
		`SELECT concat_ws(' ', id, status) FROM explorer.proposals ORDER BY id`))
}

// Migration 000031 adds the merge columns and swaps the block-event index of
// the order outcomes; the down migration brings the old one back.
func TestMigrationMergedOrders(t *testing.T) {
	dbURL := freshDatabase(t, true)
	db := connect(t, dbURL)
	migrateUp(t, dbURL)
	cols := func() []string {
		return queryStrings(t, db, `SELECT column_name FROM information_schema.columns
			WHERE table_schema = 'explorer' AND table_name = 'orders' AND column_name IN ('merged_from', 'added_amount')`)
	}
	indexes := func() []string {
		return queryStrings(t, db, `SELECT indexname FROM pg_indexes WHERE schemaname = 'explorer'
			AND tablename = 'block_events' AND indexname IN ('idx_block_events_order_outcome', 'idx_block_events_order_refund')`)
	}
	assert.Equal(t, []string{"added_amount", "merged_from"}, cols())
	assert.Equal(t, []string{"idx_block_events_order_refund"}, indexes())
	assert.Equal(t, []string{"merged_into"}, queryStrings(t, db, `SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'explorer' AND table_name = 'order_closures' AND column_name = 'merged_into'`))

	// A status the old list never had needs no change to the table.
	_, err := db.Exec(`INSERT INTO explorer.orders (order_id, market_id, owner, side, price, amount, status, created_height,
		created_time, updated_at) VALUES ('1', 'm', 'o', 'buy', 1, 1, 'merged', 1, now(), now())`)
	require.NoError(t, err)

	mg := openMigrator(t, dbURL)
	_, err = mg.Down(stepsBefore(t, 31)) // back to before 000031
	require.NoError(t, err)
	assert.Empty(t, cols())
	assert.Equal(t, []string{"idx_block_events_order_outcome"}, indexes())

	migrateUp(t, dbURL)
	assert.Equal(t, []string{"added_amount", "merged_from"}, cols())
	assert.Equal(t, []string{"idx_block_events_order_refund"}, indexes())
}

// Migration 000033 adds the factory token settings to denoms; the down
// migration drops them, and a later up brings them back empty.
func TestMigrationTokenSettings(t *testing.T) {
	dbURL := freshDatabase(t, true)
	db := connect(t, dbURL)
	migrateUp(t, dbURL)
	cols := func() []string {
		return queryStrings(t, db, `SELECT column_name FROM information_schema.columns
			WHERE table_schema = 'explorer' AND table_name = 'denoms'
			  AND column_name IN ('mintable', 'community_pool', 'transfer_fee', 'gas_surcharge') ORDER BY column_name`)
	}
	assert.Equal(t, []string{"community_pool", "gas_surcharge", "mintable", "transfer_fee"}, cols())
	_, err := db.Exec(`INSERT INTO explorer.denoms (denom, kind, exponent, supply, mintable, transfer_fee, gas_surcharge, updated_at)
		VALUES ('factory/bze1x/uz', 'factory', 6, 0, true, 0.010000000000000000, 0, now())`)
	require.NoError(t, err)

	_, err = openMigrator(t, dbURL).Down(latestVersion - 32) // back to before 000033
	require.NoError(t, err)
	assert.Empty(t, cols())
	assert.Equal(t, []string{"factory/bze1x/uz"}, queryStrings(t, db, `SELECT denom FROM explorer.denoms`), "the denom stays")

	migrateUp(t, dbURL)
	assert.Equal(t, []string{"community_pool", "gas_surcharge", "mintable", "transfer_fee"}, cols())
	assert.Equal(t, []string{"<nil>"}, queryStrings(t, db, `SELECT coalesce(mintable::text, '<nil>') FROM explorer.denoms`), "settings come back unknown")
}
