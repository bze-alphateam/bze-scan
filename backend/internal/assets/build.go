package assets

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainregistry"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/denoms"
)

// truncateLen is the length the dapps shorten a denom to.
const truncateLen = 8

// unknownNameLen is the longest raw base denom an unknown IBC denom is
// named after without shortening it.
const unknownNameLen = 10

// transferPort is the ICS-20 port, on both ends.
const transferPort = "transfer"

// truncate shortens s from its centre to truncateLen characters, as the
// dapps' truncateDenom does.
func truncate(s string) string {
	r := []rune(s)
	if len(r) <= truncateLen {
		return s
	}
	left := (truncateLen + 1) / 2
	right := len(r) - truncateLen/2 + 1
	return string(r[:left]) + "…" + string(r[right:])
}

// builder holds one build's lookups.
type builder struct {
	bze      string
	flags    flags
	chains   map[string]Chain
	byName   map[string]string // registry name → chain id
	assets   map[[2]string]*RegistryAsset
	channels map[string]Channel
	open     []Channel // open transfer channels to known chains, by id
}

// build maps every denom of t; the list is ordered by kind (native,
// factory, IBC, LP), then denom.
func build(t *Tables, cur Curation, bzeChainID string) []Asset {
	b := &builder{
		bze: bzeChainID, flags: cur.flags(), chains: map[string]Chain{}, byName: map[string]string{},
		assets: map[[2]string]*RegistryAsset{}, channels: map[string]Channel{},
	}
	for _, c := range t.Chains {
		b.chains[c.ChainID] = c
		if c.Known && c.Name != "" {
			b.byName[c.Name] = c.ChainID
		}
	}
	for i := range t.Assets {
		a := &t.Assets[i]
		b.assets[[2]string{a.ChainID, a.Base}] = a
	}
	for _, c := range t.Channels {
		b.channels[c.ChannelID] = c
		if c.PortID == transferPort && c.State == "open" && b.chains[c.CounterpartyChainID].Known {
			b.open = append(b.open, c)
		}
	}
	slices.SortFunc(b.open, func(x, y Channel) int { return strings.Compare(x.ChannelID, y.ChannelID) })

	out := make([]Asset, 0, len(t.Denoms))
	var lps []Denom
	for _, d := range t.Denoms {
		if d.Kind == denoms.KindLP {
			lps = append(lps, d)
			continue
		}
		out = append(out, b.asset(d))
	}
	// LP shares last: they are named after their assets' tickers.
	byDenom := make(map[string]Asset, len(out))
	for _, a := range out {
		byDenom[a.Denom] = a
	}
	for _, d := range lps {
		out = append(out, b.lp(d, byDenom))
	}
	for i := range out {
		b.curate(&out[i])
	}
	slices.SortFunc(out, func(x, y Asset) int {
		if r := kindRank(x.Kind) - kindRank(y.Kind); r != 0 {
			return r
		}
		return strings.Compare(x.Denom, y.Denom)
	})
	return out
}

func kindRank(kind string) int {
	return map[string]int{KindNative: 0, KindFactory: 1, KindIBC: 2, KindLP: 3}[kind]
}

// base is an asset before its precedence rules: the denom shortened, no
// decimals, no logo.
func base(d Denom, kind string) Asset {
	return Asset{Denom: d.Denom, Kind: kind, Name: truncate(d.Denom), Ticker: truncate(d.Denom), Halted: d.Halted}
}

// asset maps a native, factory or IBC denom (a denom of no known shape is
// listed as native).
func (b *builder) asset(d Denom) Asset {
	switch d.Kind {
	case denoms.KindIBC:
		return b.ibc(d)
	case denoms.KindFactory:
		a := base(d, KindFactory)
		b.ownOrMetadata(&a, d)
		a.Verified = b.flags.verified[d.Denom]
		a.Routes = b.routes(d.Denom)
		return a
	}
	a := base(d, KindNative)
	if d.Denom != chain.BondDenom {
		b.ownOrMetadata(&a, d)
		a.Verified = b.flags.verified[d.Denom]
		return a
	}
	a.Name, a.Ticker, a.Decimals = chain.BondDenomName, chain.BondDenomSymbol, chain.BondDenomExponent
	if r := b.assets[[2]string{b.bze, d.Denom}]; r != nil {
		fromOwnRegistry(&a, r)
	}
	a.Logo = ptr(BZECircleLogo)
	a.Verified = true
	a.Routes = b.routes(d.Denom)
	return a
}

// ownOrMetadata fills a from BZE's own registry asset list, else from the
// bank metadata.
func (b *builder) ownOrMetadata(a *Asset, d Denom) {
	if r := b.assets[[2]string{b.bze, d.Denom}]; r != nil {
		fromOwnRegistry(a, r)
		return
	}
	fromMetadata(a, d.Metadata)
}

// fromOwnRegistry fills a from an entry of BZE's own registry asset list:
// the ticker is the display unit upper-cased.
func fromOwnRegistry(a *Asset, r *RegistryAsset) {
	a.Name = r.Name
	a.Ticker = strings.ToUpper(r.Display)
	a.Decimals = exponent(r)
	a.Logo = registryLogo(r.Logos, r.LogoURL)
	a.Description = optional(r.Description)
}

// fromMetadata fills a from bank metadata: its name, its symbol (then its
// display unit's denom) upper-cased as ticker, the display unit's exponent,
// its URI as logo and its description; the fields it lacks stay.
func fromMetadata(a *Asset, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var md struct {
		Name        string `json:"name"`
		Symbol      string `json:"symbol"`
		Description string `json:"description"`
		Display     string `json:"display"`
		URI         string `json:"uri"`
		DenomUnits  []struct {
			Denom    string `json:"denom"`
			Exponent int    `json:"exponent"`
		} `json:"denom_units"`
	}
	if json.Unmarshal(raw, &md) != nil {
		return
	}
	if md.Name != "" {
		a.Name = md.Name
	}
	if md.Symbol != "" {
		a.Ticker = strings.ToUpper(md.Symbol)
	}
	if md.Description != "" {
		a.Description = ptr(md.Description)
	}
	if md.URI != "" {
		a.Logo = ptr(md.URI)
	}
	for _, u := range md.DenomUnits {
		if u.Denom == md.Display {
			a.Decimals = u.Exponent
			a.Ticker = strings.ToUpper(u.Denom)
		}
	}
}

// ibc maps an IBC denom: the home chain's registry asset, else BZE's own
// registry entry for the exact voucher, else the unknown treatment. Only
// the home chain's asset verifies it.
func (b *builder) ibc(d Denom) Asset {
	a := base(d, KindIBC)
	hops := pathChannels(d.IBCPath)
	if len(hops) == 0 {
		fromUnknown(&a, d.IBCBaseDenom)
		return a
	}
	ch := b.channels[hops[0]]
	cpID := ch.CounterpartyChainID
	if cpID == "" {
		cpID = d.ViaChainID
		if len(hops) == 1 {
			cpID = d.OriginChainID
		}
	}
	cpBase := d.IBCBaseDenom
	if len(hops) > 1 {
		_, rest, _ := strings.Cut(d.IBCPath, "/")
		_, rest, _ = strings.Cut(rest, "/")
		cpBase = denoms.IBCDenom(rest + "/" + d.IBCBaseDenom)
	}
	cp := b.chains[cpID]
	a.IBC = &IBC{
		ChannelID:    hops[0],
		Counterparty: Counterparty{ChainInfo: b.chainInfo(cpID), ChannelID: ch.CounterpartyChannelID, BaseDenom: cpBase},
	}

	home, homeBase, reg := b.home(d, cpID, cpBase, len(hops))
	if c := b.chains[home]; home != "" && c.Known && c.Name != "" {
		a.IBC.Origin = &Origin{ChainName: c.Name, BaseDenom: homeBase, Hops: len(hops)}
	}
	switch own := b.assets[[2]string{b.bze, d.Denom}]; {
	case reg != nil:
		a.Name = reg.Name
		a.Ticker = strings.ToUpper(reg.Symbol)
		a.Decimals = exponent(reg)
		a.Logo = registryLogo(reg.Logos, reg.LogoURL)
		a.Verified = !d.Halted
	case own != nil:
		fromOwnRegistry(&a, own)
	default:
		fromUnknown(&a, d.IBCBaseDenom)
	}
	// A voucher of a voucher arrived through an intermediary: suffixed
	// with that chain so it is not taken for the canonical asset.
	if strings.HasPrefix(cpBase, "ibc/") && cp.Name != "" {
		pretty := cp.PrettyName
		if pretty == "" {
			pretty = cp.Name
		}
		a.Ticker += "." + cp.Name
		a.Name += " (" + pretty + ")"
	}
	return a
}

// home returns the chain an IBC denom is native to, its denom there and
// its registry asset (nil when the cache has none). One hop: the
// counterparty. Several: the home the denoms set resolved, else the
// counterparty's registry asset of the voucher, followed through its
// traces.
func (b *builder) home(d Denom, cpID, cpBase string, hops int) (string, string, *RegistryAsset) {
	if hops == 1 {
		return cpID, d.IBCBaseDenom, b.assets[[2]string{cpID, d.IBCBaseDenom}]
	}
	if d.OriginChainID != "" && d.ViaChainID != "" && d.OriginChainID != d.ViaChainID {
		return d.OriginChainID, d.IBCBaseDenom, b.assets[[2]string{d.OriginChainID, d.IBCBaseDenom}]
	}
	via := b.assets[[2]string{cpID, cpBase}]
	if via == nil {
		return "", "", nil
	}
	name, homeBase, ok := chainregistry.Home(via.Traces)
	if !ok {
		return "", "", nil
	}
	home := b.byName[name]
	if home == "" {
		return "", "", nil
	}
	return home, homeBase, b.assets[[2]string{home, homeBase}]
}

// fromUnknown names an IBC denom no registry knows after its raw base denom
// (shortened past unknownNameLen), unless that has a slash; no decimals,
// no logo.
func fromUnknown(a *Asset, baseDenom string) {
	if baseDenom != "" && !strings.Contains(baseDenom, "/") {
		a.Name = baseDenom
		if len(baseDenom) > unknownNameLen {
			a.Name = truncate(baseDenom)
		}
	}
	a.Decimals = 0
	a.Logo = nil
}

// lp maps a pool share after its two assets' tickers; a share whose
// assets are not both listed keeps the shortened denom and is not
// verified.
func (b *builder) lp(d Denom, byDenom map[string]Asset) Asset {
	a := base(d, KindLP)
	a.Decimals = LPDecimals
	if d.LPBase != "" && d.LPQuote != "" {
		a.LP = &LP{Base: d.LPBase, Quote: d.LPQuote}
	}
	baseAsset, okBase := byDenom[d.LPBase]
	quoteAsset, okQuote := byDenom[d.LPQuote]
	if !okBase || !okQuote {
		return a
	}
	pair := baseAsset.Ticker + "/" + quoteAsset.Ticker
	a.Name = pair + " LP Shares"
	a.Ticker = pair + " LP"
	a.Verified = true
	a.Halted = baseAsset.Halted || quoteAsset.Halted
	return a
}

// curate applies the curation file's flags.
func (b *builder) curate(a *Asset) {
	f := b.flags
	if f.unverified[a.Denom] {
		a.Verified = false
	}
	a.Stable = f.stable[a.Denom]
	a.Excluded = f.excluded[a.Denom]
	a.DepositAllowed = !f.noDeposit[a.Denom]
	a.WithdrawAllowed = !f.noWithdraw[a.Denom]
	if n, ok := f.notices[a.Denom]; ok {
		a.Notice = &n
	}
}

// routes are denom's vouchers on every chain an open transfer channel
// leads to; empty, never nil.
func (b *builder) routes(denom string) []Route {
	out := make([]Route, 0, len(b.open))
	for _, c := range b.open {
		out = append(out, Route{
			ChainInfo:             b.chainInfo(c.CounterpartyChainID),
			ChannelID:             c.ChannelID,
			CounterpartyChannelID: c.CounterpartyChannelID,
			CounterpartyDenom:     denoms.IBCDenom(transferPort + "/" + c.CounterpartyChannelID + "/" + denom),
		})
	}
	return out
}

// chainInfo describes chainID from the registry cache.
func (b *builder) chainInfo(chainID string) ChainInfo {
	c := b.chains[chainID]
	info := ChainInfo{ChainID: chainID, REST: []string{}, RPC: []string{}}
	if !c.Known {
		return info
	}
	info.ChainName, info.PrettyName, info.Bech32Prefix = c.Name, c.PrettyName, c.Bech32Prefix
	info.Logo = registryLogo(c.Logos, c.LogoURL)
	if c.REST != nil {
		info.REST = c.REST
	}
	if c.RPC != nil {
		info.RPC = c.RPC
	}
	return info
}

// registryLogo is the SVG-first logo, else the stored PNG-first one (a row
// the registry set has not refetched since logos were kept apart); nil
// without.
func registryLogo(l chainregistry.Logos, fallback string) *string {
	if u := l.SVGFirst(); u != "" {
		return &u
	}
	return optional(fallback)
}

// exponent is a registry asset's display exponent, 0 when unknown.
func exponent(r *RegistryAsset) int {
	if r.Exponent == nil {
		return 0
	}
	return *r.Exponent
}

// pathChannels returns the channel ids of an IBC path (port/channel
// pairs), the first hop first; nil for a path that is not pairs.
func pathChannels(path string) []string {
	parts := strings.Split(path, "/")
	if path == "" || len(parts)%2 != 0 {
		return nil
	}
	out := make([]string, 0, len(parts)/2)
	for i := 1; i < len(parts); i += 2 {
		out = append(out, parts[i])
	}
	return out
}

func ptr(s string) *string { return &s }

// optional is nil for "".
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
