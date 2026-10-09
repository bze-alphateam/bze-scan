package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Label names a known address.
type Label struct {
	Address string
	Name    string
	Kind    string
}

// Account is what the explorer tables know of an address: its accounts row
// (nil FirstSeen and LastSeenHeight when it has none) and its label.
type Account struct {
	Address        string
	FirstSeen      *Seen
	LastSeenHeight *int64
	TxCount        int64
	ActivityCount  int64
	Label          *Label
}

// Seen is a height and its block time.
type Seen struct {
	Height int64
	Time   time.Time
}

// Denom is the display data of a denom the explorer knows.
type Denom struct {
	Symbol   *string
	Exponent int
}

// Account returns the accounts row and the label of address. An address
// with neither is not an error: the chain may know it.
func (r *Explorer) Account(ctx context.Context, address string) (*Account, error) {
	a := Account{Address: address}
	var firstHeight, lastHeight *int64
	var firstTime *time.Time
	var txCount, activityCount *int64
	var name, kind *string
	err := r.db.QueryRow(ctx, `SELECT a.first_seen_height, a.first_seen_time, a.last_seen_height,
			a.tx_count, a.activity_count, l.name, l.kind
		FROM (SELECT $1::text AS address) q
		LEFT JOIN explorer.accounts a ON a.address = q.address
		LEFT JOIN explorer.labels l ON l.address = q.address`, address).
		Scan(&firstHeight, &firstTime, &lastHeight, &txCount, &activityCount, &name, &kind)
	if err != nil {
		return nil, fmt.Errorf("account %s: %w", address, err)
	}
	if firstHeight != nil && firstTime != nil {
		a.FirstSeen = &Seen{Height: *firstHeight, Time: *firstTime}
	}
	a.LastSeenHeight = lastHeight
	if txCount != nil {
		a.TxCount = *txCount
	}
	if activityCount != nil {
		a.ActivityCount = *activityCount
	}
	if name != nil && kind != nil {
		a.Label = &Label{Address: address, Name: *name, Kind: *kind}
	}
	return &a, nil
}

// Denoms returns the display data of the denoms the explorer knows, by
// denom; the others are absent.
func (r *Explorer) Denoms(ctx context.Context, denoms []string) (map[string]Denom, error) {
	out := map[string]Denom{}
	if len(denoms) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT denom, symbol, exponent FROM explorer.denoms WHERE denom = ANY($1::text[])`, denoms)
	if err != nil {
		return nil, fmt.Errorf("denoms: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var denom string
		var d Denom
		if err := rows.Scan(&denom, &d.Symbol, &d.Exponent); err != nil {
			return nil, fmt.Errorf("denoms: %w", err)
		}
		out[denom] = d
	}
	return out, rows.Err()
}

// Monikers returns the monikers of the synced validators among operators,
// by operator address.
func (r *Explorer) Monikers(ctx context.Context, operators []string) (map[string]string, error) {
	out := map[string]string{}
	if len(operators) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT operator_address, moniker FROM explorer.validators
		WHERE operator_address = ANY($1::text[])`, operators)
	if err != nil {
		return nil, fmt.Errorf("monikers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var op, moniker string
		if err := rows.Scan(&op, &moniker); err != nil {
			return nil, fmt.Errorf("monikers: %w", err)
		}
		out[op] = moniker
	}
	return out, rows.Err()
}

// ValidatorMatch is a validator whose moniker matches a search.
type ValidatorMatch struct {
	OperatorAddress string
	Moniker         string
}

// likePattern is a case-insensitive substring pattern for q, its LIKE
// wildcards escaped.
func likePattern(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
}

// SearchLabels returns the labels whose name contains q (any case), by
// name.
func (r *Explorer) SearchLabels(ctx context.Context, q string, limit int) ([]Label, error) {
	rows, err := r.db.Query(ctx, `SELECT address, name, kind FROM explorer.labels
		WHERE name ILIKE $1 ORDER BY name, address LIMIT $2`, likePattern(q), limit)
	if err != nil {
		return nil, fmt.Errorf("search labels: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Label, error) {
		var l Label
		err := row.Scan(&l.Address, &l.Name, &l.Kind)
		return l, err
	})
}

// SearchValidators returns the validators whose moniker contains q (any
// case; the trigram index serves the ILIKE), by rank then moniker.
func (r *Explorer) SearchValidators(ctx context.Context, q string, limit int) ([]ValidatorMatch, error) {
	rows, err := r.db.Query(ctx, `SELECT operator_address, moniker FROM explorer.validators
		WHERE moniker ILIKE $1 ORDER BY rank NULLS LAST, moniker, operator_address LIMIT $2`, likePattern(q), limit)
	if err != nil {
		return nil, fmt.Errorf("search validators: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ValidatorMatch, error) {
		var v ValidatorMatch
		err := row.Scan(&v.OperatorAddress, &v.Moniker)
		return v, err
	})
}
