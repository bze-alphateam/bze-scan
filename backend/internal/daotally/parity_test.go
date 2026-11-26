package daotally_test

import (
	"math/big"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/daotally"
)

// The chain's own tally table (x/dao/types/tally_test.go at v8.3.0-rc1),
// case by case, through this package: the explorer's checks decide every
// tally exactly as the chain does.

const (
	half = "0.5"
	// sevenThirteenths is 7/13 truncated to 18 decimals, as the chain's
	// LegacyDec QuoTruncate computes it.
	sevenThirteenths = "0.538461538461538461"
	// beyond10e36 is 10^36: stake tallies reach 10^30 and beyond.
	beyond10e36 = "1000000000000000000000000000000000000"
)

func n(v int64) string { return strconv.FormatInt(v, 10) }

func regular(t *testing.T, quorum, threshold string, den, yes, no, abstain string) bool {
	t.Helper()
	got, err := daotally.CheckRegular(quorum, threshold, daotally.Tally{Denominator: den, Yes: yes, No: no, Abstain: abstain})
	require.NoError(t, err)
	return got.Passes
}

func expedite(t *testing.T, quorum, threshold string, den, yes, no, abstain string) bool {
	t.Helper()
	got, err := daotally.CheckExpedite(quorum, threshold, daotally.Tally{Denominator: den, Yes: yes, No: no, Abstain: abstain})
	require.NoError(t, err)
	return got.Passes
}

type row struct {
	desc                          string
	quorum, threshold             string
	denominator, yes, no, abstain int64
	want                          bool
}

func checkRows(t *testing.T, check func(*testing.T, string, string, string, string, string, string) bool, rows []row) {
	t.Helper()
	for _, r := range rows {
		t.Run(r.desc, func(t *testing.T) {
			assert.Equal(t, r.want, check(t, r.quorum, r.threshold, n(r.denominator), n(r.yes), n(r.no), n(r.abstain)))
		})
	}
}

// D16: no yes and no no votes, or an empty denominator, never pass.
func TestParityD16NoDecidedVotes(t *testing.T) {
	checkRows(t, regular, []row{
		{"abstentions only", half, half, 10, 0, 0, 10, false},
		{"nobody voted", half, half, 10, 0, 0, 0, false},
		{"empty denominator", half, half, 0, 0, 0, 0, false},
		{"zero bars, empty denominator", "0", "0", 0, 0, 0, 0, false},
		{"yes votes over an empty denominator", half, half, 0, 3, 0, 0, false},
	})
}

// D17: the boundaries at 7/13, a full quorum, and majority of one vs tie.
func TestParityD17Boundaries(t *testing.T) {
	checkRows(t, regular, []row{
		{"7 yes 6 no of 13 at 7/13", sevenThirteenths, sevenThirteenths, 13, 7, 6, 0, true},
		{"6 yes 7 no: one below the threshold", sevenThirteenths, sevenThirteenths, 13, 6, 7, 0, false},
		{"6 of 13: one below the quorum", sevenThirteenths, half, 13, 6, 0, 0, false},
		{"1 of 1", "1", half, 1, 1, 0, 0, true},
		{"1 no of 1", "1", half, 1, 0, 1, 0, false},
		{"1 of 2 misses a full quorum", "1", half, 2, 1, 0, 0, false},
	})
	for count := int64(1); count <= 50_000; count++ {
		if !regular(t, half, half, n(2*count+1), n(count+1), n(count), "0") {
			t.Fatalf("%d yes against %d no at 50%% must pass", count+1, count)
		}
		if regular(t, half, half, n(2*count), n(count), n(count), "0") {
			t.Fatalf("a %d/%d tie at 50%% must not pass", count, count)
		}
	}
}

// The threshold is strict: a tie fails at 0.5, yes exactly at the threshold
// fails, one unit more passes.
func TestParityThresholdIsStrict(t *testing.T) {
	checkRows(t, regular, []row{
		{"1/1 tie", half, half, 2, 1, 1, 0, false},
		{"50/50 tie", half, half, 100, 50, 50, 0, false},
		{"51/49", half, half, 100, 51, 49, 0, true},
		{"yes exactly at 60%", half, "0.6", 10, 6, 4, 0, false},
		{"yes one unit above 60%", half, "0.6", 100, 61, 39, 0, true},
	})
}

// Quorum 0.4 (abstentions count, met at equality), threshold 0.6 (yes over
// yes + no, must be exceeded).
func TestParityQuorumThresholdMatrix(t *testing.T) {
	const q, th = "0.4", "0.6"
	checkRows(t, regular, []row{
		{"quorum met exactly, threshold exceeded", q, th, 100, 25, 15, 0, true},
		{"threshold met exactly", q, th, 100, 24, 16, 0, false},
		{"quorum one short", q, th, 100, 24, 15, 0, false},
		{"abstain fills the quorum", q, th, 100, 4, 2, 34, true},
		{"abstain does not move the threshold", q, th, 100, 2, 3, 95, false},
		{"unanimous over quorum", q, th, 100, 40, 0, 0, true},
		{"unanimous under quorum", q, th, 100, 39, 0, 0, false},
		{"full turnout, all no", q, th, 100, 0, 100, 0, false},
	})
}

// The expedite yes bar is against the full denominator and must be
// exceeded; the quorum is met at equality.
func TestParityExpediteBars(t *testing.T) {
	const q, th = "0.6", "0.66"
	checkRows(t, expedite, []row{
		{"yes one unit above the bar", q, th, 150, 100, 0, 0, true},
		{"yes exactly at the bar", q, th, 150, 99, 0, 0, false},
		{"no votes do not lower the yes bar", q, th, 150, 99, 51, 0, false},
		{"no votes do not block a met yes bar", q, th, 150, 100, 50, 0, true},
		{"empty denominator", q, th, 0, 0, 0, 0, false},
		{"nobody voted", q, th, 150, 0, 0, 0, false},
		{"0.9 quorum one short", "0.9", th, 150, 100, 0, 34, false},
		{"0.9 quorum filled by abstain", "0.9", th, 150, 100, 0, 35, true},
	})
}

// Stakes beyond 10^36 never overflow either rule.
func TestParityLargeStake(t *testing.T) {
	total, ok := new(big.Int).SetString(beyond10e36, 10)
	require.True(t, ok)
	halfOf := new(big.Int).Quo(total, big.NewInt(2))
	plus := new(big.Int).Add(halfOf, big.NewInt(1)).String()
	minus := new(big.Int).Sub(halfOf, big.NewInt(1)).String()
	assert.True(t, regular(t, half, half, beyond10e36, plus, minus, "0"))
	assert.False(t, regular(t, half, half, beyond10e36, halfOf.String(), halfOf.String(), "0"))
	assert.True(t, expedite(t, half, half, beyond10e36, plus, "0", "0"))
	assert.False(t, expedite(t, half, half, beyond10e36, halfOf.String(), "0", "0"))
}

// D7: a tally that meets the expedite bars still passes the regular ones
// after every remaining unit of voting power votes no, for any expedite
// config at or above the regular one.
func TestParityD7NoUnitCanOverturn(t *testing.T) {
	configs := []struct{ quorum, threshold, expediteQuorum, expediteThreshold string }{
		{"0.4", "0.5", "0.6", "0.66"},
		{"0.5", "0.5", "0.5", "0.5"},
		{"0.1", "0.9", "0.1", "0.9"},
		{"1", "0.99", "1", "0.99"},
	}
	for _, c := range configs {
		for den := int64(1); den <= 40; den++ {
			for yes := int64(0); yes <= den; yes++ {
				for no := int64(0); yes+no <= den; no++ {
					for abstain := int64(0); yes+no+abstain <= den; abstain++ {
						if !expedite(t, c.expediteQuorum, c.expediteThreshold, n(den), n(yes), n(no), n(abstain)) {
							continue
						}
						rest := den - yes - no - abstain
						if !regular(t, c.quorum, c.threshold, n(den), n(yes), n(no+rest), n(abstain)) {
							t.Fatalf("%+v: D %d, %d/%d/%d passed early but %d more no votes reject it", c, den, yes, no, abstain, rest)
						}
					}
				}
			}
		}
	}
}
