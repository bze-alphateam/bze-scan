package holders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// PGStore writes the set into explorer.token_holders and
// denoms.holders_count.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// Denoms returns every stored denom, sorted.
func (s *PGStore) Denoms(ctx context.Context) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT denom FROM explorer.denoms ORDER BY denom COLLATE "C"`)
	if err != nil {
		return nil, fmt.Errorf("holders denoms: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("holders denoms: %w", err)
	}
	return out, nil
}

// Cursor returns the holders job's stored cursor.
func (s *PGStore) Cursor(ctx context.Context) (json.RawMessage, error) {
	var raw []byte
	err := s.db.QueryRow(ctx, `SELECT cursor FROM explorer.sync_jobs WHERE job = $1`, statesync.Holders).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("holders cursor: %w", err)
	}
	return raw, nil
}

// SavePage upserts one page of owners.
func (s *PGStore) SavePage(ctx context.Context, denom string, stamp time.Time, owners []Owner) error {
	addrs := make([]string, len(owners))
	balances := make([]string, len(owners))
	for i, o := range owners {
		addrs[i], balances[i] = o.Address, o.Balance
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO explorer.token_holders (denom, address, balance, updated_at)
		SELECT $1, o.address, o.balance::numeric, $4 FROM unnest($2::text[], $3::text[]) AS o(address, balance)
		ON CONFLICT (denom, address) DO UPDATE SET balance = EXCLUDED.balance, updated_at = EXCLUDED.updated_at`,
		denom, addrs, balances, stamp); err != nil {
		return fmt.Errorf("holders page %s: %w", denom, err)
	}
	return nil
}

// Finish deletes the denom's rows older than the run and recomputes its
// holders_count: the owners of at least one display unit (10^exponent base
// units).
func (s *PGStore) Finish(ctx context.Context, denom string, stamp time.Time) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("holders finish %s: %w", denom, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	batch := &pgx.Batch{}
	batch.Queue(`DELETE FROM explorer.token_holders WHERE denom = $1 AND updated_at < $2`, denom, stamp)
	batch.Queue(`UPDATE explorer.denoms d SET holders_count = (SELECT count(*) FROM explorer.token_holders h
			WHERE h.denom = d.denom AND h.balance >= power(10::numeric, d.exponent))
		WHERE d.denom = $1`, denom)
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("holders finish %s: %w", denom, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("holders finish %s: %w", denom, err)
	}
	return nil
}
