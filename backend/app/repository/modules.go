package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ModuleAccount is a module account's label.
type ModuleAccount struct {
	Address string
	Name    string
	Module  string
}

// ModuleAccounts returns the labels of kind module (the module accounts the
// labels seed writes), by name.
func (r *Explorer) ModuleAccounts(ctx context.Context) ([]ModuleAccount, error) {
	rows, err := r.db.Query(ctx, `SELECT address, name, coalesce(module, '') FROM explorer.labels
		WHERE kind = 'module' ORDER BY name, address`)
	if err != nil {
		return nil, fmt.Errorf("module accounts: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ModuleAccount, error) {
		var m ModuleAccount
		err := row.Scan(&m.Address, &m.Name, &m.Module)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("module accounts: %w", err)
	}
	return out, nil
}
