// Package daotally reproduces the x/dao decision rule (chain v8.3.0,
// x/dao/types/tally.go) bar by bar, for the explorer's proposal checks. Every
// ratio is compared by cross-multiplying exact integers (a LegacyDec is an
// integer count of 10^-18), never with floats, so a check agrees with the
// chain at every boundary.
package daotally

import (
	"fmt"
	"math/big"

	"cosmossdk.io/math"
)

// Regular is the regular rule, applied at or after the voting end:
// (yes + no + abstain) / denominator ≥ min_quorum and yes / (yes + no) >
// threshold. A zero denominator, or no yes and no votes, rejects.
type Regular struct {
	QuorumMet    bool `json:"quorum_met"`
	ThresholdMet bool `json:"threshold_met"`
	Passes       bool `json:"passes"`
}

// Expedite is the early-settle rule of an expedited proposal, both bars
// against the full denominator: (yes + no + abstain) / denominator ≥
// expedite_quorum and yes / denominator > expedite_threshold, with yes > 0.
type Expedite struct {
	QuorumMet bool `json:"quorum_met"`
	YesMet    bool `json:"yes_met"`
	Passes    bool `json:"passes"`
}

// Tally is a proposal's vote count and the voting power it is measured
// against, as decimal integer strings.
type Tally struct {
	Denominator, Yes, No, Abstain string
}

type ints struct {
	den, yes, no, abstain *big.Int
}

func (t Tally) parse() (ints, error) {
	var out ints
	for _, f := range []struct {
		name string
		s    string
		dst  **big.Int
	}{{"denominator", t.Denominator, &out.den}, {"yes", t.Yes, &out.yes}, {"no", t.No, &out.no}, {"abstain", t.Abstain, &out.abstain}} {
		v, ok := new(big.Int).SetString(f.s, 10)
		if !ok {
			return ints{}, fmt.Errorf("daotally: %s %q is not an integer", f.name, f.s)
		}
		*f.dst = v
	}
	return out, nil
}

// ratio parses a LegacyDec string (e.g. "0.500000000000000000") into its
// 10^18-scaled integer.
func ratio(name, s string) (*big.Int, error) {
	d, err := math.LegacyNewDecFromStr(s)
	if err != nil {
		return nil, fmt.Errorf("daotally: %s %q: %w", name, s, err)
	}
	return d.BigInt(), nil
}

// compare compares part / whole with r (10^18-scaled) as part · 10^18
// against r · whole.
func compare(part, whole, r *big.Int) int {
	left := new(big.Int).Mul(part, math.LegacyOneDec().BigInt())
	right := new(big.Int).Mul(r, whole)
	return left.Cmp(right)
}

// CheckRegular evaluates the regular rule of t under minQuorum and threshold
// (LegacyDec strings).
func CheckRegular(minQuorum, threshold string, t Tally) (Regular, error) {
	q, err := ratio("min_quorum", minQuorum)
	if err != nil {
		return Regular{}, err
	}
	th, err := ratio("threshold", threshold)
	if err != nil {
		return Regular{}, err
	}
	n, err := t.parse()
	if err != nil {
		return Regular{}, err
	}
	decided := new(big.Int).Add(n.yes, n.no)
	total := new(big.Int).Add(decided, n.abstain)
	var out Regular
	out.QuorumMet = n.den.Sign() > 0 && compare(total, n.den, q) >= 0
	out.ThresholdMet = decided.Sign() > 0 && compare(n.yes, decided, th) > 0
	out.Passes = out.QuorumMet && out.ThresholdMet
	return out, nil
}

// CheckExpedite evaluates the expedite rule of t under quorum and threshold
// (the pinned expedite_quorum and expedite_threshold, LegacyDec strings).
func CheckExpedite(quorum, threshold string, t Tally) (Expedite, error) {
	q, err := ratio("expedite_quorum", quorum)
	if err != nil {
		return Expedite{}, err
	}
	th, err := ratio("expedite_threshold", threshold)
	if err != nil {
		return Expedite{}, err
	}
	n, err := t.parse()
	if err != nil {
		return Expedite{}, err
	}
	total := new(big.Int).Add(new(big.Int).Add(n.yes, n.no), n.abstain)
	var out Expedite
	out.QuorumMet = n.den.Sign() > 0 && compare(total, n.den, q) >= 0
	out.YesMet = n.den.Sign() > 0 && n.yes.Sign() > 0 && compare(n.yes, n.den, th) > 0
	out.Passes = out.QuorumMet && out.YesMet
	return out, nil
}
