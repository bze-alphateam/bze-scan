package marketmerge_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/marketmerge"
)

// The mainnet value: BZE/USDC.inj folded into BZE/USDC.n.
const (
	usdcInj = "ubze/ibc/81DA528F4C5546208D1D50F84C2D0B388B45D37AA36FCA622A3CB2D8EF014838"
	usdcN   = "ubze/ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4"
)

func TestParseTheMainnetValue(t *testing.T) {
	m, err := marketmerge.Parse(usdcInj + "=" + usdcN)
	require.NoError(t, err)
	assert.Equal(t, []string{usdcInj}, m.FoldedInto(usdcN))
	assert.Empty(t, m.FoldedInto(usdcInj), "the new market reports nothing of its own")
	into, ok := m.ReportedAs(usdcInj)
	assert.True(t, ok)
	assert.Equal(t, usdcN, into)
	_, ok = m.ReportedAs(usdcN)
	assert.False(t, ok)
}

func TestFoldedIntoIsSorted(t *testing.T) {
	m, err := marketmerge.Parse("c/q=a/q,b/q=a/q,x/q=y/q")
	require.NoError(t, err)
	assert.Equal(t, []string{"b/q", "c/q"}, m.FoldedInto("a/q"))
}

func TestEmptyMergesNothing(t *testing.T) {
	m, err := marketmerge.Parse("")
	require.NoError(t, err)
	assert.Empty(t, m.FoldedInto(usdcN))
	_, ok := m.ReportedAs(usdcInj)
	assert.False(t, ok)
}

func TestParseRejects(t *testing.T) {
	for _, s := range []string{"a/q", "a=b=c", "=b", "a=", "a=b,", "a=a", "a=b,b=c"} {
		_, err := marketmerge.Parse(s)
		assert.Error(t, err, s)
	}
}
