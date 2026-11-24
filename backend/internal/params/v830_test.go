package params_test

import (
	"encoding/json"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	burnertypes "github.com/bze-alphateam/bze/x/burner/types"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/params"
)

// The upgraded snapshot is exactly the JSON the v8.3.0 types render for the
// v8.2.x answer: the same values under their new names, the new fields at
// their zero values.
func TestAStoredV82SnapshotBecomesWhatTheV830TypesRender(t *testing.T) {
	codec, err := chain.NewCodec()
	require.NoError(t, err)

	storedTradebin := `{"createMarketFee":{"denom":"ubze","amount":"25000000000"},"marketMakerFee":{"denom":"ubze","amount":"1000"},
		"marketTakerFee":{"denom":"ubze","amount":"100000"},"makerFeeDestination":"burner","takerFeeDestination":"burner",
		"native_denom":"ubze","orderBookExtraGasWindow":"100","orderBookQueueExtraGas":"25000","fillOrdersExtraGas":"5000",
		"minNativeLiquidityForModuleSwap":"100000000000","orderBookPerBlockMessages":"500","orderBookQueueMessageScanExtraGas":"5000"}`
	tradebin := tradebintypes.QueryParamsResponse{}.Params // v8.3.0 type
	tradebin.CreateMarketFee.Denom, tradebin.CreateMarketFee.Amount = "ubze", sdkmath.NewInt(25000000000)
	tradebin.MarketMakerFee.Denom, tradebin.MarketMakerFee.Amount = "ubze", sdkmath.NewInt(1000)
	tradebin.MarketTakerFee.Denom, tradebin.MarketTakerFee.Amount = "ubze", sdkmath.NewInt(100000)
	tradebin.MakerFeeDestination, tradebin.TakerFeeDestination, tradebin.NativeDenom = "burner", "burner", "ubze"
	tradebin.PriceLevelOwnersWindow, tradebin.PriceLevelExtraGas, tradebin.FillOrdersExtraGas = 100, 25000, 5000
	tradebin.MinNativeLiquidityForModuleSwap = sdkmath.NewInt(100000000000)
	tradebin.MaxFillsPerOrder = 500
	tradebin.FeeMaxPriceDrift, tradebin.ModuleMaxPriceDrift = sdkmath.LegacyZeroDec(), sdkmath.LegacyZeroDec()
	want, err := codec.ProtoJSON(&tradebin)
	require.NoError(t, err)

	got, changed, err := params.UpgradeStoredToV830("tradebin", json.RawMessage(storedTradebin))
	require.NoError(t, err)
	assert.True(t, changed)
	assert.JSONEq(t, string(want), string(got))

	burner := burnertypes.Params{PeriodicBurningWeeks: 4, IbcMoveMaxReserveRatio: sdkmath.LegacyZeroDec()}
	want, err = codec.ProtoJSON(&burner)
	require.NoError(t, err)
	got, changed, err = params.UpgradeStoredToV830("burner", json.RawMessage(`{"periodic_burning_weeks":"4"}`))
	require.NoError(t, err)
	assert.True(t, changed)
	assert.JSONEq(t, string(want), string(got))
}

func TestUpgradingTwiceOrAnotherModuleChangesNothing(t *testing.T) {
	once, _, err := params.UpgradeStoredToV830("burner", json.RawMessage(`{"periodic_burning_weeks":"4"}`))
	require.NoError(t, err)
	again, changed, err := params.UpgradeStoredToV830("burner", once)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.JSONEq(t, string(once), string(again))

	staking := json.RawMessage(`{"max_validators":40}`)
	out, changed, err := params.UpgradeStoredToV830("staking", staking)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.JSONEq(t, string(staking), string(out))

	_, _, err = params.UpgradeStoredToV830("tradebin", json.RawMessage(`not json`))
	assert.Error(t, err)
}
