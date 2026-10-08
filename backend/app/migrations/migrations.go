// Package migrations holds the explorer's versioned SQL migrations (embedded,
// applied with golang-migrate, up only) and Run, which brings a database to
// the latest version and creates the height partitions.
//
// The database must already hold the CometBFT psql sink schema in public: the
// explorer's only attachment to it is the notification trigger on
// public.blocks. Everything else lives in the explorer schema, including the
// version table explorer.schema_migrations.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

const (
	// Schema is the PostgreSQL schema the explorer owns.
	Schema = "explorer"
	// VersionTable is golang-migrate's version table, inside Schema.
	VersionTable = "schema_migrations"
	// NotifyChannel is the channel trg_notify_block notifies on, with the
	// block height as the payload.
	NotifyChannel = "explorer_block"
)

// ErrSinkSchemaMissing is returned when the database has no public.blocks,
// i.e. it does not hold the CometBFT psql sink schema.
var ErrSinkSchemaMissing = errors.New("sink table public.blocks not found: the database must hold the CometBFT psql sink schema (enable the node's psql indexer first)")

//go:embed sql/*.sql
var files embed.FS

// Source returns the embedded migrations as a golang-migrate source.
func Source() (source.Driver, error) {
	return iofs.New(files, "sql")
}

// Result describes what Run did.
type Result struct {
	// Version is the schema version after the run.
	Version uint
	// Applied is false when the schema was already at the latest version.
	Applied bool
	// PartitionsFrom and PartitionsTo are the heights passed to
	// explorer.ensure_partitions.
	PartitionsFrom int64
	PartitionsTo   int64
}

// Run connects to databaseURL, applies the pending migrations and creates the
// partitions for [0, head + PartitionLookAhead], head being the highest height
// in public.blocks or explorer.blocks. It is idempotent: a second run applies
// nothing and creates no partition. golang-migrate's advisory lock serialises
// concurrent runs.
func Run(ctx context.Context, databaseURL string) (Result, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return Result{}, fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return Result{}, fmt.Errorf("connect to database: %w", err)
	}

	var sinkPresent bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.blocks') IS NOT NULL`).Scan(&sinkPresent); err != nil {
		return Result{}, fmt.Errorf("look up public.blocks: %w", err)
	}
	if !sinkPresent {
		return Result{}, ErrSinkSchemaMissing
	}

	// golang-migrate creates its version table before running anything, so
	// the schema that holds it must exist first.
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+Schema); err != nil {
		return Result{}, fmt.Errorf("create schema %s: %w", Schema, err)
	}

	m, err := newMigrate(db)
	if err != nil {
		return Result{}, err
	}
	// Closing m releases its connection and closes db too (its pgx driver
	// owns db); runs before the deferred db.Close, which is then a no-op.
	defer func() { _, _ = m.Close() }()

	res, err := up(m)
	if err != nil {
		return Result{}, err
	}

	var head int64
	if err := db.QueryRowContext(ctx, `SELECT GREATEST(
		(SELECT max(height) FROM public.blocks),
		(SELECT max(height) FROM explorer.blocks),
		0)`).Scan(&head); err != nil {
		return Result{}, fmt.Errorf("read the highest known height: %w", err)
	}
	res.PartitionsFrom, res.PartitionsTo = PartitionRange(head)
	if _, err := db.ExecContext(ctx, `SELECT explorer.ensure_partitions($1, $2)`,
		res.PartitionsFrom, res.PartitionsTo); err != nil {
		return Result{}, fmt.Errorf("create partitions: %w", err)
	}
	return res, nil
}

// newMigrate prepares golang-migrate over the embedded migrations, with its
// version table in the explorer schema.
func newMigrate(db *sql.DB) (*migrate.Migrate, error) {
	src, err := Source()
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}
	drv, err := migratepgx.WithInstance(db, &migratepgx.Config{
		SchemaName:      Schema,
		MigrationsTable: VersionTable,
	})
	if err != nil {
		return nil, fmt.Errorf("prepare migrations: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "pgx5", drv)
	if err != nil {
		return nil, fmt.Errorf("prepare migrations: %w", err)
	}
	return m, nil
}

// up applies the pending migrations and reports the resulting version.
func up(m *migrate.Migrate) (Result, error) {
	res := Result{Applied: true}
	if err := m.Up(); err != nil {
		if !errors.Is(err, migrate.ErrNoChange) {
			return Result{}, fmt.Errorf("apply migrations: %w", err)
		}
		res.Applied = false
	}
	version, dirty, err := m.Version()
	if err != nil {
		return Result{}, fmt.Errorf("read schema version: %w", err)
	}
	if dirty {
		return Result{}, fmt.Errorf("schema version %d is dirty", version)
	}
	res.Version = version
	return res, nil
}
