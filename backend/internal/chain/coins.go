package chain

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Coin is one coin as the explorer stores it: {"denom":"ubze","amount":"70255"}.
type Coin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

// ParseCoins splits a coin string as events carry it, e.g.
// "70255ubze,12ibc/ED07A3…", into coins in their order. The amount is the
// leading digits and the denom the rest, which may contain "/". An empty
// string is no coins.
func ParseCoins(s string) ([]Coin, error) {
	out := []Coin{}
	if strings.TrimSpace(s) == "" {
		return out, nil
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		i := 0
		for i < len(part) && part[i] >= '0' && part[i] <= '9' {
			i++
		}
		if i == 0 || i == len(part) {
			return nil, fmt.Errorf("coin %q: want <amount><denom>", part)
		}
		out = append(out, Coin{Denom: part[i:], Amount: part[:i]})
	}
	return out, nil
}

// CoinsOf converts SDK coins to explorer coins.
func CoinsOf(coins sdk.Coins) []Coin {
	out := make([]Coin, 0, len(coins))
	for _, c := range coins {
		out = append(out, Coin{Denom: c.Denom, Amount: c.Amount.String()})
	}
	return out
}
