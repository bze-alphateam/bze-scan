package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Dao is an explorer.daos row with the labels of its admin and creator.
type Dao struct {
	ID            int64
	Address       string
	Type          string
	Name          string
	Admin         string
	AdminLabel    *Label
	SelfGoverned  bool
	CreatedHeight *int64
	CreatedTime   *time.Time
	CreatedTxHash *string
	Creator       *string
	CreatorLabel  *Label
	UpdatedHeight *int64
	// PendingAdmin is the address nominated as the next admin, nil when no
	// nomination is pending.
	PendingAdmin *string
	// ProposalsOpen is the number of the DAO's proposals in voting.
	ProposalsOpen int64
}

// DaoFilter narrows the DAOs list.
type DaoFilter struct {
	// Type is static_list or stake; nil lists both.
	Type *string
	// Admin lists the DAOs the address administers, the self-governed ones
	// left out; nil lists all.
	Admin *string
	// Member lists the DAOs whose current member list holds the address.
	Member *string
	// Sources lists the DAOs whose current electorate uses one of the
	// sources (sr/{id} or dr/{denom}); empty for no filter.
	Sources []string
	// PendingAdmin lists the DAOs with a nomination pending for the address.
	PendingAdmin *string
	// Name lists the DAOs whose name contains the text, ignoring case. The
	// text is literal: LIKE wildcards in it are escaped here.
	Name *string
}

// likeEscaper escapes the LIKE wildcards and the escape character.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

const daoCols = `d.id, d.address, d.type, d.name, d.admin, la.name, la.kind, d.self_governed, d.created_height,
	d.created_time, d.created_tx_hash, d.creator, lc.name, lc.kind, d.updated_height, d.pending_admin,
	(SELECT count(*) FROM explorer.dao_proposals p WHERE p.dao_id = d.id AND p.status = 'voting' AND p.cleaned_height IS NULL)`

const daoFrom = ` FROM explorer.daos d
	LEFT JOIN explorer.labels la ON la.address = d.admin
	LEFT JOIN explorer.labels lc ON lc.address = d.creator`

func scanDao(row pgx.Row) (Dao, error) {
	var d Dao
	var adminName, adminKind, creatorName, creatorKind *string
	err := row.Scan(&d.ID, &d.Address, &d.Type, &d.Name, &d.Admin, &adminName, &adminKind, &d.SelfGoverned,
		&d.CreatedHeight, &d.CreatedTime, &d.CreatedTxHash, &d.Creator, &creatorName, &creatorKind, &d.UpdatedHeight, &d.PendingAdmin, &d.ProposalsOpen)
	if err != nil {
		return Dao{}, err
	}
	d.AdminLabel = label(&d.Admin, adminName, adminKind)
	d.CreatorLabel = label(d.Creator, creatorName, creatorKind)
	return d, nil
}

// Daos returns up to limit DAOs, newest (highest id) first, below before when
// not nil.
func (r *Explorer) Daos(ctx context.Context, f DaoFilter, before *int64, limit int) ([]Dao, error) {
	var pattern *string
	if f.Name != nil {
		p := "%" + likeEscaper.Replace(*f.Name) + "%"
		pattern = &p
	}
	sources := f.Sources
	if sources == nil {
		sources = []string{}
	}
	rows, err := r.db.Query(ctx, `SELECT `+daoCols+daoFrom+`
		WHERE ($1::text IS NULL OR d.type = $1)
		  AND ($2::text IS NULL OR (d.admin = $2 AND NOT d.self_governed))
		  AND ($3::bigint IS NULL OR d.id < $3)
		  AND ($5::text IS NULL OR EXISTS (SELECT 1 FROM explorer.dao_members m WHERE m.dao_id = d.id AND m.address = $5))
		  AND (cardinality($6::text[]) = 0 OR EXISTS (SELECT 1 FROM explorer.dao_sources s WHERE s.dao_id = d.id AND s.source = ANY($6)))
		  AND ($7::text IS NULL OR d.pending_admin = $7)
		  AND ($8::text IS NULL OR d.name ILIKE $8 ESCAPE '\')
		ORDER BY d.id DESC LIMIT $4`, f.Type, f.Admin, before, limit, f.Member, sources, f.PendingAdmin, pattern)
	if err != nil {
		return nil, fmt.Errorf("daos: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Dao, error) { return scanDao(row) })
}

// Dao returns DAO id; ErrNotFound when the index has none.
func (r *Explorer) Dao(ctx context.Context, id int64) (*Dao, error) {
	d, err := scanDao(r.db.QueryRow(ctx, `SELECT `+daoCols+daoFrom+` WHERE d.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("dao %d: %w", id, err)
	}
	return &d, nil
}

// DaoByAddress returns the DAO whose treasury account is address;
// ErrNotFound when none.
func (r *Explorer) DaoByAddress(ctx context.Context, address string) (*Dao, error) {
	d, err := scanDao(r.db.QueryRow(ctx, `SELECT `+daoCols+daoFrom+` WHERE d.address = $1`, address))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("dao %s: %w", address, err)
	}
	return &d, nil
}

// Labels returns the labels of the addresses that have one, by address.
func (r *Explorer) Labels(ctx context.Context, addresses []string) (map[string]Label, error) {
	out := map[string]Label{}
	if len(addresses) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT address, name, kind FROM explorer.labels WHERE address = ANY($1::text[])`, addresses)
	if err != nil {
		return nil, fmt.Errorf("labels: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.Address, &l.Name, &l.Kind); err != nil {
			return nil, fmt.Errorf("labels: %w", err)
		}
		out[l.Address] = l
	}
	return out, rows.Err()
}

// DaoEvent is an explorer.dao_events row.
type DaoEvent struct {
	Height   int64
	TxIndex  int64
	MsgIndex int64
	Seq      int64
	Type     string
	Attrs    json.RawMessage
	TxHash   *string
	Time     time.Time
}

// DaoEventKey is the keyset position of a DAO's history, newest first.
type DaoEventKey struct {
	Height, TxIndex, MsgIndex, Seq int64
}

// DaoEvents returns up to limit events of DAO id, newest first, below before
// when not nil.
func (r *Explorer) DaoEvents(ctx context.Context, id int64, before *DaoEventKey, limit int) ([]DaoEvent, error) {
	var h, t, m, s *int64
	if before != nil {
		h, t, m, s = &before.Height, &before.TxIndex, &before.MsgIndex, &before.Seq
	}
	rows, err := r.db.Query(ctx, `SELECT height, tx_index, msg_index, seq, type, attrs, tx_hash, time
		FROM explorer.dao_events
		WHERE dao_id = $1 AND ($2::bigint IS NULL OR (height, tx_index, msg_index, seq) < ($2, $3, $4, $5))
		ORDER BY height DESC, tx_index DESC, msg_index DESC, seq DESC LIMIT $6`, id, h, t, m, s, limit)
	if err != nil {
		return nil, fmt.Errorf("dao %d events: %w", id, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (DaoEvent, error) {
		var e DaoEvent
		err := row.Scan(&e.Height, &e.TxIndex, &e.MsgIndex, &e.Seq, &e.Type, &e.Attrs, &e.TxHash, &e.Time)
		return e, err
	})
}
