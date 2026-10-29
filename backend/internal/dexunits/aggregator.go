package dexunits

import (
	"fmt"
	"math/big"

	sdkmath "cosmossdk.io/math"
)

// legacyDecimals is the precision of the chain's LegacyDec, which the
// aggregator's arithmetic rounds back to.
const legacyDecimals = 18

// TradePrice converts a trade's chain price (a LegacyDec string) to the
// display price the aggregator stores for it. When the quote has one or two
// decimals more than the base, the aggregator multiplies by 0.1 or 0.01 as a
// LegacyDec, which rounds back to 18 decimals: so does TradePrice. Otherwise
// it is Price: from three decimals more the aggregator's factor is 0 and
// Price's exact shift answers instead (BZE-133).
func TradePrice(price string, baseExp, quoteExp int) (out string, err error) {
	gap := baseExp - quoteExp
	if gap != -1 && gap != -2 {
		return Price(price, baseExp, quoteExp)
	}
	// LegacyDec panics past its 256 bits; such a price has no display value.
	defer func() {
		if r := recover(); r != nil {
			out, err = "", fmt.Errorf("price %q: %v", price, r)
		}
	}()
	d, err := sdkmath.LegacyNewDecFromStr(price)
	if err != nil {
		return "", fmt.Errorf("price %q: %w", price, err)
	}
	return TrimTrailingZeros(d.Mul(sdkmath.LegacyNewDecWithPrec(1, int64(-gap))).String()), nil
}

// QuoteAmount is the quote side of an order book trade as the aggregator
// computes it (converter.GetQuoteAmount): the display amount times the
// display price, as a LegacyDec rounded to 18 decimals, truncated to the
// quote's quoteExp decimals. Values a LegacyDec cannot hold (more than 18
// decimals, past 256 bits) are multiplied exactly instead.
func QuoteAmount(amount, price string, quoteExp int) (string, error) {
	if out, ok := legacyQuoteAmount(amount, price, quoteExp); ok {
		return out, nil
	}
	a, err := parse(amount)
	if err != nil {
		return "", fmt.Errorf("amount %q: %w", amount, err)
	}
	p, err := parse(price)
	if err != nil {
		return "", fmt.Errorf("price %q: %w", price, err)
	}
	total := decimal{unscaled: new(big.Int).Mul(a.unscaled, p.unscaled), scale: a.scale + p.scale}
	if cut := total.scale - quoteExp; cut > 0 {
		total.unscaled.Quo(total.unscaled, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(cut)), nil))
		total.scale = quoteExp
	}
	return total.String(), nil
}

func legacyQuoteAmount(amount, price string, quoteExp int) (out string, ok bool) {
	if quoteExp < 0 || quoteExp > legacyDecimals {
		return "", false
	}
	defer func() {
		if r := recover(); r != nil {
			out, ok = "", false
		}
	}()
	a, errA := sdkmath.LegacyNewDecFromStr(amount)
	p, errP := sdkmath.LegacyNewDecFromStr(price)
	if errA != nil || errP != nil {
		return "", false
	}
	scale := sdkmath.LegacyNewDecFromInt(sdkmath.NewIntWithDecimal(1, quoteExp))
	return TrimTrailingZeros(a.Mul(p).Mul(scale).TruncateDec().Quo(scale).String()), true
}
