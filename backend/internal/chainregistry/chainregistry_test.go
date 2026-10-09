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

	noble, err := c.Chain(ctx, "noble")
	require.NoError(t, err)
	assert.Equal(t, "noble-1", noble.ChainID)
	assert.Equal(t, "Noble", noble.PrettyName)

	mirage, err := c.Chain(ctx, "mirage")
	require.NoError(t, err)
	assert.Equal(t, "mirage-1", mirage.ChainID)
	assert.Empty(t, mirage.ExplorerTxURL, "a chain without explorers")
	assert.Empty(t, mirage.ExplorerAccountURL)

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
