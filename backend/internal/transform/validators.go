package transform

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/types/bech32"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// Kinds of the validator_events rows the transformer derives. The state sync
// writes the others (jailed, tombstoned, bonded, unbonded): the chain emits
// no event for them.
const (
	ValidatorCreated            = "created"
	ValidatorCommissionChanged  = "commission_changed"
	ValidatorDescriptionChanged = "description_changed"
	ValidatorUnjailed           = "unjailed"
	ValidatorSlashed            = "slashed"
)

// Type URLs of the validator messages.
const (
	msgCreateValidator = "/cosmos.staking.v1beta1.MsgCreateValidator"
	msgEditValidator   = "/cosmos.staking.v1beta1.MsgEditValidator"
	msgUnjail          = "/cosmos.slashing.v1beta1.MsgUnjail"
)

// doNotModify is the value MsgEditValidator gives the description fields it
// leaves alone.
const doNotModify = "[do-not-modify]"

// ValidatorEvent is one explorer.validator_events row.
type ValidatorEvent struct {
	Height int64
	// TxIndex is -1 for a block-level event (a slash).
	TxIndex int
	// Seq orders the events of one transaction (or of the block's
	// finalize events), from 0.
	Seq int
	// Operator is empty for a slash: the event names a consensus address
	// only, which the writer resolves through validators.consensus_address
	// (and the state sync later, when the validator is not synced yet).
	Operator string
	// ConsensusAddress is the slashed validator's, upper-case hex; empty for
	// the other kinds.
	ConsensusAddress string
	Kind             string
	// Details is the row's details object; nil is NULL.
	Details map[string]any
	// CommissionFrom asks the writer to add details.from, the validator's
	// stored commission rate when it writes the row: the edit_validator event
	// carries the new rate only. The state sync updates the stored rate after
	// the live write, so "from" is exact on the live path; the backfill
	// writes history with today's rate, an approximation.
	CommissionFrom bool
	Time           time.Time
}

// slashEvents appends the slashed rows of the block's finalize events.
func slashEvents(ents *Entities, b Block, events []node.Event) {
	seq := 0
	for _, ev := range events {
		if ev.Type != "slash" {
			continue
		}
		addr, _ := ev.Get("address")
		details := map[string]any{"consensus_address": consHex(addr)}
		for key, name := range map[string]string{"reason": "reason", "power": "power", "burned_coins": "burned"} {
			if v, ok := ev.Get(key); ok {
				details[name] = v
			}
		}
		if details["consensus_address"] == "" {
			details["address"] = addr
		}
		ents.ValidatorEvents = append(ents.ValidatorEvents, ValidatorEvent{
			Height: b.Height, TxIndex: -1, Seq: seq, ConsensusAddress: consHex(addr),
			Kind: ValidatorSlashed, Details: details, Time: b.Time,
		})
		seq++
	}
}

// txValidatorEvents appends the rows of the validator messages of a
// successful transaction. byIndex are its events by message.
func txValidatorEvents(ents *Entities, b Block, txIndex int, msgs []chain.Msg, byIndex map[int][]node.Event) {
	seq := 0
	add := func(operator, kind string, details map[string]any, commissionFrom bool) {
		if operator == "" {
			return
		}
		ents.ValidatorEvents = append(ents.ValidatorEvents, ValidatorEvent{
			Height: b.Height, TxIndex: txIndex, Seq: seq, Operator: operator, Kind: kind,
			Details: details, CommissionFrom: commissionFrom, Time: b.Time,
		})
		seq++
	}
	for j, m := range msgs {
		var body map[string]any
		_ = json.Unmarshal(m.Body, &body) // an undecodable body leaves it nil
		switch m.TypeURL {
		case msgCreateValidator:
			operator := str(body, "validator_address")
			if operator == "" {
				operator = eventAttr(byIndex[j], "create_validator", "validator")
			}
			details := map[string]any{}
			if d, ok := body["description"].(map[string]any); ok {
				details["moniker"] = str(d, "moniker")
			}
			if c, ok := body["commission"].(map[string]any); ok {
				details["commission_rate"] = str(c, "rate")
			}
			if v, ok := body["value"].(map[string]any); ok {
				details["self_bond"] = str(v, "amount") + str(v, "denom")
			} else if amount := eventAttr(byIndex[j], "create_validator", "amount"); amount != "" {
				details["self_bond"] = amount
			}
			add(operator, ValidatorCreated, details, false)
		case msgEditValidator:
			operator := str(body, "validator_address")
			if d, ok := body["description"].(map[string]any); ok {
				changed := map[string]any{}
				for _, f := range []string{"moniker", "identity", "website", "security_contact", "details"} {
					if v, ok := d[f].(string); ok && v != doNotModify {
						changed[f] = v
					}
				}
				if len(changed) > 0 {
					add(operator, ValidatorDescriptionChanged, map[string]any{"to": changed}, false)
				}
			}
			if rate := str(body, "commission_rate"); rate != "" {
				add(operator, ValidatorCommissionChanged, map[string]any{"to": rate}, true)
			}
		case msgUnjail:
			add(str(body, "validator_addr"), ValidatorUnjailed, nil, false)
		}
	}
}

// markValidators marks the validators an event changes: the delegation
// events name the validator, the slash and liveness events its consensus
// address.
func markValidators(d *statesync.Dirty, ev node.Event) {
	switch ev.Type {
	case "delegate", "unbond", "create_validator", "cancel_unbonding_delegation":
		v, _ := ev.Get("validator")
		d.Mark(statesync.Validators, v)
	case "redelegate":
		src, _ := ev.Get("source_validator")
		dst, _ := ev.Get("destination_validator")
		d.Mark(statesync.Validators, src)
		d.Mark(statesync.Validators, dst)
	case "slash", "liveness":
		addr, _ := ev.Get("address")
		if h := consHex(addr); h != "" {
			d.Mark(statesync.Validators, statesync.ConsKey(h))
		} else if addr != "" {
			d.Mark(statesync.Validators, statesync.All)
		}
	}
}

// markValidatorMsg marks the validator of the messages whose events do not
// name it: an edit (description, commission) and an unjail.
func markValidatorMsg(d *statesync.Dirty, m chain.Msg) {
	var body map[string]any
	if json.Unmarshal(m.Body, &body) != nil {
		return
	}
	switch m.TypeURL {
	case msgEditValidator:
		d.Mark(statesync.Validators, str(body, "validator_address"))
	case msgUnjail:
		d.Mark(statesync.Validators, str(body, "validator_addr"))
	}
}

// markValidatorUpdates marks every validator whose consensus power the block
// changes, by consensus address: one entering or leaving the active set
// changes status without any event naming it. A key the transformer cannot
// read asks for every validator.
func markValidatorUpdates(d *statesync.Dirty, updates []node.ValidatorUpdate) {
	for _, u := range updates {
		if h := updateConsHex(u.PubKey); h != "" {
			d.Mark(statesync.Validators, statesync.ConsKey(h))
		} else {
			d.Mark(statesync.Validators, statesync.All)
		}
	}
}

// updateConsHex is the consensus address of a validator_updates pub_key: the
// first 20 bytes of the SHA-256 of an ed25519 key. Empty for another key
// type or an unreadable entry.
func updateConsHex(raw json.RawMessage) string {
	var pk struct {
		Sum struct {
			Value struct {
				Ed25519 string `json:"ed25519"`
			} `json:"value"`
		} `json:"Sum"`
	}
	if json.Unmarshal(raw, &pk) != nil || pk.Sum.Value.Ed25519 == "" {
		return ""
	}
	key, err := base64.StdEncoding.DecodeString(pk.Sum.Value.Ed25519)
	if err != nil || len(key) != 32 {
		return ""
	}
	sum := sha256.Sum256(key)
	return strings.ToUpper(hex.EncodeToString(sum[:20]))
}

// consHex turns a bech32 consensus address (bzevalcons1…) into the
// upper-case hex form /block and the validators table use; empty when it is
// not one.
func consHex(addr string) string {
	hrp, b, err := bech32.DecodeAndConvert(addr)
	if err != nil || !strings.HasSuffix(hrp, "valcons") {
		return ""
	}
	return strings.ToUpper(hex.EncodeToString(b))
}

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// eventAttr is attribute key of the first event of type typ.
func eventAttr(events []node.Event, typ, key string) string {
	for _, ev := range events {
		if ev.Type == typ {
			v, _ := ev.Get(key)
			return v
		}
	}
	return ""
}
