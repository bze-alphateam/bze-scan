package openorders_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/openorders"
)

var (
	// A factory denom: its market id has four slashes.
	vdl     = openorders.Market{ID: "factory/bze1creator/uvdl/ubze", Base: "factory/bze1creator/uvdl", Quote: "ubze"}
	usdc    = openorders.Market{ID: "ubze/ibc/USDC", Base: "ubze", Quote: "ibc/USDC"}
	markets = map[string]openorders.Market{vdl.ID: vdl, usdc.ID: usdc}
)

func order(id string, m openorders.Market, side, amount, price string) openorders.Order {
	return openorders.Order{ID: id, MarketID: m.ID, Side: side, Amount: amount, Price: price}
}

func TestLockedIsTheChainsFormula(t *testing.T) {
	denom, amount, err := openorders.Locked(order("1", usdc, "sell", "1000", "0.25"), usdc)
	require.NoError(t, err)
	assert.Equal(t, "ubze", denom, "a sell locks the base")
	assert.Equal(t, "1000", amount.String())

	denom, amount, err = openorders.Locked(order("2", usdc, "buy", "1001", "0.25"), usdc)
	require.NoError(t, err)
	assert.Equal(t, "ibc/USDC", denom, "a buy locks the quote")
	assert.Equal(t, "250", amount.String(), "trunc(1001 × 0.25 = 250.25)")

	_, _, err = openorders.Locked(order("3", usdc, "hold", "1", "1"), usdc)
	require.Error(t, err)
	_, _, err = openorders.Locked(order("4", usdc, "buy", "1", "x"), usdc)
	require.Error(t, err)
	_, _, err = openorders.Locked(order("5", usdc, "sell", "1.5", "1"), usdc)
	require.Error(t, err)
}

// Two buys of 3 at 0.5 lock 1 each (1.5 truncated per order): 2, where
// truncating the sum (6 × 0.5) would say 3.
func TestBuysAreTruncatedPerOrderBeforeSumming(t *testing.T) {
	s, err := openorders.Summarise([]openorders.Order{
		order("1", usdc, "buy", "3", "0.5"),
		order("2", usdc, "buy", "3", "0.5"),
	}, markets)
	require.NoError(t, err)
	require.Len(t, s.Assets, 1)
	assert.Equal(t, "2", s.Assets[0].Amount.String())
	assert.Equal(t, "2", s.Assets[0].Markets[0].Amount.String())
}

func TestSummariseGroupsByDenomThenMarket(t *testing.T) {
	s, err := openorders.Summarise([]openorders.Order{
		order("1", usdc, "sell", "100", "2"),  // 100 ubze
		order("2", usdc, "sell", "50", "3"),   // 50 ubze
		order("3", usdc, "buy", "10", "1.5"),  // 15 ibc/USDC
		order("4", usdc, "buy", "10", "1.5"),  // 15 ibc/USDC
		order("5", vdl, "buy", "1000", "0.2"), // 200 ubze
		order("6", vdl, "sell", "7", "0.2"),   // 7 uvdl
	}, markets)
	require.NoError(t, err)

	assert.Equal(t, 6, s.OrdersCount)
	assert.Equal(t, 2, s.MarketsCount)
	require.Len(t, s.Assets, 3)
	type row struct {
		denom, amount string
		orders        int
		markets       []string
	}
	var got []row
	for _, a := range s.Assets {
		r := row{denom: a.Denom, amount: a.Amount.String(), orders: a.OrdersCount}
		for _, m := range a.Markets {
			r.markets = append(r.markets, m.Market.ID+" "+m.Amount.String()+" "+string(rune('0'+m.OrdersCount)))
		}
		got = append(got, r)
	}
	assert.Equal(t, []row{
		{"factory/bze1creator/uvdl", "7", 1, []string{vdl.ID + " 7 1"}},
		{"ibc/USDC", "30", 2, []string{usdc.ID + " 30 2"}},
		// Sorted by amount: the vdl market's buys lock more ubze than the
		// usdc market's sells.
		{"ubze", "350", 3, []string{vdl.ID + " 200 1", usdc.ID + " 150 2"}},
	}, got)
	assert.Equal(t, vdl.Base, s.Assets[2].Markets[0].Market.Base, "the market id is never split")
}

func TestSummariseOfNothingIsEmpty(t *testing.T) {
	s, err := openorders.Summarise(nil, nil)
	require.NoError(t, err)
	assert.Zero(t, s.OrdersCount)
	assert.Zero(t, s.MarketsCount)
	assert.NotNil(t, s.Assets)
	assert.Empty(t, s.Assets)
}

func TestSummariseRefusesAnUnknownMarket(t *testing.T) {
	_, err := openorders.Summarise([]openorders.Order{{ID: "1", MarketID: "a/b", Side: "sell", Amount: "1"}}, markets)
	require.Error(t, err)
}
