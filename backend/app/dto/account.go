package dto

import (
	"time"

	sdkmath "cosmossdk.io/math"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainstate"
)

// Account is GET /api/v1/accounts/{address}: the explorer's counters and
// label plus the live balances, staking and rewards. When the live read
// failed, Live.Available is false and the live lists are empty.
type Account struct {
	Address        string              `json:"address"`
	Label          *Label              `json:"label"`
	FirstSeen      *Seen               `json:"first_seen"`
	LastSeenHeight *int64              `json:"last_seen_height"`
	TxCount        int64               `json:"tx_count"`
	ActivityCount  int64               `json:"activity_count"`
	Balances       []Balance           `json:"balances"`
	Delegations    []AccountDelegation `json:"delegations"`
	Unbonding      []AccountUnbonding  `json:"unbonding"`
	Rewards        []AccountReward     `json:"rewards"`
	// TotalStaked is the sum of the delegations, in the bond denom's base
	// units; null when the live read failed.
	TotalStaked *string `json:"total_staked"`
	// TotalRewards are the pending rewards of every validator together.
	TotalRewards []Coin `json:"total_rewards"`
	Live         Live   `json:"live"`
}

// Label names a known address.
type Label struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// Seen is a height and its block time.
type Seen struct {
	Height int64     `json:"height"`
	Time   time.Time `json:"time"`
}

// Balance is a balance with the denom's display data (null when the
// explorer does not know the denom).
type Balance struct {
	Denom    string  `json:"denom"`
	Amount   string  `json:"amount"`
	Symbol   *string `json:"symbol"`
	Exponent *int    `json:"exponent"`
}

// Coin is an amount of a denom in base units.
type Coin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

// AccountDelegation is the stake with one validator, in the bond denom's
// base units; moniker is null when the validator is not synced.
type AccountDelegation struct {
	Validator string  `json:"validator"`
	Moniker   *string `json:"moniker"`
	Amount    string  `json:"amount"`
}

// AccountUnbonding is one unbonding entry.
type AccountUnbonding struct {
	Validator      string    `json:"validator"`
	Moniker        *string   `json:"moniker"`
	Amount         string    `json:"amount"`
	CompletionTime time.Time `json:"completion_time"`
}

// AccountReward is the pending reward from one validator.
type AccountReward struct {
	Validator string `json:"validator"`
	Coins     []Coin `json:"coins"`
}

// Live tells whether the live part of the account could be read.
type Live struct {
	Available bool `json:"available"`
}

// NewAccount maps the explorer's account and, when the live read succeeded
// (live not nil), the live state with the denoms' display data and the
// validators' monikers.
func NewAccount(a *repository.Account, live *chainstate.Account, denoms map[string]repository.Denom,
	monikers map[string]string) Account {
	out := Account{
		Address: a.Address, LastSeenHeight: a.LastSeenHeight, TxCount: a.TxCount, ActivityCount: a.ActivityCount,
		Balances: []Balance{}, Delegations: []AccountDelegation{}, Unbonding: []AccountUnbonding{},
		Rewards: []AccountReward{}, TotalRewards: []Coin{},
	}
	out.Label = newLabel(a.Label)
	if a.FirstSeen != nil {
		out.FirstSeen = &Seen{Height: a.FirstSeen.Height, Time: a.FirstSeen.Time.UTC()}
	}
	if live == nil {
		return out
	}
	out.Live.Available = true

	for _, b := range live.Balances {
		bal := Balance{Denom: b.Denom, Amount: b.Amount}
		if d, ok := denoms[b.Denom]; ok {
			exp := d.Exponent
			bal.Symbol, bal.Exponent = d.Symbol, &exp
		}
		out.Balances = append(out.Balances, bal)
	}
	moniker := func(op string) *string {
		if m, ok := monikers[op]; ok {
			return &m
		}
		return nil
	}
	staked := sdkmath.ZeroInt()
	for _, d := range live.Delegations {
		out.Delegations = append(out.Delegations, AccountDelegation{
			Validator: d.Validator, Moniker: moniker(d.Validator), Amount: d.Amount.Amount,
		})
		if n, ok := sdkmath.NewIntFromString(d.Amount.Amount); ok {
			staked = staked.Add(n)
		}
	}
	total := staked.String()
	out.TotalStaked = &total
	for _, u := range live.Unbonding {
		out.Unbonding = append(out.Unbonding, AccountUnbonding{
			Validator: u.Validator, Moniker: moniker(u.Validator), Amount: u.Amount.Amount,
			CompletionTime: u.CompletionTime.UTC(),
		})
	}
	for _, r := range live.Rewards.ByValidator {
		out.Rewards = append(out.Rewards, AccountReward{Validator: r.Validator, Coins: coinsOf(r.Coins)})
	}
	out.TotalRewards = coinsOf(live.Rewards.Total)
	return out
}

func coinsOf(cs []chainstate.Coin) []Coin {
	out := make([]Coin, 0, len(cs))
	for _, c := range cs {
		out = append(out, Coin(c))
	}
	return out
}
