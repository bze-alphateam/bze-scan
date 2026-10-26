package dto

import (
	"strconv"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/upgrades"
)

// LiveHalt is GET /api/v1/upgrades/live while the chain waits at
// HaltHeight for the software upgrade planned at Height. Powers are consensus powers as decimal strings;
// consensus resumes once PrecommitPower is more than two thirds of
// TotalPower.
type LiveHalt struct {
	Halted         bool             `json:"halted"`
	Height         int64            `json:"height"`
	HaltHeight     int64            `json:"halt_height"`
	ProposalID     int64            `json:"proposal_id"`
	Name           string           `json:"name"`
	Round          int32            `json:"round"`
	HaltedSince    time.Time        `json:"halted_since"`
	CheckedAt      time.Time        `json:"checked_at"`
	TotalPower     string           `json:"total_power"`
	BackPower      string           `json:"back_power"`
	PrevotePower   string           `json:"prevote_power"`
	PrecommitPower string           `json:"precommit_power"`
	Members        []LiveHaltMember `json:"members"`
}

// NotHalted is GET /api/v1/upgrades/live while the chain is not halted
// for a software upgrade: {"halted": false}.
type NotHalted struct {
	Halted bool `json:"halted"`
}

// LiveHaltMember is a validator of the set at the halt height, by voting
// power descending. Back: it voted at the halt height in any round;
// Prevote/Precommit: in the current round. Operator and Moniker are null
// when the explorer does not know the validator.
type LiveHaltMember struct {
	ConsensusAddress string  `json:"consensus_address"`
	Operator         *string `json:"operator_address"`
	Moniker          *string `json:"moniker"`
	VotingPower      string  `json:"voting_power"`
	Back             bool    `json:"back"`
	Prevote          bool    `json:"prevote"`
	Precommit        bool    `json:"precommit"`
}

// NewLiveHalt shapes a watch snapshot, naming its members by monikers
// (operator → moniker).
func NewLiveHalt(l upgrades.Live, monikers map[string]string) LiveHalt {
	out := LiveHalt{
		Halted: true, Height: l.Height, HaltHeight: l.Height + 1, ProposalID: l.ProposalID, Name: l.Name,
		Round: l.Round, HaltedSince: l.HaltedSince, CheckedAt: l.CheckedAt,
		Members: make([]LiveHaltMember, len(l.Members)),
	}
	var total, back, prevote, precommit int64
	for i, m := range l.Members {
		total += m.VotingPower
		if m.Back {
			back += m.VotingPower
		}
		if m.Prevote {
			prevote += m.VotingPower
		}
		if m.Precommit {
			precommit += m.VotingPower
		}
		lm := LiveHaltMember{
			ConsensusAddress: m.ConsensusAddress, VotingPower: strconv.FormatInt(m.VotingPower, 10),
			Back: m.Back, Prevote: m.Prevote, Precommit: m.Precommit,
		}
		if m.Operator != "" {
			op := m.Operator
			lm.Operator = &op
			if name, ok := monikers[op]; ok {
				lm.Moniker = &name
			}
		}
		out.Members[i] = lm
	}
	out.TotalPower = strconv.FormatInt(total, 10)
	out.BackPower = strconv.FormatInt(back, 10)
	out.PrevotePower = strconv.FormatInt(prevote, 10)
	out.PrecommitPower = strconv.FormatInt(precommit, 10)
	return out
}
