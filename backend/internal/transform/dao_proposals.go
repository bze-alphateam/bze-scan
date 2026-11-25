package transform

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// msgSubmitDaoProposal is the message that submits a DAO proposal (chain
// v8.3.0, x/dao); its body holds the title, summary and msgs the event lacks.
const msgSubmitDaoProposal = "/bze.dao.MsgSubmitProposal"

// The x/dao proposal events.
const (
	evDaoProposalSubmitted = daoEventPrefix + "EventProposalSubmitted"
	evDaoVote              = daoEventPrefix + "EventVote"
	evDaoBallotReduced     = daoEventPrefix + "EventBallotReduced"
	evDaoProposalCancelled = daoEventPrefix + "EventProposalCancelled"
	evDaoDepositReleased   = daoEventPrefix + "EventDepositReleased"
	evDaoProposalSettled   = daoEventPrefix + "EventProposalSettled"
	evDaoProposalExecuted  = daoEventPrefix + "EventProposalExecuted"
	evDaoProposalCleaned   = daoEventPrefix + "EventProposalCleaned"
)

// DAO proposal statuses, as the dao_proposals.status column stores them (the
// chain's ProposalStatus names in lower case).
const (
	DaoProposalVoting          = "voting"
	DaoProposalRejected        = "rejected"
	DaoProposalExecuted        = "executed"
	DaoProposalExecutionFailed = "execution_failed"
	DaoProposalExpired         = "expired"
	DaoProposalCancelled       = "cancelled"
)

// daoProposalStatuses are the chain's ProposalStatus values by number.
var daoProposalStatuses = []string{
	DaoProposalVoting, DaoProposalRejected, DaoProposalExecuted, DaoProposalExecutionFailed,
	DaoProposalExpired, DaoProposalCancelled,
}

// daoVoteOptions are the chain's VoteOption values by number.
var daoVoteOptions = []string{"", "yes", "no", "abstain"}

// DaoProposal is the explorer.dao_proposals row an EventProposalSubmitted
// writes: what only the submission knows.
type DaoProposal struct {
	ID          int64
	DaoID       int64
	Proposer    string
	Expedited   bool
	VotingEnd   time.Time
	Denominator string
	// HasBody is set when the MsgSubmitProposal body was read: Title,
	// Summary and Msgs are then the proposal's. A submission the explorer
	// cannot read (nested in authz, an admin execution or another DAO's
	// proposal) leaves them to the state sync.
	HasBody bool
	Title   string
	Summary string
	// Msgs is the body's msgs as proto JSON (a JSON array).
	Msgs   json.RawMessage
	Height int64
	Time   time.Time
	// TxHash is empty at block level.
	TxHash string
}

// DaoProposalOutcome is a status change of a DAO proposal: a settle (first
// settle, retry or expiry) or a cancel.
type DaoProposalOutcome struct {
	ID     int64
	Status string
	Height int64
	// TxIndex and MsgIndex are -1 at block level; Seq is the event's
	// position among the transaction's (or the block's) events.
	TxIndex  int
	MsgIndex int
	Seq      int
	TxHash   string
	Time     time.Time
	// Cancelled is a cancel; the other fields below are then empty.
	Cancelled bool
	// Yes, No, Abstain and Denominator are the settle's tally.
	Yes, No, Abstain, Denominator string
	ExpeditedEarly                bool
	// Executed is the outcome of the messages' run in this settle; nil when
	// the settle ran nothing (rejected, expired).
	Executed       *bool
	ExecutionError string
}

// DaoProposalDeposit is the release of a proposal's deposit.
type DaoProposalDeposit struct {
	ID          int64
	Refunded    bool
	Destination string
	Amount      string
	Height      int64
}

// DaoProposalCleaned is the cleanup that removed a proposal from the chain.
type DaoProposalCleaned struct {
	ID     int64
	Height int64
}

// DaoVote is one explorer.dao_votes row: a ballot, replacing the voter's
// earlier one.
type DaoVote struct {
	ProposalID int64
	Voter      string
	// Option is yes, no or abstain.
	Option   string
	Weight   string
	Height   int64
	TxIndex  int
	MsgIndex int
	Seq      int
	TxHash   string
	Time     time.Time
}

// DaoBallotReduction is one explorer.dao_ballot_reductions row.
type DaoBallotReduction struct {
	Height     int64
	TxIndex    int
	MsgIndex   int
	Seq        int
	ProposalID int64
	Voter      string
	Source     string
	Amount     string
	TxHash     string
}

// daoProposalEvents is every x/dao proposal event.
var daoProposalEvents = map[string]bool{
	evDaoProposalSubmitted: true, evDaoVote: true, evDaoBallotReduced: true, evDaoProposalCancelled: true,
	evDaoDepositReleased: true, evDaoProposalSettled: true, evDaoProposalExecuted: true, evDaoProposalCleaned: true,
}

// daoProposalPos is where an event sits.
type daoProposalPos struct {
	height            int64
	txIndex, msgIndex int
	seq               int
	txHash            string
	time              time.Time
}

// txDaoProposalEvents appends the DAO proposal rows of a successful
// transaction. Every event of the transaction counts, also the ones a
// proposal's execution, an authz MsgExec or an admin execution emitted
// under the outer message's index.
func txDaoProposalEvents(ents *Entities, b Block, txIndex int, txHash string, msgs []chain.Msg, events []node.Event) {
	daoProposalEventsAt(ents, b, txIndex, txHash, msgs, events)
}

// blockDaoProposalEvents appends the DAO proposal rows of the block's
// finalize events (governance running a DAO message in its EndBlocker).
func blockDaoProposalEvents(ents *Entities, b Block, events []node.Event) {
	daoProposalEventsAt(ents, b, -1, "", nil, events)
}

func daoProposalEventsAt(ents *Entities, b Block, txIndex int, txHash string, msgs []chain.Msg, events []node.Event) {
	reductionSeqs := map[int]int{}
	// executed holds the EventProposalExecuted of a settle until its
	// EventProposalSettled (emitted after it, by the same message).
	type runKey struct {
		msgIndex int
		id       int64
	}
	executed := map[runKey]DaoProposalOutcome{}
	for i, ev := range events {
		if !daoProposalEvents[ev.Type] {
			continue
		}
		pos := daoProposalPos{height: b.Height, txIndex: txIndex, msgIndex: -1, seq: i, txHash: txHash, time: b.Time}
		if txIndex >= 0 {
			if v, ok := ev.Get("msg_index"); ok {
				if j, err := strconv.Atoi(v); err == nil {
					pos.msgIndex = j
				}
			}
		}
		attrs := storedEvent(ev).Attrs
		idKey := "id"
		if ev.Type == evDaoVote || ev.Type == evDaoBallotReduced {
			idKey = "proposal_id"
		}
		id, err := int64Attr(attrs[idKey])
		if err != nil || id <= 0 {
			continue
		}
		ents.Dirty.Mark(statesync.DaoProposals, strconv.FormatInt(id, 10))

		switch ev.Type {
		case evDaoProposalSubmitted:
			ents.DaoProposals = append(ents.DaoProposals, daoSubmitted(id, attrs, pos, msgs))
		case evDaoVote:
			voter := anyString(attrs["voter"])
			if voter == "" {
				continue
			}
			ents.DaoVotes = append(ents.DaoVotes, DaoVote{
				ProposalID: id, Voter: voter, Option: enumName(attrs["option"], daoVoteOptions),
				Weight: intString(attrs["weight"]), Height: pos.height, TxIndex: pos.txIndex, MsgIndex: pos.msgIndex,
				Seq: pos.seq, TxHash: txHash, Time: b.Time,
			})
		case evDaoBallotReduced:
			ents.DaoBallotReductions = append(ents.DaoBallotReductions, DaoBallotReduction{
				Height: pos.height, TxIndex: pos.txIndex, MsgIndex: pos.msgIndex, Seq: reductionSeqs[pos.msgIndex],
				ProposalID: id, Voter: anyString(attrs["voter"]), Source: anyString(attrs["source"]),
				Amount: intString(attrs["amount"]), TxHash: txHash,
			})
			reductionSeqs[pos.msgIndex]++
		case evDaoProposalCancelled:
			ents.DaoProposalOutcomes = append(ents.DaoProposalOutcomes, outcomeAt(id, pos, DaoProposalCancelled, true))
		case evDaoDepositReleased:
			ents.DaoProposalDeposits = append(ents.DaoProposalDeposits, DaoProposalDeposit{
				ID: id, Refunded: attrs["refunded"] == true, Destination: anyString(attrs["destination"]),
				Amount: anyString(attrs["amount"]), Height: pos.height,
			})
		case evDaoProposalExecuted:
			ok := attrs["success"] == true
			executed[runKey{pos.msgIndex, id}] = DaoProposalOutcome{Executed: &ok, ExecutionError: anyString(attrs["error"])}
		case evDaoProposalSettled:
			o := outcomeAt(id, pos, enumName(attrs["status"], daoProposalStatuses), false)
			o.Yes, o.No, o.Abstain = intString(attrs["yes"]), intString(attrs["no"]), intString(attrs["abstain"])
			o.Denominator, o.ExpeditedEarly = intString(attrs["denominator"]), attrs["expedited_early"] == true
			if run, ok := executed[runKey{pos.msgIndex, id}]; ok {
				o.Executed, o.ExecutionError = run.Executed, run.ExecutionError
				delete(executed, runKey{pos.msgIndex, id})
			}
			ents.DaoProposalOutcomes = append(ents.DaoProposalOutcomes, o)
		case evDaoProposalCleaned:
			if attrs["completed"] == true {
				ents.DaoProposalsCleaned = append(ents.DaoProposalsCleaned, DaoProposalCleaned{ID: id, Height: pos.height})
			}
		}
	}
}

func outcomeAt(id int64, pos daoProposalPos, status string, cancelled bool) DaoProposalOutcome {
	return DaoProposalOutcome{
		ID: id, Status: status, Height: pos.height, TxIndex: pos.txIndex, MsgIndex: pos.msgIndex, Seq: pos.seq,
		TxHash: pos.txHash, Time: pos.time, Cancelled: cancelled,
	}
}

// daoSubmitted builds the row of an EventProposalSubmitted, with the body of
// the MsgSubmitProposal that emitted it when that message is the
// transaction's own.
func daoSubmitted(id int64, attrs map[string]any, pos daoProposalPos, msgs []chain.Msg) DaoProposal {
	daoID, _ := int64Attr(attrs["dao_id"])
	p := DaoProposal{
		ID: id, DaoID: daoID, Proposer: anyString(attrs["proposer"]), Expedited: attrs["expedited"] == true,
		Denominator: intString(attrs["denominator"]), Height: pos.height, Time: pos.time, TxHash: pos.txHash,
	}
	if end, err := int64Attr(attrs["voting_end"]); err == nil && end > 0 {
		p.VotingEnd = time.Unix(end, 0).UTC()
	}
	if pos.msgIndex < 0 || pos.msgIndex >= len(msgs) || msgs[pos.msgIndex].TypeURL != msgSubmitDaoProposal {
		return p
	}
	var body struct {
		DaoID   json.Number     `json:"dao_id"`
		Title   string          `json:"title"`
		Summary string          `json:"summary"`
		Msgs    json.RawMessage `json:"msgs"`
	}
	if json.Unmarshal(msgs[pos.msgIndex].Body, &body) != nil || body.DaoID.String() != strconv.FormatInt(daoID, 10) {
		return p
	}
	p.HasBody, p.Title, p.Summary, p.Msgs = true, body.Title, body.Summary, body.Msgs
	if len(p.Msgs) == 0 || string(p.Msgs) == "null" {
		p.Msgs = json.RawMessage(`[]`)
	}
	return p
}

// enumName reads a proto enum the typed event wrote by name (a number is
// taken too) as the lower-case name of values; "" when unknown.
func enumName(v any, values []string) string {
	var n int
	switch x := v.(type) {
	case string:
		if i, err := strconv.Atoi(x); err == nil {
			n = i
			break
		}
		name := strings.ToLower(x)
		for _, val := range values {
			if val != "" && val == name {
				return val
			}
		}
		return ""
	case json.Number:
		i, err := x.Int64()
		if err != nil {
			return ""
		}
		n = int(i)
	case nil:
		n = 0 // proto JSON may omit the zero value
	default:
		return ""
	}
	if n < 0 || n >= len(values) {
		return ""
	}
	return values[n]
}

// intString reads an Int attribute (a quoted string, or a number); "0" when
// missing.
func intString(v any) string {
	switch x := v.(type) {
	case string:
		if x != "" {
			return x
		}
	case json.Number:
		return x.String()
	}
	return "0"
}
