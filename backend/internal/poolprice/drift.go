package poolprice

import (
	sdkmath "cosmossdk.io/math"
)

// Spot is the pool's price in chain units, quote reserve over base reserve,
// as the chain's poolPrice computes it. ok is false for a pool without a
// price (an empty or unknown reserve).
func Spot(reserveBase, reserveQuote sdkmath.Int) (price sdkmath.LegacyDec, ok bool) {
	if reserveBase.IsNil() || reserveQuote.IsNil() || !reserveBase.IsPositive() || !reserveQuote.IsPositive() {
		return sdkmath.LegacyDec{}, false
	}
	price = sdkmath.LegacyNewDecFromInt(reserveQuote).Quo(sdkmath.LegacyNewDecFromInt(reserveBase))
	if !price.IsPositive() {
		return sdkmath.LegacyDec{}, false
	}
	return price, true
}

// Drifted is the chain's poolPriceDrifted: the spot is further from avg than
// limit (relative to avg). Exactly at the limit is not drifted. A pool
// without a price (hasSpot false) or without a positive average counts as
// drifted.
func Drifted(spot sdkmath.LegacyDec, hasSpot bool, avg, limit sdkmath.LegacyDec) bool {
	if !hasSpot {
		return true
	}
	if avg.IsNil() || !avg.IsPositive() {
		return true
	}
	// |spot - avg| / avg > limit, written as a product so there is no division.
	return spot.Sub(avg).Abs().GT(limit.Mul(avg))
}

// DriftPct is |spot − avg| / avg in percent with one decimal ("12.3"); false
// when there is no price or the average is not positive.
func DriftPct(spot sdkmath.LegacyDec, hasSpot bool, avg sdkmath.LegacyDec) (string, bool) {
	if !hasSpot || avg.IsNil() || !avg.IsPositive() {
		return "", false
	}
	pct := spot.Sub(avg).Abs().MulInt64(100).Quo(avg)
	tenths := pct.MulInt64(10).Add(sdkmath.LegacyNewDecWithPrec(5, 1)).TruncateInt().Int64()
	return formatTenths(tenths), true
}

func formatTenths(n int64) string {
	whole, frac := n/10, n%10
	return itoa(whole) + "." + itoa(frac)
}

func itoa(n int64) string {
	return sdkmath.NewInt(n).String()
}
