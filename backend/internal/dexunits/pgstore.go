package dexunits

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Exponents reads the display exponents of denoms; PGStore implements it.
type Exponents interface {
	// Exponents returns the exponent of every denom of denoms explorer.denoms
	// knows; a denom it does not know is missing from the map.
	Exponents(ctx context.Context, denoms []string) (map[string]int, error)
}

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PGStore reads the exponents from explorer.denoms: the chain registry's
// for IBC denoms, the bank metadata's otherwise.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// Exponents implements Exponents.
func (s *PGStore) Exponents(ctx context.Context, denoms []string) (map[string]int, error) {
	rows, err := s.db.Query(ctx, `SELECT denom, exponent FROM explorer.denoms WHERE denom = ANY($1::text[])`, denoms)
	if err != nil {
		return nil, fmt.Errorf("exponents: %w", err)
	}
	out := make(map[string]int, len(denoms))
	var (
		denom string
		exp   int16
	)
	_, err = pgx.ForEachRow(rows, []any{&denom, &exp}, func() error {
		out[denom] = int(exp)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("exponents: %w", err)
	}
	return out, nil
}
