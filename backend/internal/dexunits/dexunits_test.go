package dexunits_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/dexunits"
)

func TestPrice(t *testing.T) {
	for _, tc := range []struct {
		name        string
		price       string
		base, quote int
		want        string
	}{
		{"same exponents", "0.000334", 6, 6, "0.000334"},
		{"same exponents, trailing zeros trimmed", "0.500000000000000000", 6, 6, "0.5"},
		{"integer price", "12", 6, 6, "12"},
		{"base has more decimals", "0.5", 8, 6, "50"},
		{"quote has more decimals", "0.5", 6, 8, "0.005"},
		// BZE-133: the aggregator's %.2f multiplier is 0 from a gap of three.
		{"quote has three more decimals", "1234.5", 6, 9, "1.2345"},
		{"quote has twelve more decimals", "123456789012", 6, 18, "0.123456789012"},
		{"no digit lost past 18 decimals", "0.000000000000000001", 6, 18, "0.000000000000000000000000000001"},
		{"base has 18 decimals", "0.000000000002", 18, 6, "2"},
		{"zero", "0", 6, 9, "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := dexunits.Price(tc.price, tc.base, tc.quote)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestPriceRejectsNonDecimals(t *testing.T) {
	for _, s := range []string{"", ".", "abc", "1.2.3", "1e-5", "0x10", " 1"} {
		_, err := dexunits.Price(s, 6, 6)
		assert.Error(t, err, s)
	}
}

func TestAmount(t *testing.T) {
	for _, tc := range []struct {
		amount string
		exp    int
		want   string
	}{
		{"4919636072", 6, "4919.636072"},
		{"347000000", 6, "347"},
		{"5", 6, "0.000005"},
		{"0", 6, "0"},
		{"12", 0, "12"},
		// beyond int64: 18-decimal assets
		{"123456789012345678901234567890", 18, "123456789012.34567890123456789"},
	} {
		got, err := dexunits.Amount(tc.amount, tc.exp)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, tc.amount)
	}
	_, err := dexunits.Amount("1.5", 6)
	require.Error(t, err, "chain amounts are integers")
	_, err = dexunits.Amount("abc", 6)
	require.Error(t, err)
}

func TestCompare(t *testing.T) {
	assert.Equal(t, -1, dexunits.Compare("0.00034", "0.0006"))
	assert.Equal(t, 1, dexunits.Compare("10", "9.999"), "by value, not as text")
	assert.Equal(t, 0, dexunits.Compare("0.5", "0.500"))
	assert.Equal(t, -1, dexunits.Compare("x", "0"), "an unparsable string sorts first")
	assert.Equal(t, 1, dexunits.Compare("0", "x"))
}

func TestTrimTrailingZeros(t *testing.T) {
	assert.Equal(t, "1.5", dexunits.TrimTrailingZeros("1.500"))
	assert.Equal(t, "2", dexunits.TrimTrailingZeros("2.000"))
	assert.Equal(t, "100", dexunits.TrimTrailingZeros("100"), "an integer is left alone")
}

func TestTradePrice(t *testing.T) {
	for _, tc := range []struct {
		name        string
		price       string
		base, quote int
		want        string
	}{
		{"same exponents", "0.500000000000000000", 6, 6, "0.5"},
		{"base has more decimals", "0.05", 8, 6, "5"},
		// The aggregator's LegacyDec product, rounded back to 18 decimals.
		{"quote has one more decimal", "0.666666666666666667", 6, 7, "0.066666666666666667"},
		{"quote has two more decimals", "33.333333333333333333", 6, 8, "0.333333333333333333"},
		// BZE-133: from three more, exact where the aggregator answers 0.
		{"quote has three more decimals", "1234.5", 6, 9, "1.2345"},
		// Rounded half to even.
		{"one decimal short", "0.000000000000000015", 6, 7, "0.000000000000000002"},
		{"two decimals short", "0.000000000000000125", 6, 8, "0.000000000000000001"},
		{"one decimal short, exact", "1.5", 6, 7, "0.15"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := dexunits.TradePrice(tc.price, tc.base, tc.quote)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	_, err := dexunits.TradePrice("abc", 6, 7)
	assert.Error(t, err)
}

func TestQuoteAmount(t *testing.T) {
	for _, tc := range []struct {
		name, amount, price string
		exp                 int
		want                string
	}{
		{"truncated to the quote's decimals", "123.456789", "0.000334", 6, "0.041234"},
		{"integer", "2", "1.5", 6, "3"},
		{"rounded to 18 decimals first, as a LegacyDec", "3", "0.066666666666666667", 7, "0.2"},
		{"18-decimal quote rounds, as the LegacyDec product does", "0.000000000000000001", "1.5", 18, "0.000000000000000002"},
		{"no quote decimals", "2.5", "1.5", 0, "3"},
		{"a price past 18 decimals is multiplied exactly", "7", "0.0000000000000000001", 19, "0.0000000000000000007"},
		{"exact, truncated", "7", "0.000000000000000000000000000000000001", 18, "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := dexunits.QuoteAmount(tc.amount, tc.price, tc.exp)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	_, err := dexunits.QuoteAmount("x", "1", 6)
	assert.Error(t, err)
}
