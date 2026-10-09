package statesync

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// DB is the part of a pgx pool the stores need; *pgxpool.Pool satisfies it.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// PGJobStore records runs in explorer.sync_jobs.
type PGJobStore struct {
	db DB
}

// NewPGJobStore returns a job store over db.
func NewPGJobStore(db DB) *PGJobStore {
	return &PGJobStore{db: db}
}

// RecordRun upserts the job's row: last_run_at is the run's start,
// last_success_at moves on success only, last_error is the run's error (NULL
// on success), and the cursor is replaced when the run carries one.
func (s *PGJobStore) RecordRun(ctx context.Context, r Run) error {
	var success, lastErr, cursor any
	if r.Err == nil {
		success = r.Finished
	} else {
		lastErr = r.Err.Error()
	}
	if r.Cursor != nil {
		cursor = string(r.Cursor)
	}
	_, err := s.db.Exec(ctx, `INSERT INTO explorer.sync_jobs (job, last_run_at, last_success_at, cursor, last_error)
		VALUES ($1, $2, $3, $4::jsonb, $5)
		ON CONFLICT (job) DO UPDATE SET
		  last_run_at     = EXCLUDED.last_run_at,
		  last_success_at = COALESCE(EXCLUDED.last_success_at, explorer.sync_jobs.last_success_at),
		  cursor          = COALESCE(EXCLUDED.cursor, explorer.sync_jobs.cursor),
		  last_error      = EXCLUDED.last_error`,
		r.Job, r.Started, success, cursor, lastErr)
	if err != nil {
		return fmt.Errorf("sync_jobs %s: %w", r.Job, err)
	}
	return nil
}
