package transform

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// evChainHaltScheduled is emitted when the guardian (a transaction) or
// governance (its EndBlocker running the message) schedules an emergency
// halt: an x/upgrade plan at the next block, replacing any plan already
// scheduled (chain v8.3.0).
const evChainHaltScheduled = "bze.guardian.v1.ChainHaltScheduledEvent"

// haltPlanPrefix starts the name of every emergency halt plan; the height
// follows it (x/guardian HaltPlanPrefix).
const haltPlanPrefix = "emergency-halt-"

// EmergencyHalt is an explorer.upgrades row of kind emergency: the plan
// height H the chain stops at, and who scheduled it why.
type EmergencyHalt struct {
	Height int64
	Name   string
	Reason string
	// ScheduledBy is the signer of the message: the guardian, or the gov
	// module when a proposal ran it.
	ScheduledBy string
	// ScheduledHeight is the block whose event scheduled it.
	ScheduledHeight int64
	// TxHash is the scheduling transaction, empty at block level.
	TxHash string
}

// emergencyHalts appends the halts that events schedule. An event without
// a valid height is skipped: nothing can be watched for it.
func emergencyHalts(ents *Entities, b Block, txHash string, events []node.Event) {
	for _, ev := range events {
		if ev.Type != evChainHaltScheduled {
			continue
		}
		attrs := storedEvent(ev).Attrs
		h, err := int64Attr(attrs["height"])
		if err != nil || h <= 0 {
			continue
		}
		name := anyString(attrs["name"])
		if name == "" {
			name = haltPlanPrefix + strconv.FormatInt(h, 10)
		}
		ents.EmergencyHalts = append(ents.EmergencyHalts, EmergencyHalt{
			Height: h, Name: name, Reason: anyString(attrs["reason"]), ScheduledBy: anyString(attrs["signer"]),
			ScheduledHeight: b.Height, TxHash: txHash,
		})
	}
}

// int64Attr reads a typed-event integer: proto JSON writes an int64 as a
// string, a plain number is taken too.
func int64Attr(v any) (int64, error) {
	switch x := v.(type) {
	case string:
		return strconv.ParseInt(x, 10, 64)
	case json.Number:
		return x.Int64()
	default:
		return 0, fmt.Errorf("not an integer: %v", v)
	}
}
