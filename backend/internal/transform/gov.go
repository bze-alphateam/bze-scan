package transform

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/proposals"
)

// Type URLs of the proposal submissions.
const (
	msgSubmitProposal       = "/cosmos.gov.v1.MsgSubmitProposal"
	msgSubmitProposalLegacy = "/cosmos.gov.v1beta1.MsgSubmitProposal"
	msgExecLegacyContent    = "/cosmos.gov.v1.MsgExecLegacyContent"
)

// Gov events (Cosmos SDK x/gov/types/events.go).
const (
	evSubmitProposal   = "submit_proposal"
	evProposalDeposit  = "proposal_deposit"
	evProposalVote     = "proposal_vote"
	evCancelProposal   = "cancel_proposal"
	evActiveProposal   = "active_proposal"
	evInactiveProposal = "inactive_proposal"

	resultPassed            = "proposal_passed"
	resultFailed            = "proposal_failed"
	resultExpeditedRejected = "expedited_proposal_rejected"
)

// ProposalSubmission is the explorer.proposals row of a MsgSubmitProposal,
// as its body and events describe it. The state sync completes it (times,
// deposits, tally).
type ProposalSubmission struct {
	ID           uint64
	Title        string
	Summary      string
	Metadata     string
	Proposer     string
	Expedited    bool
	Kind         string
	MessageTypes []string
	// Messages are the proposal's messages as proto JSON; a legacy
	// (v1beta1) content is wrapped in a MsgExecLegacyContent, as gov v1
	// queries show it.
	Messages json.RawMessage
	// Status is deposit_period, or voting_period when the initial deposit
	// opened the voting.
	Status string
	Height int64
	TxHash string
	Time   time.Time
}

// ProposalDeposit is one explorer.proposal_deposits row: a deposit,
// initial or not, of one depositor in one transaction.
type ProposalDeposit struct {
	ProposalID uint64
	Depositor  string
	Height     int64
	TxIndex    int
	Amount     []chain.Coin
	Time       time.Time
}

// ProposalVote is one explorer.proposal_votes row.
type ProposalVote struct {
	ProposalID uint64
	Voter      string
	// Options are the weighted options as the v0.50 proposal_vote event
	// encodes them: [{"option":1,"weight":"1.000000000000000000"}].
	Options json.RawMessage
	// Option is yes, no, abstain or no_with_veto when one option holds the
	// whole weight; empty for a split vote.
	Option  string
	Height  int64
	TxIndex int
	// MsgIndex orders the votes of one transaction.
	MsgIndex int
	Time     time.Time
}

// ProposalStatus is a status change events carry: the voting period
// opening, a resolution, a cancel, an expedited proposal turned regular.
type ProposalStatus struct {
	ProposalID uint64
	Status     string
	Height     int64
	// Resolved sets resolved_height to Height.
	Resolved bool
	// ExpeditedOff clears expedited: an expedited proposal that missed its
	// threshold votes again as a regular one.
	ExpeditedOff bool
}

// txGovEvents appends the governance rows of a successful transaction:
// submissions, deposits, votes and cancels. byIndex are its events by
// message.
func txGovEvents(ents *Entities, b Block, txIndex int, txHash string, msgs []chain.Msg, byIndex map[int][]node.Event) {
	for j, m := range msgs {
		events := byIndex[j]
		for _, ev := range events {
			id, ok := proposalID(ev)
			if !ok && ev.Type != evSubmitProposal && ev.Type != evProposalDeposit {
				continue
			}
			switch ev.Type {
			case evSubmitProposal, evProposalDeposit:
				if v, ok := ev.Get("voting_period_start"); ok {
					if vid, err := strconv.ParseUint(v, 10, 64); err == nil {
						ents.ProposalStatuses = append(ents.ProposalStatuses,
							ProposalStatus{ProposalID: vid, Status: proposals.StatusVoting, Height: b.Height})
						ents.Dirty.Mark(statesync.Proposals, proposals.Key(vid))
					}
				}
				if ev.Type == evProposalDeposit && ok {
					addDeposit(ents, b, txIndex, id, ev, m.Signer)
				}
			case evProposalVote:
				addVote(ents, b, txIndex, j, id, ev, m.Signer)
			case evCancelProposal:
				ents.ProposalStatuses = append(ents.ProposalStatuses,
					ProposalStatus{ProposalID: id, Status: proposals.StatusCanceled, Height: b.Height, Resolved: true})
				ents.Dirty.Mark(statesync.Proposals, proposals.Key(id))
			}
		}
		if m.TypeURL == msgSubmitProposal || m.TypeURL == msgSubmitProposalLegacy {
			if s, ok := submission(m, events); ok {
				s.Height, s.TxHash, s.Time = b.Height, txHash, b.Time
				ents.Proposals = append(ents.Proposals, s)
				ents.Dirty.Mark(statesync.Proposals, proposals.Key(s.ID))
			}
		}
	}
}

// blockGovEvents appends the resolutions of the block's finalize events.
func blockGovEvents(ents *Entities, b Block, events []node.Event) {
	for _, ev := range events {
		if ev.Type != evActiveProposal && ev.Type != evInactiveProposal {
			continue
		}
		id, ok := proposalID(ev)
		if !ok {
			continue
		}
		result, _ := ev.Get("proposal_result")
		st := ProposalStatus{ProposalID: id, Height: b.Height, Resolved: true}
		switch {
		case ev.Type == evInactiveProposal && result == resultFailed:
			st.Status = proposals.StatusFailed
		case ev.Type == evInactiveProposal:
			st.Status = proposals.StatusRejected
		case result == resultPassed:
			st.Status = proposals.StatusPassed
		case result == resultFailed:
			st.Status = proposals.StatusFailed
		case result == resultExpeditedRejected:
			st = ProposalStatus{ProposalID: id, Height: b.Height, Status: proposals.StatusVoting, ExpeditedOff: true}
		default:
			st.Status = proposals.StatusRejected
		}
		ents.ProposalStatuses = append(ents.ProposalStatuses, st)
		ents.Dirty.Mark(statesync.Proposals, proposals.Key(id))
	}
}

func proposalID(ev node.Event) (uint64, bool) {
	v, ok := ev.Get("proposal_id")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseUint(v, 10, 64)
	return id, err == nil
}

// submission is the proposals row of a submit message, false when its
// events name no proposal (an undecodable message still has them).
func submission(m chain.Msg, events []node.Event) (ProposalSubmission, bool) {
	var s ProposalSubmission
	found := false
	for _, ev := range events {
		if ev.Type != evSubmitProposal {
			continue
		}
		if id, ok := proposalID(ev); ok && !found {
			s.ID, found = id, true
			s.Proposer, _ = ev.Get("proposal_proposer")
			if s.Proposer == "" {
				s.Proposer, _ = ev.Get("proposer")
			}
		}
		if v, ok := ev.Get("voting_period_start"); ok && v == strconv.FormatUint(s.ID, 10) {
			s.Status = proposals.StatusVoting
		}
	}
	if !found {
		return s, false
	}
	if s.Status == "" {
		s.Status = proposals.StatusDeposit
	}
	var body struct {
		Messages  json.RawMessage `json:"messages"`
		Content   json.RawMessage `json:"content"`
		Metadata  string          `json:"metadata"`
		Title     string          `json:"title"`
		Summary   string          `json:"summary"`
		Proposer  string          `json:"proposer"`
		Expedited bool            `json:"expedited"`
	}
	_ = json.Unmarshal(m.Body, &body) // an undecodable body leaves the fields empty
	if s.Proposer == "" {
		s.Proposer = body.Proposer
	}
	if s.Proposer == "" {
		s.Proposer = m.Signer
	}
	s.Metadata, s.Title, s.Summary, s.Expedited, s.Messages = body.Metadata, body.Title, body.Summary, body.Expedited, body.Messages
	if len(body.Content) > 0 && string(body.Content) != "null" {
		var content struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		}
		_ = json.Unmarshal(body.Content, &content)
		s.Title, s.Summary = content.Title, content.Description
		wrapped, err := json.Marshal([]map[string]any{{
			"@type": msgExecLegacyContent, "content": body.Content, "authority": chain.ModuleAddress(chain.Gov),
		}})
		if err == nil {
			s.Messages = wrapped
		}
	}
	if string(s.Messages) == "null" {
		s.Messages = nil
	}
	types, contents := proposals.Messages(s.Messages)
	s.MessageTypes, s.Kind = types, proposals.Kind(types, contents)
	if s.MessageTypes == nil {
		s.MessageTypes = []string{}
	}
	return s, true
}

// addDeposit appends the deposit a proposal_deposit event describes. Two
// deposits of one depositor in one transaction are one row.
func addDeposit(ents *Entities, b Block, txIndex int, id uint64, ev node.Event, signer string) {
	depositor, _ := ev.Get("depositor")
	if depositor == "" {
		depositor = signer
	}
	raw, _ := ev.Get("amount")
	coins, err := chain.ParseCoins(raw)
	if err != nil || len(coins) == 0 || depositor == "" {
		return
	}
	ents.Dirty.Mark(statesync.Proposals, proposals.Key(id))
	for i := range ents.ProposalDeposits {
		d := &ents.ProposalDeposits[i]
		if d.ProposalID == id && d.Depositor == depositor && d.Height == b.Height && d.TxIndex == txIndex {
			d.Amount = addCoins(d.Amount, coins)
			return
		}
	}
	ents.ProposalDeposits = append(ents.ProposalDeposits, ProposalDeposit{
		ProposalID: id, Depositor: depositor, Height: b.Height, TxIndex: txIndex, Amount: coins, Time: b.Time,
	})
}

func addCoins(a, b []chain.Coin) []chain.Coin {
	out := append([]chain.Coin(nil), a...)
	for _, c := range b {
		merged := false
		for i := range out {
			if out[i].Denom != c.Denom {
				continue
			}
			x, ok1 := sdkmath.NewIntFromString(out[i].Amount)
			y, ok2 := sdkmath.NewIntFromString(c.Amount)
			if ok1 && ok2 {
				out[i].Amount = x.Add(y).String()
				merged = true
			}
			break
		}
		if !merged {
			out = append(out, c)
		}
	}
	return out
}

// addVote appends the vote a proposal_vote event describes. The SDK before
// v0.47 named no voter (the archive adapter fills it); the signer stands in
// when it is still missing.
func addVote(ents *Entities, b Block, txIndex, msgIndex int, id uint64, ev node.Event, signer string) {
	voter, _ := ev.Get("voter")
	if voter == "" {
		voter = signer
	}
	raw, _ := ev.Get("option")
	options, option, err := VoteOptions(raw)
	if voter == "" || err != nil {
		return
	}
	ents.ProposalVotes = append(ents.ProposalVotes, ProposalVote{
		ProposalID: id, Voter: voter, Options: options, Option: option,
		Height: b.Height, TxIndex: txIndex, MsgIndex: msgIndex, Time: b.Time,
	})
	ents.Dirty.Mark(statesync.Proposals, proposals.Key(id))
}

// weightedOption is one option of a vote in the v0.50 event encoding.
type weightedOption struct {
	Option govv1.VoteOption `json:"option"`
	Weight string           `json:"weight"`
}

// legacyOption matches one option of the pre-v0.47 encoding
// (option: VOTE_OPTION_YES\nweight: "1.000000000000000000").
var legacyOption = regexp.MustCompile(`option:\s*"?(VOTE_OPTION_[A-Z_]+)"?\s*weight:\s*"?([0-9.]+)"?`)

// VoteOptions reads a proposal_vote event's option attribute (the v0.50
// JSON array, the pre-v0.47 text, or a bare option name) into the v0.50 JSON
// encoding, and the single option holding the whole weight (yes, no,
// abstain, no_with_veto; "" for a split vote).
func VoteOptions(raw string) (json.RawMessage, string, error) {
	var opts []weightedOption
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		var generic []map[string]any
		if json.Unmarshal([]byte(raw), &generic) == nil {
			opts = nil
			for _, g := range generic {
				opts = append(opts, weightedOption{Option: optionOf(fmt.Sprint(g["option"])), Weight: fmt.Sprint(g["weight"])})
			}
		} else {
			for _, m := range legacyOption.FindAllStringSubmatch(raw, -1) {
				opts = append(opts, weightedOption{Option: optionOf(m[1]), Weight: m[2]})
			}
			if len(opts) == 0 && optionOf(strings.TrimSpace(raw)) != govv1.OptionEmpty {
				opts = []weightedOption{{Option: optionOf(strings.TrimSpace(raw)), Weight: sdkmath.LegacyOneDec().String()}}
			}
		}
	}
	if len(opts) == 0 {
		return nil, "", fmt.Errorf("vote option %q is not readable", raw)
	}
	out, err := json.Marshal(opts)
	if err != nil {
		return nil, "", err
	}
	single := ""
	if len(opts) == 1 {
		if w, err := sdkmath.LegacyNewDecFromStr(opts[0].Weight); err == nil && w.Equal(sdkmath.LegacyOneDec()) {
			single = optionName(opts[0].Option)
		}
	}
	return out, single, nil
}

// optionOf reads an option given by number or by name.
func optionOf(s string) govv1.VoteOption {
	if n, err := strconv.Atoi(s); err == nil {
		return govv1.VoteOption(n) //nolint:gosec // vote options are 0..4
	}
	if o, err := govv1.VoteOptionFromString(s); err == nil {
		return o
	}
	return govv1.OptionEmpty
}

func optionName(o govv1.VoteOption) string {
	switch o {
	case govv1.OptionYes:
		return "yes"
	case govv1.OptionNo:
		return "no"
	case govv1.OptionAbstain:
		return "abstain"
	case govv1.OptionNoWithVeto:
		return "no_with_veto"
	default:
		return ""
	}
}
