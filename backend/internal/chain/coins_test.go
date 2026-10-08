package chain_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
)

func TestParseCoins(t *testing.T) {
	cases := map[string][]chain.Coin{
		"":          {},
		"70255ubze": {{Denom: "ubze", Amount: "70255"}},
		"70255ubze,12ibc/ED07A3391A112B175915CD8FAF43A2DA8E4790EDE12566649D0C2F97716B8518": {
			{Denom: "ubze", Amount: "70255"},
			{Denom: "ibc/ED07A3391A112B175915CD8FAF43A2DA8E4790EDE12566649D0C2F97716B8518", Amount: "12"},
		},
		"5factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uvdl": {
			{Denom: "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uvdl", Amount: "5"},
		},
		"1ulp_ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4_ubze": {
			{Denom: "ulp_ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4_ubze", Amount: "1"},
		},
		"340282366920938463463374607431768211456ubze": {{Denom: "ubze", Amount: "340282366920938463463374607431768211456"}},
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			got, err := chain.ParseCoins(in)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestParseCoinsRejectsMalformed(t *testing.T) {
	for _, in := range []string{"ubze", "100", "5ubze,", "5ubze,,3uatom", "-5ubze"} {
		_, err := chain.ParseCoins(in)
		assert.Error(t, err, in)
	}
}

func TestCoinsOf(t *testing.T) {
	assert.Equal(t, []chain.Coin{}, chain.CoinsOf(nil))
	assert.Equal(t,
		[]chain.Coin{{Denom: "uatom", Amount: "3"}, {Denom: "ubze", Amount: "1000000000000000000000"}},
		chain.CoinsOf(sdk.NewCoins(sdk.NewInt64Coin("uatom", 3), sdk.NewCoin("ubze", sdkmath.NewIntWithDecimal(1, 21)))))
}
