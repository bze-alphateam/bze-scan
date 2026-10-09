package proposals

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PGStore writes the set into explorer.proposals.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// resolvedAt is the height of the stored block event that resolved proposal
// $1, for a proposal the sync wrote before its events were indexed.
const resolvedAt = `(SELECT max(e.height) FROM explorer.block_events e
		  WHERE e.type IN ('active_proposal', 'inactive_proposal') AND e.attrs->>'proposal_id' = $1::bigint::text
		    AND e.attrs->>'proposal_result' IS DISTINCT FROM 'expedited_proposal_rejected')`

// frozen is true when the stored row is resolved but the node answers an
// open status: an answer read before the resolution was indexed. Such an
// answer never overwrites the row's status or its final tally.
const frozen = `(p.resolved_height IS NOT NULL AND EXCLUDED.status IN ('deposit_period', 'voting_period'))`

// upsertSQL writes one proposal. Submit height and transaction are the
// transformer's; messages the node cleared (a failed proposal's) keep the
// stored ones; a tally the node did not give keeps the stored one.
var upsertSQL = `INSERT INTO explorer.proposals AS p (
		id, title, summary, metadata, proposer, kind, message_types, messages, status, expedited,
		submit_time, deposit_end_time, voting_start_time, voting_end_time, total_deposit,
		tally_yes, tally_no, tally_abstain, tally_veto, tally_bonded_tokens, tally_updated_at,
		resolved_height, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21,
		CASE WHEN $9::text IN ('passed', 'rejected', 'failed') THEN ` + resolvedAt + ` END, now())
	ON CONFLICT (id) DO UPDATE SET
		title = EXCLUDED.title, summary = EXCLUDED.summary, metadata = EXCLUDED.metadata,
		proposer = COALESCE(EXCLUDED.proposer, p.proposer),
		kind = CASE WHEN EXCLUDED.messages IS NULL THEN p.kind ELSE EXCLUDED.kind END,
		message_types = CASE WHEN EXCLUDED.messages IS NULL THEN p.message_types ELSE EXCLUDED.message_types END,
		messages = COALESCE(EXCLUDED.messages, p.messages),
		status = CASE WHEN ` + frozen + ` THEN p.status ELSE EXCLUDED.status END,
		expedited = EXCLUDED.expedited, submit_time = EXCLUDED.submit_time,
		deposit_end_time = EXCLUDED.deposit_end_time, voting_start_time = EXCLUDED.voting_start_time,
		voting_end_time = EXCLUDED.voting_end_time, total_deposit = EXCLUDED.total_deposit,
		tally_yes = CASE WHEN ` + frozen + ` THEN p.tally_yes ELSE COALESCE(EXCLUDED.tally_yes, p.tally_yes) END,
		tally_no = CASE WHEN ` + frozen + ` THEN p.tally_no ELSE COALESCE(EXCLUDED.tally_no, p.tally_no) END,
		tally_abstain = CASE WHEN ` + frozen + ` THEN p.tally_abstain ELSE COALESCE(EXCLUDED.tally_abstain, p.tally_abstain) END,
		tally_veto = CASE WHEN ` + frozen + ` THEN p.tally_veto ELSE COALESCE(EXCLUDED.tally_veto, p.tally_veto) END,
		tally_bonded_tokens = CASE WHEN ` + frozen + ` THEN p.tally_bonded_tokens
			ELSE COALESCE(EXCLUDED.tally_bonded_tokens, p.tally_bonded_tokens) END,
		tally_updated_at = CASE WHEN ` + frozen + ` THEN p.tally_updated_at
			ELSE COALESCE(EXCLUDED.tally_updated_at, p.tally_updated_at) END,
		resolved_height = COALESCE(p.resolved_height, EXCLUDED.resolved_height),
		updated_at = now()`

// Save upserts proposals in one transaction.
func (s *PGStore) Save(ctx context.Context, ps []Proposal) error {
	if len(ps) == 0 {
		return nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("proposals save: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	batch := &pgx.Batch{}
	for _, p := range ps {
		var messages, yes, no, abstain, veto any
		if p.Messages != nil {
			messages = string(p.Messages)
		}
		if p.Tally != nil {
			yes, no, abstain, veto = p.Tally.Yes, p.Tally.No, p.Tally.Abstain, p.Tally.Veto
		}
		batch.Queue(upsertSQL, int64(p.ID), p.Title, null(p.Summary), null(p.Metadata), null(p.Proposer), p.Kind, //nolint:gosec // proposal ids are far below 2^63
			p.MessageTypes, messages, p.Status, p.Expedited, p.SubmitTime, p.DepositEndTime, p.VotingStartTime,
			p.VotingEndTime, string(p.TotalDeposit), yes, no, abstain, veto, null(p.BondedTokens), p.TallyAt)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("proposals save: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("proposals save: %w", err)
	}
	return nil
}

// Voting returns the ids of the stored proposals in their voting period.
func (s *PGStore) Voting(ctx context.Context) ([]uint64, error) {
	rows, err := s.db.Query(ctx, `SELECT id FROM explorer.proposals WHERE status = 'voting_period' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("proposals voting: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("proposals voting: %w", err)
	}
	out := make([]uint64, len(ids))
	for i, id := range ids {
		out[i] = uint64(id) //nolint:gosec // ids are positive
	}
	return out, nil
}

func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}
