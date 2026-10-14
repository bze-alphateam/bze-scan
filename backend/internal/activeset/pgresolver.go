package activeset

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DB is the part of a pgx pool the resolver needs; *pgxpool.Pool satisfies
// it.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PGResolver names operators from the database: the validators the state
// sync keeps (every validator staking still has), then the indexed
// MsgCreateValidator of the ones staking deleted since.
type PGResolver struct {
	db DB
}

var _ Resolver = (*PGResolver)(nil)

// NewPGResolver returns a resolver over db.
func NewPGResolver(db DB) *PGResolver {
	return &PGResolver{db: db}
}

// ed25519KeyType is the type URL of an ed25519 consensus key in a message
// body.
const ed25519KeyType = "/cosmos.crypto.ed25519.PubKey"

func scanPair(row pgx.CollectableRow) ([2]string, error) {
	var p [2]string
	err := row.Scan(&p[0], &p[1])
	return p, err
}

// Operators maps the consensus addresses the validators table or an indexed
// creation knows.
func (r *PGResolver) Operators(ctx context.Context, consensus []string) (map[string]string, error) {
	out := map[string]string{}
	rows, err := r.db.Query(ctx, `SELECT consensus_address, operator_address FROM explorer.validators
		WHERE consensus_address = ANY($1)`, consensus)
	if err != nil {
		return nil, fmt.Errorf("operators: %w", err)
	}
	pairs, err := pgx.CollectRows(rows, scanPair)
	if err != nil {
		return nil, fmt.Errorf("operators: %w", err)
	}
	for _, p := range pairs {
		out[p[0]] = p[1]
	}
	if len(out) == len(consensus) {
		return out, nil
	}

	// A creation whose transaction failed has no created event: only the
	// messages that created a validator are read.
	rows, err = r.db.Query(ctx, `SELECT m.body->>'validator_address', m.body->'pubkey'->>'key'
		FROM explorer.messages m
		WHERE m.type_url = '/cosmos.staking.v1beta1.MsgCreateValidator'
		  AND m.body->'pubkey'->>'@type' = $1
		  AND EXISTS (SELECT 1 FROM explorer.validator_events e WHERE e.height = m.height AND e.tx_index = m.tx_index
		              AND e.kind = 'created' AND e.operator_address = m.body->>'validator_address')
		ORDER BY m.height, m.tx_index, m.msg_index`, ed25519KeyType)
	if err != nil {
		return nil, fmt.Errorf("operators from creations: %w", err)
	}
	created, err := pgx.CollectRows(rows, scanPair)
	if err != nil {
		return nil, fmt.Errorf("operators from creations: %w", err)
	}
	wanted := map[string]bool{}
	for _, c := range consensus {
		wanted[c] = out[c] == ""
	}
	for _, c := range created {
		key, err := base64.StdEncoding.DecodeString(c[1])
		if err != nil {
			continue
		}
		if cons := ConsensusAddress(key); wanted[cons] {
			out[cons] = c[0]
		}
	}
	return out, nil
}
