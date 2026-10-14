package nonvoters

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PGStore reads explorer.proposals and proposal_votes and writes
// explorer.proposal_non_voters.
type PGStore struct {
	db DB
}

var _ Store = (*PGStore)(nil)

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// closed selects the proposals whose voting ended in a tally (a canceled or
// dropped proposal never had one) with a resolution height.
const closed = `SELECT id, submit_height, resolved_height, coalesce(tally_source, ''), coalesce(tally_bonded_tokens::text, '')
	FROM explorer.proposals
	WHERE status IN ('passed', 'rejected', 'failed') AND resolved_height IS NOT NULL`

func scanProposal(row pgx.Row) (Proposal, error) {
	var p Proposal
	err := row.Scan(&p.ID, &p.SubmitHeight, &p.ResolvedHeight, &p.TallySource, &p.BondedTokens)
	return p, err
}

// Pending lists the closed proposals without non-voters written.
func (s *PGStore) Pending(ctx context.Context) ([]Proposal, error) {
	rows, err := s.db.Query(ctx, closed+` AND non_voters_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("proposals without non-voters: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Proposal, error) { return scanProposal(r) })
	if err != nil {
		return nil, fmt.Errorf("proposals without non-voters: %w", err)
	}
	return out, nil
}

// Proposal returns closed proposal id.
func (s *PGStore) Proposal(ctx context.Context, id int64) (Proposal, bool, error) {
	p, err := scanProposal(s.db.QueryRow(ctx, closed+` AND id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Proposal{}, false, nil
	}
	if err != nil {
		return Proposal{}, false, fmt.Errorf("proposal %d: %w", id, err)
	}
	return p, true, nil
}

// Indexed counts the blocks of [lo, hi].
func (s *PGStore) Indexed(ctx context.Context, lo, hi int64) (bool, error) {
	if hi < lo {
		return false, nil
	}
	var n int64
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM explorer.blocks WHERE height BETWEEN $1 AND $2`, lo, hi).Scan(&n); err != nil {
		return false, fmt.Errorf("blocks %d to %d: %w", lo, hi, err)
	}
	return n == hi-lo+1, nil
}

// Voters lists the voters of proposal id, indexed or read from the archive.
func (s *PGStore) Voters(ctx context.Context, id int64) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT voter FROM explorer.proposal_votes WHERE proposal_id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("proposal %d voters: %w", id, err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("proposal %d voters: %w", id, err)
	}
	return out, nil
}

// Save replaces the non-voters of r's proposal and stamps the proposal.
func (s *PGStore) Save(ctx context.Context, r Result) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("save non-voters: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	cons := make([]string, len(r.NonVoters))
	ops := make([]string, len(r.NonVoters))
	power := make([]int64, len(r.NonVoters))
	pcts := make([]string, len(r.NonVoters))
	for i, nv := range r.NonVoters {
		cons[i], ops[i], power[i], pcts[i] = nv.ConsensusAddress, nv.Operator, nv.VotingPower, nv.PowerPct
	}
	batch := &pgx.Batch{}
	batch.Queue(`DELETE FROM explorer.proposal_non_voters WHERE proposal_id = $1`, r.ProposalID)
	batch.Queue(`INSERT INTO explorer.proposal_non_voters (proposal_id, consensus_address, operator_address, voting_power, power_pct)
		SELECT $1, c, NULLIF(o, ''), p, pct
		FROM unnest($2::text[], $3::text[], $4::bigint[], $5::numeric[]) AS u(c, o, p, pct)`,
		r.ProposalID, cons, ops, power, pcts)
	batch.Queue(`UPDATE explorer.proposals SET active_set_height = $2, active_set_size = $3, non_voters_at = now(), updated_at = now()
		WHERE id = $1`, r.ProposalID, r.SetHeight, r.SetSize)
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("save non-voters of proposal %d: %w", r.ProposalID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("save non-voters of proposal %d: %w", r.ProposalID, err)
	}
	return nil
}
