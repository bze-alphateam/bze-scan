package dto

import (
	"time"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// ProposalUpgrade is the "upgrade" of GET /api/v1/proposals/{id} for a
// passed software upgrade: its plan height, when the chain halted for it
// and resumed (null until the state sync read the blocks after it),
// whether the halt watch saw the halt, and the halt's length.
type ProposalUpgrade struct {
	Height          int64      `json:"height"`
	HaltTime        *time.Time `json:"halt_time"`
	ResumeTime      *time.Time `json:"resume_time"`
	Watched         bool       `json:"watched"`
	DowntimeSeconds *int64     `json:"downtime_seconds"`
}

// Upgrade is GET /api/v1/upgrades/{height}: a software upgrade planned at
// Height (the old binary stops after deciding that block, consensus waits
// at Height+1), and every validator of the set at Height+1 with when it
// came back. ComputedAt is null while the commits after the upgrade are
// still being read; WindowEndHeight is the last height read.
type Upgrade struct {
	Height          int64           `json:"height"`
	ProposalID      int64           `json:"proposal_id"`
	Name            string          `json:"name"`
	HaltTime        *time.Time      `json:"halt_time"`
	ResumeTime      *time.Time      `json:"resume_time"`
	DowntimeSeconds *int64          `json:"downtime_seconds"`
	Watched         bool            `json:"watched"`
	ActiveSetSize   *int            `json:"active_set_size"`
	WindowEndHeight *int64          `json:"window_end_height"`
	ComputedAt      *time.Time      `json:"computed_at"`
	Members         []UpgradeMember `json:"members"`
}

// UpgradeMember is a validator of the set at the upgrade's halt, sorted by
// BackTime (null last: not back within the window read). BackSource is
// "halt_vote" (seen voting during the halt by the halt watch: BackTime is
// the poll that saw it) or "commit" (BackTime is when the first block it
// signed was decided: the resume for a validator that signed the first
// block after the halt). VoteTime is its own time on that vote or
// signature, SecondsBack BackTime minus the halt. FirstSignedHeight and
// BlocksNeeded (heights after the halt height) are set for "commit" only.
// OperatorAddress and Moniker are null when no validator is known with
// the consensus address; PowerPct is its share of the set's power, percent
// with five decimals.
type UpgradeMember struct {
	OperatorAddress   *string    `json:"operator_address"`
	ConsensusAddress  string     `json:"consensus_address"`
	Moniker           *string    `json:"moniker"`
	VotingPower       int64      `json:"voting_power"`
	PowerPct          string     `json:"power_pct"`
	BackTime          *time.Time `json:"back_time"`
	BackSource        *string    `json:"back_source"`
	VoteTime          *time.Time `json:"vote_time"`
	SecondsBack       *int       `json:"seconds_back"`
	FirstSignedHeight *int64     `json:"first_signed_height"`
	BlocksNeeded      *int64     `json:"blocks_needed"`
}

// NewProposalUpgrade maps a proposal's upgrade; nil stays nil.
func NewProposalUpgrade(u *repository.ProposalUpgrade) *ProposalUpgrade {
	if u == nil {
		return nil
	}
	return &ProposalUpgrade{Height: u.Height, HaltTime: utc(u.HaltTime), ResumeTime: utc(u.ResumeTime),
		Watched: u.Watched, DowntimeSeconds: downtime(u.HaltTime, u.ResumeTime)}
}

// NewUpgrade maps an upgrade and its members.
func NewUpgrade(u *repository.Upgrade, members []repository.UpgradeMember) Upgrade {
	out := Upgrade{
		Height: u.Height, ProposalID: u.ProposalID, Name: u.Name, HaltTime: utc(u.HaltTime), ResumeTime: utc(u.ResumeTime),
		DowntimeSeconds: downtime(u.HaltTime, u.ResumeTime), Watched: u.Watched, ActiveSetSize: u.ActiveSetSize,
		WindowEndHeight: u.WindowEndHeight, ComputedAt: utc(u.ComputedAt),
		Members: make([]UpgradeMember, 0, len(members)),
	}
	for _, m := range members {
		out.Members = append(out.Members, UpgradeMember{
			OperatorAddress: m.OperatorAddress, ConsensusAddress: m.ConsensusAddress, Moniker: m.Moniker,
			VotingPower: m.VotingPower, PowerPct: m.PowerPct, BackTime: utc(m.BackTime), BackSource: m.BackSource,
			VoteTime: utc(m.VoteTime), SecondsBack: m.SecondsBack, FirstSignedHeight: m.FirstSignedHeight,
			BlocksNeeded: m.BlocksNeeded,
		})
	}
	return out
}

// downtime is resume − halt in whole seconds, null without both.
func downtime(halt, resume *time.Time) *int64 {
	if halt == nil || resume == nil {
		return nil
	}
	s := int64(resume.Sub(*halt) / time.Second)
	return &s
}
