package registry

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bze-alphateam/bze-scan/backend/internal/chainregistry"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// PGStore writes the set into explorer.chains and explorer.registry_assets.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

const wantedSQL = `SELECT counterparty_chain_id FROM explorer.ibc_channels WHERE counterparty_chain_id IS NOT NULL
	UNION SELECT origin_chain_id FROM explorer.denoms WHERE origin_chain_id IS NOT NULL
	UNION SELECT chain_id FROM explorer.chains
	ORDER BY 1`

// Wanted returns the chain ids the explorer has met.
func (s *PGStore) Wanted(ctx context.Context) ([]string, error) {
	rows, err := s.db.Query(ctx, wantedSQL)
	if err != nil {
		return nil, fmt.Errorf("chain registry wanted: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("chain registry wanted: %w", err)
	}
	return out, nil
}

// chainSQL upserts a chain; updated_at moves only when a value changes, the
// fetch time on every fetch.
const chainSQL = `INSERT INTO explorer.chains AS c (chain_id, registry_name, pretty_name, network_type, bech32_prefix,
		logo_url, explorer_tx_url, explorer_account_url, registry_fetched_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
	ON CONFLICT (chain_id) DO UPDATE SET
		registry_name = EXCLUDED.registry_name, pretty_name = EXCLUDED.pretty_name,
		network_type = EXCLUDED.network_type, bech32_prefix = EXCLUDED.bech32_prefix,
		logo_url = EXCLUDED.logo_url, explorer_tx_url = EXCLUDED.explorer_tx_url,
		explorer_account_url = EXCLUDED.explorer_account_url, registry_fetched_at = EXCLUDED.registry_fetched_at,
		updated_at = CASE WHEN (c.registry_name, c.pretty_name, c.network_type, c.bech32_prefix, c.logo_url,
				c.explorer_tx_url, c.explorer_account_url)
			IS DISTINCT FROM (EXCLUDED.registry_name, EXCLUDED.pretty_name, EXCLUDED.network_type, EXCLUDED.bech32_prefix,
				EXCLUDED.logo_url, EXCLUDED.explorer_tx_url, EXCLUDED.explorer_account_url)
			THEN EXCLUDED.updated_at ELSE c.updated_at END`

// assetSQL upserts an asset; updated_at moves only when a value changes.
const assetSQL = `INSERT INTO explorer.registry_assets AS a (chain_id, base, symbol, name, display, exponent, logo_url,
		coingecko_id, traces, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10)
	ON CONFLICT (chain_id, base) DO UPDATE SET
		symbol = EXCLUDED.symbol, name = EXCLUDED.name, display = EXCLUDED.display, exponent = EXCLUDED.exponent,
		logo_url = EXCLUDED.logo_url, coingecko_id = EXCLUDED.coingecko_id, traces = EXCLUDED.traces,
		updated_at = CASE WHEN (a.symbol, a.name, a.display, a.exponent, a.logo_url, a.coingecko_id, a.traces)
			IS DISTINCT FROM (EXCLUDED.symbol, EXCLUDED.name, EXCLUDED.display, EXCLUDED.exponent, EXCLUDED.logo_url,
				EXCLUDED.coingecko_id, EXCLUDED.traces)
			THEN EXCLUDED.updated_at ELSE a.updated_at END`

// goneAssetsSQL deletes the assets of a chain its asset list no longer has.
const goneAssetsSQL = `DELETE FROM explorer.registry_assets WHERE chain_id = $1 AND base <> ALL($2::text[])`

// Save writes a chain and replaces its assets, in one transaction.
func (s *PGStore) Save(ctx context.Context, chain chainregistry.Chain, assets []chainregistry.Asset, fetched time.Time) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("chain registry save %s: %w", chain.ChainID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	batch := &pgx.Batch{}
	batch.Queue(chainSQL, chain.ChainID, null(chain.Name), null(chain.PrettyName), null(chain.NetworkType),
		null(chain.Bech32Prefix), null(chain.LogoURL), null(chain.ExplorerTxURL), null(chain.ExplorerAccountURL), fetched)
	bases := make([]string, 0, len(assets))
	for _, a := range assets {
		bases = append(bases, a.Base)
		var traces any
		if a.Traces != nil {
			traces = string(a.Traces)
		}
		batch.Queue(assetSQL, chain.ChainID, a.Base, null(a.Symbol), null(a.Name), null(a.Display), a.Exponent,
			null(a.LogoURL), null(a.CoingeckoID), traces, fetched)
	}
	batch.Queue(goneAssetsSQL, chain.ChainID, bases)
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("chain registry save %s: %w", chain.ChainID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("chain registry save %s: %w", chain.ChainID, err)
	}
	return nil
}

// SaveUnknown records a chain id the registry does not know.
func (s *PGStore) SaveUnknown(ctx context.Context, chainID string) error {
	if _, err := s.db.Exec(ctx, `INSERT INTO explorer.chains (chain_id, updated_at) VALUES ($1, now())
		ON CONFLICT (chain_id) DO NOTHING`, chainID); err != nil {
		return fmt.Errorf("chain registry unknown %s: %w", chainID, err)
	}
	return nil
}

func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}
