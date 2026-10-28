// Package marketmerge folds DEX markets into a market CoinGecko already
// tracks, for the aggregator-compatible routes' coingecko format only
// (COINGECKO_MERGED_MARKETS). When a market's quote asset is replaced
// (USDC.n by USDC.inj), the new market's figures are reported under the old
// market's ticker id, so CoinGecko keeps reading the ticker it knows. The
// default format keeps every market separate.
package marketmerge

import (
	"fmt"
	"slices"
	"strings"
)

// Merges maps folded (new) market ids to the reported (tracked) ones. The
// zero value merges nothing.
type Merges map[string]string

// New validates folded → reported market ids: a market is not folded into
// itself, and a reported market is not folded into another one.
func New(folded map[string]string) (Merges, error) {
	m := make(Merges, len(folded))
	for from, into := range folded {
		if from == into {
			return nil, fmt.Errorf("market %s cannot be folded into itself", from)
		}
		if _, ok := folded[into]; ok {
			return nil, fmt.Errorf("market %s is folded into %s, which is folded itself", from, into)
		}
		m[from] = into
	}
	return m, nil
}

// Parse reads the COINGECKO_MERGED_MARKETS format,
// "<new market>=<tracked market>,…", the aggregator's: every pair has
// exactly one "=" and two non-empty sides. Empty is nil, which merges
// nothing.
func Parse(s string) (Merges, error) {
	if s == "" {
		return nil, nil
	}
	folded := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		parts := strings.Split(pair, "=")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("unknown format %q: want <new market>=<tracked market>,…", s)
		}
		folded[parts[0]] = parts[1]
	}
	return New(folded)
}

// FoldedInto returns the markets folded into reported, sorted.
func (m Merges) FoldedInto(reported string) []string {
	var folded []string
	for from, into := range m {
		if into == reported {
			folded = append(folded, from)
		}
	}
	slices.Sort(folded)
	return folded
}

// ReportedAs returns the market folded reports under, and whether it is
// folded at all.
func (m Merges) ReportedAs(folded string) (string, bool) {
	into, ok := m[folded]
	return into, ok
}
