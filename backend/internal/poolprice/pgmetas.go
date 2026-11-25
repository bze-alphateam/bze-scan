package poolprice

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DB is the part of a pgx pool PGMetas needs; *pgxpool.Pool satisfies it.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PGMetas reads symbols and exponents from explorer.denoms.
type PGMetas struct {
	db DB
}

// NewPGMetas returns metas over db.
func NewPGMetas(db DB) *PGMetas {
	return &PGMetas{db: db}
}

// Metas implements Metas.
func (m *PGMetas) Metas(ctx context.Context, denoms []string) (map[string]Meta, error) {
	rows, err := m.db.Query(ctx, `SELECT denom, coalesce(symbol, ''), exponent FROM explorer.denoms WHERE denom = ANY($1::text[])`, denoms)
	if err != nil {
		return nil, fmt.Errorf("denom metas: %w", err)
	}
	out := make(map[string]Meta, len(denoms))
	var (
		denom, symbol string
		exp           int16
	)
	_, err = pgx.ForEachRow(rows, []any{&denom, &symbol, &exp}, func() error {
		out[denom] = Meta{Symbol: symbol, Exponent: int(exp)}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("denom metas: %w", err)
	}
	return out, nil
}
