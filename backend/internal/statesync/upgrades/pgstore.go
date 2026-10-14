package upgrades

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bze-alphateam/bze-scan/backend/internal/activeset"
)

// DB is the part of a pgx pool the store needs; *pgxpool.Pool satisfies it.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PGStore reads explorer.proposals and writes explorer.upgrades and
// explorer.upgrade_recovery.
type PGStore struct {
	db DB
}

var (
	_ Store      = (*PGStore)(nil)
	_ WatchStore = (*PGStore)(nil)
)

// NewPGStore returns a store over db.
func NewPGStore(db DB) *PGStore {
	return &PGStore{db: db}
}

// discoverSQL adds the passed upgrades by the plan of their message: a
// MsgSoftwareUpgrade's, or the one of the legacy content a
// MsgExecLegacyContent carries. A height planned twice keeps the latest
// proposal.
const discoverSQL = `INSERT INTO explorer.upgrades (height, proposal_id, name)
	SELECT DISTINCT ON (plan.h::bigint) plan.h::bigint, p.id, coalesce(plan.n, '')
	FROM explorer.proposals p,
	     jsonb_array_elements(CASE WHEN jsonb_typeof(p.messages) = 'array' THEN p.messages END) m,
	     LATERAL (SELECT coalesce(m->'plan'->>'height', m->'content'->'plan'->>'height') AS h,
	                     coalesce(m->'plan'->>'name', m->'content'->'plan'->>'name') AS n) plan
	WHERE p.status = 'passed' AND p.kind = 'software_upgrade' AND plan.h ~ '^[0-9]{1,18}$' AND plan.h::bigint > 0
	ORDER BY plan.h::bigint, p.id DESC
	ON CONFLICT (height) DO NOTHING`

// Discover adds the passed upgrades not held yet.
func (s *PGStore) Discover(ctx context.Context) error {
	if _, err := s.db.Exec(ctx, discoverSQL); err != nil {
		return fmt.Errorf("discover upgrades: %w", err)
	}
	return nil
}

// upgradeCols reads an upgrade row u, Resumed from the indexed blocks.
const upgradeCols = `u.height, u.proposal_id, u.name, u.halt_time, u.resume_time, u.watched, coalesce(u.window_end_height, 0),
	(SELECT count(*) FROM explorer.blocks b WHERE b.height IN (u.height + 1, u.height + 2)) = 2`

func scanUpgrade(row pgx.Row) (Upgrade, error) {
	var u Upgrade
	err := row.Scan(&u.Height, &u.ProposalID, &u.Name, &u.HaltTime, &u.ResumeTime, &u.Watched, &u.ScannedTo, &u.Resumed)
	return u, err
}

// Pending lists the upgrades without computed_at, by height.
func (s *PGStore) Pending(ctx context.Context) ([]Upgrade, error) {
	rows, err := s.db.Query(ctx, `SELECT `+upgradeCols+` FROM explorer.upgrades u WHERE u.computed_at IS NULL ORDER BY u.height`)
	if err != nil {
		return nil, fmt.Errorf("pending upgrades: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Upgrade, error) { return scanUpgrade(r) })
	if err != nil {
		return nil, fmt.Errorf("pending upgrades: %w", err)
	}
	return out, nil
}

// Upgrade returns the upgrade planned at height.
func (s *PGStore) Upgrade(ctx context.Context, height int64) (Upgrade, bool, error) {
	u, err := scanUpgrade(s.db.QueryRow(ctx, `SELECT `+upgradeCols+` FROM explorer.upgrades u WHERE u.height = $1`, height))
	if errors.Is(err, pgx.ErrNoRows) {
		return Upgrade{}, false, nil
	}
	if err != nil {
		return Upgrade{}, false, fmt.Errorf("upgrade %d: %w", height, err)
	}
	return u, true, nil
}

// Tip reads indexer_state.last_indexed_height.
func (s *PGStore) Tip(ctx context.Context) (int64, bool, error) {
	var v string
	err := s.db.QueryRow(ctx, `SELECT value FROM explorer.indexer_state WHERE key = 'last_indexed_height'`).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("live cursor: %w", err)
	}
	h, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("live cursor %q: %w", v, err)
	}
	return h, true, nil
}

// Members lists the upgrade's members.
func (s *PGStore) Members(ctx context.Context, height int64) ([]Member, error) {
	rows, err := s.db.Query(ctx, `SELECT consensus_address, back_time IS NOT NULL
		FROM explorer.upgrade_recovery WHERE upgrade_height = $1 ORDER BY consensus_address`, height)
	if err != nil {
		return nil, fmt.Errorf("upgrade %d members: %w", height, err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Member, error) {
		var m Member
		return m, r.Scan(&m.ConsensusAddress, &m.Back)
	})
	if err != nil {
		return nil, fmt.Errorf("upgrade %d members: %w", height, err)
	}
	return out, nil
}

// AddMembers writes the set at U+1; the members already held keep their
// recovery, their operator is refreshed when one is known now.
func (s *PGStore) AddMembers(ctx context.Context, height int64, set []activeset.Member) error {
	cons := make([]string, len(set))
	ops := make([]string, len(set))
	power := make([]int64, len(set))
	for i, m := range set {
		cons[i], ops[i], power[i] = m.ConsensusAddress, m.Operator, m.VotingPower
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO explorer.upgrade_recovery (upgrade_height, consensus_address, operator_address, voting_power)
		SELECT $1, c, NULLIF(o, ''), p FROM unnest($2::text[], $3::text[], $4::bigint[]) AS u(c, o, p)
		ON CONFLICT (upgrade_height, consensus_address) DO UPDATE
		SET operator_address = coalesce(EXCLUDED.operator_address, upgrade_recovery.operator_address)`,
		height, cons, ops, power); err != nil {
		return fmt.Errorf("upgrade %d members: %w", height, err)
	}
	return nil
}

// HaltVotes records the members first seen voting during the halt and
// marks the upgrade watched.
func (s *PGStore) HaltVotes(ctx context.Context, height int64, votes []HaltVote) error {
	cons := make([]string, len(votes))
	backs := make([]time.Time, len(votes))
	at := make([]time.Time, len(votes))
	for i, v := range votes {
		cons[i], backs[i], at[i] = v.ConsensusAddress, v.BackTime, v.VoteTime
	}
	batch := &pgx.Batch{}
	batch.Queue(`UPDATE explorer.upgrade_recovery r
		SET back_time = u.b, vote_time = u.v, back_source = 'halt_vote',
		    first_signed_height = NULL, blocks_needed = NULL, seconds_back = NULL
		FROM unnest($2::text[], $3::timestamptz[], $4::timestamptz[]) AS u(c, b, v)
		WHERE r.upgrade_height = $1 AND r.consensus_address = u.c AND r.back_source IS DISTINCT FROM 'halt_vote'`,
		height, cons, backs, at)
	batch.Queue(`UPDATE explorer.upgrades SET watched = true WHERE height = $1`, height)
	return s.send(ctx, batch, fmt.Sprintf("upgrade %d halt votes", height))
}

// SaveScan writes a scan in one transaction.
func (s *PGStore) SaveScan(ctx context.Context, sc Scan) error {
	n := len(sc.Backs)
	cons := make([]string, n)
	backs := make([]time.Time, n)
	at := make([]time.Time, n)
	first := make([]int64, n)
	needed := make([]int64, n)
	for i, b := range sc.Backs {
		cons[i], backs[i], at[i], first[i], needed[i] = b.ConsensusAddress, b.BackTime, b.VoteTime, b.FirstSignedHeight, b.BlocksNeeded
	}
	batch := &pgx.Batch{}
	batch.Queue(`UPDATE explorer.upgrade_recovery r
		SET back_time = u.b, vote_time = u.v, back_source = 'commit', first_signed_height = u.f, blocks_needed = u.n
		FROM unnest($2::text[], $3::timestamptz[], $4::timestamptz[], $5::bigint[], $6::bigint[]) AS u(c, b, v, f, n)
		WHERE r.upgrade_height = $1 AND r.consensus_address = u.c AND r.back_time IS NULL`,
		sc.Height, cons, backs, at, first, needed)
	batch.Queue(`UPDATE explorer.upgrades
		SET halt_time = coalesce($2, halt_time), resume_time = coalesce($3, resume_time), active_set_size = $4,
		    window_end_height = nullif($5::bigint, 0), computed_at = CASE WHEN $6 THEN now() END
		WHERE height = $1`, sc.Height, sc.HaltTime, sc.ResumeTime, sc.SetSize, sc.ScannedTo, sc.Complete)
	batch.Queue(`UPDATE explorer.upgrade_recovery r
		SET seconds_back = greatest(0, floor(extract(epoch FROM r.back_time - u.halt_time)))::integer
		FROM explorer.upgrades u
		WHERE u.height = $1 AND r.upgrade_height = $1 AND r.back_time IS NOT NULL AND u.halt_time IS NOT NULL`, sc.Height)
	return s.send(ctx, batch, fmt.Sprintf("upgrade %d scan", sc.Height))
}

// Reset clears what the commits gave and the progress; the halt votes stay.
func (s *PGStore) Reset(ctx context.Context, height int64) error {
	batch := &pgx.Batch{}
	batch.Queue(`UPDATE explorer.upgrade_recovery
		SET back_time = NULL, back_source = NULL, vote_time = NULL, first_signed_height = NULL, blocks_needed = NULL, seconds_back = NULL
		WHERE upgrade_height = $1 AND back_source IS DISTINCT FROM 'halt_vote'`, height)
	batch.Queue(`UPDATE explorer.upgrades
		SET halt_time = NULL, resume_time = NULL, window_end_height = NULL, computed_at = NULL WHERE height = $1`, height)
	return s.send(ctx, batch, fmt.Sprintf("reset upgrade %d", height))
}

// send runs batch in one transaction.
func (s *PGStore) send(ctx context.Context, batch *pgx.Batch, what string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}
