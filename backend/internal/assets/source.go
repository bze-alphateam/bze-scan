package assets

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/bze-alphateam/bze-scan/backend/internal/chainregistry"
)

// Denom is the part of an explorer.denoms row the list is built from.
type Denom struct {
	Denom string
	// Kind is the denoms set's kind: native, factory, ibc, lp or unknown.
	Kind string
	// Metadata is the bank metadata as the node returns it; nil without.
	Metadata      json.RawMessage
	OriginChainID string
	ViaChainID    string
	IBCBaseDenom  string
	IBCPath       string
	LPBase        string
	LPQuote       string
	Halted        bool
}

// Chain is the part of an explorer.chains row the list is built from.
type Chain struct {
	ChainID      string
	Name         string
	PrettyName   string
	Bech32Prefix string
	// LogoURL is the explorer's PNG-first pick, for rows Logos does not
	// fill yet.
	LogoURL string
	Logos   chainregistry.Logos
	REST    []string
	RPC     []string
	// Known is whether the registry has the chain (a fetched row).
	Known bool
}

// RegistryAsset is the part of an explorer.registry_assets row the list is
// built from.
type RegistryAsset struct {
	ChainID     string
	Base        string
	Symbol      string
	Name        string
	Display     string
	Exponent    *int
	LogoURL     string
	Logos       chainregistry.Logos
	Description string
	Traces      json.RawMessage
}

// Channel is the part of an explorer.ibc_channels row the list is built
// from.
type Channel struct {
	ChannelID             string
	PortID                string
	CounterpartyChainID   string
	CounterpartyChannelID string
	State                 string
	// ClientStatus is the light client's status (active, expired…); empty
	// when unknown.
	ClientStatus string
	// Confirmed is whether the channel's counterparty was confirmed to be
	// the chain its light client claims.
	Confirmed bool
}

// Tables is everything one build reads.
type Tables struct {
	// Denoms are the denoms the bank holds a supply of.
	Denoms   []Denom
	Chains   []Chain
	Assets   []RegistryAsset
	Channels []Channel
}

// Source reads the tables of a build.
type Source interface {
	Load(ctx context.Context) (*Tables, error)
}

// DB is the part of a pgx pool the source needs; *pgxpool.Pool satisfies
// it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PGSource reads the explorer tables.
type PGSource struct {
	db DB
}

// NewPGSource returns a source over db.
func NewPGSource(db DB) *PGSource {
	return &PGSource{db: db}
}

var _ Source = (*PGSource)(nil)

const (
	denomsSQL = `SELECT denom, kind, metadata, coalesce(origin_chain_id, ''), coalesce(via_chain_id, ''),
			coalesce(ibc_base_denom, ''), coalesce(ibc_path, ''), coalesce(lp_base, ''), coalesce(lp_quote, ''), halted
		FROM explorer.denoms WHERE supply > 0 ORDER BY denom`
	chainsSQL = `SELECT chain_id, coalesce(registry_name, ''), coalesce(pretty_name, ''), coalesce(bech32_prefix, ''),
			coalesce(logo_url, ''), logos, rest_urls, rpc_urls, registry_fetched_at IS NOT NULL
		FROM explorer.chains ORDER BY chain_id`
	assetsSQL = `SELECT chain_id, base, coalesce(symbol, ''), coalesce(name, ''), coalesce(display, ''), exponent,
			coalesce(logo_url, ''), logos, coalesce(description, ''), traces
		FROM explorer.registry_assets ORDER BY chain_id, base`
	channelsSQL = `SELECT channel_id, port_id, coalesce(counterparty_chain_id, ''), coalesce(counterparty_channel_id, ''),
			coalesce(state, ''), coalesce(client_status, ''), confirmed_via IS NOT NULL
		FROM explorer.ibc_channels ORDER BY channel_id`
)

// Load reads the four tables in one read-only snapshot.
func (s *PGSource) Load(ctx context.Context) (*Tables, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("asset list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return nil, fmt.Errorf("asset list: %w", err)
	}

	var t Tables
	rows, _ := tx.Query(ctx, denomsSQL)
	t.Denoms, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Denom, error) {
		var d Denom
		var md []byte
		err := r.Scan(&d.Denom, &d.Kind, &md, &d.OriginChainID, &d.ViaChainID, &d.IBCBaseDenom, &d.IBCPath,
			&d.LPBase, &d.LPQuote, &d.Halted)
		d.Metadata = md
		return d, err
	})
	if err != nil {
		return nil, fmt.Errorf("asset list denoms: %w", err)
	}
	rows, _ = tx.Query(ctx, chainsSQL)
	t.Chains, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Chain, error) {
		var c Chain
		var logos []byte
		err := r.Scan(&c.ChainID, &c.Name, &c.PrettyName, &c.Bech32Prefix, &c.LogoURL, &logos, &c.REST, &c.RPC, &c.Known)
		if err == nil {
			c.Logos, err = parseLogos(logos)
		}
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("asset list chains: %w", err)
	}
	rows, _ = tx.Query(ctx, assetsSQL)
	t.Assets, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (RegistryAsset, error) {
		var a RegistryAsset
		var exp *int16
		var logos, traces []byte
		err := r.Scan(&a.ChainID, &a.Base, &a.Symbol, &a.Name, &a.Display, &exp, &a.LogoURL, &logos, &a.Description, &traces)
		if err != nil {
			return a, err
		}
		if exp != nil {
			e := int(*exp)
			a.Exponent = &e
		}
		a.Traces = traces
		a.Logos, err = parseLogos(logos)
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("asset list registry assets: %w", err)
	}
	rows, _ = tx.Query(ctx, channelsSQL)
	t.Channels, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Channel, error) {
		var c Channel
		err := r.Scan(&c.ChannelID, &c.PortID, &c.CounterpartyChainID, &c.CounterpartyChannelID, &c.State, &c.ClientStatus, &c.Confirmed)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("asset list channels: %w", err)
	}
	return &t, nil
}

// parseLogos reads a logos column; NULL is no logo.
func parseLogos(raw []byte) (chainregistry.Logos, error) {
	var l chainregistry.Logos
	if len(raw) == 0 {
		return l, nil
	}
	err := json.Unmarshal(raw, &l)
	return l, err
}
