// Package migrations holds the explorer's versioned SQL migrations
// (NNNNNN_<name>.up.sql and .down.sql, embedded) and the Migrator that applies
// them with golang-migrate, then runs the post-migration Go steps.
//
// The database must already hold the CometBFT psql sink schema in public: the
// explorer's only attachment to it is the notification trigger on
// public.blocks. Everything else lives in the explorer schema. The version
// table lives in its own schema, VersionSchema, so that migrating down to
// version 0 can drop the explorer schema without losing it.
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
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

const (
	// Schema is the PostgreSQL schema the explorer owns.
	Schema = "explorer"
	// VersionSchema holds golang-migrate's version table, VersionTable.
	VersionSchema = "explorer_migrations"
	// VersionTable is golang-migrate's version table.
	VersionTable = "schema_migrations"
	// NotifyChannel is the channel trg_notify_block notifies on, with the
	// block height as the payload.
	NotifyChannel = "explorer_block"
)

// ErrSinkSchemaMissing is returned when the database has no public.blocks,
// i.e. it does not hold the CometBFT psql sink schema.
var ErrSinkSchemaMissing = errors.New("sink table public.blocks not found: the database must hold the CometBFT psql sink schema (enable the node's psql indexer first)")

//go:embed *.sql
var files embed.FS

// Source returns the embedded migrations as a golang-migrate source.
func Source() (source.Driver, error) {
	return iofs.New(files, ".")
}

// engine is the part of golang-migrate the Migrator drives;
// *migrate.Migrate satisfies it.
type engine interface {
	Up() error
	Steps(n int) error
	Version() (version uint, dirty bool, err error)
	Close() (source error, database error)
}

// Migrator applies the embedded migrations to one database. Close it when
// done.
type Migrator struct {
	url string
	m   engine
}

// Open connects to databaseURL, checks that it holds the sink schema and
// prepares golang-migrate. Nothing is created when the sink schema is
// missing.
func Open(ctx context.Context, databaseURL string) (*Migrator, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	mg, err := open(ctx, databaseURL, db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return mg, nil
}

func open(ctx context.Context, databaseURL string, db *sql.DB) (*Migrator, error) {
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	var sinkPresent bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.blocks') IS NOT NULL`).Scan(&sinkPresent); err != nil {
		return nil, fmt.Errorf("look up public.blocks: %w", err)
	}
	if !sinkPresent {
		return nil, ErrSinkSchemaMissing
	}

	// golang-migrate creates its version table when it is prepared, so the
	// schema that holds it must exist first.
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+VersionSchema); err != nil {
		return nil, fmt.Errorf("create schema %s: %w", VersionSchema, err)
	}

	src, err := Source()
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}
	drv, err := migratepgx.WithInstance(db, &migratepgx.Config{
		SchemaName:      VersionSchema,
		MigrationsTable: VersionTable,
	})
	if err != nil {
		return nil, fmt.Errorf("prepare migrations: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "pgx5", drv)
	if err != nil {
		return nil, fmt.Errorf("prepare migrations: %w", err)
	}
	return &Migrator{url: databaseURL, m: m}, nil
}

// Close releases the connections.
func (mg *Migrator) Close() error {
	// The pgx driver owns db and closes it along with its own connection.
	srcErr, dbErr := mg.m.Close()
	return errors.Join(srcErr, dbErr)
}

// UpResult describes what Up did.
type UpResult struct {
	// Version is the schema version after the run.
	Version uint
	// Applied is false when the schema was already at the latest version.
	Applied bool
	// Steps are the names of the post-migration steps that ran, in order.
	Steps []string
}

// Up applies every pending migration, then runs the post-migration steps. It
// is idempotent: a second run applies nothing and the steps change nothing.
// golang-migrate's advisory lock serialises concurrent runs.
func (mg *Migrator) Up(ctx context.Context) (UpResult, error) {
	res := UpResult{Applied: true}
	if err := mg.m.Up(); err != nil {
		if !errors.Is(err, migrate.ErrNoChange) {
			return UpResult{}, fmt.Errorf("apply migrations: %w", err)
		}
		res.Applied = false
	}
	version, _, err := mg.Version()
	if err != nil {
		return UpResult{}, err
	}
	res.Version = version

	pool, err := pgxpool.New(ctx, mg.url)
	if err != nil {
		return UpResult{}, fmt.Errorf("open pool for post-migration steps: %w", err)
	}
	defer pool.Close()
	for _, step := range postMigrate {
		if err := step.Run(ctx, pool); err != nil {
			return UpResult{}, fmt.Errorf("post-migration step %s: %w", step.Name, err)
		}
		res.Steps = append(res.Steps, step.Name)
	}
	return res, nil
}

// Down reverts the last n applied migrations (n >= 1) and returns the
// resulting version (0 when none is left). Post-migration steps do not run.
func (mg *Migrator) Down(n int) (uint, error) {
	if n < 1 {
		return 0, fmt.Errorf("down needs a positive number of migrations, got %d", n)
	}
	if err := mg.m.Steps(-n); err != nil {
		return 0, fmt.Errorf("revert migrations: %w", err)
	}
	version, _, err := mg.Version()
	return version, err
}

// Version returns the current schema version (0 when no migration is
// applied) and whether a failed migration left it dirty.
func (mg *Migrator) Version() (uint, bool, error) {
	version, dirty, err := mg.m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read schema version: %w", err)
	}
	return version, dirty, nil
}
