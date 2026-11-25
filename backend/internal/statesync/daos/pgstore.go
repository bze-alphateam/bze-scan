package daos

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/bze-alphateam/bze-scan/backend/internal/labels"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PGStore writes the set into explorer.daos and explorer.labels.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// upsertSQL writes the DAOs the node holds. A DAO the index missed gets a row
// without creation facts (the transformer fills them when it sees the
// creation); one it has keeps them. Rows are written in id order, so
// concurrent saves lock in the same order.
const upsertSQL = `INSERT INTO explorer.daos AS d (id, address, type, name, admin, self_governed)
	SELECT r.id, r.address, r.type, r.name, r.admin, r.self_governed
	  FROM jsonb_to_recordset($1::jsonb) AS r(id bigint, address text, type text, name text, admin text, self_governed boolean)
	 ORDER BY r.id
	ON CONFLICT (id) DO UPDATE SET
		address = EXCLUDED.address, type = EXCLUDED.type, name = EXCLUDED.name, admin = EXCLUDED.admin,
		self_governed = EXCLUDED.self_governed, updated_at = now()
	WHERE (d.address, d.type, d.name, d.admin, d.self_governed) IS DISTINCT FROM
	      (EXCLUDED.address, EXCLUDED.type, EXCLUDED.name, EXCLUDED.admin, EXCLUDED.self_governed)`

// labelSQL names the DAOs' treasury accounts. An address a label of another
// kind already names keeps it.
var labelSQL = `INSERT INTO explorer.labels AS l (address, name, kind, source, updated_at)
	SELECT r.address,
	       CASE WHEN r.name = '' THEN 'DAO #' || r.id ELSE 'DAO #' || r.id || ' · ' || r.name END,
	       '` + labels.KindDao + `', 'sync', now()
	  FROM jsonb_to_recordset($1::jsonb) AS r(id bigint, address text, name text)
	 ORDER BY r.address
	ON CONFLICT (address) DO UPDATE SET name = EXCLUDED.name, updated_at = EXCLUDED.updated_at
	WHERE l.kind = '` + labels.KindDao + `' AND l.name IS DISTINCT FROM EXCLUDED.name`

type row struct {
	ID           uint64 `json:"id"`
	Address      string `json:"address"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	Admin        string `json:"admin"`
	SelfGoverned bool   `json:"self_governed"`
}

// Save upserts the DAOs and their labels in one transaction.
func (s *PGStore) Save(ctx context.Context, daos []Dao) error {
	rows := make([]row, 0, len(daos))
	for _, d := range daos {
		rows = append(rows, row{ID: d.ID, Address: d.Address, Type: d.Type, Name: d.Name, Admin: d.Admin, SelfGoverned: d.SelfGoverned()})
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("daos: save: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("daos: save: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, sql := range []string{upsertSQL, labelSQL} {
		if _, err := tx.Exec(ctx, sql, string(payload)); err != nil {
			return fmt.Errorf("daos: save: %w", err)
		}
	}
	return tx.Commit(ctx)
}
