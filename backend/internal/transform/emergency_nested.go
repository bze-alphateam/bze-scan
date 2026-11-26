package transform

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/emergency"
)

// The ways a nested emergency action reached the chain.
const (
	// ViaAuthz is an emergency message an authz MsgExec ran: the guardian
	// granted it to the MsgExec's signer.
	ViaAuthz = "authz"
	// ViaDao is an emergency message a DAO ran: its admin's MsgAdminExecute
	// or one of its executed proposals.
	ViaDao = "dao"
)

// NestedEmergencyAction is one explorer.emergency_nested_actions row: an
// emergency message (see emergency.MessageAction) run inside another message,
// so not a transaction message of its own. Its position is the wrapper's
// transaction and message plus Seq, the inner message's place among the
// messages the wrapper ran (unwrapped like innerMsgs and msgsOf do), so a
// reindex numbers it the same.
type NestedEmergencyAction struct {
	Height   int64
	TxIndex  int
	MsgIndex int
	Seq      int
	TxHash   string
	Time     time.Time
	// TypeURL and Body are the inner message's; Signer is its signer or
	// authority field (the guardian), not the wrapper's signer.
	TypeURL string
	Signer  string
	Body    json.RawMessage
	// Via is ViaAuthz or ViaDao: how the outermost wrapper ran it. A wrapper
	// inside a wrapper (a MsgExec inside a DAO proposal, a MsgAdminExecute
	// inside a MsgExec) is named by the outermost one.
	Via string
	// Grantee is the MsgExec's signer (ViaAuthz).
	Grantee string
	// DaoID is the DAO (ViaDao); ProposalID its executed proposal, 0 for a
	// MsgAdminExecute.
	DaoID      int64
	ProposalID int64
}

// txNestedEmergency appends the nested emergency actions of the messages of
// a successful transaction: those its MsgExec and MsgAdminExecute messages
// ran. A settle's are the PostFlush hook's (see SettledEmergencyActions).
func txNestedEmergency(ents *Entities, b Block, txIndex int, txHash string, msgs []chain.Msg) {
	for j, m := range msgs {
		inner := innerMsgs(m)
		if len(inner) == 0 {
			continue
		}
		base := NestedEmergencyAction{Height: b.Height, TxIndex: txIndex, MsgIndex: j, TxHash: txHash, Time: b.Time}
		var body map[string]any
		_ = json.Unmarshal(m.Body, &body)
		if m.TypeURL == msgAuthzExec {
			base.Via, base.Grantee = ViaAuthz, m.Signer
			if base.Grantee == "" {
				base.Grantee = str(body, "grantee")
			}
		} else {
			base.Via = ViaDao
			base.DaoID, _ = strconv.ParseInt(str(body, "dao_id"), 10, 64)
		}
		ents.NestedEmergencyActions = append(ents.NestedEmergencyActions, nestedEmergency(base, inner, 0)...)
	}
}

// SettledProposal is a DAO proposal a settle executed: the settle's
// transaction and message, and the proposal's rank among the proposals that
// transaction executed (by id, from 0).
type SettledProposal struct {
	ID       int64
	DaoID    int64
	Height   int64
	TxIndex  int
	MsgIndex int
	TxHash   string
	Time     time.Time
	Ordinal  int
}

// SettledEmergencyActions are the nested emergency actions of an executed
// DAO proposal: the emergency messages among msgs, its stored msgs (the
// MsgSubmitProposal body's, wrappers unwrapped as by NestedActions). They
// take the proposal's range of NestedSeqBase seqs, as NestedActions' rows do,
// so the proposals of one settle transaction keep distinct positions; the
// rows past the range are left out.
func SettledEmergencyActions(msgs json.RawMessage, p SettledProposal) []NestedEmergencyAction {
	base := NestedEmergencyAction{Height: p.Height, TxIndex: p.TxIndex, MsgIndex: p.MsgIndex, TxHash: p.TxHash,
		Time: p.Time, Via: ViaDao, DaoID: p.DaoID, ProposalID: p.ID}
	return nestedEmergency(base, msgsOf(msgs, 0), NestedSeqBase*(p.Ordinal+1))
}

// nestedEmergency returns a row of base for every emergency message of inner,
// numbered first + its index in inner. Indexes past NestedSeqBase are left
// out (the chain caps a wrapper's messages far below).
func nestedEmergency(base NestedEmergencyAction, inner []chain.Msg, first int) []NestedEmergencyAction {
	var out []NestedEmergencyAction
	for k, in := range inner {
		if k >= NestedSeqBase {
			break
		}
		if _, ok := emergency.MessageAction(in.TypeURL); !ok {
			continue
		}
		var who struct {
			Signer    string `json:"signer"`
			Authority string `json:"authority"`
		}
		_ = json.Unmarshal(in.Body, &who)
		r := base
		r.Seq, r.TypeURL, r.Body = first+k, in.TypeURL, in.Body
		r.Signer = who.Signer
		if r.Signer == "" {
			r.Signer = who.Authority
		}
		out = append(out, r)
	}
	return out
}
