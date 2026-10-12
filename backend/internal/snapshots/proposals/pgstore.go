package proposals

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PGStore reads and writes explorer.proposals and its votes and deposits.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// Resolved lists the proposals passed, rejected or failed after a voting
// period: a proposal dropped in its deposit period has no voting start, a
// canceled one is not resolved this way.
func (s *PGStore) Resolved(ctx context.Context, ids []uint64) ([]Proposal, error) {
	var filter []int64
	for _, id := range ids {
		filter = append(filter, int64(id)) //nolint:gosec // proposal ids are far below 2^63
	}
	rows, err := s.db.Query(ctx, `SELECT id, resolved_height, submit_height, voting_end_time
		FROM explorer.proposals
		WHERE status IN ('passed', 'rejected', 'failed')
		  AND voting_start_time IS NOT NULL AND voting_end_time IS NOT NULL
		  AND ($1::bigint[] IS NULL OR id = ANY($1))
		ORDER BY id`, filter)
	if err != nil {
		return nil, fmt.Errorf("snapshot proposals: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Proposal, error) {
		var p Proposal
		var id int64
		err := row.Scan(&id, &p.ResolvedHeight, &p.SubmitHeight, &p.VotingEndTime)
		p.ID = uint64(id) //nolint:gosec // ids are positive
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("snapshot proposals: %w", err)
	}
	return out, nil
}

// updateSQL overwrites the bonded tokens and fills a missing resolution
// height.
const updateSQL = `UPDATE explorer.proposals SET
		tally_bonded_tokens = $2::numeric, tally_source = 'archive',
		resolved_height = COALESCE(resolved_height, $3), updated_at = now()
	WHERE id = $1
	  AND (tally_bonded_tokens, tally_source, resolved_height) IS DISTINCT FROM
	      ($2::numeric, 'archive', COALESCE(resolved_height, $3))`

// votesSQL rewrites the options of a stored vote (its height, transaction
// and time stay) and adds a missing voter as an archive row.
const votesSQL = `INSERT INTO explorer.proposal_votes AS v (proposal_id, voter, options, option, height, tx_index, time, source)
	SELECT $1, r.voter, r.options, r.option, $2, -1, $3, 'archive'
	  FROM jsonb_to_recordset($4::jsonb) AS r(voter text, options jsonb, option text)
	ON CONFLICT (proposal_id, voter) DO UPDATE SET options = EXCLUDED.options, option = EXCLUDED.option
	WHERE (v.options, v.option) IS DISTINCT FROM (EXCLUDED.options, EXCLUDED.option)`

// depositsSQL adds the total of a depositor without a row as an archive
// row, or rewrites the archive row it has.
const depositsSQL = `INSERT INTO explorer.proposal_deposits AS d (proposal_id, depositor, height, tx_index, amount, time, source)
	SELECT $1, r.depositor, $2, -1, r.amount, $3, 'archive'
	  FROM jsonb_to_recordset($4::jsonb) AS r(depositor text, amount jsonb)
	 WHERE NOT EXISTS (SELECT 1 FROM explorer.proposal_deposits e
	                    WHERE e.proposal_id = $1 AND e.depositor = r.depositor AND e.source = 'event')
	ON CONFLICT (proposal_id, depositor, height, tx_index) DO UPDATE SET amount = EXCLUDED.amount, time = EXCLUDED.time
	WHERE (d.amount, d.time) IS DISTINCT FROM (EXCLUDED.amount, EXCLUDED.time)`

type voteRow struct {
	Voter   string          `json:"voter"`
	Options json.RawMessage `json:"options"`
	Option  *string         `json:"option"`
}

type depositRow struct {
	Depositor string          `json:"depositor"`
	Amount    json.RawMessage `json:"amount"`
}

// Save writes s in one transaction.
func (s *PGStore) Save(ctx context.Context, snap Snapshot) error {
	votes := make([]voteRow, 0, len(snap.Votes))
	for _, v := range snap.Votes {
		var option *string
		if v.Option != "" {
			option = &v.Option
		}
		votes = append(votes, voteRow{Voter: v.Voter, Options: v.Options, Option: option})
	}
	deposits := make([]depositRow, 0, len(snap.Deposits))
	for _, d := range snap.Deposits {
		deposits = append(deposits, depositRow(d))
	}
	votesJSON, err := json.Marshal(votes)
	if err != nil {
		return fmt.Errorf("snapshot proposal %d: %w", snap.ID, err)
	}
	depositsJSON, err := json.Marshal(deposits)
	if err != nil {
		return fmt.Errorf("snapshot proposal %d: %w", snap.ID, err)
	}
	var resolved *int64
	if snap.ResolvedHeight > 0 {
		resolved = &snap.ResolvedHeight
	}
	id := int64(snap.ID) //nolint:gosec // proposal ids are far below 2^63
	at := snap.Time.UTC().Truncate(time.Microsecond)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("snapshot proposal %d: %w", snap.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	batch := &pgx.Batch{}
	batch.Queue(updateSQL, id, snap.BondedTokens, resolved)
	batch.Queue(votesSQL, id, snap.Height, at, string(votesJSON))
	batch.Queue(depositsSQL, id, snap.Height, at, string(depositsJSON))
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("snapshot proposal %d: %w", snap.ID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("snapshot proposal %d: %w", snap.ID, err)
	}
	return nil
}
