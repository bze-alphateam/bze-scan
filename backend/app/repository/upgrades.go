package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Upgrade is an explorer.upgrades row: a software upgrade planned at
// Height and how the chain came back from its halt.
type Upgrade struct {
	Height     int64
	ProposalID int64
	Name       string
	// HaltTime and ResumeTime are nil until the state sync read the
	// commits after the upgrade.
	HaltTime        *time.Time
	ResumeTime      *time.Time
	Watched         bool
	ActiveSetSize   *int
	WindowEndHeight *int64
	ComputedAt      *time.Time
}

// ProposalUpgrade is the upgrade a proposal planned, as its page shows it.
type ProposalUpgrade struct {
	Height     int64
	HaltTime   *time.Time
	ResumeTime *time.Time
	Watched    bool
}

// UpgradeMember is an explorer.upgrade_recovery row with the moniker of its
// validator and its share of the set's power.
type UpgradeMember struct {
	ConsensusAddress string
	// OperatorAddress is nil when no validator was known with the
	// consensus address.
	OperatorAddress   *string
	Moniker           *string
	VotingPower       int64
	PowerPct          string
	BackTime          *time.Time
	BackSource        *string
	VoteTime          *time.Time
	FirstSignedHeight *int64
	BlocksNeeded      *int64
	SecondsBack       *int
}

// Upgrade returns the upgrade planned at height, or ErrNotFound.
func (r *Explorer) Upgrade(ctx context.Context, height int64) (*Upgrade, error) {
	var u Upgrade
	err := r.db.QueryRow(ctx, `SELECT height, proposal_id, name, halt_time, resume_time, watched,
			active_set_size, window_end_height, computed_at
		FROM explorer.upgrades WHERE height = $1`, height).
		Scan(&u.Height, &u.ProposalID, &u.Name, &u.HaltTime, &u.ResumeTime, &u.Watched,
			&u.ActiveSetSize, &u.WindowEndHeight, &u.ComputedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("upgrade %d: %w", height, err)
	}
	return &u, nil
}

// UpgradeMembers returns the members of the set at the upgrade's halt, by
// back_time (the ones never back last), then voting power descending.
func (r *Explorer) UpgradeMembers(ctx context.Context, height int64) ([]UpgradeMember, error) {
	rows, err := r.db.Query(ctx, `SELECT m.consensus_address, m.operator_address, v.moniker, m.voting_power,
			coalesce(round(m.voting_power * 100.0 / nullif(sum(m.voting_power) OVER (), 0), 5), 0)::text,
			m.back_time, m.back_source, m.vote_time, m.first_signed_height, m.blocks_needed, m.seconds_back
		FROM explorer.upgrade_recovery m
		LEFT JOIN explorer.validators v ON v.operator_address = m.operator_address
		WHERE m.upgrade_height = $1
		ORDER BY m.back_time NULLS LAST, m.voting_power DESC, m.consensus_address`, height)
	if err != nil {
		return nil, fmt.Errorf("upgrade %d members: %w", height, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (UpgradeMember, error) {
		var m UpgradeMember
		err := row.Scan(&m.ConsensusAddress, &m.OperatorAddress, &m.Moniker, &m.VotingPower, &m.PowerPct,
			&m.BackTime, &m.BackSource, &m.VoteTime, &m.FirstSignedHeight, &m.BlocksNeeded, &m.SecondsBack)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("upgrade %d members: %w", height, err)
	}
	return out, nil
}
