package daotally_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/daotally"
)

func tally(den, yes, no, abstain string) daotally.Tally {
	return daotally.Tally{Denominator: den, Yes: yes, No: no, Abstain: abstain}
}

func ints(t daotally.Tally) (den, yes, no, abstain math.Int) {
	p := func(s string) math.Int { v, _ := math.NewIntFromString(s); return v }
	return p(t.Denominator), p(t.Yes), p(t.No), p(t.Abstain)
}

func TestRegularRuleAtItsBoundaries(t *testing.T) {
	const quorum, threshold = "0.334000000000000000", "0.500000000000000000"
	cases := []struct {
		name string
		t    daotally.Tally
		want daotally.Regular
	}{
		// 334 of 1000 voted: exactly the quorum, which is met (≥).
		{"quorum exactly met", tally("1000", "300", "34", "0"), daotally.Regular{QuorumMet: true, ThresholdMet: true, Passes: true}},
		{"quorum one short", tally("1000", "300", "33", "0"), daotally.Regular{QuorumMet: false, ThresholdMet: true}},
		// yes / (yes + no) exactly at the threshold fails (strictly above).
		{"tie at 0.5", tally("10", "3", "3", "0"), daotally.Regular{QuorumMet: true}},
		{"one yes above the tie", tally("10", "4", "3", "0"), daotally.Regular{QuorumMet: true, ThresholdMet: true, Passes: true}},
		{"zero denominator", tally("0", "5", "0", "0"), daotally.Regular{ThresholdMet: true}},
		{"no yes or no votes", tally("10", "0", "0", "10"), daotally.Regular{QuorumMet: true}},
		{"nobody voted", tally("10", "0", "0", "0"), daotally.Regular{}},
		{"abstain counts for the quorum only", tally("100", "20", "10", "10"), daotally.Regular{QuorumMet: true, ThresholdMet: true, Passes: true}},
		{"amounts beyond int64", tally("100000000000000000000000000", "60000000000000000000000000", "1", "0"),
			daotally.Regular{QuorumMet: true, ThresholdMet: true, Passes: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := daotally.CheckRegular(quorum, threshold, c.t)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
			den, yes, no, abstain := ints(c.t)
			assert.Equal(t, daotypes.TallyPasses(math.LegacyMustNewDecFromStr(quorum), math.LegacyMustNewDecFromStr(threshold),
				den, yes, no, abstain), got.Passes, "agrees with the chain")
		})
	}
}

func TestThresholdExactlyMetFails(t *testing.T) {
	// 2/3 threshold, yes / (yes + no) = 2/3 exactly.
	got, err := daotally.CheckRegular("0", "0.666666666666666666", tally("3", "2", "1", "0"))
	require.NoError(t, err)
	assert.True(t, got.ThresholdMet, "2/3 is above 0.666…666")
	got, err = daotally.CheckRegular("0", "0.750000000000000000", tally("4", "3", "1", "0"))
	require.NoError(t, err)
	assert.False(t, got.ThresholdMet, "exactly at 0.75 fails")
	assert.False(t, got.Passes)
}

func TestExpediteRuleAgainstTheFullDenominator(t *testing.T) {
	const quorum, threshold = "0.500000000000000000", "0.667000000000000000"
	cases := []struct {
		name string
		t    daotally.Tally
		want daotally.Expedite
	}{
		{"both bars met", tally("1000", "668", "0", "0"), daotally.Expedite{QuorumMet: true, YesMet: true, Passes: true}},
		// 667 / 1000 is exactly the expedite threshold: fails (strictly above).
		{"yes exactly at the bar", tally("1000", "667", "0", "0"), daotally.Expedite{QuorumMet: true}},
		// yes / (yes + no) would be 100 %, but the bar is against the denominator.
		{"yes share of the votes is not enough", tally("1000", "600", "0", "0"), daotally.Expedite{QuorumMet: true}},
		{"quorum exactly met", tally("1000", "0", "500", "0"), daotally.Expedite{QuorumMet: true}},
		{"quorum short", tally("1000", "499", "0", "0"), daotally.Expedite{}},
		{"zero denominator", tally("0", "1", "0", "0"), daotally.Expedite{}},
		{"no yes", tally("10", "0", "0", "10"), daotally.Expedite{QuorumMet: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := daotally.CheckExpedite(quorum, threshold, c.t)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
			den, yes, no, abstain := ints(c.t)
			assert.Equal(t, daotypes.ExpediteTallyPasses(math.LegacyMustNewDecFromStr(quorum), math.LegacyMustNewDecFromStr(threshold),
				den, yes, no, abstain), got.Passes, "agrees with the chain")
		})
	}
}

func TestBadInputsAreErrors(t *testing.T) {
	_, err := daotally.CheckRegular("half", "0.5", tally("1", "1", "0", "0"))
	assert.Error(t, err)
	_, err = daotally.CheckRegular("0.5", "0.5", tally("x", "1", "0", "0"))
	assert.Error(t, err)
	_, err = daotally.CheckExpedite("0.5", "nope", tally("1", "1", "0", "0"))
	assert.Error(t, err)
}
