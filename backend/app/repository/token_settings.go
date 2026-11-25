package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// TokenSettings is the factory-token settings of a denoms row (chain
// v8.3.0). Mintable and Config are nil while unknown: not a factory denom,
// or not yet read from a v8.3.0 node.
type TokenSettings struct {
	Mintable *bool
	Config   *TokenConfig
}

// TokenConfig is a denom's DenomConfig, the ratios as decimal strings
// without trailing zeros (0.01, 0.5, 0). CommunityPool is nil for none,
// including an address that is a module account (the chain blocks those
// from receiving, so they count as none).
type TokenConfig struct {
	CommunityPool      *string
	CommunityPoolLabel *Label
	TransferFee        string
	GasSurcharge       string
}

// TokenSettings returns the settings of denom, ErrNotFound when the denom
// is unknown.
func (r *Explorer) TokenSettings(ctx context.Context, denom string) (*TokenSettings, error) {
	var s TokenSettings
	var pool, name, kind, fee, gas *string
	err := r.db.QueryRow(ctx, `SELECT d.mintable, d.community_pool, trim_scale(d.transfer_fee)::text, trim_scale(d.gas_surcharge)::text, l.name, l.kind
		FROM explorer.denoms d
		LEFT JOIN explorer.labels l ON l.address = d.community_pool
		WHERE d.denom = $1`, denom).Scan(&s.Mintable, &pool, &fee, &gas, &name, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("token settings %s: %w", denom, err)
	}
	if fee == nil || gas == nil {
		return &s, nil
	}
	c := &TokenConfig{TransferFee: *fee, GasSurcharge: *gas}
	if pool != nil && (kind == nil || *kind != "module") {
		c.CommunityPool = pool
		c.CommunityPoolLabel = label(pool, name, kind)
	}
	s.Config = c
	return &s, nil
}
