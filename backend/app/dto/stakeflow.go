package dto

import (
	"strconv"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/stakeflow"
)

// StakeChange is one validator's net stake change (ubze, signed) over the
// last 24 hours and the last 7 days.
type StakeChange struct {
	Day  string `json:"24h"`
	Week string `json:"7d"`
}

// StakeChanges is GET /api/v1/validators/stake-changes, keyed by operator
// address. A validator without movements in the last 7 days is left out.
type StakeChanges struct {
	// Complete is false while some height of the window is not indexed: the
	// figures are then partial.
	Complete   bool                   `json:"complete"`
	AsOfHeight int64                  `json:"as_of_height"`
	Items      map[string]StakeChange `json:"items"`
}

// NewStakeChanges shapes the set's changes.
func NewStakeChanges(c stakeflow.Changes) StakeChanges {
	out := StakeChanges{Complete: c.Complete, AsOfHeight: c.AsOfHeight, Items: make(map[string]StakeChange, len(c.Items))}
	for v, ch := range c.Items {
		out.Items[v] = StakeChange{Day: strconv.FormatInt(ch.Day, 10), Week: strconv.FormatInt(ch.Week, 10)}
	}
	return out
}

// CounterpartyValidator is the other validator of a redelegation; Moniker
// is null when the explorer does not know it.
type CounterpartyValidator struct {
	OperatorAddress string  `json:"operator_address"`
	Moniker         *string `json:"moniker"`
}

// StakeMovement is an item of GET /api/v1/validators/{operator}/movements.
type StakeMovement struct {
	Height    int64     `json:"height"`
	TxHash    string    `json:"tx_hash"`
	Time      time.Time `json:"time"`
	Kind      string    `json:"kind"`
	Delegator string    `json:"delegator"`
	// CounterpartyValidator is set on redelegations only.
	CounterpartyValidator *CounterpartyValidator `json:"counterparty_validator"`
	// Amount is positive; Kind tells the direction.
	Amount string `json:"amount"`
}

// StakeMovements is GET /api/v1/validators/{operator}/movements: one page
// of the last 7 days, newest first.
type StakeMovements struct {
	Complete bool `json:"complete"`
	// WindowFrom is the window's start, null before the set is loaded.
	WindowFrom *time.Time      `json:"window_from"`
	Page       int64           `json:"page"`
	Total      int             `json:"total"`
	Items      []StakeMovement `json:"items"`
}

// NewStakeMovements shapes a page; monikers names the counterparties.
func NewStakeMovements(p stakeflow.Page, page int64, monikers map[string]string) StakeMovements {
	out := StakeMovements{Complete: p.Complete, Page: page, Total: p.Total, Items: make([]StakeMovement, 0, len(p.Items))}
	if !p.WindowFrom.IsZero() {
		from := p.WindowFrom.UTC()
		out.WindowFrom = &from
	}
	for _, m := range p.Items {
		item := StakeMovement{
			Height: m.Height, TxHash: m.TxHash, Time: m.Time.UTC(), Kind: m.Kind,
			Delegator: m.Delegator, Amount: strconv.FormatInt(m.Amount, 10),
		}
		if m.Counterparty != "" {
			cp := &CounterpartyValidator{OperatorAddress: m.Counterparty}
			if name, ok := monikers[m.Counterparty]; ok {
				cp.Moniker = &name
			}
			item.CounterpartyValidator = cp
		}
		out.Items = append(out.Items, item)
	}
	return out
}
