package transform_test

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// The block-level activity of the burner, rewards, staking and epochs
// events, from the recorded mainnet heights. Each row's details are exactly
// the stored events naming its address.
func TestFixtureBlockLevelActivity(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	transformed := func(h int64) *transform.Entities {
		ents, err := tr.Transform(fetchInput(t, n, h))
		require.NoError(t, err)
		return ents
	}
	blockRows := func(ents *transform.Entities) []transform.Activity {
		var out []transform.Activity
		for _, r := range ents.Activity {
			if r.TxIndex == transform.BlockTxIndex {
				out = append(out, r)
			}
		}
		return out
	}
	eventTypes := func(ents *transform.Entities) []string {
		var out []string
		for _, e := range ents.BlockEvents {
			out = append(out, e.Type)
		}
		return out
	}
	blockTransfers := func(ents *transform.Entities) []transform.Transfer {
		var out []transform.Transfer
		for _, r := range ents.Transfers {
			if r.TxIndex == transform.BlockTxIndex {
				out = append(out, r)
			}
		}
		return out
	}
	// one checks the single block-level row of a height.
	one := func(t *testing.T, ents *transform.Entities, address, kind, category string, deltas []chain.Coin,
		event transform.Event) {
		t.Helper()
		rows := blockRows(ents)
		require.Len(t, rows, 1)
		r := rows[0]
		assert.Equal(t, address, r.Address)
		assert.Equal(t, kind, r.Kind)
		assert.Equal(t, category, r.Category)
		assert.False(t, r.IsSigner)
		assert.True(t, r.Success)
		assert.Empty(t, r.MsgTypes)
		if deltas == nil {
			deltas = []chain.Coin{}
		}
		assert.Equal(t, deltas, r.Deltas)
		assert.Equal(t, []transform.Event{event}, r.Details)
	}

	t.Run("raffle won: the winner's row with the prize, paid once (v8.0.x)", func(t *testing.T) {
		const winner = "bze1fdyfrv09slmg0q9c8hu334h5gdq4aexcgjghpf"
		ents := transformed(20267245)
		one(t, ents, winner, "raffle_won", "burner", []chain.Coin{{Denom: "ubze", Amount: "13100000000"}},
			transform.Event{Type: "bze.burner.RaffleWinnerEvent", Attrs: map[string]any{
				"amount": "13100000000", "denom": "ubze", "winner": winner, "mode": "EndBlock",
			}})
		require.Len(t, blockTransfers(ents), 1)
		assert.Equal(t, winner, blockTransfers(ents)[0].Recipient)
		assert.Equal(t, "13100000000", blockTransfers(ents)[0].Amount)
	})

	t.Run("raffle lost: the participant's row, no coins", func(t *testing.T) {
		const participant = "bze1z3mkcr2jz424w6m49frgjmy9uhlrx69phqwg3l"
		one(t, transformed(20329219), participant, "raffle_lost", "burner", nil,
			transform.Event{Type: "bze.burner.RaffleLostEvent", Attrs: map[string]any{
				"denom": "ubze", "participant": participant, "mode": "EndBlock",
			}})
	})

	t.Run("raffle finished: stored, the rest of the pot burnt, no address row", func(t *testing.T) {
		ents := transformed(20342600)
		assert.Contains(t, eventTypes(ents), "bze.burner.RaffleFinishedEvent")
		for _, r := range blockRows(ents) {
			assert.NotEqual(t, "raffle_finished", r.Kind)
		}
		burns := 0
		for _, tr := range blockTransfers(ents) {
			if tr.Kind == transform.TransferKindBurn {
				burns++
				assert.Equal(t, "51443750000", tr.Amount)
			}
		}
		assert.Equal(t, 1, burns)
	})

	t.Run("periodic burn: stored with one burn row per coin, no address row", func(t *testing.T) {
		ents := transformed(25006237)
		assert.Contains(t, eventTypes(ents), "bze.burner.CoinsBurnedEvent")
		var burns []string
		for _, tr := range blockTransfers(ents) {
			if tr.Kind == transform.TransferKindBurn {
				burns = append(burns, tr.Amount+tr.Denom)
			}
		}
		assert.Equal(t, []string{"1903489326factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uvdl", "3565085014ubze"}, burns)
		for _, r := range blockRows(ents) {
			assert.Equal(t, "dex_fill", r.Kind, "the block's only address rows are its DEX fill")
		}
	})

	t.Run("staking reward distribution: stored, no address row", func(t *testing.T) {
		ents := transformed(25035768)
		assert.Contains(t, eventTypes(ents), "bze.rewards.StakingRewardDistributionEvent")
		assert.Empty(t, blockRows(ents))
	})

	t.Run("epochs: stored, no address row", func(t *testing.T) {
		ents := transformed(25041911)
		assert.Equal(t, []string{"bze.epochs.EpochEndEvent", "bze.epochs.EpochStartEvent"}, eventTypes(ents))
		assert.Empty(t, blockRows(ents))
	})

	t.Run("unbonding completed: the delegator's row with the released coins", func(t *testing.T) {
		const delegator = "bze1k4flz542uu5qlw7t069lqn7enylku4ge65fd5z"
		one(t, transformed(24871232), delegator, "unbonding_completed", "staking",
			[]chain.Coin{{Denom: "ubze", Amount: "10000000"}},
			transform.Event{Type: "complete_unbonding", Attrs: map[string]any{
				"amount": "10000000ubze", "validator": "bzevaloper1k27v68x9gtppsgt9scr3649tpjfxnqljh3qa3e",
				"delegator": delegator, "mode": "EndBlock",
			}})
	})

	t.Run("redelegation completed: the delegator's row, no coins move", func(t *testing.T) {
		const delegator = "bze12wp3kzu0w0z3zt2n2a4vvhaaset2ytx9xuh9ju"
		one(t, transformed(24844265), delegator, "redelegation_completed", "staking", nil,
			transform.Event{Type: "complete_redelegation", Attrs: map[string]any{
				"amount": "1000000ubze", "delegator": delegator,
				"source_validator":      "bzevaloper10dcm7hqv9a5ajdjr9ctcu5pgg2969ag7qx0nyg",
				"destination_validator": "bzevaloper13snue6qplvfgk40gps54v84wgthj8ekfg5sl6q",
				"mode":                  "EndBlock",
			}})
	})
}

func attr(key, value string) node.Attribute { return node.Attribute{Key: key, Value: value} }

func coinReceived(receiver, amount string) node.Event {
	return node.Event{Type: "coin_received", Attributes: []node.Attribute{
		attr("receiver", receiver), attr("amount", amount), attr("mode", "BeginBlock"),
	}}
}

// Mainnet has never had a trading reward or a denom reward (v8.2.0): their
// block events are synthetic, in the chain's typed-event encoding.
func TestSyntheticRewardsBlockEvents(t *testing.T) {
	const (
		alice = "bze1fdyfrv09slmg0q9c8hu334h5gdq4aexcgjghpf"
		bob   = "bze1z3mkcr2jz424w6m49frgjmy9uhlrx69phqwg3l"
	)
	rewards := chain.ModuleAddress("rewards")
	tr, hook := mockTransformer(mockDecoder{})
	in := baseInput()
	in.Results.FinalizeBlockEvents = []node.Event{
		{Type: "bze.rewards.TradingRewardDistributionEvent", Attributes: []node.Attribute{
			attr("reward_id", `"7"`), attr("prize_amount", `"500"`), attr("prize_denom", `"ubze"`),
			attr("winners", `["`+alice+`","`+bob+`","`+rewards+`"]`), attr("mode", "BeginBlock"),
		}},
		coinReceived(alice, "500ubze"),
		coinReceived(bob, "500ubze"),
		{Type: "bze.rewards.TradingRewardExpireEvent", Attributes: []node.Attribute{
			attr("reward_id", `"8"`), attr("mode", "BeginBlock"),
		}},
		{Type: "bze.rewards.DenomRewardScheduleFinishEvent", Attributes: []node.Attribute{
			attr("schedule_id", `"3"`), attr("mode", "EndBlock"),
		}},
	}
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	assert.Empty(t, hook.AllEntries(), "every event is classified")

	var types []string
	for _, e := range ents.BlockEvents {
		types = append(types, e.Type)
	}
	assert.Equal(t, []string{
		"bze.rewards.TradingRewardDistributionEvent", "bze.rewards.TradingRewardExpireEvent",
		"bze.rewards.DenomRewardScheduleFinishEvent",
	}, types)

	require.Len(t, ents.Activity, 2, "one row per winner; the module account has none")
	won := transform.Event{Type: "bze.rewards.TradingRewardDistributionEvent", Attrs: map[string]any{
		"reward_id": "7", "prize_amount": "500", "prize_denom": "ubze",
		"winners": []any{alice, bob, rewards}, "mode": "BeginBlock",
	}}
	for _, r := range ents.Activity {
		assert.Contains(t, []string{alice, bob}, r.Address)
		assert.Equal(t, transform.BlockTxIndex, r.TxIndex)
		assert.Equal(t, "reward_won", r.Kind)
		assert.Equal(t, "rewards", r.Category)
		assert.Equal(t, []chain.Coin{{Denom: "ubze", Amount: "500"}}, r.Deltas)
		assert.Equal(t, []transform.Event{won}, r.Details)
	}
}

func TestUnclassifiedBZEBlockEventsWarn(t *testing.T) {
	tr, hook := mockTransformer(mockDecoder{})
	in := baseInput()
	in.Results.FinalizeBlockEvents = []node.Event{
		{Type: "bze.burner.FutureEvent", Attributes: []node.Attribute{attr("x", `"1"`)}},
		{Type: "bze.burner.FutureEvent", Attributes: []node.Attribute{attr("x", `"2"`)}},
		{Type: "bze.rewards.DenomRewardDistributionEvent", Attributes: []node.Attribute{attr("denom", `"ubze"`)}},
		{Type: "cosmos.future.Event", Attributes: []node.Attribute{attr("x", `"1"`)}},
		{Type: "mint", Attributes: []node.Attribute{attr("amount", "1")}},
	}
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	assert.Empty(t, ents.BlockEvents, "not stored")

	var warned []any
	for _, e := range hook.AllEntries() {
		assert.Equal(t, logrus.WarnLevel, e.Level)
		assert.Equal(t, int64(10), e.Data["height"])
		warned = append(warned, e.Data["event_type"])
	}
	assert.Equal(t, []any{"bze.burner.FutureEvent", "bze.rewards.DenomRewardDistributionEvent"}, warned,
		"once per BZE type; other modules' events are routine")
}
