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
const upsertSQL = `INSERT INTO explorer.daos AS d (id, address, type, name, admin, self_governed, pending_admin)
	SELECT r.id, r.address, r.type, r.name, r.admin, r.self_governed, NULLIF(r.pending_admin, '')
	  FROM jsonb_to_recordset($1::jsonb) AS r(id bigint, address text, type text, name text, admin text, self_governed boolean, pending_admin text)
	 ORDER BY r.id
	ON CONFLICT (id) DO UPDATE SET
		address = EXCLUDED.address, type = EXCLUDED.type, name = EXCLUDED.name, admin = EXCLUDED.admin,
		self_governed = EXCLUDED.self_governed, pending_admin = EXCLUDED.pending_admin, updated_at = now()
	WHERE (d.address, d.type, d.name, d.admin, d.self_governed, d.pending_admin) IS DISTINCT FROM
	      (EXCLUDED.address, EXCLUDED.type, EXCLUDED.name, EXCLUDED.admin, EXCLUDED.self_governed, EXCLUDED.pending_admin)`

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

// replaceSQL drops the saved DAOs' members and sources, then writes the
// current ones: $1 the saved DAO ids.
var replaceSQL = []string{
	`DELETE FROM explorer.dao_members WHERE dao_id IN (SELECT id FROM jsonb_to_recordset($1::jsonb) AS r(id bigint))`,
	`DELETE FROM explorer.dao_sources WHERE dao_id IN (SELECT id FROM jsonb_to_recordset($1::jsonb) AS r(id bigint))`,
}

const membersSQL = `INSERT INTO explorer.dao_members (dao_id, address)
	SELECT r.dao_id, r.value FROM jsonb_to_recordset($1::jsonb) AS r(dao_id bigint, value text)
	ON CONFLICT DO NOTHING`

const sourcesSQL = `INSERT INTO explorer.dao_sources (dao_id, source)
	SELECT r.dao_id, r.value FROM jsonb_to_recordset($1::jsonb) AS r(dao_id bigint, value text)
	ON CONFLICT DO NOTHING`

type row struct {
	ID           uint64 `json:"id"`
	Address      string `json:"address"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	Admin        string `json:"admin"`
	SelfGoverned bool   `json:"self_governed"`
	PendingAdmin string `json:"pending_admin"`
}

type pair struct {
	DaoID uint64 `json:"dao_id"`
	Value string `json:"value"`
}

// Save upserts the DAOs and their labels in one transaction.
func (s *PGStore) Save(ctx context.Context, daos []Dao) error {
	rows := make([]row, 0, len(daos))
	members, sources := []pair{}, []pair{}
	for _, d := range daos {
		rows = append(rows, row{ID: d.ID, Address: d.Address, Type: d.Type, Name: d.Name, Admin: d.Admin,
			SelfGoverned: d.SelfGoverned(), PendingAdmin: d.PendingAdmin})
		for _, m := range d.Members {
			members = append(members, pair{DaoID: d.ID, Value: m})
		}
		for _, src := range d.Sources {
			sources = append(sources, pair{DaoID: d.ID, Value: src})
		}
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("daos: save: %w", err)
	}
	idPayload, err := marshalIDs(daos)
	if err != nil {
		return err
	}
	memberPayload, err := json.Marshal(members)
	if err != nil {
		return fmt.Errorf("daos: save: %w", err)
	}
	sourcePayload, err := json.Marshal(sources)
	if err != nil {
		return fmt.Errorf("daos: save: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("daos: save: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, st := range []struct{ sql, payload string }{
		{upsertSQL, string(payload)}, {labelSQL, string(payload)},
		{replaceSQL[0], idPayload}, {replaceSQL[1], idPayload},
		{membersSQL, string(memberPayload)}, {sourcesSQL, string(sourcePayload)},
	} {
		if _, err := tx.Exec(ctx, st.sql, st.payload); err != nil {
			return fmt.Errorf("daos: save: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func marshalIDs(daos []Dao) (string, error) {
	type id struct {
		ID uint64 `json:"id"`
	}
	out := make([]id, 0, len(daos))
	for _, d := range daos {
		out = append(out, id{ID: d.ID})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("daos: save: %w", err)
	}
	return string(b), nil
}
