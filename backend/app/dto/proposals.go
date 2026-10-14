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
// its bonded tokens. TallySource says where the bonded tokens come from:
// "sync" (read while the proposal voted), "archive" (read by the snapshots
// command from the state at the voting end: historical), null without them.
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
	TallySource     *string    `json:"tally_source"`
}

// ValidatorsVoted is how many validators have an owner that voted, whatever
// their status today, and, while the proposal is in its voting period, how
// many validators are active (null once voting ended: today's active set is
// not the one that could vote then).
type ValidatorsVoted struct {
	Voted int  `json:"voted"`
	Total *int `json:"total"`
}

// Proposal is GET /api/v1/proposals/{id}. Messages are the proposal's
// messages as proto JSON (a legacy content inside a MsgExecLegacyContent);
// TotalDeposit is a coin list.
type Proposal struct {
	ProposalSummary
	Summary           *string          `json:"summary"`
	Metadata          *string          `json:"metadata"`
	Proposer          *string          `json:"proposer"`
	ProposerLabel     *Label           `json:"proposer_label"`
	MessageTypes      []string         `json:"message_types"`
	Messages          json.RawMessage  `json:"messages"`
	TotalDeposit      json.RawMessage  `json:"total_deposit"`
	TallyBondedTokens *string          `json:"tally_bonded_tokens"`
	TallyUpdatedAt    *time.Time       `json:"tally_updated_at"`
	SubmitHeight      *int64           `json:"submit_height"`
	SubmitTxHash      *string          `json:"submit_tx_hash"`
	ResolvedHeight    *int64           `json:"resolved_height"`
	ValidatorsVoted   ValidatorsVoted  `json:"validators_voted"`
	VotesCount        int              `json:"votes_count"`
	ActiveSet         *ActiveSet       `json:"active_set"`
	Upgrade           *ProposalUpgrade `json:"upgrade"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

// ActiveSet is the validator set a closed proposal's tally counted: the
// CometBFT set at Height (the validators jailed in the tally's block left
// out), Size validators of which NonVotersCount did not vote. Null until
// the state sync recorded it.
type ActiveSet struct {
	Height         int64 `json:"height"`
	Size           int   `json:"size"`
	NonVotersCount int   `json:"non_voters_count"`
}

// ProposalNonVoter is an item of GET /api/v1/proposals/{id}/non-voters: a
// validator of the tally's set that did not vote. OperatorAddress and
// Moniker are null when no validator is known with the consensus address;
// VotingPower is its consensus power then, PowerPct its share of the set's
// power in percent with five decimals.
type ProposalNonVoter struct {
	OperatorAddress  *string `json:"operator_address"`
	ConsensusAddress string  `json:"consensus_address"`
	Moniker          *string `json:"moniker"`
	VotingPower      int64   `json:"voting_power"`
	PowerPct         string  `json:"power_pct"`
}

// ProposalVote is an item of GET /api/v1/proposals/{id}/votes. Option is
// null for a split vote (Options holds the weights). Validator,
// ValidatorOperator and VotingPowerPct name the validator the voter owns,
// with its current share; null for other voters. Source is "event", or
// "archive" for a vote read from the state before the tally: historical, no
// transaction (tx_index -1, tx_hash null).
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
	Source            string          `json:"source"`
}

// ProposalDeposit is an item of GET /api/v1/proposals/{id}/deposits. Source
// is "event", or "archive" for a depositor's total read from the state
// before the tally: historical, no transaction (tx_index -1, tx_hash null).
type ProposalDeposit struct {
	Depositor      string          `json:"depositor"`
	DepositorLabel *Label          `json:"depositor_label"`
	Amount         json.RawMessage `json:"amount"`
	Height         int64           `json:"height"`
	TxIndex        int64           `json:"tx_index"`
	TxHash         *string         `json:"tx_hash"`
	Time           time.Time       `json:"time"`
	Source         string          `json:"source"`
}

// NewProposalSummary maps a proposals list row.
func NewProposalSummary(p repository.ProposalSummary) ProposalSummary {
	out := ProposalSummary{
		ID: p.ID, Title: p.Title, Kind: p.Kind, Status: p.Status, Expedited: p.Expedited,
		SubmitTime: p.SubmitTime.UTC(), DepositEndTime: utc(p.DepositEndTime), VotingStartTime: utc(p.VotingStartTime),
		VotingEndTime: utc(p.VotingEndTime), TallySource: p.TallySource,
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
		VotesCount:      p.VotesCount,
		ActiveSet:       newActiveSet(p.ActiveSet),
		Upgrade:         NewProposalUpgrade(p.Upgrade),
		UpdatedAt:       p.UpdatedAt.UTC(),
	}
}

func newActiveSet(s *repository.ActiveSet) *ActiveSet {
	if s == nil {
		return nil
	}
	return &ActiveSet{Height: s.Height, Size: s.Size, NonVotersCount: s.NonVotersCount}
}

// NewProposalNonVoter maps a proposal_non_voters row.
func NewProposalNonVoter(n repository.ProposalNonVoter) ProposalNonVoter {
	return ProposalNonVoter{
		OperatorAddress: n.OperatorAddress, ConsensusAddress: n.ConsensusAddress, Moniker: n.Moniker,
		VotingPower: n.VotingPower, PowerPct: n.PowerPct,
	}
}

// nonVoterCursor is the JSON of a non-voters cursor.
type nonVoterCursor struct {
	Power int64  `json:"p"`
	Cons  string `json:"c"`
}

// EncodeNonVoterCursor is the cursor after a non-voter: base64 (URL
// alphabet, no padding) of its keyset position.
func EncodeNonVoterCursor(k repository.NonVoterKey) string {
	raw, _ := json.Marshal(nonVoterCursor{Power: k.VotingPower, Cons: k.ConsensusAddress}) // an int and a string always marshal
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeNonVoterCursor reverses EncodeNonVoterCursor.
func DecodeNonVoterCursor(cursor string) (*repository.NonVoterKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || !utf8.Valid(raw) {
		return nil, ErrInvalidCursor
	}
	var c nonVoterCursor
	if err := json.Unmarshal(raw, &c); err != nil || c.Cons == "" || c.Power < 0 {
		return nil, ErrInvalidCursor
	}
	return &repository.NonVoterKey{VotingPower: c.Power, ConsensusAddress: c.Cons}, nil
}

// NewProposalVote maps a proposal_votes row.
func NewProposalVote(v repository.ProposalVote) ProposalVote {
	return ProposalVote{
		Voter: v.Voter, VoterLabel: newLabel(v.VoterLabel), Option: v.Option, Options: jsonOrNull(v.Options),
		Height: v.Height, TxIndex: v.TxIndex, TxHash: v.TxHash, Time: v.Time.UTC(),
		Validator: v.ValidatorMoniker, ValidatorOperator: v.ValidatorOperator, VotingPowerPct: v.VotingPowerPct,
		Source: v.Source,
	}
}

// NewProposalDeposit maps a proposal_deposits row.
func NewProposalDeposit(d repository.ProposalDeposit) ProposalDeposit {
	return ProposalDeposit{
		Depositor: d.Depositor, DepositorLabel: newLabel(d.DepositorLabel), Amount: jsonOrNull(d.Amount),
		Height: d.Height, TxIndex: d.TxIndex, TxHash: d.TxHash, Time: d.Time.UTC(), Source: d.Source,
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
	out := pct(sum, den)
	return &out
}

// pct is part / whole in percent, rounded half up to five decimals; whole
// is positive, part non-negative.
func pct(part, whole *big.Int) string {
	// round(part * 100 * 10^5 / whole), half up: (2·num + whole) / (2·whole).
	num := new(big.Int).Mul(part, big.NewInt(10_000_000))
	num.Mul(num, big.NewInt(2)).Add(num, whole)
	q := num.Quo(num, new(big.Int).Mul(whole, big.NewInt(2))).String()
	if len(q) <= 5 {
		q = strings.Repeat("0", 6-len(q)) + q
	}
	return q[:len(q)-5] + "." + q[len(q)-5:]
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
	// tx_index -1: an archive row, which has no transaction.
	if err := json.Unmarshal(raw, &c); err != nil || c.Address == "" || c.Height < 0 || c.TxIndex < -1 {
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
