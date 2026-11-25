package transform

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"
	tokenfactorytypes "github.com/bze-alphateam/bze/x/tokenfactory/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// Messages that run other messages. Their inner messages leave their events
// under the wrapper's msg_index; their bodies are in the wrapper's body
// (authz MsgExec, dao MsgAdminExecute) or, for a settle, in the proposal the
// settle executes (explorer.dao_proposals.msgs).
const (
	msgAuthzExec       = "/cosmos.authz.v1beta1.MsgExec"
	msgDaoAdminExecute = "/bze.dao.MsgAdminExecute"
	msgDaoSettle       = "/bze.dao.MsgSettleProposal"
)

// maxNestedDepth bounds the unwrapping of wrappers inside wrappers (an authz
// exec inside a DAO proposal, a parent DAO's admin execution for its
// sub-DAO). The chain allows far fewer levels.
const maxNestedDepth = 16

// tokenFactoryAddress is the tokenfactory module account: the minter of a
// MsgMint's coinbase and the burner of a MsgBurn's burn.
var tokenFactoryAddress = chain.ModuleAddress(tokenfactorytypes.ModuleName)

// innerMsgs returns the messages a wrapper message (authz MsgExec, dao
// MsgAdminExecute) runs, wrappers among them unwrapped in turn, in execution
// order; nil for any other message. A settle's messages are not in its body:
// see NestedActions.
func innerMsgs(m chain.Msg) []chain.Msg {
	if m.TypeURL != msgAuthzExec && m.TypeURL != msgDaoAdminExecute {
		return nil
	}
	var body struct {
		Msgs json.RawMessage `json:"msgs"`
	}
	if json.Unmarshal(m.Body, &body) != nil {
		return nil
	}
	return msgsOf(body.Msgs, 0)
}

// msgsOf reads a JSON array of proto JSON messages (each with its "@type")
// into the messages it runs: the leaves, with every wrapper unwrapped.
func msgsOf(raw json.RawMessage, depth int) []chain.Msg {
	if depth >= maxNestedDepth || len(raw) == 0 {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var out []chain.Msg
	for _, item := range items {
		var head struct {
			Type string          `json:"@type"`
			Msgs json.RawMessage `json:"msgs"`
		}
		if json.Unmarshal(item, &head) != nil || head.Type == "" {
			continue
		}
		typeURL := chain.CanonicalTypeURL(head.Type)
		if typeURL == msgAuthzExec || typeURL == msgDaoAdminExecute {
			out = append(out, msgsOf(head.Msgs, depth+1)...)
			continue
		}
		out = append(out, chain.Msg{TypeURL: typeURL, Body: bytes.Clone(item)})
	}
	return out
}

// isWrapper reports whether m runs other messages.
func isWrapper(m chain.Msg) bool {
	return m.TypeURL == msgAuthzExec || m.TypeURL == msgDaoAdminExecute || m.TypeURL == msgDaoSettle
}

// wrapperActor is the account a wrapper's inner messages run as, when the
// wrapper's body names it: the DAO of a MsgAdminExecute. An authz MsgExec
// keeps its signer (the grantee), as before; a settle's DAO is not in its
// body, so the typed events' own attributes name the actor.
func wrapperActor(m chain.Msg, body map[string]any) string {
	switch m.TypeURL {
	case msgDaoAdminExecute:
		id, err := strconv.ParseUint(str(body, "dao_id"), 10, 64)
		if err != nil || id == 0 {
			return ""
		}
		return daotypes.DaoAddress(id).String()
	case msgDaoSettle:
		return ""
	default:
		return m.Signer
	}
}

// eventMintBurnRows appends the minted and burned rows of the tokenfactory
// mints and burns that one message's events record: a coinbase minted by the
// tokenfactory module, received by the account its next transfer from the
// module pays (the admin who minted), and a burn by the module, of the coins
// its previous transfer to the module brought (from the admin who burned).
// This is how a mint or a burn run inside a wrapper is seen: its body is not
// the transaction's message. One row per coin per event.
func eventMintBurnRows(rows *tokenRows, events []node.Event) error {
	moved, err := moves(rows.height, rows.txIndex, events, nil)
	if err != nil {
		return err
	}
	for i, t := range moved {
		switch {
		case t.Kind == TransferKindMint && t.Recipient == tokenFactoryAddress:
			to := ""
			for _, n := range moved[i+1:] {
				if n.Kind == TransferKindTransfer && n.Sender == tokenFactoryAddress && n.Denom == t.Denom {
					to = n.Recipient
					break
				}
			}
			var details map[string]any
			if to != "" {
				details = map[string]any{"recipient": to}
			}
			rows.add(t.Denom, TokenMinted, to, t.Amount, details)
		case t.Kind == TransferKindBurn && t.Sender == tokenFactoryAddress:
			from := ""
			for k := i - 1; k >= 0; k-- {
				p := moved[k]
				if p.Kind == TransferKindTransfer && p.Recipient == tokenFactoryAddress && p.Denom == t.Denom {
					from = p.Sender
					break
				}
			}
			rows.add(t.Denom, TokenBurned, from, t.Amount, nil)
		}
	}
	return nil
}

// NestedSeqBase is the first seq of the rows NestedActions derives: the
// token_events and validator_events rows of a settle's executed messages
// sit above the rows the transformer derives from the settle's transaction
// (whose events already give the mints and burns), and each proposal
// executed in that transaction has its own range of NestedSeqBase seqs.
const NestedSeqBase = 1000

// NestedActions are the token_events and validator_events rows of the
// messages a DAO proposal ran when a settle executed it: msgs is the
// proposal's stored msgs (the MsgSubmitProposal body's, proto JSON with
// "@type"), height, txIndex and time are the settle's transaction, and
// ordinal is the proposal's position among the proposals that transaction
// executed (by id, from 0), so that two settles of one transaction keep
// distinct seqs. The mints and burns are not here: the settle's events give
// them. The rows are those the same messages give at the top level, with
// the DAO as their actor. Dirty marks the denoms and validators they change
// (the token rows mark their denoms).
func NestedActions(msgs json.RawMessage, height int64, txIndex int, t time.Time, ordinal int) *Entities {
	ents := &Entities{}
	inner := msgsOf(msgs, 0)
	if len(inner) == 0 {
		return ents
	}
	first := NestedSeqBase * (ordinal + 1)
	rows := &tokenRows{ents: ents, height: height, txIndex: txIndex, time: t, seq: first}
	vals := &validatorRows{ents: ents, height: height, txIndex: txIndex, time: t, seq: first}
	for _, m := range inner {
		bodyTokenRows(rows, m, nil)
		bodyValidatorRows(vals, m, nil)
		markValidatorMsg(&ents.Dirty, m)
	}
	// The rows of one proposal stay in its range.
	ents.TokenEvents = keepBelow(ents.TokenEvents, first+NestedSeqBase, func(e TokenEvent) int { return e.Seq })
	ents.ValidatorEvents = keepBelow(ents.ValidatorEvents, first+NestedSeqBase, func(e ValidatorEvent) int { return e.Seq })
	return ents
}

func keepBelow[T any](rows []T, limit int, seq func(T) int) []T {
	out := rows[:0]
	for _, r := range rows {
		if seq(r) < limit {
			out = append(out, r)
		}
	}
	return out
}
