package dto_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// mainnetState is chain_state as the sync writes it, with mainnet numbers.
func mainnetState() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"staking_pool":     json.RawMessage(`{"bonded_tokens":"147027371221650","not_bonded_tokens":"6018644775575"}`),
		"supply":           json.RawMessage(`{"denom":"ubze","amount":"281824500438386"}`),
		"mint":             json.RawMessage(`{"inflation":"0.050000000000000000","annual_provisions":"14092966046876.35","params":{}}`),
		"chain_params":     json.RawMessage(`{"max_validators":40,"unbonding_time_s":1814400,"community_tax":"0.050000000000000000"}`),
		"community_pool":   json.RawMessage(`[{"denom":"factory/x/2MARS","amount":"581.7"},{"denom":"ubze","amount":"34939146569152.357342022929412238"}]`),
		"validator_counts": json.RawMessage(`{"bonded":22,"jailed":3,"total":31}`),
		"price_ubze":       json.RawMessage(`{"price_usd":"0.00016806","price_change_24h_pct":"-2.1362"}`),
	}
}

func i64(v int64) *int64 { return &v }

func TestStatsComputeTheRatiosFromChainState(t *testing.T) {
	at := time.Date(2026, 10, 10, 9, 0, 0, 0, time.FixedZone("x", 3600))
	s := dto.NewStats(&repository.BlockStats{LatestHeight: i64(25000000), LatestTime: &at, AvgBlockTimeMs: i64(5650), Txs24h: 812},
		mainnetState(), "ubze")

	assert.Equal(t, i64(25000000), s.LatestHeight)
	assert.Equal(t, at.UTC(), *s.LatestTime)
	assert.Equal(t, strp("147027371221650"), s.BondedTokens)
	assert.Equal(t, strp("52.16983"), s.StakedShare, "bonded over supply")
	assert.Equal(t, strp("5.00000"), s.Inflation)
	assert.Equal(t, strp("9.10488"), s.RewardRate, "5% × (1 − 5%) ÷ 52.16983%")
	assert.Equal(t, &dto.ValidatorCounts{Active: 22, Total: 31}, s.Validators)
	assert.Equal(t, i64(40), s.MaxValidators)
	assert.Equal(t, i64(1814400), s.UnbondingPeriod)
	assert.Equal(t, strp("34939146569152"), s.CommunityPool, "the bond denom's whole base units")
	assert.Equal(t, strp("281824500438386"), s.Supply)
	assert.Equal(t, strp("0.00016806"), s.PriceUSD)
	assert.Equal(t, strp("-2.1362"), s.PriceChange24hPct)

	raw, err := json.Marshal(s)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"daily":[]`)
}

func TestStatsBeforeAnythingIsSynced(t *testing.T) {
	s := dto.NewStats(&repository.BlockStats{}, map[string]json.RawMessage{
		"price_ubze": json.RawMessage(`{"price_usd":null,"price_change_24h_pct":null}`),
		"mint":       json.RawMessage(`not json`),
	}, "ubze")
	raw, err := json.Marshal(s)
	require.NoError(t, err)
	assert.JSONEq(t, `{"latest_height":null,"latest_time":null,"avg_block_time_ms":null,"txs_24h":0,
		"bonded_tokens":null,"staked_share":null,"inflation":null,"reward_rate":null,"validators":null,
		"max_validators":null,"price_usd":null,"price_change_24h_pct":null,"unbonding_period":null,
		"community_pool":null,"supply":null,"daily":[]}`, string(raw))
}

func TestRewardRateNeedsEveryInput(t *testing.T) {
	for _, drop := range []string{"staking_pool", "supply", "mint", "chain_params"} {
		state := mainnetState()
		delete(state, drop)
		assert.Nil(t, dto.NewStats(&repository.BlockStats{}, state, "ubze").RewardRate, drop)
	}
	state := mainnetState()
	state["staking_pool"] = json.RawMessage(`{"bonded_tokens":"0","not_bonded_tokens":"0"}`)
	s := dto.NewStats(&repository.BlockStats{}, state, "ubze")
	assert.Nil(t, s.RewardRate, "nothing bonded")
	assert.Equal(t, strp("0.00000"), s.StakedShare)

	state = mainnetState()
	state["community_pool"] = json.RawMessage(`[]`)
	assert.Equal(t, strp("0"), dto.NewStats(&repository.BlockStats{}, state, "ubze").CommunityPool)
}

func TestValidatorsSummary(t *testing.T) {
	s := dto.NewValidatorsSummary(mainnetState(), "58810948488660", "ubze")
	assert.Equal(t, &dto.ValidatorsSummary{
		BondedTokens: strp("147027371221650"), StakedShare: strp("52.16983"), RewardRate: strp("9.10488"),
		UnbondingPeriod: i64(1814400), Top5Share: strp("40.00000"), MaxValidators: i64(40),
	}, s)

	empty := dto.NewValidatorsSummary(map[string]json.RawMessage{}, "0", "ubze")
	assert.Equal(t, &dto.ValidatorsSummary{}, empty)
}
