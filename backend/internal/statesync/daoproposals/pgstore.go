package daoproposals

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

// PGStore writes the set into explorer.dao_proposals.
type PGStore struct {
	db DB
}

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// upsertSQL writes what the chain says of the proposals.
//
//   - Submission facts only fill what the events did not write (the same
//     values; the events' own are kept).
//   - The pinned config is always the chain's.
//   - The status moves only forward from what is stored (voting → any,
//     execution_failed → executed | expired): an answer read before an
//     indexed settle never reopens it. A status the sync changes loses its
//     height (status_height NULL), so an older event replayed later cannot
//     move it back.
//   - The running tally and expedite_settleable are written only while the
//     proposal is voting both here and on chain; a final tally is the
//     settle's.
const upsertSQL = `INSERT INTO explorer.dao_proposals AS p (id, dao_id, proposer, title, summary, msgs, expedited,
		submit_height, submit_time, voting_end, denominator, pinned_config, status, yes, no, abstain, tally_updated_at,
		expedite_settleable, retry_deadline, updated_at)
	SELECT r.id, r.dao_id, r.proposer, r.title, r.summary, r.msgs, r.expedited, r.submit_height, r.submit_time,
	       r.voting_end, r.denominator, r.pinned_config, r.status, r.yes, r.no, r.abstain, r.tally_at,
	       r.status = 'voting' AND r.expedite_settleable, r.retry_deadline, now()
	  FROM jsonb_to_recordset($1::jsonb) AS r(id bigint, dao_id bigint, proposer text, title text, summary text,
	       msgs jsonb, expedited boolean, submit_height bigint, submit_time timestamptz, voting_end timestamptz,
	       denominator numeric, pinned_config jsonb, status text, yes numeric, no numeric, abstain numeric,
	       tally_at timestamptz, expedite_settleable boolean, retry_deadline timestamptz)
	 ORDER BY r.id
	ON CONFLICT (id) DO UPDATE SET
		dao_id = COALESCE(p.dao_id, EXCLUDED.dao_id), proposer = COALESCE(p.proposer, EXCLUDED.proposer),
		title = COALESCE(p.title, EXCLUDED.title), summary = COALESCE(p.summary, EXCLUDED.summary),
		msgs = COALESCE(p.msgs, EXCLUDED.msgs), expedited = COALESCE(p.expedited, EXCLUDED.expedited),
		submit_height = COALESCE(p.submit_height, EXCLUDED.submit_height),
		submit_time = COALESCE(p.submit_time, EXCLUDED.submit_time),
		voting_end = COALESCE(p.voting_end, EXCLUDED.voting_end),
		denominator = COALESCE(p.denominator, EXCLUDED.denominator),
		pinned_config = EXCLUDED.pinned_config,
		status = CASE WHEN ` + forward + ` THEN EXCLUDED.status ELSE p.status END,
		status_height = CASE WHEN ` + forward + ` AND p.status IS DISTINCT FROM EXCLUDED.status THEN NULL
		                     ELSE p.status_height END,
		yes = CASE WHEN ` + running + ` THEN EXCLUDED.yes ELSE COALESCE(p.yes, EXCLUDED.yes) END,
		no = CASE WHEN ` + running + ` THEN EXCLUDED.no ELSE COALESCE(p.no, EXCLUDED.no) END,
		abstain = CASE WHEN ` + running + ` THEN EXCLUDED.abstain ELSE COALESCE(p.abstain, EXCLUDED.abstain) END,
		tally_updated_at = CASE WHEN ` + running + ` THEN EXCLUDED.tally_updated_at
		                        ELSE COALESCE(p.tally_updated_at, EXCLUDED.tally_updated_at) END,
		expedite_settleable = ` + running + ` AND EXCLUDED.expedite_settleable,
		retry_deadline = CASE WHEN EXCLUDED.status = 'execution_failed'
		                           AND COALESCE(p.status, 'voting') IN ('voting', 'execution_failed')
		                      THEN EXCLUDED.retry_deadline ELSE p.retry_deadline END,
		updated_at = now()`

// forward is true when the chain's status may replace the stored one.
const forward = `(p.status IS NULL OR p.status = 'voting' OR p.status = EXCLUDED.status
	OR (p.status = 'execution_failed' AND EXCLUDED.status IN ('executed', 'expired')))`

// running is true when the chain's tally is a running one the row takes.
const running = `(COALESCE(p.status, 'voting') = 'voting' AND EXCLUDED.status = 'voting')`

type row struct {
	ID                 uint64          `json:"id"`
	DaoID              uint64          `json:"dao_id"`
	Proposer           string          `json:"proposer"`
	Title              string          `json:"title"`
	Summary            string          `json:"summary"`
	Msgs               json.RawMessage `json:"msgs"`
	Expedited          bool            `json:"expedited"`
	SubmitHeight       int64           `json:"submit_height"`
	SubmitTime         time.Time       `json:"submit_time"`
	VotingEnd          time.Time       `json:"voting_end"`
	Denominator        string          `json:"denominator"`
	PinnedConfig       json.RawMessage `json:"pinned_config"`
	Status             string          `json:"status"`
	Yes                string          `json:"yes"`
	No                 string          `json:"no"`
	Abstain            string          `json:"abstain"`
	TallyAt            time.Time       `json:"tally_at"`
	ExpediteSettleable bool            `json:"expedite_settleable"`
	RetryDeadline      *time.Time      `json:"retry_deadline"`
}

// Save upserts the proposals in one transaction.
func (s *PGStore) Save(ctx context.Context, ps []Proposal) error {
	rows := make([]row, 0, len(ps))
	for _, p := range ps {
		rows = append(rows, row(p))
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("dao proposals: save: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dao proposals: save: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, upsertSQL, string(payload)); err != nil {
		return fmt.Errorf("dao proposals: save: %w", err)
	}
	return tx.Commit(ctx)
}

// Voting returns the ids of the stored proposals in voting (a placeholder
// without a status, or a proposal cleaned up, is not).
func (s *PGStore) Voting(ctx context.Context) ([]uint64, error) {
	rows, err := s.db.Query(ctx, `SELECT id FROM explorer.dao_proposals
		WHERE status = 'voting' AND cleaned_height IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("dao proposals: voting: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("dao proposals: voting: %w", err)
	}
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		out = append(out, uint64(id))
	}
	return out, nil
}
