//go:build e2e

package e2e_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/assets"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/denoms"
)

// fakeHub is the Cosmos Hub's REST endpoint as the channel confirmation
// asks it: its channel-1898 leads to BZE's channel-11 over the counterparty
// of BZE's connection-11 (connection-1294, recorded), whose client tracks
// beezee-1. Its channel-1295 leads elsewhere: whoever opened BZE's
// channel-8 is not the hub.
func fakeHub(t *testing.T) *httptest.Server {
	t.Helper()
	answers := map[string]string{
		"/ibc/core/channel/v1/channels/channel-1898/ports/transfer": `{"channel":{"state":"STATE_OPEN",
			"counterparty":{"port_id":"transfer","channel_id":"channel-11"},"connection_hops":["connection-1294"]}}`,
		"/ibc/core/connection/v1/connections/connection-1294/client_state": `{"identified_client_state":{
			"client_id":"07-tendermint-1488","client_state":{"@type":"/ibc.lightclients.tendermint.v1.ClientState",
			"chain_id":"beezee-1"}}}`,
		"/ibc/core/channel/v1/channels/channel-1295/ports/transfer": `{"channel":{"state":"STATE_OPEN",
			"counterparty":{"port_id":"transfer","channel_id":"channel-77"},"connection_hops":["connection-12"]}}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := answers[r.URL.Path]
		if !ok {
			http.Error(w, `{"code":5,"message":"not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Three channels: Noble's channel-3 confirmed by the registry's
// _IBC/beezee-noble.json, the Cosmos Hub's channel-11 by the hub's
// endpoints, and channel-8, whose light client also claims cosmoshub-4,
// refused by them. A coin over channel-8 that calls itself uatom is not
// served as ATOM; routes go over the two confirmed channels only.
func TestIBCChannelConfirmation(t *testing.T) {
	e := newSyncEnv(t)
	hub := fakeHub(t)
	e.reg.SetREST("cosmoshub", hub.URL)
	spoofed := denoms.IBCDenom("transfer/channel-8/uatom")
	e.grpc.SetResponse("bank", "TotalSupply", "", withSupply(t, spoofed, "1000000"))
	e.grpc.SetResponse("transfer", "DenomTrace", strings.TrimPrefix(spoofed, "ibc/"),
		[]byte(`{"denom_trace":{"path":"transfer/channel-8","base_denom":"uatom"}}`))

	// The first run meets the counterparties before the registry caches
	// them; the channels' next run checks them.
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	assert.Equal(t, []string{"0"}, queryStrings(t, e.db,
		`SELECT count(*)::text FROM explorer.ibc_channels WHERE confirm_checked_at IS NOT NULL`), "no chain cached yet")
	code, out = e.syncState(t, "--job", statesync.Channels)
	require.Equal(t, 0, code, out)

	assert.Equal(t, []string{"channel-11 endpoint", "channel-3 registry", "channel-8 -"}, queryStrings(t, e.db,
		`SELECT channel_id || ' ' || coalesce(confirmed_via, '-') FROM explorer.ibc_channels
		  WHERE channel_id IN ('channel-3', 'channel-8', 'channel-11') ORDER BY channel_id`))
	assert.Equal(t, []string{"0"}, queryStrings(t, e.db,
		`SELECT count(*)::text FROM explorer.ibc_channels
		  WHERE confirmed_via IS NOT NULL AND (confirmed_at IS NULL OR confirm_note IS NOT NULL)`))
	note := queryStrings(t, e.db, `SELECT confirm_note FROM explorer.ibc_channels WHERE channel_id = 'channel-8'`)
	require.Len(t, note, 1)
	assert.Contains(t, note[0], "channel-1295 leads to transfer/channel-77, not transfer/channel-8")
	assert.Equal(t, []string{"0"}, queryStrings(t, e.db,
		`SELECT count(*)::text FROM explorer.ibc_channels
		  WHERE counterparty_chain_id NOT IN ('noble-1', 'cosmoshub-4') AND confirm_checked_at IS NOT NULL`),
		"a chain the registry does not know is not checked")

	api := e.serveAPI(t)
	var r response
	waitFor(t, 10*time.Second, func() bool {
		r = fetch(t, api+"/registry/assets")
		return r.status == http.StatusOK
	}, "the first build")
	got := assetsByDenom(into[assetListBody](t, r))

	usdc := got[usdcDenom]
	require.NotNil(t, usdc.IBC)
	assert.True(t, usdc.IBC.ChannelConfirmed, "channel-3, by the registry")
	assert.True(t, usdc.Verified)
	assert.False(t, usdc.DepositAllowed, "the curation's notice stands")

	atom := got[atomDenom] // over channel-11, the hub's channel-1898
	require.NotNil(t, atom.IBC)
	assert.True(t, atom.IBC.ChannelConfirmed, "channel-11, by the hub's endpoints")
	assert.Equal(t, "ATOM", atom.Ticker)
	assert.True(t, atom.Verified)
	assert.True(t, atom.DepositAllowed)
	assert.True(t, atom.WithdrawAllowed)

	s := got[spoofed]
	require.NotNil(t, s.IBC)
	assert.False(t, s.IBC.ChannelConfirmed)
	assert.Equal(t, "uatom", s.Name, "its raw base denom, not ATOM")
	assert.NotEqual(t, "ATOM", s.Ticker)
	assert.Zero(t, s.Decimals)
	assert.Nil(t, s.Logo)
	assert.False(t, s.Verified)
	assert.False(t, s.DepositAllowed)
	assert.False(t, s.WithdrawAllowed)
	assert.Equal(t, assets.UnknownChain, s.IBC.Counterparty.PrettyName)
	assert.Nil(t, s.IBC.Counterparty.ChainName)
	assert.Equal(t, "cosmoshub-4", s.IBC.Counterparty.ChainID, "the light client's claim")
	assert.Nil(t, s.IBC.Origin)

	var routes []string
	for _, rt := range got["ubze"].Routes {
		routes = append(routes, rt.ChannelID)
	}
	assert.Equal(t, []string{"channel-11", "channel-3"}, routes, "the canonical confirmed channels only")

	raw, err := json.Marshal(s.IBC.Counterparty)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"chain_name":null`)
}
