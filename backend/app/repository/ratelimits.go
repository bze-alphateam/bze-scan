package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DenomBrief is how a denom is shown beside other data: symbol, exponent and
// logo.
type DenomBrief struct {
	Denom    string
	Symbol   *string
	Exponent int
	LogoURL  *string
}

// DenomBriefs returns the brief of every known denom among denoms, by denom.
func (r *Explorer) DenomBriefs(ctx context.Context, denoms []string) (map[string]DenomBrief, error) {
	out := make(map[string]DenomBrief, len(denoms))
	if len(denoms) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT denom, symbol, exponent, logo_url FROM explorer.denoms WHERE denom = ANY($1)`, denoms)
	if err != nil {
		return nil, fmt.Errorf("denom briefs: %w", err)
	}
	briefs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (DenomBrief, error) {
		var b DenomBrief
		err := row.Scan(&b.Denom, &b.Symbol, &b.Exponent, &b.LogoURL)
		return b, err
	})
	if err != nil {
		return nil, fmt.Errorf("denom briefs: %w", err)
	}
	for _, b := range briefs {
		out[b.Denom] = b
	}
	return out, nil
}
