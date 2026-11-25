package denoms_test

import (
	"context"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tokenfactorytypes "github.com/bze-alphateam/bze/x/tokenfactory/types"
)

const factoryWithAdmin = vdl

func TestFactorySettingsAreStored(t *testing.T) {
	set, n, st := newSet(t)
	n.mintable = map[string]bool{factoryWithAdmin: true}
	n.configs = map[string]tokenfactorytypes.DenomConfig{factoryWithAdmin: {
		CommunityPool: "bze1pool",
		TransferFee:   sdkmath.LegacyNewDecWithPrec(1, 2),
		GasSurcharge:  sdkmath.LegacyNewDecWithPrec(5, 1),
	}}

	require.NoError(t, set.ResyncOne(context.Background(), factoryWithAdmin))
	d := st.last(t).Denoms[0]
	require.NotNil(t, d.Mintable)
	assert.True(t, *d.Mintable)
	require.NotNil(t, d.Config)
	assert.Equal(t, "bze1pool", d.Config.CommunityPool)
	assert.Equal(t, "0.010000000000000000", d.Config.TransferFee)
	assert.Equal(t, "0.500000000000000000", d.Config.GasSurcharge)
}

func TestFactoryDefaultsAreAnEmptyConfigAndMintingOff(t *testing.T) {
	set, _, st := newSet(t)
	require.NoError(t, set.ResyncOne(context.Background(), factoryWithAdmin))
	d := st.last(t).Denoms[0]
	require.NotNil(t, d.Mintable)
	assert.False(t, *d.Mintable, "a v8.3.0 authority that says minting is off")
	require.NotNil(t, d.Config)
	assert.Empty(t, d.Config.CommunityPool)
	assert.Equal(t, "0.000000000000000000", d.Config.TransferFee)
}

func TestAV82NodeLeavesTheSettingsUnknown(t *testing.T) {
	set, n, st := newSet(t)
	n.noConfig = true
	require.NoError(t, set.FullResync(context.Background()))
	for _, d := range st.last(t).Denoms {
		assert.Nil(t, d.Mintable, d.Denom)
		assert.Nil(t, d.Config, d.Denom)
	}
	assert.Equal(t, 1, n.callsOf("DenomConfig"), "probed once per run")

	n.noConfig = false
	require.NoError(t, set.FullResync(context.Background()))
	assert.Greater(t, n.callsOf("DenomConfig"), 1, "the next run asks again")
}

func TestNonFactoryDenomsAskForNoSettings(t *testing.T) {
	set, n, st := newSet(t)
	require.NoError(t, set.ResyncOne(context.Background(), "ubze"))
	d := st.last(t).Denoms[0]
	assert.Nil(t, d.Mintable)
	assert.Nil(t, d.Config)
	assert.Zero(t, n.callsOf("DenomConfig"))
}

func TestADenomWithoutAnAuthorityKeepsItsRowAndAsksForNoConfig(t *testing.T) {
	set, n, st := newSet(t)
	n.noAuthority = true
	require.NoError(t, set.ResyncOne(context.Background(), factoryWithAdmin))
	d := st.last(t).Denoms[0]
	assert.Empty(t, d.Admin)
	assert.Nil(t, d.Mintable)
	assert.Nil(t, d.Config)
	assert.Equal(t, 1, n.callsOf("DenomAuthority"))
	assert.Zero(t, n.callsOf("DenomConfig"), "NotFound authority: DenomConfig is never asked")
}
