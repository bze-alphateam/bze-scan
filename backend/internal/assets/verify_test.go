package assets_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/assets"
)

const (
	spoofOSMO   = "ibc/SPOOFEDOSMO"
	dupUSDC     = "ibc/DUPLICATEUSDC"
	directATONE = "ibc/DIRECTATONE"
)

// channel returns tb's channel id, for a test to change it.
func channel(t *testing.T, tb *assets.Tables, id string) *assets.Channel {
	t.Helper()
	for i := range tb.Channels {
		if tb.Channels[i].ChannelID == id {
			return &tb.Channels[i]
		}
	}
	t.Fatalf("no channel %s", id)
	return nil
}

func routeChannels(a assets.Asset) []string {
	out := []string{}
	for _, r := range a.Routes {
		out = append(out, r.ChannelID)
	}
	return out
}

// A chain that calls itself Osmosis over a channel no check confirmed sends
// a coin whose trace says uosmo: it is not served as OSMO.
func TestAnUnconfirmedChannelsCoinGetsTheUnknownTreatment(t *testing.T) {
	tb := tables()
	tb.Channels = append(tb.Channels, assets.Channel{ChannelID: "channel-5", PortID: "transfer",
		CounterpartyChainID: "osmosis-1", CounterpartyChannelID: "channel-66", State: "open", ClientStatus: "active"})
	tb.Denoms = append(tb.Denoms, assets.Denom{Denom: spoofOSMO, Kind: "ibc", IBCPath: "transfer/channel-5",
		IBCBaseDenom: "uosmo", OriginChainID: "osmosis-1"})
	tb.Assets = append(tb.Assets, assets.RegistryAsset{ChainID: "osmosis-1", Base: "uosmo", Name: "Osmosis",
		Symbol: "OSMO", Display: "osmo", Exponent: exp(6), Logos: svg("https://r/osmo")})
	got, _ := built(t, tb, assets.Curation{})

	s := got[spoofOSMO]
	assert.Equal(t, "uosmo", s.Name, "the raw base denom")
	assert.Equal(t, "ibc/…SMO", s.Ticker)
	assert.Zero(t, s.Decimals)
	assert.Nil(t, s.Logo)
	assert.Nil(t, s.Description)
	assert.False(t, s.Verified)
	assert.False(t, s.DepositAllowed)
	assert.False(t, s.WithdrawAllowed, "nowhere to route it")
	require.NotNil(t, s.IBC)
	assert.False(t, s.IBC.ChannelConfirmed)
	assert.Equal(t, "channel-5", s.IBC.ChannelID)
	assert.Nil(t, s.IBC.Origin)
	cp := s.IBC.Counterparty
	assert.Equal(t, assets.UnknownChain, cp.PrettyName)
	assert.Nil(t, cp.ChainName)
	assert.Nil(t, cp.Logo)
	assert.Nil(t, cp.Bech32Prefix)
	assert.Equal(t, []string{}, cp.REST)
	assert.Equal(t, []string{}, cp.RPC)
	assert.Equal(t, "osmosis-1", cp.ChainID, "the light client's claim, as it is")
	assert.Equal(t, "channel-66", cp.ChannelID)
	assert.Equal(t, "uosmo", cp.BaseDenom)
	assert.Equal(t, []string{"channel-0", "channel-3"}, routeChannels(got["ubze"]), "no route over it")
}

// Of a chain's confirmed channels the oldest usable one is canonical; a
// newer duplicate's coin is labelled but not verified, and routes go over
// the canonical channel only.
func TestCanonicalChannelPerChain(t *testing.T) {
	tb := tables()
	tb.Channels = append(tb.Channels, assets.Channel{ChannelID: "channel-12", PortID: "transfer",
		CounterpartyChainID: "noble-1", CounterpartyChannelID: "channel-200", State: "open", ClientStatus: "active", Confirmed: true})
	tb.Denoms = append(tb.Denoms, assets.Denom{Denom: dupUSDC, Kind: "ibc", IBCPath: "transfer/channel-12",
		IBCBaseDenom: "uusdc", OriginChainID: "noble-1"})
	got, _ := built(t, tb, assets.Curation{})

	assert.True(t, got[usdc].Verified, "channel-3: the lowest usable channel (channel-1 is closed)")
	d := got[dupUSDC]
	assert.Equal(t, "USDC", d.Ticker, "labelled with its real asset")
	assert.Equal(t, str("noble"), d.IBC.Counterparty.ChainName, "and its real chain")
	assert.True(t, d.IBC.ChannelConfirmed)
	assert.False(t, d.Verified, "over a newer duplicate channel")
	assert.True(t, d.DepositAllowed)
	assert.True(t, d.WithdrawAllowed)
	assert.Equal(t, []string{"channel-0", "channel-3"}, routeChannels(got["ubze"]), "one route per chain")

	// channel-12 < channel-3 as strings, not as channel numbers.
	tb.Channels = append(tb.Channels, assets.Channel{ChannelID: "channel-10", PortID: "transfer",
		CounterpartyChainID: "noble-1", CounterpartyChannelID: "channel-201", State: "open", ClientStatus: "active", Confirmed: true})
	got, _ = built(t, tb, assets.Curation{})
	assert.True(t, got[usdc].Verified)
	assert.Equal(t, []string{"channel-0", "channel-3"}, routeChannels(got["ubze"]))

	// An older channel whose client expired is passed over while a newer one
	// is usable; open with an active client, it is canonical again.
	old := channel(t, tb, "channel-1")
	old.State, old.ClientStatus = "open", "expired"
	got, _ = built(t, tb, assets.Curation{})
	assert.True(t, got[usdc].Verified)
	old.ClientStatus = "active"
	got, _ = built(t, tb, assets.Curation{})
	assert.False(t, got[usdc].Verified, "channel-1 is canonical")
	assert.Equal(t, []string{"channel-0", "channel-1"}, routeChannels(got["ubze"]))

	// An unconfirmed older channel is never canonical.
	old.Confirmed = false
	got, _ = built(t, tb, assets.Curation{})
	assert.True(t, got[usdc].Verified)
}

// Each verified rule failing alone clears the flag of a coin that is
// verified otherwise.
func TestEachVerifiedRuleFailingAlone(t *testing.T) {
	sibling := func(tb *assets.Tables) {
		tb.Channels = append(tb.Channels, assets.Channel{ChannelID: "channel-7", PortID: "transfer",
			CounterpartyChainID: "atomone-1", CounterpartyChannelID: "channel-2", State: "open", ClientStatus: "active", Confirmed: true})
		tb.Denoms = append(tb.Denoms, assets.Denom{Denom: directATONE, Kind: "ibc", IBCPath: "transfer/channel-7",
			IBCBaseDenom: "uatone", OriginChainID: "atomone-1"})
	}
	for _, tc := range []struct {
		rule  string
		denom string
		fail  func(t *testing.T, tb *assets.Tables, cur *assets.Curation)
	}{
		{"1: first hop confirmed", usdc, func(t *testing.T, tb *assets.Tables, _ *assets.Curation) {
			channel(t, tb, "channel-3").Confirmed = false
			channel(t, tb, "channel-1").Confirmed = false
		}},
		{"2: one hop, the base denom in the counterparty's registry", usdc, func(_ *testing.T, tb *assets.Tables, _ *assets.Curation) {
			for i := range tb.Assets {
				if tb.Assets[i].ChainID == "noble-1" && tb.Assets[i].Base == "uusdc" {
					tb.Assets[i].Base = "uother"
				}
			}
		}},
		{"2: several hops, the traces lead to the origin", atone, func(_ *testing.T, tb *assets.Tables, _ *assets.Curation) {
			for i := range tb.Assets {
				if tb.Assets[i].ChainID == "osmosis-1" {
					tb.Assets[i].Traces = nil
				}
			}
		}},
		{"3: the canonical channel", usdc, func(t *testing.T, tb *assets.Tables, _ *assets.Curation) {
			c := channel(t, tb, "channel-1")
			c.State, c.ClientStatus = "open", "active"
		}},
		{"4: no direct sibling", atone, func(_ *testing.T, tb *assets.Tables, _ *assets.Curation) { sibling(tb) }},
		{"5: not halted", usdc, func(_ *testing.T, tb *assets.Tables, _ *assets.Curation) {
			for i := range tb.Denoms {
				if tb.Denoms[i].Denom == usdc {
					tb.Denoms[i].Halted = true
				}
			}
		}},
		{"6: not on the curation's unverified list", usdc, func(_ *testing.T, _ *assets.Tables, cur *assets.Curation) {
			cur.Unverified = []string{usdc}
		}},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			got, _ := built(t, tables(), assets.Curation{})
			require.True(t, got[tc.denom].Verified, "verified with every rule holding")
			tb, cur := tables(), assets.Curation{}
			tc.fail(t, tb, &cur)
			got, _ = built(t, tb, cur)
			assert.False(t, got[tc.denom].Verified)
		})
	}
}

// A two-hop coin stays depositable without a direct alternative and
// withdrawable always; with a direct sibling only the sibling is
// depositable.
func TestTwoHopDepositAndWithdraw(t *testing.T) {
	got, _ := built(t, tables(), assets.Curation{})
	assert.True(t, got[atone].DepositAllowed, "no direct ATONE")
	assert.True(t, got[atone].WithdrawAllowed)

	tb := tables()
	tb.Channels = append(tb.Channels, assets.Channel{ChannelID: "channel-7", PortID: "transfer",
		CounterpartyChainID: "atomone-1", CounterpartyChannelID: "channel-2", State: "open", ClientStatus: "active", Confirmed: true})
	tb.Denoms = append(tb.Denoms, assets.Denom{Denom: directATONE, Kind: "ibc", IBCPath: "transfer/channel-7",
		IBCBaseDenom: "uatone", OriginChainID: "atomone-1"})
	got, _ = built(t, tb, assets.Curation{})
	assert.False(t, got[atone].DepositAllowed, "ATONE arrives directly")
	assert.True(t, got[atone].WithdrawAllowed, "holders are never stranded")
	assert.True(t, got[directATONE].Verified)
	assert.True(t, got[directATONE].DepositAllowed)

	// A direct coin over a non-canonical channel is no sibling.
	channel(t, tb, "channel-7").Confirmed = false
	got, _ = built(t, tb, assets.Curation{})
	assert.True(t, got[atone].DepositAllowed)
	assert.True(t, got[atone].Verified)

	// The curation's exclusions still apply.
	got, _ = built(t, tables(), assets.Curation{DepositExcluded: []string{atone}, WithdrawExcluded: []string{atone}})
	assert.False(t, got[atone].DepositAllowed)
	assert.False(t, got[atone].WithdrawAllowed)
}
