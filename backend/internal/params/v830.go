package params

import (
	"encoding/json"
	"fmt"
)

// The explorer decodes each module's params answer into the Go types of the
// chain it is built from and renders them again, so a build on the chain
// v8.3.0 types shows every network, v8.2.x ones included, in the v8.3.0
// shape: tradebin's fields 7, 8 and 11 under their new names, field 12 gone,
// and the fields v8.3.0 added with their zero values until the network
// upgrades. UpgradeStoredToV830 rewrites a snapshot stored before that build
// into the same shape, so the snapshot set does not record the change of the
// explorer's own types as a change of the chain's parameters.
//
// The renamed keys keep their field numbers, and so their values: the gRPC
// answer of a v8.2.x node is decoded by number.
//
// Transition code: removed once both networks run v8.3.0 (the last story of
// the v8.3.0 plan).

// v830Rename is a key the v8.3.0 types name differently.
var v830Rename = map[string]map[string]string{
	"tradebin": {
		"orderBookExtraGasWindow":   "priceLevelOwnersWindow",
		"orderBookQueueExtraGas":    "priceLevelExtraGas",
		"orderBookPerBlockMessages": "maxFillsPerOrder",
	},
}

// v830Drop is a key the v8.3.0 types no longer have.
var v830Drop = map[string][]string{
	"tradebin": {"orderBookQueueMessageScanExtraGas"},
}

// v830Add is a key the v8.3.0 types added, with the zero value the types
// render for a node that does not send it.
var v830Add = map[string]map[string]any{
	"tradebin": {
		"poolPriceHalfLifeBlocks": "0",
		"feeMaxPriceDrift":        "0.000000000000000000",
		"moduleMaxPriceDrift":     "0.000000000000000000",
	},
	"burner": {
		"ibc_move_max_reserve_ratio": "0.000000000000000000",
		"ibc_move_cooldown_blocks":   "0",
	},
}

// UpgradeStoredToV830 returns the stored params of module as the v8.3.0
// types render the answer of a v8.2.x node. changed is false when the
// params already have that shape (or the module's did not change).
func UpgradeStoredToV830(module string, stored json.RawMessage) (out json.RawMessage, changed bool, err error) {
	if len(v830Rename[module]) == 0 && len(v830Drop[module]) == 0 && len(v830Add[module]) == 0 {
		return stored, false, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(stored, &doc); err != nil {
		return nil, false, fmt.Errorf("%s params: %w", module, err)
	}
	for oldKey, newKey := range v830Rename[module] {
		v, ok := doc[oldKey]
		if !ok {
			continue
		}
		delete(doc, oldKey)
		changed = true
		if _, has := doc[newKey]; !has {
			doc[newKey] = v
		}
	}
	for _, k := range v830Drop[module] {
		if _, ok := doc[k]; ok {
			delete(doc, k)
			changed = true
		}
	}
	for k, zero := range v830Add[module] {
		if _, ok := doc[k]; ok {
			continue
		}
		raw, err := json.Marshal(zero)
		if err != nil {
			return nil, false, err
		}
		doc[k] = raw
		changed = true
	}
	if !changed {
		return stored, false, nil
	}
	out, err = json.Marshal(doc)
	return out, true, err
}
