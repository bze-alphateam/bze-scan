package dto

import (
	"encoding/json"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/emergency"
)

// EmergencyDenom is a denom named in an emergency list, with what the
// denoms table knows of it (null symbol and exponent 0 when it does not).
type EmergencyDenom struct {
	Denom    string  `json:"denom"`
	Symbol   *string `json:"symbol"`
	Exponent int     `json:"exponent"`
	LogoURL  *string `json:"logo_url"`
}

// EmergencyChannel is a closed transfer channel with its counterparty chain
// (null when the explorer does not know it).
type EmergencyChannel struct {
	ChannelID             string  `json:"channel_id"`
	CounterpartyChainID   *string `json:"counterparty_chain_id"`
	CounterpartyChainName *string `json:"counterparty_chain_name"`
}

// EmergencyGuardian is the guardian address with its label and the label's
// kind (both null when it has none; kind `dao` for a DAO).
type EmergencyGuardian struct {
	Address   string  `json:"address"`
	Label     *string `json:"label"`
	LabelKind *string `json:"label_kind"`
}

// EmergencyPlan is the scheduled upgrade plan; Emergency marks the plan of
// an emergency halt.
type EmergencyPlan struct {
	Name      string `json:"name"`
	Height    int64  `json:"height"`
	Info      string `json:"info"`
	Emergency bool   `json:"emergency"`
}

// Emergency is GET /api/v1/emergency: every emergency switch at Height.
// Supported is false on a node from before chain v8.3.0 (every list is
// then empty). Active is true when any list is non-empty, automation is
// paused or an emergency halt is scheduled. Guardian is null when no
// guardian is set.
type Emergency struct {
	Supported        bool               `json:"supported"`
	Height           int64              `json:"height"`
	Active           bool               `json:"active"`
	Guardian         *EmergencyGuardian `json:"guardian"`
	AutomationPaused bool               `json:"automation_paused"`
	ClosedChannels   []EmergencyChannel `json:"closed_channels"`
	BlockedDenoms    []EmergencyDenom   `json:"blocked_denoms"`
	DisabledMessages []string           `json:"disabled_messages"`
	HaltedDenoms     []EmergencyDenom   `json:"halted_denoms"`
	Plan             *EmergencyPlan     `json:"plan"`
}

// NewEmergency maps a live state with the rows that name its denoms and
// channels and the label of its guardian.
func NewEmergency(s *emergency.State, denoms map[string]repository.EmergencyDenom,
	channels map[string]repository.EmergencyChannel, guardian *repository.EmergencyLabel) Emergency {
	out := Emergency{
		Supported: s.Supported, Height: s.Height, Active: s.Active(), AutomationPaused: s.AutomationPaused,
		ClosedChannels:   make([]EmergencyChannel, 0, len(s.ClosedChannels)),
		BlockedDenoms:    emergencyDenoms(s.BlockedDenoms, denoms),
		DisabledMessages: append([]string{}, s.DisabledMessages...),
		HaltedDenoms:     emergencyDenoms(s.HaltedDenoms, denoms),
	}
	if s.Guardian != "" {
		g := &EmergencyGuardian{Address: s.Guardian}
		if guardian != nil {
			g.Label, g.LabelKind = &guardian.Name, &guardian.Kind
		}
		out.Guardian = g
	}
	for _, id := range s.ClosedChannels {
		c := channels[id]
		out.ClosedChannels = append(out.ClosedChannels, EmergencyChannel{
			ChannelID: id, CounterpartyChainID: c.CounterpartyChainID, CounterpartyChainName: c.CounterpartyChainName,
		})
	}
	if s.Plan != nil {
		out.Plan = &EmergencyPlan{Name: s.Plan.Name, Height: s.Plan.Height, Info: s.Plan.Info, Emergency: s.Plan.Emergency}
	}
	return out
}

func emergencyDenoms(list []string, known map[string]repository.EmergencyDenom) []EmergencyDenom {
	out := make([]EmergencyDenom, 0, len(list))
	for _, d := range list {
		k := known[d]
		out = append(out, EmergencyDenom{Denom: d, Symbol: k.Symbol, Exponent: k.Exponent, LogoURL: k.LogoURL})
	}
	return out
}

// govAddress is the governance module account: a message it signs is a
// governance action.
var govAddress = chain.ModuleAddress("gov")

// EmergencyAction is an item of GET /api/v1/emergency/actions: Action is
// one of the emergency.* action names; By is "guardian" or "governance"
// (the signer is the governance module, or the row is block-level, a
// governance proposal's execution); TxHash is null for a block-level row;
// Details is the message body or the event attributes.
type EmergencyAction struct {
	Height  int64           `json:"height"`
	Time    time.Time       `json:"time"`
	TxHash  *string         `json:"tx_hash"`
	Action  string          `json:"action"`
	By      string          `json:"by"`
	Signer  *string         `json:"signer"`
	Details json.RawMessage `json:"details"`
	// ProposalID is the passed proposal that executed the action; null for
	// the others.
	ProposalID *int64 `json:"proposal_id"`
	// Via is how a guardian action run inside another message reached the
	// chain: through authz (a grantee's MsgExec) or a DAO (its admin's
	// MsgAdminExecute, or an executed proposal); null for the others.
	Via *EmergencyVia `json:"via"`
}

// EmergencyVia is the wrapper that ran a nested guardian action: Kind is
// "authz", with Grantee the MsgExec's signer, or "dao", with DaoID and
// ProposalID (the executed DAO proposal; null for a MsgAdminExecute).
type EmergencyVia struct {
	Kind       string  `json:"kind"`
	Grantee    *string `json:"grantee,omitempty"`
	DaoID      *int64  `json:"dao_id,omitempty"`
	ProposalID *int64  `json:"proposal_id,omitempty"`
}

// MarshalJSON writes the fields of the kind: {"kind": "authz", "grantee"}
// or {"kind": "dao", "dao_id", "proposal_id"}, the proposal null for a
// MsgAdminExecute.
func (v EmergencyVia) MarshalJSON() ([]byte, error) {
	if v.Kind == "dao" {
		return json.Marshal(struct {
			Kind       string `json:"kind"`
			DaoID      *int64 `json:"dao_id"`
			ProposalID *int64 `json:"proposal_id"`
		}{v.Kind, v.DaoID, v.ProposalID})
	}
	return json.Marshal(struct {
		Kind    string  `json:"kind"`
		Grantee *string `json:"grantee"`
	}{v.Kind, v.Grantee})
}

// NewEmergencyAction maps a stored row; ok is false for a row of a kind the
// action names do not cover. A block-level row and a passed proposal's
// message are governance's (the proposal's signer is the governance
// module); a transaction's message is the guardian's unless the governance
// module signed it, and so is a nested one (its signer is the inner
// message's, the guardian; the wrapper is in Via).
func NewEmergencyAction(a repository.EmergencyAction) (EmergencyAction, bool) {
	var action string
	var ok bool
	if a.Source == repository.EmergencyFromEvent {
		action, ok = emergency.EventAction(a.Kind)
	} else {
		action, ok = emergency.MessageAction(a.Kind)
	}
	if !ok {
		return EmergencyAction{}, false
	}
	out := EmergencyAction{Height: a.Height, Time: a.Time.UTC(), Action: action, By: "guardian",
		Details: jsonOr(a.Details, "{}"), ProposalID: a.ProposalID}
	signer := a.Signer
	switch a.Source {
	case repository.EmergencyFromEvent:
		out.By = "governance"
		signer = eventSigner(a.Details)
	case repository.EmergencyFromProposal:
		out.By = "governance"
		signer = govAddress
	case repository.EmergencyFromNested:
		t := a.TxHash
		out.TxHash = &t
		out.Via = newEmergencyVia(a)
	default:
		t := a.TxHash
		out.TxHash = &t
	}
	if signer != "" {
		out.Signer = &signer
		if signer == govAddress {
			out.By = "governance"
		}
	}
	return out, true
}

// newEmergencyVia is the Via of a nested row; nil when the row names no
// wrapper.
func newEmergencyVia(a repository.EmergencyAction) *EmergencyVia {
	if a.ViaKind == nil {
		return nil
	}
	v := &EmergencyVia{Kind: *a.ViaKind}
	if v.Kind == "dao" {
		v.DaoID, v.ProposalID = a.DaoID, a.DaoProposalID
	} else {
		v.Grantee = a.Grantee
	}
	return v
}

// eventSigner reads who acted from the attributes of a guardian event: the
// signer of a guardian power, the authority of an undo.
func eventSigner(attrs json.RawMessage) string {
	var v struct {
		Signer    string `json:"signer"`
		Authority string `json:"authority"`
	}
	if json.Unmarshal(attrs, &v) != nil {
		return ""
	}
	if v.Signer != "" {
		return v.Signer
	}
	return v.Authority
}
