package dto

import (
	"encoding/json"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// ActivityCategories are the category filters of the activity list.
var ActivityCategories = []string{
	"sent", "received", "staking", "dex", "governance", "tokens", "rewards", "burner", "cross_chain", "other",
}

// Activity is an item of GET /api/v1/accounts/{address}/activity: what one
// transaction (tx_hash set) or one block's own events (tx_hash null) did
// to the address.
type Activity struct {
	Time     time.Time `json:"time"`
	Height   int64     `json:"height"`
	TxHash   *string   `json:"tx_hash"`
	Kind     string    `json:"kind"`
	Category string    `json:"category"`
	IsSigner bool      `json:"is_signer"`
	Success  bool      `json:"success"`
	// Counterparty is null when there is not exactly one.
	Counterparty *Counterparty `json:"counterparty"`
	// Deltas is the net balance change per denom, the fee included.
	Deltas   []Balance `json:"deltas"`
	MsgTypes []string  `json:"msg_types"`
	// Details are the typed events naming the address (block-level rows);
	// null on transaction rows.
	Details json.RawMessage `json:"details"`
	// Fee is the transaction's fee when the address signed or paid it; null
	// otherwise.
	Fee []Balance `json:"fee"`
}

// Counterparty is the other party of an activity row: an account, a
// validator operator (with its moniker) or a module account (with its
// label).
type Counterparty struct {
	Address string  `json:"address"`
	Label   *Label  `json:"label"`
	Moniker *string `json:"moniker"`
}

// ActivityDenoms returns the denoms of the rows' deltas and fees, for the
// display data lookup.
func ActivityDenoms(rows []repository.Activity) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		for _, c := range append(coinsIn(r.Deltas), coinsIn(r.Fee)...) {
			if !seen[c.Denom] {
				seen[c.Denom] = true
				out = append(out, c.Denom)
			}
		}
	}
	return out
}

// NewActivity maps an activity row, with the denoms' display data.
func NewActivity(a repository.Activity, denoms map[string]repository.Denom) Activity {
	out := Activity{
		Time: a.Time.UTC(), Height: a.Height, TxHash: a.TxHash, Kind: a.Kind, Category: a.Category,
		IsSigner: a.IsSigner, Success: a.Success, Deltas: balances(coinsIn(a.Deltas), denoms),
		MsgTypes: nonNil(a.MsgTypes), Details: jsonOrNull(a.Details),
	}
	if a.Counterparty != nil {
		out.Counterparty = &Counterparty{Address: *a.Counterparty, Label: newLabel(a.CounterpartyLabel),
			Moniker: a.CounterpartyMoniker}
	}
	if len(a.Fee) > 0 && string(a.Fee) != "null" {
		out.Fee = balances(coinsIn(a.Fee), denoms)
	}
	return out
}

// EncodeActivityCursor is the cursor after an activity row; tx_index is
// shifted by one so the block-level -1 stays a non-negative key.
func EncodeActivityCursor(a repository.Activity) string {
	return EncodeCursor(a.Height, a.TxIndex+1)
}

// DecodeActivityCursor reverses EncodeActivityCursor.
func DecodeActivityCursor(cursor string) (*repository.ActivityKey, error) {
	keys, err := DecodeCursor(cursor, 2)
	if err != nil {
		return nil, err
	}
	return &repository.ActivityKey{Height: keys[0], TxIndex: keys[1] - 1}, nil
}

// coinsIn decodes a JSON array of {"denom","amount"}; anything else is no
// coin.
func coinsIn(raw json.RawMessage) []Coin {
	var out []Coin
	if len(raw) == 0 || json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func balances(coins []Coin, denoms map[string]repository.Denom) []Balance {
	out := make([]Balance, 0, len(coins))
	for _, c := range coins {
		b := Balance{Denom: c.Denom, Amount: c.Amount}
		if d, ok := denoms[c.Denom]; ok {
			exp := d.Exponent
			b.Symbol, b.Exponent = d.Symbol, &exp
		}
		out = append(out, b)
	}
	return out
}
