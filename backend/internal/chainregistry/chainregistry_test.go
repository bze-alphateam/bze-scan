package chainregistry_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chainregistry"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakeregistry"
)

func client(t *testing.T) (*chainregistry.Client, *fakeregistry.Registry) {
	t.Helper()
	reg := fakeregistry.New(t)
	return chainregistry.New(http.DefaultClient, reg.APIURL, reg.RawURL), reg
}

func TestDirsSkipsFilesAndTheRegistrysOwnDirectories(t *testing.T) {
	c, reg := client(t)
	reg.AddDirs("osmosis")
	dirs, err := c.Dirs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"beezee", "cosmoshub", "mirage", "noble", "osmosis"}, dirs,
		"README.md and _IBC are not chains; testnets/ lists none")
	assert.Equal(t, 1, reg.Requests("/api/testnets"))
}

func TestRecordedChains(t *testing.T) {
	c, _ := client(t)
	ctx := context.Background()

	hub, err := c.Chain(ctx, "cosmoshub")
	require.NoError(t, err)
	assert.Equal(t, "cosmoshub-4", hub.ChainID)
	assert.Equal(t, "cosmoshub", hub.Name)
	assert.Equal(t, "Cosmos Hub", hub.PrettyName)
	assert.Equal(t, "mainnet", hub.NetworkType)
	assert.Equal(t, "cosmos", hub.Bech32Prefix)
	assert.Contains(t, hub.LogoURL, ".png")
	assert.Contains(t, hub.ExplorerTxURL, "${txHash}", "the first explorer's template")
	assert.Contains(t, hub.ExplorerAccountURL, "${accountAddress}")
	assert.Equal(t, "live", hub.Status)

	noble, err := c.Chain(ctx, "noble")
	require.NoError(t, err)
	assert.Equal(t, "noble-1", noble.ChainID)
	assert.Equal(t, "Noble", noble.PrettyName)

	mirage, err := c.Chain(ctx, "mirage")
	require.NoError(t, err)
	assert.Equal(t, "mirage-1", mirage.ChainID)
	assert.Empty(t, mirage.ExplorerTxURL, "a chain without explorers")
	assert.Empty(t, mirage.ExplorerAccountURL)
	assert.Equal(t, "killed", mirage.Status, "a chain whose network was shut down")

	_, err = c.Chain(ctx, "nosuchchain")
	assert.ErrorIs(t, err, chainregistry.ErrNotFound)
}

func TestRecordedAssets(t *testing.T) {
	c, _ := client(t)
	ctx := context.Background()

	hub, err := c.Assets(ctx, "cosmoshub")
	require.NoError(t, err)
	require.NotEmpty(t, hub)
	atom := hub[0]
	assert.Equal(t, "uatom", atom.Base)
	assert.Equal(t, "ATOM", atom.Symbol)
	assert.Equal(t, "atom", atom.Display)
	require.NotNil(t, atom.Exponent)
	assert.Equal(t, 6, *atom.Exponent, "the display unit's exponent")
	assert.Equal(t, "cosmos", atom.CoingeckoID)
	assert.Contains(t, atom.LogoURL, "atom.png")
	assert.Nil(t, atom.Traces, "a native asset")

	noble, err := c.Assets(ctx, "noble")
	require.NoError(t, err)
	var usdc *chainregistry.Asset
	for i := range noble {
		if noble[i].Base == "uusdc" {
			usdc = &noble[i]
		}
	}
	require.NotNil(t, usdc)
	assert.Equal(t, "USDC.n", usdc.Symbol, "noble names its own USDC")

	bze, err := c.Assets(ctx, "beezee")
	require.NoError(t, err)
	var ubze, atomOnBZE *chainregistry.Asset
	for i := range bze {
		switch bze[i].Base {
		case "ubze":
			ubze = &bze[i]
		case "ibc/5FEB332D2B121921C792F1A0DBF7C3163FF205337B4AFE6E14F69E8E49545F49":
			atomOnBZE = &bze[i]
		}
	}
	require.NotNil(t, ubze)
	assert.Equal(t, "bzedge", ubze.CoingeckoID, "how the aggregator's prices map to ubze")
	require.NotNil(t, atomOnBZE)
	chain, base, ok := chainregistry.Home(atomOnBZE.Traces)
	require.True(t, ok)
	assert.Equal(t, "cosmoshub", chain)
	assert.Equal(t, "uatom", base)

	none, err := c.Assets(ctx, "osmosis")
	require.NoError(t, err, "a chain without an asset list")
	assert.Empty(t, none)
}

func TestHomeIsTheEarliestIBCTrace(t *testing.T) {
	for _, tc := range []struct {
		name   string
		traces string
		chain  string
		base   string
		ok     bool
	}{
		{"two hops", `[{"type":"ibc","counterparty":{"chain_name":"noble","base_denom":"uusdc"}},
			{"type":"ibc","counterparty":{"chain_name":"osmosis","base_denom":"ibc/498A"}}]`, "noble", "uusdc", true},
		{"bridged first", `[{"type":"bridge","counterparty":{"chain_name":"ethereum","base_denom":"0xa0b8"}},
			{"type":"ibc","counterparty":{"chain_name":"axelar","base_denom":"uusdc"}}]`, "axelar", "uusdc", true},
		{"cw20", `[{"type":"ibc-cw20","counterparty":{"chain_name":"juno","base_denom":"cw20:juno1"}}]`, "juno", "cw20:juno1", true},
		{"no ibc trace", `[{"type":"liquid-stake","counterparty":{"chain_name":"stride","base_denom":"uatom"}}]`, "", "", false},
		{"no traces", ``, "", "", false},
		{"not json", `{`, "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chain, base, ok := chainregistry.Home(json.RawMessage(tc.traces))
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.chain, chain)
			assert.Equal(t, tc.base, base)
		})
	}
}

func TestParseRejectsChainsWithoutIDs(t *testing.T) {
	_, err := chainregistry.ParseChain([]byte(`{"chain_name":"x"}`))
	require.Error(t, err)
	_, err = chainregistry.ParseChain([]byte(`[`))
	require.Error(t, err)
	_, err = chainregistry.ParseAssets([]byte(`{"assets":7}`))
	require.Error(t, err)

	assets, err := chainregistry.ParseAssets([]byte(`{"assets":[{"symbol":"NOBASE"},
		{"base":"ufoo","display":"foo","denom_units":[{"denom":"ufoo","exponent":0}],"images":[{"svg":"https://x/foo.svg"}]}]}`))
	require.NoError(t, err)
	require.Len(t, assets, 1, "an asset without a base is skipped")
	assert.Nil(t, assets[0].Exponent, "the display unit is not listed")
	assert.Equal(t, "https://x/foo.svg", assets[0].LogoURL, "from images when logo_URIs is absent")
}

func TestUnavailableRegistry(t *testing.T) {
	c, reg := client(t)
	reg.SetDown(true)
	ctx := context.Background()
	_, err := c.Dirs(ctx)
	require.Error(t, err)
	_, err = c.Chain(ctx, "cosmoshub")
	require.Error(t, err)
	assert.False(t, errors.Is(err, chainregistry.ErrNotFound))
	_, err = c.Assets(ctx, "cosmoshub")
	require.Error(t, err, "a failure is not an absent asset list")
}

func TestLogosAreKeptApartAndPickedSVGFirst(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
	}{
		{"logo_URIs svg first", `"logo_URIs":{"png":"a.png","svg":"a.svg"},"images":[{"png":"b.png","svg":"b.svg"}]`, "a.svg"},
		{"logo_URIs png", `"logo_URIs":{"png":"a.png"},"images":[{"png":"b.png","svg":"b.svg"}]`, "a.png"},
		{"images svg", `"images":[{"png":"b.png","svg":"b.svg"},{"svg":"c.svg"}]`, "b.svg"},
		{"images png", `"images":[{"png":"b.png"}]`, "b.png"},
		{"none", ``, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extra := ""
			if tc.raw != "" {
				extra = "," + tc.raw
			}
			ch, err := chainregistry.ParseChain([]byte(`{"chain_id":"x-1","chain_name":"x"` + extra + `}`))
			require.NoError(t, err)
			assert.Equal(t, tc.want, ch.Logos.SVGFirst())
			assets, err := chainregistry.ParseAssets([]byte(`{"assets":[{"base":"ux"` + extra + `}]}`))
			require.NoError(t, err)
			require.Len(t, assets, 1)
			assert.Equal(t, tc.want, assets[0].Logos.SVGFirst())
		})
	}

	ch, err := chainregistry.ParseChain([]byte(`{"chain_id":"x-1","chain_name":"x","logo_URIs":{"png":"a.png","svg":"a.svg"}}`))
	require.NoError(t, err)
	assert.Equal(t, "a.png", ch.LogoURL, "the explorer's own logo stays PNG first")
	assert.Equal(t, chainregistry.Logos{URIs: chainregistry.LogoPair{PNG: "a.png", SVG: "a.svg"}}, ch.Logos)
}

func TestChainAPIsAndAssetDescriptions(t *testing.T) {
	c, _ := client(t)
	ctx := context.Background()
	bze, err := c.Chain(ctx, "beezee")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(bze.REST), 2)
	assert.Equal(t, []string{"https://rest.getbze.com", "https://rest-1.getbze.com"}, bze.REST[:2], "in the registry's order")
	assert.NotEmpty(t, bze.RPC)

	ch, err := chainregistry.ParseChain([]byte(`{"chain_id":"x-1","chain_name":"x","apis":{"rest":[{"address":"r1"},{"address":""},{"address":"r2"}]}}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"r1", "r2"}, ch.REST)
	assert.Nil(t, ch.RPC)

	assets, err := c.Assets(ctx, "beezee")
	require.NoError(t, err)
	require.NotEmpty(t, assets)
	assert.Equal(t, "ubze", assets[0].Base)
	assert.NotEmpty(t, assets[0].Description)
	assert.NotEmpty(t, assets[0].Logos.URIs.SVG)
}

func TestIBCPathSortsTheChains(t *testing.T) {
	assert.Equal(t, "_IBC/beezee-osmosis.json", chainregistry.IBCPath("osmosis", "beezee", false))
	assert.Equal(t, "_IBC/beezee-osmosis.json", chainregistry.IBCPath("beezee", "osmosis", false))
	assert.Equal(t, "testnets/_IBC/beezeetestnet-osmosistestnet.json",
		chainregistry.IBCPath("osmosistestnet", "beezeetestnet", true))
}

func TestRecordedIBCFile(t *testing.T) {
	c, reg := client(t)
	ctx := context.Background()
	f, err := c.IBC(ctx, "noble", "beezee", false)
	require.NoError(t, err)
	require.NotNil(t, f)
	assert.Equal(t, 1, reg.Requests("/raw/_IBC/beezee-noble.json"))
	assert.Equal(t, "beezee", f.Chain1.ChainName)
	assert.Equal(t, "connection-3", f.Chain1.ConnectionID)

	other, chain, ok := f.Channel("beezee", "transfer", "channel-3")
	require.True(t, ok)
	assert.Equal(t, "noble", chain)
	assert.Equal(t, chainregistry.IBCEnd{ChannelID: "channel-95", PortID: "transfer"}, other)
	other, chain, ok = f.Channel("noble", "transfer", "channel-95")
	require.True(t, ok, "from the second chain's side")
	assert.Equal(t, "beezee", chain)
	assert.Equal(t, "channel-3", other.ChannelID)
	_, _, ok = f.Channel("beezee", "transfer", "channel-95")
	assert.False(t, ok, "BZE's end is channel-3")
	_, _, ok = f.Channel("osmosis", "transfer", "channel-3")
	assert.False(t, ok, "a chain the file does not connect")

	none, err := c.IBC(ctx, "beezee", "cosmoshub", false)
	require.NoError(t, err, "no file is no error")
	assert.Nil(t, none)

	reg.SetDown(true)
	_, err = c.IBC(ctx, "beezee", "noble", false)
	require.ErrorContains(t, err, "_IBC/beezee-noble.json")

	_, err = chainregistry.ParseIBC([]byte(`[`))
	require.Error(t, err)
}
