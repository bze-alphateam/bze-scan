package dto

import (
	"encoding/base64"
	"encoding/json"
	"time"
	"unicode/utf8"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/daotally"
)

// DaoProposalSummary is an item of GET /api/v1/daos/{id}/proposals. Tallies
// and the denominator are base units of the DAO's stake denom (stake DAO) or
// member votes (static list: one per member), as strings.
type DaoProposalSummary struct {
	ID            int64      `json:"id"`
	DaoID         *int64     `json:"dao_id"`
	Title         *string    `json:"title"`
	Status        string     `json:"status"`
	Proposer      *string    `json:"proposer"`
	ProposerLabel *Label     `json:"proposer_label"`
	Expedited     *bool      `json:"expedited"`
	SubmitTime    *time.Time `json:"submit_time"`
	VotingEnd     *time.Time `json:"voting_end"`
	Yes           *string    `json:"yes"`
	No            *string    `json:"no"`
	Abstain       *string    `json:"abstain"`
	Denominator   *string    `json:"denominator"`
	SettledHeight *int64     `json:"settled_height"`
	// MessageTypes are the type URLs of the proposal's messages.
	MessageTypes []string `json:"message_types"`
}

// NewDaoProposalSummary builds a list item.
func NewDaoProposalSummary(p repository.DaoProposal) DaoProposalSummary {
	return DaoProposalSummary{
		ID: p.ID, DaoID: p.DaoID, Title: p.Title, Status: p.Status, Proposer: p.Proposer,
		ProposerLabel: newLabel(p.ProposerLabel), Expedited: p.Expedited, SubmitTime: utc(p.SubmitTime),
		VotingEnd: utc(p.VotingEnd), Yes: p.Yes, No: p.No, Abstain: p.Abstain, Denominator: p.Denominator,
		SettledHeight: p.SettledHeight, MessageTypes: messageTypes(p.Msgs),
	}
}

// DaoRef names a proposal's DAO.
type DaoRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// WeightDenom is the denom a stake DAO's weights and tallies are counted in.
type WeightDenom struct {
	Denom    string  `json:"denom"`
	Symbol   *string `json:"symbol"`
	Exponent *int    `json:"exponent"`
}

// NewWeightDenom builds the weight denom of denom; nil for "" (a
// static-list DAO, or a stake DAO whose live electorate was unavailable).
func NewWeightDenom(denom string, known map[string]repository.Denom) *WeightDenom {
	if denom == "" {
		return nil
	}
	w := &WeightDenom{Denom: denom}
	if d, ok := known[denom]; ok {
		exp := d.Exponent
		w.Symbol, w.Exponent = d.Symbol, &exp
	}
	return w
}

// DaoProposalChecks are the decision bars of a proposal over its stored
// tally and pinned config, computed exactly like the chain. Expedite is set
// for an expedited proposal only.
type DaoProposalChecks struct {
	Regular  daotally.Regular   `json:"regular"`
	Expedite *daotally.Expedite `json:"expedite"`
}

// DaoProposal is GET /api/v1/dao-proposals/{id}.
type DaoProposal struct {
	DaoProposalSummary
	Dao                *DaoRef            `json:"dao"`
	Summary            *string            `json:"summary"`
	Msgs               json.RawMessage    `json:"msgs"`
	SubmitHeight       *int64             `json:"submit_height"`
	SubmitTxHash       *string            `json:"submit_tx_hash"`
	PinnedConfig       json.RawMessage    `json:"pinned_config"`
	TallyUpdatedAt     *time.Time         `json:"tally_updated_at"`
	VotingEnded        bool               `json:"voting_ended"`
	ExpediteSettleable bool               `json:"expedite_settleable"`
	RetryDeadline      *time.Time         `json:"retry_deadline"`
	SettledTxHash      *string            `json:"settled_tx_hash"`
	ExpeditedEarly     *bool              `json:"expedited_early"`
	Executed           *bool              `json:"executed"`
	ExecutionError     *string            `json:"execution_error"`
	DepositRefunded    *bool              `json:"deposit_refunded"`
	DepositDestination *string            `json:"deposit_destination"`
	DepositAmount      *string            `json:"deposit_amount"`
	CancelledHeight    *int64             `json:"cancelled_height"`
	CleanedHeight      *int64             `json:"cleaned_height"`
	VotesCount         int64              `json:"votes_count"`
	WeightDenom        *WeightDenom       `json:"weight_denom"`
	Checks             *DaoProposalChecks `json:"checks"`
}

// NewDaoProposal builds the detail of p at now. voting_ended is the chain's
// rule (voting and now ≥ voting_end); expedite_settleable is the node's
// answer at the state sync's last refresh, false once not voting. Checks are
// nil without a pinned config or a tally.
func NewDaoProposal(p repository.DaoProposal, votes int64, weight *WeightDenom, now time.Time) DaoProposal {
	out := DaoProposal{
		DaoProposalSummary: NewDaoProposalSummary(p), Summary: p.Summary, Msgs: jsonOrNull(p.Msgs),
		SubmitHeight: p.SubmitHeight, SubmitTxHash: p.SubmitTxHash, PinnedConfig: jsonOrNull(p.PinnedConfig),
		TallyUpdatedAt: utc(p.TallyUpdatedAt), RetryDeadline: utc(p.RetryDeadline), SettledTxHash: p.SettledTxHash,
		ExpeditedEarly: p.ExpeditedEarly, Executed: p.Executed, ExecutionError: p.ExecutionError,
		DepositRefunded: p.DepositRefunded, DepositDestination: p.DepositDestination, DepositAmount: p.DepositAmount,
		CancelledHeight: p.CancelledHeight, CleanedHeight: p.CleanedHeight, VotesCount: votes, WeightDenom: weight,
	}
	if p.DaoID != nil && p.DaoName != nil && p.DaoType != nil {
		out.Dao = &DaoRef{ID: *p.DaoID, Name: *p.DaoName, Type: *p.DaoType}
	}
	voting := p.Status == "voting"
	out.VotingEnded = voting && p.VotingEnd != nil && !now.Before(*p.VotingEnd)
	out.ExpediteSettleable = voting && p.ExpediteSettleable
	out.Checks = checks(p)
	return out
}

// pinnedBars are the fields of a pinned DaoConfig the checks read.
type pinnedBars struct {
	MinQuorum         string `json:"min_quorum"`
	Threshold         string `json:"threshold"`
	ExpediteQuorum    string `json:"expedite_quorum"`
	ExpediteThreshold string `json:"expedite_threshold"`
}

func checks(p repository.DaoProposal) *DaoProposalChecks {
	if len(p.PinnedConfig) == 0 || p.Denominator == nil || p.Yes == nil || p.No == nil || p.Abstain == nil {
		return nil
	}
	var bars pinnedBars
	if json.Unmarshal(p.PinnedConfig, &bars) != nil {
		return nil
	}
	t := daotally.Tally{Denominator: *p.Denominator, Yes: *p.Yes, No: *p.No, Abstain: *p.Abstain}
	regular, err := daotally.CheckRegular(bars.MinQuorum, bars.Threshold, t)
	if err != nil {
		return nil
	}
	out := &DaoProposalChecks{Regular: regular}
	if p.Expedited != nil && *p.Expedited {
		if e, err := daotally.CheckExpedite(bars.ExpediteQuorum, bars.ExpediteThreshold, t); err == nil {
			out.Expedite = &e
		}
	}
	return out
}

// DaoVote is an item of GET /api/v1/dao-proposals/{id}/votes. Weight is the
// ballot's weight when cast; EffectiveWeight what is left of it after the
// ballot reductions since (a voter leaving the stake, a member removed).
type DaoVote struct {
	Voter           string    `json:"voter"`
	VoterLabel      *Label    `json:"voter_label"`
	Option          string    `json:"option"`
	Weight          string    `json:"weight"`
	EffectiveWeight string    `json:"effective_weight"`
	Height          int64     `json:"height"`
	TxHash          *string   `json:"tx_hash"`
	Time            time.Time `json:"time"`
}

// NewDaoVote builds a votes item.
func NewDaoVote(v repository.DaoVote) DaoVote {
	return DaoVote{Voter: v.Voter, VoterLabel: newLabel(v.VoterLabel), Option: v.Option, Weight: v.Weight,
		EffectiveWeight: v.EffectiveWeight, Height: v.Height, TxHash: v.TxHash, Time: v.Time.UTC()}
}

// DaoVoteList is GET /api/v1/dao-proposals/{id}/votes.
type DaoVoteList struct {
	Items       []DaoVote    `json:"items"`
	NextCursor  *string      `json:"next_cursor"`
	WeightDenom *WeightDenom `json:"weight_denom"`
}

// daoVoteCursor is the JSON of a votes cursor.
type daoVoteCursor struct {
	Weight string `json:"w"`
	Voter  string `json:"v"`
}

// EncodeDaoVoteCursor is the cursor after a votes row: base64 (URL alphabet,
// no padding) of its effective weight (a decimal integer, beyond int64) and
// voter.
func EncodeDaoVoteCursor(v repository.DaoVote) string {
	raw, _ := json.Marshal(daoVoteCursor{Weight: v.EffectiveWeight, Voter: v.Voter}) // strings always marshal
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeDaoVoteCursor reverses EncodeDaoVoteCursor.
func DecodeDaoVoteCursor(cursor string) (*repository.DaoVoteKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || !utf8.Valid(raw) {
		return nil, ErrInvalidCursor
	}
	var c daoVoteCursor
	if err := json.Unmarshal(raw, &c); err != nil || c.Voter == "" || !isDecimalInt(c.Weight) {
		return nil, ErrInvalidCursor
	}
	return &repository.DaoVoteKey{EffectiveWeight: c.Weight, Voter: c.Voter}, nil
}

func isDecimalInt(s string) bool {
	if s == "" || len(s) > 80 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// messageTypes reads the @type of each message of a msgs JSON array; an
// empty list for anything else.
func messageTypes(raw json.RawMessage) []string {
	var msgs []struct {
		Type string `json:"@type"`
	}
	out := []string{}
	if json.Unmarshal(raw, &msgs) != nil {
		return out
	}
	for _, m := range msgs {
		out = append(out, m.Type)
	}
	return out
}
