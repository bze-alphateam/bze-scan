// Package dexunits converts the DEX's chain amounts and prices to display
// units, the way the aggregator-compatible routes answer them (orders,
// history, intervals, tickers): exact decimal arithmetic, trailing zeros
// trimmed as the aggregator trims them. A chain price is quote base units per
// base base unit; a display price is quote display units per base display
// unit.
//
// Unlike the aggregator, which multiplies by a %.2f-formatted power of ten
// (zero once the quote has three or more decimals more than the base), the
// conversion never loses a digit.
package dexunits

import (
	"fmt"
	"math/big"
	"strings"
)

// Price converts a chain price to the display price of a market whose base
// has baseExp decimals and quote quoteExp: price × 10^(baseExp − quoteExp).
func Price(price string, baseExp, quoteExp int) (string, error) {
	d, err := parse(price)
	if err != nil {
		return "", fmt.Errorf("price %q: %w", price, err)
	}
	return d.shift(baseExp - quoteExp).String(), nil
}

// Amount converts a chain amount (base units, an integer) of a denom with exp
// decimals to display units: amount × 10^−exp.
func Amount(amount string, exp int) (string, error) {
	d, err := parse(amount)
	if err != nil || d.scale != 0 {
		return "", fmt.Errorf("amount %q is not an integer", amount)
	}
	return d.shift(-exp).String(), nil
}

// Compare compares two decimal strings (chain prices) by value: −1, 0 or +1.
// An unparsable string sorts before every number.
func Compare(a, b string) int {
	da, errA := parse(a)
	db, errB := parse(b)
	switch {
	case errA != nil && errB != nil:
		return strings.Compare(a, b)
	case errA != nil:
		return -1
	case errB != nil:
		return 1
	}
	return da.rat().Cmp(db.rat())
}

// TrimTrailingZeros drops the trailing zeros of a decimal's fraction and the
// point when nothing is left of it ("1.500" → "1.5", "2.000" → "2"); an
// integer is returned as is. The aggregator's TrimAmountTrailingZeros.
func TrimTrailingZeros(amount string) string {
	if !strings.Contains(amount, ".") {
		return amount
	}
	return strings.TrimSuffix(strings.TrimRight(amount, "0"), ".")
}

// decimal is unscaled × 10^−scale, scale ≥ 0.
type decimal struct {
	unscaled *big.Int
	scale    int
}

// parse reads an unsigned or signed plain decimal ("12", "0.0006", "-1.5").
func parse(s string) (decimal, error) {
	digits, frac, _ := strings.Cut(s, ".")
	neg := strings.HasPrefix(digits, "-")
	digits = strings.TrimPrefix(digits, "-")
	if digits == "" && frac == "" || !allDigits(digits) || !allDigits(frac) || strings.Count(s, ".") > 1 {
		return decimal{}, fmt.Errorf("not a decimal")
	}
	u, ok := new(big.Int).SetString("0"+digits+frac, 10)
	if !ok {
		return decimal{}, fmt.Errorf("not a decimal")
	}
	if neg {
		u.Neg(u)
	}
	return decimal{unscaled: u, scale: len(frac)}, nil
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// shift multiplies d by 10^n.
func (d decimal) shift(n int) decimal {
	scale := d.scale - n
	if scale >= 0 {
		return decimal{unscaled: d.unscaled, scale: scale}
	}
	u := new(big.Int).Mul(d.unscaled, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-scale)), nil))
	return decimal{unscaled: u}
}

func (d decimal) rat() *big.Rat {
	den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d.scale)), nil)
	return new(big.Rat).SetFrac(d.unscaled, den)
}

// String writes d in plain notation, trailing zeros trimmed.
func (d decimal) String() string {
	digits := new(big.Int).Abs(d.unscaled).String()
	sign := ""
	if d.unscaled.Sign() < 0 {
		sign = "-"
	}
	if d.scale == 0 {
		return sign + digits
	}
	if len(digits) <= d.scale {
		digits = strings.Repeat("0", d.scale-len(digits)+1) + digits
	}
	cut := len(digits) - d.scale
	return sign + TrimTrailingZeros(digits[:cut]+"."+digits[cut:])
}
