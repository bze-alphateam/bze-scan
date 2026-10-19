package openorders

import (
	"cmp"
	"fmt"
	"slices"

	"cosmossdk.io/math"
)

// Order sides, as the chain names them.
const (
	SideBuy  = "buy"
	SideSell = "sell"
)

// Order is an open order: Amount is the base amount still on the book.
type Order struct {
	ID       string
	MarketID string
	Side     string
	Amount   string
	Price    string
}

// Market is a tradebin market; ID is the chain's market id (base/quote).
type Market struct {
	ID    string
	Base  string
	Quote string
}

// Locked returns the coin an order locks, by the chain's own formula
// (tradebin GetOrderSdkCoin): a sell locks its amount of the base denom, a
// buy trunc(amount × price) of the quote denom, truncated per order.
func Locked(o Order, m Market) (string, math.Int, error) {
	amount, ok := math.NewIntFromString(o.Amount)
	if !ok {
		return "", math.Int{}, fmt.Errorf("order %s: amount %q is not an integer", o.ID, o.Amount)
	}
	switch o.Side {
	case SideSell:
		return m.Base, amount, nil
	case SideBuy:
		price, err := math.LegacyNewDecFromStr(o.Price)
		if err != nil {
			return "", math.Int{}, fmt.Errorf("order %s: price %q: %w", o.ID, o.Price, err)
		}
		return m.Quote, math.LegacyNewDecFromInt(amount).Mul(price).TruncateInt(), nil
	default:
		return "", math.Int{}, fmt.Errorf("order %s: unknown side %q", o.ID, o.Side)
	}
}

// Summary is the answer for one account: what its open orders lock, per
// denom, then per market.
type Summary struct {
	// Source is SourceIndex or SourceNode.
	Source string
	// Height is the height the answer describes.
	Height       int64
	OrdersCount  int
	MarketsCount int
	// Assets are sorted by denom.
	Assets []Asset
}

// Asset is what the orders lock of one denom.
type Asset struct {
	Denom       string
	Amount      math.Int
	OrdersCount int
	// Markets are sorted by amount, largest first, then by market id.
	Markets []AssetMarket
}

// AssetMarket is what the orders of one market lock of one denom: the sells
// under the base denom, the buys under the quote denom.
type AssetMarket struct {
	Market      Market
	Amount      math.Int
	OrdersCount int
}

// Summarise groups orders by locked denom, then by market. markets holds
// every market the orders name.
func Summarise(orders []Order, markets map[string]Market) (Summary, error) {
	type key struct{ denom, market string }
	byKey := map[key]*AssetMarket{}
	marketIDs := map[string]bool{}
	for _, o := range orders {
		m, ok := markets[o.MarketID]
		if !ok {
			return Summary{}, fmt.Errorf("order %s: unknown market %q", o.ID, o.MarketID)
		}
		denom, amount, err := Locked(o, m)
		if err != nil {
			return Summary{}, err
		}
		marketIDs[o.MarketID] = true
		k := key{denom, o.MarketID}
		am := byKey[k]
		if am == nil {
			am = &AssetMarket{Market: m, Amount: math.ZeroInt()}
			byKey[k] = am
		}
		am.Amount = am.Amount.Add(amount)
		am.OrdersCount++
	}

	byDenom := map[string]*Asset{}
	for k, am := range byKey {
		a := byDenom[k.denom]
		if a == nil {
			a = &Asset{Denom: k.denom, Amount: math.ZeroInt()}
			byDenom[k.denom] = a
		}
		a.Amount = a.Amount.Add(am.Amount)
		a.OrdersCount += am.OrdersCount
		a.Markets = append(a.Markets, *am)
	}
	s := Summary{OrdersCount: len(orders), MarketsCount: len(marketIDs), Assets: make([]Asset, 0, len(byDenom))}
	for _, a := range byDenom {
		slices.SortFunc(a.Markets, func(x, y AssetMarket) int {
			if c := y.Amount.BigInt().Cmp(x.Amount.BigInt()); c != 0 {
				return c
			}
			return cmp.Compare(x.Market.ID, y.Market.ID)
		})
		s.Assets = append(s.Assets, *a)
	}
	slices.SortFunc(s.Assets, func(x, y Asset) int { return cmp.Compare(x.Denom, y.Denom) })
	return s, nil
}
