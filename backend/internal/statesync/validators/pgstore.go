package validators

import (
	"context"
	"encoding/json"
	"fmt"

	sdkmath "cosmossdk.io/math"
	"github.com/jackc/pgx/v5"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PGStore writes the set into explorer.validators and explorer.labels.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// Standings lists every stored validator.
func (s *PGStore) Standings(ctx context.Context) ([]Standing, error) {
	rows, err := s.db.Query(ctx, `SELECT operator_address, status, tokens::text FROM explorer.validators`)
	if err != nil {
		return nil, fmt.Errorf("validators standings: %w", err)
	}
	defer rows.Close()
	var out []Standing
	for rows.Next() {
		var st Standing
		var tokens string
		if err := rows.Scan(&st.Operator, &st.Status, &tokens); err != nil {
			return nil, fmt.Errorf("validators standings: %w", err)
		}
		t, ok := sdkmath.NewIntFromString(tokens)
		if !ok {
			return nil, fmt.Errorf("validators standings: tokens %q of %s", tokens, st.Operator)
		}
		st.Tokens = t
		out = append(out, st)
	}
	return out, rows.Err()
}

// upsertSQL writes one validator. First sight is set on insert only: the
// height and time of its created event when indexed, else the live cursor
// and now.
const upsertSQL = `INSERT INTO explorer.validators (
		operator_address, account_address, consensus_address, consensus_pubkey, moniker,
		identity, website, security_contact, details, status, jailed, tombstoned, jailed_until,
		tokens, delegator_shares, commission_rate, commission_max_rate, commission_max_change_rate,
		commission_update_time, min_self_delegation, self_delegation, delegator_count,
		missed_blocks, signed_blocks_window, first_seen_height, first_seen_time, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24,
		COALESCE((SELECT min(height) FROM explorer.validator_events WHERE operator_address = $1 AND kind = 'created'),
		         (SELECT value::bigint FROM explorer.indexer_state WHERE key = 'last_indexed_height')),
		COALESCE((SELECT min(time) FROM explorer.validator_events WHERE operator_address = $1 AND kind = 'created'), now()),
		now())
	ON CONFLICT (operator_address) DO UPDATE SET
		account_address = EXCLUDED.account_address, consensus_address = EXCLUDED.consensus_address,
		consensus_pubkey = EXCLUDED.consensus_pubkey, moniker = EXCLUDED.moniker, identity = EXCLUDED.identity,
		website = EXCLUDED.website, security_contact = EXCLUDED.security_contact, details = EXCLUDED.details,
		status = EXCLUDED.status, jailed = EXCLUDED.jailed, tombstoned = EXCLUDED.tombstoned,
		jailed_until = EXCLUDED.jailed_until, tokens = EXCLUDED.tokens, delegator_shares = EXCLUDED.delegator_shares,
		commission_rate = EXCLUDED.commission_rate, commission_max_rate = EXCLUDED.commission_max_rate,
		commission_max_change_rate = EXCLUDED.commission_max_change_rate,
		commission_update_time = EXCLUDED.commission_update_time, min_self_delegation = EXCLUDED.min_self_delegation,
		self_delegation = EXCLUDED.self_delegation, delegator_count = EXCLUDED.delegator_count,
		missed_blocks = EXCLUDED.missed_blocks, signed_blocks_window = EXCLUDED.signed_blocks_window,
		updated_at = now()`

// labelSQL names the owner account after the moniker. Labels of another
// source (seed, manual) are never overwritten.
const labelSQL = `INSERT INTO explorer.labels (address, name, kind, source, updated_at)
	VALUES ($1, $2, 'validator_owner', 'sync', now())
	ON CONFLICT (address) DO UPDATE SET name = EXCLUDED.name, kind = EXCLUDED.kind, updated_at = now()
	WHERE explorer.labels.source = 'sync'
	  AND (explorer.labels.name, explorer.labels.kind) IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.kind)`

// Save writes a snapshot in one transaction: the validators and their
// labels, the unbonded ones, then the ranks.
func (s *PGStore) Save(ctx context.Context, snap Snapshot) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("validators: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	operators := make([]string, 0, len(snap.Validators))
	for _, v := range snap.Validators {
		operators = append(operators, v.OperatorAddress)
		if _, err := tx.Exec(ctx, upsertSQL,
			v.OperatorAddress, v.AccountAddress, nullable(v.ConsensusAddress), nullable(v.ConsensusPubkey), v.Moniker,
			nullable(v.Identity), nullable(v.Website), nullable(v.SecurityContact), nullable(v.Details),
			v.Status, v.Jailed, v.Tombstoned, v.JailedUntil,
			v.Tokens, v.DelegatorShares, v.CommissionRate, v.CommissionMaxRate, v.CommissionMaxChangeRate,
			v.CommissionUpdateTime, nullable(v.MinSelfDelegation), nullable(v.SelfDelegation), v.DelegatorCount,
			v.MissedBlocks, v.SignedBlocksWindow); err != nil {
			return fmt.Errorf("validator %s: %w", v.OperatorAddress, err)
		}
		if _, err := tx.Exec(ctx, labelSQL, v.AccountAddress, v.Moniker); err != nil {
			return fmt.Errorf("validator %s: label: %w", v.OperatorAddress, err)
		}
	}
	if snap.Full {
		if _, err := tx.Exec(ctx, `UPDATE explorer.validators SET status = 'unbonded', updated_at = now()
			WHERE operator_address <> ALL($1::text[]) AND status <> 'unbonded'`, operators); err != nil {
			return fmt.Errorf("validators: absent ones: %w", err)
		}
	}
	if len(snap.Unbonded) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE explorer.validators SET status = 'unbonded', updated_at = now()
			WHERE operator_address = ANY($1::text[]) AND status <> 'unbonded'`, snap.Unbonded); err != nil {
			return fmt.Errorf("validators: unbonded: %w", err)
		}
	}

	type rankRow struct {
		Operator string `json:"operator"`
		Rank     int    `json:"rank"`
		Pct      string `json:"pct"`
	}
	ranks := make([]rankRow, len(snap.Ranks))
	ranked := make([]string, len(snap.Ranks))
	for i, r := range snap.Ranks {
		ranks[i] = rankRow{Operator: r.Operator, Rank: r.Rank, Pct: r.VotingPowerPct}
		ranked[i] = r.Operator
	}
	payload, err := json.Marshal(ranks)
	if err != nil {
		return fmt.Errorf("validators: ranks: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE explorer.validators v SET rank = r.rank, voting_power_pct = r.pct
		FROM jsonb_to_recordset($1::jsonb) AS r(operator text, rank integer, pct numeric)
		WHERE v.operator_address = r.operator
		  AND (v.rank, v.voting_power_pct) IS DISTINCT FROM (r.rank, r.pct)`, payload); err != nil {
		return fmt.Errorf("validators: ranks: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE explorer.validators SET rank = NULL, voting_power_pct = NULL
		WHERE operator_address <> ALL($1::text[]) AND (rank IS NOT NULL OR voting_power_pct IS NOT NULL)`, ranked); err != nil {
		return fmt.Errorf("validators: unranked: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("validators: commit: %w", err)
	}
	return nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
