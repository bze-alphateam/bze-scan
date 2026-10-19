package dto

import "github.com/bze-alphateam/bze-scan/backend/internal/openorders"

// OpenOrders is GET /api/v1/accounts/{address}/open-orders: what the
// account's open DEX orders lock, per denom (sorted by denom), then per
// market (largest amount first). Amounts are base units as strings. Source
// says where the answer comes from ("index" or "node", for debugging) and
// height is the height it describes.
type OpenOrders struct {
	Source       string            `json:"source"`
	Height       int64             `json:"height"`
	OrdersCount  int               `json:"orders_count"`
	MarketsCount int               `json:"markets_count"`
	Assets       []OpenOrdersAsset `json:"assets"`
}

// OpenOrdersAsset is what the open orders lock of one denom.
type OpenOrdersAsset struct {
	Denom       string             `json:"denom"`
	Amount      string             `json:"amount"`
	OrdersCount int                `json:"orders_count"`
	Markets     []OpenOrdersMarket `json:"markets"`
}

// OpenOrdersMarket is what the open orders of one market lock of the
// asset: its sells under the base denom, its buys under the quote denom.
type OpenOrdersMarket struct {
	MarketID    string `json:"market_id"`
	Base        string `json:"base"`
	Quote       string `json:"quote"`
	Amount      string `json:"amount"`
	OrdersCount int    `json:"orders_count"`
}

// NewOpenOrders shapes a summary.
func NewOpenOrders(s openorders.Summary) OpenOrders {
	out := OpenOrders{
		Source: s.Source, Height: s.Height, OrdersCount: s.OrdersCount, MarketsCount: s.MarketsCount,
		Assets: make([]OpenOrdersAsset, 0, len(s.Assets)),
	}
	for _, a := range s.Assets {
		asset := OpenOrdersAsset{Denom: a.Denom, Amount: a.Amount.String(), OrdersCount: a.OrdersCount,
			Markets: make([]OpenOrdersMarket, 0, len(a.Markets))}
		for _, m := range a.Markets {
			asset.Markets = append(asset.Markets, OpenOrdersMarket{
				MarketID: m.Market.ID, Base: m.Market.Base, Quote: m.Market.Quote,
				Amount: m.Amount.String(), OrdersCount: m.OrdersCount,
			})
		}
		out.Assets = append(out.Assets, asset)
	}
	return out
}
