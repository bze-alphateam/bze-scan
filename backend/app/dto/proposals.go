package dto

import (
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// Tally is a proposal's vote count in base units of the bond denom.
type Tally struct {
	Yes        string `json:"yes"`
	No         string `json:"no"`
	Abstain    string `json:"abstain"`
	NoWithVeto string `json:"no_with_veto"`
}

// ProposalSummary is an item of GET /api/v1/proposals. Tally is null until
// the state sync tallies the proposal; TurnoutPct is the share of the bonded
// tokens that voted, in percent with five decimals, null without a tally or
// its bonded tokens.
type ProposalSummary struct {
	ID              int64      `json:"id"`
	Title           string     `json:"title"`
	Kind            string     `json:"kind"`
	Status          string     `json:"status"`
	Expedited       bool       `json:"expedited"`
	SubmitTime      time.Time  `json:"submit_time"`
	DepositEndTime  *time.Time `json:"deposit_end_time"`
	VotingStartTime *time.Time `json:"voting_start_time"`
	VotingEndTime   *time.Time `json:"voting_end_time"`
	Tally           *Tally     `json:"tally"`
	TurnoutPct      *string    `json:"turnout_pct"`
}

// ValidatorsVoted is how many of the bonded validators have an owner that
// voted.
type ValidatorsVoted struct {
	Voted int `json:"voted"`
	Total int `json:"total"`
}

// Proposal is GET /api/v1/proposals/{id}. Messages are the proposal's
// messages as proto JSON (a legacy content inside a MsgExecLegacyContent);
// TotalDeposit is a coin list.
type Proposal struct {
	ProposalSummary
	Summary           *string         `json:"summary"`
	Metadata          *string         `json:"metadata"`
	Proposer          *string         `json:"proposer"`
	ProposerLabel     *Label          `json:"proposer_label"`
	MessageTypes      []string        `json:"message_types"`
	Messages          json.RawMessage `json:"messages"`
	TotalDeposit      json.RawMessage `json:"total_deposit"`
	TallyBondedTokens *string         `json:"tally_bonded_tokens"`
	TallyUpdatedAt    *time.Time      `json:"tally_updated_at"`
	SubmitHeight      *int64          `json:"submit_height"`
	SubmitTxHash      *string         `json:"submit_tx_hash"`
	ResolvedHeight    *int64          `json:"resolved_height"`
	ValidatorsVoted   ValidatorsVoted `json:"validators_voted"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// ProposalVote is an item of GET /api/v1/proposals/{id}/votes. Option is
// null for a split vote (Options holds the weights). Validator,
// ValidatorOperator and VotingPowerPct name the validator the voter owns,
// with its current share; null for other voters.
type ProposalVote struct {
	Voter             string          `json:"voter"`
	VoterLabel        *Label          `json:"voter_label"`
	Option            *string         `json:"option"`
	Options           json.RawMessage `json:"options"`
	Height            int64           `json:"height"`
	TxIndex           int64           `json:"tx_index"`
	TxHash            *string         `json:"tx_hash"`
	Time              time.Time       `json:"time"`
	Validator         *string         `json:"validator"`
	ValidatorOperator *string         `json:"validator_operator"`
	VotingPowerPct    *string         `json:"voting_power_pct"`
}

// ProposalDeposit is an item of GET /api/v1/proposals/{id}/deposits.
type ProposalDeposit struct {
	Depositor      string          `json:"depositor"`
	DepositorLabel *Label          `json:"depositor_label"`
	Amount         json.RawMessage `json:"amount"`
	Height         int64           `json:"height"`
	TxIndex        int64           `json:"tx_index"`
	TxHash         *string         `json:"tx_hash"`
	Time           time.Time       `json:"time"`
}

// NewProposalSummary maps a proposals list row.
func NewProposalSummary(p repository.ProposalSummary) ProposalSummary {
	out := ProposalSummary{
		ID: p.ID, Title: p.Title, Kind: p.Kind, Status: p.Status, Expedited: p.Expedited,
		SubmitTime: p.SubmitTime.UTC(), DepositEndTime: utc(p.DepositEndTime), VotingStartTime: utc(p.VotingStartTime),
		VotingEndTime: utc(p.VotingEndTime),
	}
	if p.TallyYes != nil && p.TallyNo != nil && p.TallyAbstain != nil && p.TallyVeto != nil {
		out.Tally = &Tally{Yes: *p.TallyYes, No: *p.TallyNo, Abstain: *p.TallyAbstain, NoWithVeto: *p.TallyVeto}
		out.TurnoutPct = TurnoutPct(out.Tally, p.TallyBondedTokens)
	}
	return out
}

// NewProposal maps the proposal page.
func NewProposal(p *repository.Proposal) Proposal {
	return Proposal{
		ProposalSummary: NewProposalSummary(p.ProposalSummary),
		Summary:         p.Summary, Metadata: p.Metadata, Proposer: p.Proposer, ProposerLabel: newLabel(p.ProposerLabel),
		MessageTypes: nonNil(p.MessageTypes), Messages: jsonOrNull(p.Messages), TotalDeposit: jsonOrNull(p.TotalDeposit),
		TallyBondedTokens: p.TallyBondedTokens, TallyUpdatedAt: utc(p.TallyUpdatedAt),
		SubmitHeight: p.SubmitHeight, SubmitTxHash: p.SubmitTxHash, ResolvedHeight: p.ResolvedHeight,
		ValidatorsVoted: ValidatorsVoted{Voted: p.ValidatorsVoted, Total: p.ValidatorsTotal},
		UpdatedAt:       p.UpdatedAt.UTC(),
	}
}

// NewProposalVote maps a proposal_votes row.
func NewProposalVote(v repository.ProposalVote) ProposalVote {
	return ProposalVote{
		Voter: v.Voter, VoterLabel: newLabel(v.VoterLabel), Option: v.Option, Options: jsonOrNull(v.Options),
		Height: v.Height, TxIndex: v.TxIndex, TxHash: v.TxHash, Time: v.Time.UTC(),
		Validator: v.ValidatorMoniker, ValidatorOperator: v.ValidatorOperator, VotingPowerPct: v.VotingPowerPct,
	}
}

// NewProposalDeposit maps a proposal_deposits row.
func NewProposalDeposit(d repository.ProposalDeposit) ProposalDeposit {
	return ProposalDeposit{
		Depositor: d.Depositor, DepositorLabel: newLabel(d.DepositorLabel), Amount: jsonOrNull(d.Amount),
		Height: d.Height, TxIndex: d.TxIndex, TxHash: d.TxHash, Time: d.Time.UTC(),
	}
}

// TurnoutPct is (yes + no + abstain + no_with_veto) / bonded in percent,
// rounded half up to five decimals; nil when bonded is unknown or zero or a
// count is not an integer.
func TurnoutPct(t *Tally, bonded *string) *string {
	if t == nil || bonded == nil {
		return nil
	}
	den, ok := new(big.Int).SetString(*bonded, 10)
	if !ok || den.Sign() <= 0 {
		return nil
	}
	sum := new(big.Int)
	for _, s := range []string{t.Yes, t.No, t.Abstain, t.NoWithVeto} {
		n, ok := new(big.Int).SetString(s, 10)
		if !ok || n.Sign() < 0 {
			return nil
		}
		sum.Add(sum, n)
	}
	// round(sum * 100 * 10^5 / den), half up: (2·num + den) / (2·den).
	num := new(big.Int).Mul(sum, big.NewInt(10_000_000))
	num.Mul(num, big.NewInt(2)).Add(num, den)
	q := num.Quo(num, new(big.Int).Mul(den, big.NewInt(2))).String()
	if len(q) <= 5 {
		q = strings.Repeat("0", 6-len(q)) + q
	}
	out := q[:len(q)-5] + "." + q[len(q)-5:]
	return &out
}

// textCursor is the JSON of a cursor whose key holds an address.
type textCursor struct {
	Height  int64  `json:"h"`
	TxIndex int64  `json:"i"`
	Address string `json:"a"`
}

func encodeTextCursor(c textCursor) string {
	raw, _ := json.Marshal(c) // ints and a string always marshal
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeTextCursor(cursor string) (textCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || !utf8.Valid(raw) {
		return textCursor{}, ErrInvalidCursor
	}
	var c textCursor
	if err := json.Unmarshal(raw, &c); err != nil || c.Address == "" || c.Height < 0 || c.TxIndex < 0 {
		return textCursor{}, ErrInvalidCursor
	}
	return c, nil
}

// EncodeVoteCursor is the cursor after a vote: base64 (URL alphabet, no
// padding) of its keyset position, which holds the voter's address.
func EncodeVoteCursor(k repository.VoteKey) string {
	return encodeTextCursor(textCursor{Height: k.Height, TxIndex: k.TxIndex, Address: k.Voter})
}

// DecodeVoteCursor reverses EncodeVoteCursor.
func DecodeVoteCursor(cursor string) (*repository.VoteKey, error) {
	c, err := decodeTextCursor(cursor)
	if err != nil {
		return nil, err
	}
	return &repository.VoteKey{Height: c.Height, TxIndex: c.TxIndex, Voter: c.Address}, nil
}

// EncodeDepositCursor is the cursor after a deposit.
func EncodeDepositCursor(k repository.DepositKey) string {
	return encodeTextCursor(textCursor{Height: k.Height, TxIndex: k.TxIndex, Address: k.Depositor})
}

// DecodeDepositCursor reverses EncodeDepositCursor.
func DecodeDepositCursor(cursor string) (*repository.DepositKey, error) {
	c, err := decodeTextCursor(cursor)
	if err != nil {
		return nil, err
	}
	return &repository.DepositKey{Height: c.Height, TxIndex: c.TxIndex, Depositor: c.Address}, nil
}
