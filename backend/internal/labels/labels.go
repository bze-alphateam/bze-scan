// Package labels names well-known addresses: the chain's module accounts and
// a list of known accounts (treasuries, exchanges), seeded into
// explorer.labels by `migrate`. The state sync adds the validator owners
// (and the IBC escrow accounts, later); those rows are not seeded here.
package labels

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
)

// Kinds of labels.
const (
	KindModule         = "module"
	KindIBCEscrow      = "ibc_escrow"
	KindValidatorOwner = "validator_owner"
	KindKnown          = "known"
)

// Sources of labels.
const (
	SourceSeed   = "seed"
	SourceSync   = "sync"
	SourceManual = "manual"
)

// Label is one explorer.labels row the seed writes.
type Label struct {
	Address string `json:"address"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	// Module is the module name of a module account; empty otherwise.
	Module string `json:"module"`
	URL    string `json:"url"`
	// Retired marks a module account the chain dropped (v8.3.0). The label
	// stays: history names the address, and the seed's stale-row delete must
	// not turn it into an ordinary account.
	Retired bool `json:"retired"`
}

// modules are the module accounts the chain's app declares (its module
// account permissions, v8.3.0 included: guardian and ratelimit have none),
// with the names the explorer shows. The retired ones (nft, feeibc,
// txfeecollector_burner) are kept on purpose.
var modules = []struct {
	module, name string
	retired      bool
}{
	{"fee_collector", "Fee collector", false},
	{"distribution", "Distribution", false},
	{"mint", "Mint", false},
	{"bonded_tokens_pool", "Bonded tokens pool", false},
	{"not_bonded_tokens_pool", "Unbonding tokens pool", false},
	{"gov", "Governance", false},
	{"dao", "DAO module (proposal deposits)", false},
	{"nft", "NFT (retired in v8.3.0)", true},
	{"transfer", "Cross-chain transfer", false},
	{"feeibc", "Cross-chain relayer fees (retired in v8.3.0)", true},
	{"interchainaccounts", "Interchain accounts", false},
	{"rewards", "Rewards", false},
	{"burner", "Burner", false},
	{"burner_black_hole", "Burner black hole", false},
	{"burner_raffle", "Burner raffle", false},
	{"tokenfactory", "Factory", false},
	{"tradebin", "DEX", false},
	{"txfeecollector", "Stakers fee collector", false},
	{"txfeecollector_burner", "Burner fee collector (retired in v8.3.0)", true},
	{"txfeecollector_cp", "CP fee collector", false},
}

// IsRetired reports whether a module account was retired by the chain.
func IsRetired(module string) bool {
	for _, m := range modules {
		if m.module == module {
			return m.retired
		}
	}
	return false
}

// known are the known accounts (treasuries, exchanges). Rows are added here,
// in code, never by hand on the server.
var known []Label

// Modules returns the labels of the module accounts, in declaration order.
// The address is the module's (chain.ModuleAddress).
func Modules() []Label {
	out := make([]Label, 0, len(modules))
	for _, m := range modules {
		out = append(out, Label{Address: chain.ModuleAddress(m.module), Name: m.name, Kind: KindModule, Module: m.module, Retired: m.retired})
	}
	return out
}

// Known returns the labels of the known accounts.
func Known() []Label {
	return append([]Label(nil), known...)
}

// Execer runs a statement; *pgxpool.Pool and pgx.Tx satisfy it.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// seedSQL upserts the seed rows. A row that would not change is not
// rewritten, so seeding again changes nothing.
const seedSQL = `INSERT INTO explorer.labels AS l (address, name, kind, module, url, source, updated_at)
	SELECT r.address, r.name, r.kind, NULLIF(r.module, ''), NULLIF(r.url, ''), 'seed', now()
	  FROM jsonb_to_recordset($1::jsonb) AS r(address text, name text, kind text, module text, url text)
	ON CONFLICT (address) DO UPDATE SET
		name = EXCLUDED.name, kind = EXCLUDED.kind, module = EXCLUDED.module, url = EXCLUDED.url,
		source = EXCLUDED.source, updated_at = now()
	WHERE (l.name, l.kind, l.module, l.url, l.source)
	      IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.kind, EXCLUDED.module, EXCLUDED.url, EXCLUDED.source)`

// staleSQL deletes the seed rows the seed no longer lists (a module the
// chain dropped).
const staleSQL = `DELETE FROM explorer.labels WHERE source = 'seed' AND address <> ALL($1::text[])`

// Seed writes the module and known labels (source seed) and deletes the
// seed rows no longer listed. Idempotent; `migrate up` runs it after the SQL
// migrations.
func Seed(ctx context.Context, db Execer) error {
	rows := append(Modules(), Known()...)
	payload, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("labels: %w", err)
	}
	if _, err := db.Exec(ctx, seedSQL, payload); err != nil {
		return fmt.Errorf("labels: %w", err)
	}
	addrs := make([]string, len(rows))
	for i, r := range rows {
		addrs[i] = r.Address
	}
	if _, err := db.Exec(ctx, staleSQL, addrs); err != nil {
		return fmt.Errorf("labels: delete stale: %w", err)
	}
	return nil
}
