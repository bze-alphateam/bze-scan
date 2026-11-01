//go:build e2e

package e2e_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/assets"
)

// ubzeOnNoble is the ICS-20 voucher of ubze on Noble over BZE's channel-3
// (Noble's channel-95).
const ubzeOnNoble = "ibc/013AE50CCCE7DC02463EE602010A0F25C8405F39D7CB3BB4E944DCF2213C18AF"

type assetListBody struct {
	Version string         `json:"version"`
	Assets  []assets.Asset `json:"assets"`
}

func assetsByDenom(list assetListBody) map[string]assets.Asset {
	out := map[string]assets.Asset{}
	for _, a := range list.Assets {
		out[a.Denom] = a
	}
	return out
}

// The asset list is built at start from the synced tables, served from
// memory with its version as ETag, and rebuilt when a block makes the
// denoms set write: USDC.n, first seen in a block, joins the list of the
// serving process and moves its version.
func TestRegistryAssetsFollowTheDenomsSet(t *testing.T) {
	e := newSyncEnv(t)
	e.grpc.SetResponse("bank", "TotalSupply", "", withoutDenom(t, "TotalSupply", "supply", "denom", usdcDenom))
	e.grpc.SetResponse("bank", "DenomsMetadata", "", withoutDenom(t, "DenomsMetadata", "metadatas", "base", usdcDenom))
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)

	// An API-only process: the list is built from the tables at start.
	api := e.serveAPI(t)
	var r response
	waitFor(t, 10*time.Second, func() bool {
		r = fetch(t, api+"/registry/assets")
		return r.status == http.StatusOK
	}, "the first build")
	assert.Equal(t, "public, max-age=30", r.header.Get("Cache-Control"))
	before := into[assetListBody](t, r)
	assert.Equal(t, `"`+before.Version+`"`, r.header.Get("ETag"))
	got := assetsByDenom(before)
	assert.Len(t, got, recordedDenoms-2, "every denom with a supply but USDC.n")
	assert.NotContains(t, got, usdcDenom)

	bze := got["ubze"]
	assert.Equal(t, "BeeZee", bze.Name)
	assert.Equal(t, "BZE", bze.Ticker)
	assert.Equal(t, 6, bze.Decimals)
	assert.Equal(t, assets.BZECircleLogo, *bze.Logo)
	assert.True(t, bze.Verified)
	var toNoble *assets.Route
	for i := range bze.Routes {
		if bze.Routes[i].ChannelID == "channel-3" {
			toNoble = &bze.Routes[i]
		}
	}
	require.NotNil(t, toNoble, "an open channel to a chain the registry knows")
	assert.Equal(t, "noble-1", toNoble.ChainID)
	assert.Equal(t, "noble", toNoble.ChainName)
	assert.Equal(t, "channel-95", toNoble.CounterpartyChannelID)
	assert.Equal(t, ubzeOnNoble, toNoble.CounterpartyDenom)

	vdl := got[vdlDenom]
	assert.Equal(t, "Vidulum", vdl.Name)
	assert.Equal(t, "VDL", vdl.Ticker)
	require.NotNil(t, vdl.Logo)
	assert.Contains(t, *vdl.Logo, "vdl.svg", "SVG first")
	assert.True(t, vdl.Verified, "on the curation list")
	lp := got[vdlLPDenom]
	assert.Equal(t, "VDL/BZE LP", lp.Ticker)
	assert.Equal(t, &assets.LP{Base: vdlDenom, Quote: "ubze"}, lp.LP)

	one := fetch(t, api+"/registry/assets/"+url.PathEscape(vdlDenom))
	require.Equal(t, http.StatusOK, one.status)
	assert.Equal(t, `"`+vdl.Version+`"`, one.header.Get("ETag"))
	assert.Equal(t, http.StatusNotModified, fetch(t, api+"/registry/assets", "If-None-Match", `"`+before.Version+`"`).status)
	assert.Equal(t, http.StatusNotFound, fetch(t, api+"/registry/assets/"+url.PathEscape(usdcDenom)).status)

	// The indexing process: block hOrders moves USDC.n, the denoms set
	// resyncs it and the list is rebuilt with it.
	base := e.serve(t, hOrders)
	var after assetListBody
	waitFor(t, 20*time.Second, func() bool {
		r := fetch(t, base+"/api/v1/registry/assets")
		if r.status != http.StatusOK {
			return false
		}
		after = into[assetListBody](t, r)
		_, ok := assetsByDenom(after)[usdcDenom]
		return ok
	}, "USDC.n in the list")
	assert.NotEqual(t, before.Version, after.Version)
	usdc := assetsByDenom(after)[usdcDenom]
	assert.Equal(t, "USDC.N", usdc.Ticker, "Noble's registry asset")
	assert.Equal(t, 6, usdc.Decimals)
	assert.True(t, usdc.Stable)
	require.NotNil(t, usdc.Notice)
	require.NotNil(t, usdc.IBC)
	assert.Equal(t, "channel-3", usdc.IBC.ChannelID)
	assert.Equal(t, "noble-1", usdc.IBC.Counterparty.ChainID)
	assert.False(t, usdc.IBC.ChannelConfirmed)
	assert.Equal(t, http.StatusOK, fetch(t, base+"/api/v1/registry/assets/"+url.PathEscape(usdcDenom)).status)
}
