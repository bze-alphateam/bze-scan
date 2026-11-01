package assets_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/assets"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainregistry"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

const (
	vdl       = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uvdl"
	listed    = "factory/bze1listed/ulisted"
	withMeta  = "factory/bze1meta/umeta"
	bare      = "factory/bze1nothingknown/ubare"
	usdc      = "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4"
	haltedIBC = "ibc/HALTED"
	ownIBC    = "ibc/OWN"
	unknown   = "ibc/UNKNOWN0123456789"
	atone     = "ibc/E7CF15949EE673E0BD3F5AA7D711DD0A6C22992E7E5714A174DCEA289EB696A1"
	lpKnown   = "ulp_" + vdl + "_ubze"
	lpHalted  = "ulp/HALTEDPOOL"
	lpOrphan  = "ulp/ORPHAN"

	// The ICS-20 vouchers of ubze and of uatone over the given
	// counterparty channels: ibc/ + SHA-256 of transfer/<channel>/<denom>.
	ubzeOnOsmosis = "ibc/7E9D1FBCFE65238F9591CA2503388239CC3D482E14106BC44867D81A633EE4ED"
	atoneVoucher  = "ibc/BC26A7A805ECD6822719472BCB7842A48EF09DF206182F8F259B2593EB5D23FB"
)

func exp(n int) *int { return &n }

func svg(u string) chainregistry.Logos {
	return chainregistry.Logos{URIs: chainregistry.LogoPair{SVG: u + ".svg", PNG: u + ".png"}}
}

// tables is a small chain: BZE's own registry list, Noble, Osmosis and
// AtomOne in the registry cache, four channels.
func tables() *assets.Tables {
	return &assets.Tables{
		Denoms: []assets.Denom{
			{Denom: "ubze", Kind: "native"},
			{Denom: vdl, Kind: "factory", Metadata: json.RawMessage(`{"name":"meta vdl","symbol":"MVDL"}`)},
			{Denom: listed, Kind: "factory"},
			{Denom: withMeta, Kind: "factory", Metadata: json.RawMessage(`{"name":"Meta Token","symbol":"mt",
				"description":"from the bank","display":"meta","uri":"https://x/meta.png",
				"denom_units":[{"denom":"umeta","exponent":0},{"denom":"meta","exponent":6}]}`)},
			{Denom: bare, Kind: "factory"},
			{Denom: usdc, Kind: "ibc", IBCPath: "transfer/channel-3", IBCBaseDenom: "uusdc", OriginChainID: "noble-1",
				Metadata: json.RawMessage(`{"name":"placeholder","symbol":"UUSDC"}`)},
			{Denom: haltedIBC, Kind: "ibc", IBCPath: "transfer/channel-3", IBCBaseDenom: "uhalt", OriginChainID: "noble-1", Halted: true},
			{Denom: ownIBC, Kind: "ibc", IBCPath: "transfer/channel-9", IBCBaseDenom: "uown", OriginChainID: "own-1"},
			{Denom: unknown, Kind: "ibc", IBCPath: "transfer/channel-9", IBCBaseDenom: "averyveryverylongdenom"},
			{Denom: atone, Kind: "ibc", IBCPath: "transfer/channel-0/transfer/channel-94814", IBCBaseDenom: "uatone",
				OriginChainID: "osmosis-1", ViaChainID: "osmosis-1"},
			{Denom: lpKnown, Kind: "lp", LPBase: vdl, LPQuote: "ubze"},
			{Denom: lpHalted, Kind: "lp", LPBase: haltedIBC, LPQuote: "ubze"},
			{Denom: lpOrphan, Kind: "lp", LPBase: "factory/bze1gone/ugone", LPQuote: "ubze"},
		},
		Chains: []assets.Chain{
			{ChainID: "beezee-1", Name: "beezee", PrettyName: "BeeZee", Known: true},
			{ChainID: "noble-1", Name: "noble", PrettyName: "Noble", Bech32Prefix: "noble", Logos: svg("https://r/noble"),
				REST: []string{"https://rest.noble"}, RPC: []string{"https://rpc.noble", "https://rpc2.noble"}, Known: true},
			{ChainID: "osmosis-1", Name: "osmosis", PrettyName: "Osmosis", Bech32Prefix: "osmo", LogoURL: "https://r/osmo-old.png", Known: true},
			{ChainID: "atomone-1", Name: "atomone", PrettyName: "AtomOne", Known: true},
			{ChainID: "nowhere-1"},
		},
		Assets: []assets.RegistryAsset{
			{ChainID: "beezee-1", Base: "ubze", Name: "BeeZee", Symbol: "BZE", Display: "bze", Exponent: exp(6),
				Logos: svg("https://r/bze"), Description: "the native coin"},
			{ChainID: "beezee-1", Base: vdl, Name: "Vidulum", Symbol: "VDL", Display: "vdl", Exponent: exp(6),
				Logos: chainregistry.Logos{URIs: chainregistry.LogoPair{PNG: "https://r/vdl.png"}, Image: chainregistry.LogoPair{SVG: "https://r/vdl-image.svg"}}},
			{ChainID: "beezee-1", Base: listed, Name: "Listed", Symbol: "LST", Display: "lst", Exponent: exp(3)},
			{ChainID: "beezee-1", Base: ownIBC, Name: "Own Coin", Symbol: "OWN", Display: "own", Exponent: exp(8), Description: "listed by BZE"},
			{ChainID: "beezee-1", Base: usdc, Name: "BZE's USDC entry", Display: "bzeusdc", Exponent: exp(2)},
			{ChainID: "noble-1", Base: "uusdc", Name: "USDC", Symbol: "usdc", Display: "usdc", Exponent: exp(6), Logos: svg("https://r/usdc")},
			{ChainID: "noble-1", Base: "uhalt", Name: "Halted", Symbol: "hlt", Display: "hlt", Exponent: exp(6)},
			{ChainID: "osmosis-1", Base: atoneVoucher, Name: "ATONE on Osmosis", Symbol: "ATONE.o", Display: "atone",
				Traces: json.RawMessage(`[{"type":"ibc","counterparty":{"chain_name":"atomone","base_denom":"uatone"}}]`)},
			{ChainID: "atomone-1", Base: "uatone", Name: "AtomOne", Symbol: "atone", Display: "atone", Exponent: exp(6),
				LogoURL: "https://r/atone-old.png"},
		},
		Channels: []assets.Channel{
			{ChannelID: "channel-0", PortID: "transfer", CounterpartyChainID: "osmosis-1", CounterpartyChannelID: "channel-750", State: "open"},
			{ChannelID: "channel-1", PortID: "transfer", CounterpartyChainID: "noble-1", CounterpartyChannelID: "channel-1", State: "closed"},
			{ChannelID: "channel-2", PortID: "transfer", CounterpartyChainID: "nowhere-1", CounterpartyChannelID: "channel-5", State: "open"},
			{ChannelID: "channel-3", PortID: "transfer", CounterpartyChainID: "noble-1", CounterpartyChannelID: "channel-95", State: "open"},
		},
	}
}

var curation = assets.Curation{
	Verified:         []string{"ubze", vdl, lpOrphan},
	Unverified:       []string{usdc},
	Stable:           []string{usdc},
	Excluded:         []string{bare},
	DepositExcluded:  []string{ownIBC},
	WithdrawExcluded: []string{withMeta},
	Notices:          map[string]assets.Notice{usdc: {Title: "retired", Text: "no deposits"}},
}

// fakeSource serves tables, failing while fail is set.
type fakeSource struct {
	mu    sync.Mutex
	t     *assets.Tables
	fail  error
	loads int
}

func (s *fakeSource) Load(context.Context) (*assets.Tables, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	if s.fail != nil {
		return nil, s.fail
	}
	return s.t, nil
}

func (s *fakeSource) set(t *assets.Tables, fail error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.t, s.fail = t, fail
}

func newList(t *testing.T, src assets.Source, cur assets.Curation) *assets.List {
	t.Helper()
	return assets.New(assets.Config{}, assets.Deps{Source: src, Curation: cur, ChainID: "beezee-1"})
}

// built builds tables once and returns the assets by denom, decoded from
// the served JSON.
func built(t *testing.T, tb *assets.Tables, cur assets.Curation) (map[string]assets.Asset, *assets.Snapshot) {
	t.Helper()
	l := newList(t, &fakeSource{t: tb}, cur)
	require.NoError(t, l.FullResync(context.Background()))
	snap := l.Current()
	require.NotNil(t, snap)
	var body struct {
		Version string         `json:"version"`
		Assets  []assets.Asset `json:"assets"`
	}
	require.NoError(t, json.Unmarshal(snap.Body, &body))
	assert.Equal(t, snap.Version, body.Version)
	out := map[string]assets.Asset{}
	for _, a := range body.Assets {
		out[a.Denom] = a
		v, raw, ok := snap.Asset(a.Denom)
		require.True(t, ok)
		assert.Equal(t, a.Version, v)
		var one assets.Asset
		require.NoError(t, json.Unmarshal(raw, &one))
		assert.Equal(t, a, one, "the one-asset body is the list's entry")
	}
	return out, snap
}

func str(s string) *string { return &s }

func TestTheNativeCoin(t *testing.T) {
	got, _ := built(t, tables(), curation)
	bze := got["ubze"]
	assert.Equal(t, "native", bze.Kind)
	assert.Equal(t, "BeeZee", bze.Name)
	assert.Equal(t, "BZE", bze.Ticker, "the display unit upper-cased")
	assert.Equal(t, 6, bze.Decimals)
	assert.Equal(t, str(assets.BZECircleLogo), bze.Logo, "forced over the registry's logo")
	assert.Equal(t, str("the native coin"), bze.Description)
	assert.True(t, bze.Verified)
	assert.Nil(t, bze.IBC)
	assert.Nil(t, bze.LP)

	// One route per open transfer channel to a chain the registry knows:
	// not the closed channel-1, not channel-2 to an unknown chain.
	require.Len(t, bze.Routes, 2)
	osmo := bze.Routes[0]
	assert.Equal(t, "channel-0", osmo.ChannelID)
	assert.Equal(t, "channel-750", osmo.CounterpartyChannelID)
	assert.Equal(t, ubzeOnOsmosis, osmo.CounterpartyDenom, "the ICS-20 voucher of ubze on the counterparty")
	assert.Equal(t, "osmosis", osmo.ChainName)
	assert.Equal(t, "osmosis-1", osmo.ChainID)
	assert.Equal(t, "Osmosis", osmo.PrettyName)
	assert.Equal(t, "osmo", osmo.Bech32Prefix)
	assert.Equal(t, str("https://r/osmo-old.png"), osmo.Logo, "a row without logos falls back to the stored logo")
	assert.Equal(t, []string{}, osmo.REST)
	noble := bze.Routes[1]
	assert.Equal(t, "channel-3", noble.ChannelID)
	assert.Equal(t, str("https://r/noble.svg"), noble.Logo, "SVG first")
	assert.Equal(t, []string{"https://rest.noble"}, noble.REST)
	assert.Equal(t, []string{"https://rpc.noble", "https://rpc2.noble"}, noble.RPC, "in the registry's order")
}

func TestNativeCoinWithoutARegistryEntry(t *testing.T) {
	tb := tables()
	tb.Assets = tb.Assets[1:]
	tb.Channels = nil
	got, _ := built(t, tb, curation)
	bze := got["ubze"]
	assert.Equal(t, "BeeZee", bze.Name, "the chain's constants")
	assert.Equal(t, "BZE", bze.Ticker)
	assert.Equal(t, 6, bze.Decimals)
	assert.Equal(t, str(assets.BZECircleLogo), bze.Logo)
	assert.Equal(t, []assets.Route{}, bze.Routes, "no channels: an empty list, not null")
}

func TestFactoryPrecedence(t *testing.T) {
	got, _ := built(t, tables(), curation)

	v := got[vdl]
	assert.Equal(t, "factory", v.Kind)
	assert.Equal(t, "Vidulum", v.Name, "BZE's registry entry wins over the bank metadata")
	assert.Equal(t, "VDL", v.Ticker)
	assert.Equal(t, 6, v.Decimals)
	assert.Equal(t, str("https://r/vdl.png"), v.Logo, "logo_URIs.png before images[0].svg")
	assert.True(t, v.Verified, "on the curation list")
	assert.Len(t, v.Routes, 2)
	assert.Equal(t, "ibc/", v.Routes[0].CounterpartyDenom[:4])

	l := got[listed]
	assert.Equal(t, "Listed", l.Name)
	assert.Equal(t, 3, l.Decimals)
	assert.Nil(t, l.Logo)
	assert.False(t, l.Verified, "a registry-listed factory token off the curation list is not verified")

	m := got[withMeta]
	assert.Equal(t, "Meta Token", m.Name)
	assert.Equal(t, "META", m.Ticker, "the display unit's denom upper-cased, over the symbol")
	assert.Equal(t, 6, m.Decimals)
	assert.Equal(t, str("https://x/meta.png"), m.Logo)
	assert.Equal(t, str("from the bank"), m.Description)
	assert.False(t, m.Verified)

	b := got[bare]
	assert.Equal(t, "fact…are", b.Name, "the denom shortened as the dapps do")
	assert.Equal(t, "fact…are", b.Ticker)
	assert.Zero(t, b.Decimals)
	assert.Nil(t, b.Logo)
	assert.Nil(t, b.Description)
}

func TestIBCPrecedence(t *testing.T) {
	got, _ := built(t, tables(), assets.Curation{})

	u := got[usdc]
	assert.Equal(t, "ibc", u.Kind)
	assert.Equal(t, "USDC", u.Name, "the origin chain's registry asset wins over BZE's list and the placeholder metadata")
	assert.Equal(t, "USDC", u.Ticker, "its symbol upper-cased")
	assert.Equal(t, 6, u.Decimals)
	assert.Equal(t, str("https://r/usdc.svg"), u.Logo)
	assert.True(t, u.Verified, "in the origin chain's registry, not halted")
	assert.Nil(t, u.Routes)
	require.NotNil(t, u.IBC)
	assert.Equal(t, "channel-3", u.IBC.ChannelID)
	assert.Equal(t, "channel-95", u.IBC.Counterparty.ChannelID)
	assert.Equal(t, "uusdc", u.IBC.Counterparty.BaseDenom)
	assert.Equal(t, "noble", u.IBC.Counterparty.ChainName)
	assert.Equal(t, "noble-1", u.IBC.Counterparty.ChainID)
	assert.Equal(t, "Noble", u.IBC.Counterparty.PrettyName)
	assert.Equal(t, []string{"https://rest.noble"}, u.IBC.Counterparty.REST)
	assert.Equal(t, &assets.Origin{ChainName: "noble", BaseDenom: "uusdc", Hops: 1}, u.IBC.Origin)
	assert.False(t, u.IBC.ChannelConfirmed)

	h := got[haltedIBC]
	assert.Equal(t, "HLT", h.Ticker)
	assert.True(t, h.Halted)
	assert.False(t, h.Verified, "a halted denom is not verified")

	o := got[ownIBC]
	assert.Equal(t, "Own Coin", o.Name, "BZE's own entry for the exact voucher")
	assert.Equal(t, "OWN", o.Ticker)
	assert.Equal(t, 8, o.Decimals)
	assert.Equal(t, str("listed by BZE"), o.Description)
	assert.False(t, o.Verified, "only the origin chain's registry verifies")
	require.NotNil(t, o.IBC)
	assert.Equal(t, "own-1", o.IBC.Counterparty.ChainID, "a channel the explorer does not hold keeps the denom's chain")
	assert.Nil(t, o.IBC.Origin, "a chain the registry does not know")

	x := got[unknown]
	assert.Equal(t, "aver…nom", x.Name, "the raw base denom, shortened past 10 characters")
	assert.Equal(t, "ibc/…789", x.Ticker)
	assert.Zero(t, x.Decimals)
	assert.Nil(t, x.Logo)
	assert.False(t, x.Verified)
}

func TestUnknownIBCShortBaseAndSlashedBase(t *testing.T) {
	tb := tables()
	tb.Denoms = []assets.Denom{
		{Denom: "ibc/SHORT", Kind: "ibc", IBCPath: "transfer/channel-9", IBCBaseDenom: "ushort"},
		{Denom: "ibc/SLASHED", Kind: "ibc", IBCPath: "transfer/channel-9", IBCBaseDenom: "gamm/pool/1"},
		{Denom: "ibc/NOTRACE", Kind: "ibc"},
	}
	got, _ := built(t, tb, assets.Curation{})
	assert.Equal(t, "ushort", got["ibc/SHORT"].Name)
	assert.Equal(t, "ibc/…HED", got["ibc/SLASHED"].Name, "a base denom with a slash names nothing")
	assert.Equal(t, "ibc/…ACE", got["ibc/NOTRACE"].Name)
	assert.Nil(t, got["ibc/NOTRACE"].IBC, "no trace, no channel")
}

// A voucher of a voucher: resolved through the first hop chain's registry
// asset and its traces, then suffixed with the first hop chain.
func TestTwoHopIBCIsSuffixedWithItsFirstHop(t *testing.T) {
	got, _ := built(t, tables(), assets.Curation{})
	a := got[atone]
	assert.Equal(t, "ATONE.osmosis", a.Ticker)
	assert.Equal(t, "AtomOne (Osmosis)", a.Name)
	assert.Equal(t, 6, a.Decimals)
	assert.Equal(t, str("https://r/atone-old.png"), a.Logo)
	assert.True(t, a.Verified)
	require.NotNil(t, a.IBC)
	assert.Equal(t, "channel-0", a.IBC.ChannelID)
	assert.Equal(t, atoneVoucher, a.IBC.Counterparty.BaseDenom, "the denom on the first hop chain is a voucher itself")
	assert.Equal(t, "osmosis", a.IBC.Counterparty.ChainName)
	assert.Equal(t, &assets.Origin{ChainName: "atomone", BaseDenom: "uatone", Hops: 2}, a.IBC.Origin)

	// Resolved by the denoms set already: its home is used as it is.
	tb := tables()
	for i := range tb.Denoms {
		if tb.Denoms[i].Denom == atone {
			tb.Denoms[i].OriginChainID = "atomone-1"
		}
	}
	tb.Assets = tb.Assets[:len(tb.Assets)-2]
	tb.Assets = append(tb.Assets, assets.RegistryAsset{ChainID: "atomone-1", Base: "uatone", Name: "AtomOne", Symbol: "atone", Display: "atone", Exponent: exp(6)})
	got, _ = built(t, tb, assets.Curation{})
	assert.Equal(t, "ATONE.osmosis", got[atone].Ticker)
	assert.Equal(t, "atomone", got[atone].IBC.Origin.ChainName)

	// Nothing in the cache: unknown, still suffixed.
	tb.Assets = tb.Assets[:len(tb.Assets)-1]
	got, _ = built(t, tb, assets.Curation{})
	assert.Equal(t, "uatone (Osmosis)", got[atone].Name)
	assert.Equal(t, "ibc/…6A1.osmosis", got[atone].Ticker)
	assert.False(t, got[atone].Verified)
}

func TestLPShares(t *testing.T) {
	got, _ := built(t, tables(), assets.Curation{})
	lp := got[lpKnown]
	assert.Equal(t, "lp", lp.Kind)
	assert.Equal(t, "VDL/BZE LP Shares", lp.Name, "after the two assets' tickers")
	assert.Equal(t, "VDL/BZE LP", lp.Ticker)
	assert.Equal(t, assets.LPDecimals, lp.Decimals)
	assert.True(t, lp.Verified, "both assets known")
	assert.False(t, lp.Halted)
	assert.Equal(t, &assets.LP{Base: vdl, Quote: "ubze"}, lp.LP)
	assert.Nil(t, lp.Routes)
	assert.Nil(t, lp.IBC)

	h := got[lpHalted]
	assert.Equal(t, "HLT/BZE LP", h.Ticker)
	assert.True(t, h.Halted, "halted when one of its assets is")

	o := got[lpOrphan]
	assert.Equal(t, "ulp/…HAN", o.Name)
	assert.False(t, o.Verified, "an asset is not listed")
	assert.Equal(t, &assets.LP{Base: "factory/bze1gone/ugone", Quote: "ubze"}, o.LP)
}

func TestCurationFlagsAndDefaults(t *testing.T) {
	got, _ := built(t, tables(), curation)
	u := got[usdc]
	assert.False(t, u.Verified, "explicitly unverified")
	assert.True(t, u.Stable)
	assert.Equal(t, &assets.Notice{Title: "retired", Text: "no deposits"}, u.Notice)
	assert.True(t, got[bare].Excluded)
	assert.False(t, got[ownIBC].DepositAllowed)
	assert.True(t, got[ownIBC].WithdrawAllowed)
	assert.False(t, got[withMeta].WithdrawAllowed)
	assert.True(t, got[withMeta].DepositAllowed)
	assert.False(t, got[lpOrphan].Verified, "the verified list does not override an LP's own rule")

	d := got[listed]
	assert.False(t, d.Stable)
	assert.False(t, d.Excluded)
	assert.True(t, d.DepositAllowed)
	assert.True(t, d.WithdrawAllowed)
	assert.Nil(t, d.Notice)
}

func TestTheListIsOrderedByKindThenDenom(t *testing.T) {
	_, snap := built(t, tables(), curation)
	var body struct {
		Assets []struct{ Denom, Kind string } `json:"assets"`
	}
	require.NoError(t, json.Unmarshal(snap.Body, &body))
	var kinds []string
	for _, a := range body.Assets {
		if len(kinds) == 0 || kinds[len(kinds)-1] != a.Kind {
			kinds = append(kinds, a.Kind)
		}
	}
	assert.Equal(t, []string{"native", "factory", "ibc", "lp"}, kinds)
	assert.Equal(t, "ubze", body.Assets[0].Denom)
}

// Every asset carries every key, null or not.
func TestEveryKeyIsPresent(t *testing.T) {
	_, snap := built(t, tables(), curation)
	var body struct {
		Assets []map[string]json.RawMessage `json:"assets"`
	}
	require.NoError(t, json.Unmarshal(snap.Body, &body))
	for _, a := range body.Assets {
		for _, k := range []string{"denom", "kind", "name", "ticker", "decimals", "logo", "description", "verified",
			"stable", "excluded", "halted", "deposit_allowed", "withdraw_allowed", "notice", "ibc", "lp", "routes", "version"} {
			assert.Contains(t, a, k)
		}
		assert.NotContains(t, a, "supply")
	}
}

func TestVersions(t *testing.T) {
	src := &fakeSource{t: tables()}
	l := newList(t, src, curation)
	ctx := context.Background()
	require.NoError(t, l.FullResync(ctx))
	first := l.Current()
	assert.Len(t, first.Version, 16)
	vdlVersion, _, _ := first.Asset(vdl)
	usdcVersion, _, _ := first.Asset(usdc)

	require.NoError(t, l.ResyncOne(ctx, statesync.All))
	assert.Same(t, first, l.Current(), "an identical rebuild keeps the snapshot and its version")
	assert.Equal(t, 2, src.loads)

	tb := tables()
	tb.Denoms[3].Metadata = json.RawMessage(`{"name":"Renamed"}`)
	src.set(tb, nil)
	require.NoError(t, l.FullResync(ctx))
	second := l.Current()
	assert.NotEqual(t, first.Version, second.Version, "a field change moves the list's version")
	before, _, _ := first.Asset(withMeta)
	after, body, _ := second.Asset(withMeta)
	assert.NotEqual(t, before, after, "and the changed asset's")
	assert.Contains(t, string(body), `"name":"Renamed"`)
	v, _, _ := second.Asset(vdl)
	assert.Equal(t, vdlVersion, v, "the others keep theirs")
	v, _, _ = second.Asset(usdc)
	assert.Equal(t, usdcVersion, v)
	_, _, ok := second.Asset("factory/bze1nobody/unone")
	assert.False(t, ok)
}

func TestAFailedRebuildKeepsTheList(t *testing.T) {
	src := &fakeSource{t: tables()}
	l := newList(t, src, curation)
	require.NoError(t, l.FullResync(context.Background()))
	first := l.Current()
	src.set(nil, errors.New("db down"))
	require.ErrorContains(t, l.FullResync(context.Background()), "db down")
	assert.Same(t, first, l.Current())
	assert.Equal(t, statesync.AssetList, l.Name())
	assert.Equal(t, assets.DefaultInterval, l.Interval())
}

// manualClock hands out timers the test fires.
type manualClock struct{ waits chan chan time.Time }

func (c *manualClock) After(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	c.waits <- ch
	return ch
}

func TestRunBuildsAtStartRetryingUntilItSucceeds(t *testing.T) {
	src := &fakeSource{fail: errors.New("db not up yet")}
	clock := &manualClock{waits: make(chan chan time.Time)}
	l := assets.New(assets.Config{}, assets.Deps{Source: src, Curation: curation, ChainID: "beezee-1", Clock: clock})
	done := make(chan error, 1)
	go func() { done <- l.Run(context.Background()) }()

	wait := <-clock.waits
	assert.Nil(t, l.Current(), "nothing built yet")
	src.set(tables(), nil)
	wait <- time.Time{}
	require.NoError(t, <-done)
	require.NotNil(t, l.Current())
	assert.Equal(t, 2, src.loads)
}

func TestRunStopsWithItsContext(t *testing.T) {
	src := &fakeSource{fail: errors.New("db down")}
	clock := &manualClock{waits: make(chan chan time.Time, 1)}
	l := assets.New(assets.Config{}, assets.Deps{Source: src, Curation: curation, ChainID: "beezee-1", Clock: clock})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Run(ctx) }()
	<-clock.waits
	cancel()
	require.NoError(t, <-done)
	assert.Nil(t, l.Current())
}

func TestTheEmbeddedCuration(t *testing.T) {
	main, err := assets.CurationFor("beezee-1")
	require.NoError(t, err)
	assert.Contains(t, main.Verified, "ubze")
	assert.Contains(t, main.Verified, vdl)
	assert.Contains(t, main.Stable, usdc)
	assert.Contains(t, main.Notices, usdc)
	assert.NotContains(t, main.Verified, "utbz", "testnet denoms are in the testnet section")
	for _, d := range append(append([]string{}, main.Verified...), main.Stable...) {
		assert.NotContains(t, d, "testbz1")
	}

	test, err := assets.CurationFor("bzetestnet-3")
	require.NoError(t, err)
	assert.Contains(t, test.Verified, "utbz")
	assert.Contains(t, test.Stable, "factory/testbz1z3mkcr2jz424w6m49frgjmy9uhlrx69p4cvrgf/uusdt")

	_, err = assets.ParseCuration([]byte(`{"testnet":{}}`), "beezee-1")
	require.ErrorContains(t, err, "no mainnet section")
	_, err = assets.ParseCuration([]byte(`[`), "beezee-1")
	require.Error(t, err)
}
