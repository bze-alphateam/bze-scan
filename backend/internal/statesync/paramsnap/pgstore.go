package paramsnap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PGStore reads and writes explorer.param_snapshots and reads the
// proposals.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// Latest returns the newest snapshot of every module.
func (s *PGStore) Latest(ctx context.Context) (map[string]Latest, error) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT ON (module) module, height, params
		FROM explorer.param_snapshots ORDER BY module, height DESC`)
	if err != nil {
		return nil, fmt.Errorf("latest param snapshots: %w", err)
	}
	defer rows.Close()
	out := map[string]Latest{}
	for rows.Next() {
		var m string
		var l Latest
		var raw []byte
		if err := rows.Scan(&m, &l.Height, &raw); err != nil {
			return nil, fmt.Errorf("latest param snapshots: %w", err)
		}
		l.Params = json.RawMessage(raw)
		out[m] = l
	}
	return out, rows.Err()
}

// ProposalKind reads a proposal's kind and status.
func (s *PGStore) ProposalKind(ctx context.Context, id uint64) (kind, status string, found bool, err error) {
	err = s.db.QueryRow(ctx, `SELECT kind, status FROM explorer.proposals WHERE id = $1`, int64(id)).Scan(&kind, &status) //nolint:gosec // proposal ids fit an int64
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("proposal %d: %w", id, err)
	}
	return kind, status, true, nil
}

// upgradeSQL finds a passed upgrade by the plan height of its message: a
// MsgSoftwareUpgrade's plan, or the plan of the legacy content a
// MsgExecLegacyContent carries.
const upgradeSQL = `SELECT p.id
	FROM explorer.proposals p,
	     jsonb_array_elements(CASE WHEN jsonb_typeof(p.messages) = 'array' THEN p.messages END) m,
	     LATERAL (SELECT coalesce(m->'plan'->>'height', m->'content'->'plan'->>'height') AS h) plan
	WHERE p.status = 'passed' AND p.kind = 'software_upgrade' AND plan.h ~ '^[0-9]{1,18}$'
	  AND plan.h::bigint > $1 AND plan.h::bigint <= $2
	ORDER BY plan.h::bigint DESC, p.id DESC LIMIT 1`

// UpgradeBetween returns the passed upgrade planned in (after, upTo].
func (s *PGStore) UpgradeBetween(ctx context.Context, after, upTo int64) (*uint64, error) {
	var id int64
	err := s.db.QueryRow(ctx, upgradeSQL, after, upTo).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("upgrade between %d and %d: %w", after, upTo, err)
	}
	u := uint64(id) //nolint:gosec // proposal ids are positive
	return &u, nil
}

// Insert writes the snapshots in one transaction; a second snapshot of a
// module at one height replaces the first.
func (s *PGStore) Insert(ctx context.Context, snaps []Snapshot) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("param snapshots insert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	batch := &pgx.Batch{}
	for _, sn := range snaps {
		var proposal *int64
		if sn.ProposalID != nil {
			p := int64(*sn.ProposalID) //nolint:gosec // proposal ids fit an int64
			proposal = &p
		}
		batch.Queue(`INSERT INTO explorer.param_snapshots (module, height, time, params, changed_keys, proposal_id)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (module, height) DO UPDATE SET time = EXCLUDED.time, params = EXCLUDED.params,
			  changed_keys = EXCLUDED.changed_keys, proposal_id = EXCLUDED.proposal_id`,
			sn.Module, sn.Height, sn.Time, string(sn.Params), sn.ChangedKeys, proposal)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("param snapshots insert: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("param snapshots insert: %w", err)
	}
	return nil
}
