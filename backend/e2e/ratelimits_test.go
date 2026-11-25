//go:build e2e

package e2e_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
)

func TestIBCRateLimits(t *testing.T) {
	e := newSyncEnv(t)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	base := e.serveAPI(t)

	r := fetch(t, base+"/ibc/rate-limits")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
	list := into[dto.RateLimitList](t, r)
	require.True(t, list.Supported)
	require.Len(t, list.Items, 3)
	assert.Equal(t, []string{"ibc/ED07A3391A112B175915CD8FAF43A2DA8E4790EDE12566649D0C2F97716B8518"}, list.BlacklistedDenoms)

	// The recorded limits: ubze 2%/2% of 10^13 per 24 h; one side used.
	ubze0 := list.Items[len(list.Items)-2]
	require.Equal(t, "ubze", ubze0.Denom)
	assert.Equal(t, "channel-0", ubze0.ChannelID)
	require.NotNil(t, ubze0.Symbol)
	assert.Equal(t, "BZE", *ubze0.Symbol)
	assert.Equal(t, uint64(24), ubze0.WindowHours)
	require.NotNil(t, ubze0.NextReset)
	assert.Zero(t, ubze0.NextReset.Hour())
	assert.Equal(t, "200000000000", ubze0.Send.Threshold)
	assert.Equal(t, "1500000000", ubze0.Send.NetFlow)
	assert.Equal(t, "198500000000", ubze0.Send.Remaining)
	assert.Equal(t, "-1500000000", ubze0.Recv.NetFlow, "more went out than came in")
	assert.Equal(t, "0", ubze0.Recv.Used)
	assert.Equal(t, "201500000000", ubze0.Recv.Remaining, "the room above the threshold")
	require.NotNil(t, ubze0.Counterparty.ChainID, "the channel's chain is named from the channels table")
	assert.Equal(t, "osmosis-1", *ubze0.Counterparty.ChainID)

	// USDC.n: send everything, receive nothing.
	var usdc dto.RateLimit
	for _, it := range list.Items {
		if it.Denom == usdcDenom {
			usdc = it
		}
	}
	require.Equal(t, "channel-3", usdc.ChannelID)
	assert.Equal(t, "20000000", usdc.Send.Threshold)
	assert.Equal(t, "15000000", usdc.Send.Remaining)
	require.NotNil(t, usdc.Send.UsedPct)
	assert.InDelta(t, 25.0, *usdc.Send.UsedPct, 0.001)
	assert.Equal(t, "0", usdc.Recv.Threshold)
	assert.Equal(t, "0", usdc.Recv.Remaining)
	assert.Nil(t, usdc.Recv.UsedPct)

	// The filters.
	r = fetch(t, base+"/ibc/rate-limits?denom=ubze&channel=channel-3")
	items := into[dto.RateLimitList](t, r).Items
	require.Len(t, items, 1)
	assert.Equal(t, "channel-3", items[0].ChannelID)
	r = fetch(t, base+"/ibc/rate-limits?denom=ubze")
	assert.Len(t, into[dto.RateLimitList](t, r).Items, 2)
	r = fetch(t, base+"/ibc/rate-limits?denom=nothing")
	assert.Empty(t, into[dto.RateLimitList](t, r).Items)

	// The token page carries the denom's limits, from the cached read: the
	// node is asked once for all the requests above and below.
	for _, path := range []string{"/tokens/ubze", "/token?denom=" + url.QueryEscape("ubze")} {
		r = fetch(t, base+path)
		require.Equal(t, http.StatusOK, r.status, path+": "+string(r.body))
		tok := into[dto.Token](t, r)
		require.Len(t, tok.RateLimits, 2, path)
		assert.Equal(t, "ubze", tok.RateLimits[0].Denom)
	}
	r = fetch(t, base+"/tokens/"+url.PathEscape(vdlDenom))
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.JSONEq(t, "[]", string(into[map[string]json.RawMessage](t, r)["rate_limits"]), "a denom without limits has an empty list")
	assert.Equal(t, 1, e.grpc.Requests("ratelimit", "AllRateLimits"), "the 30 s cache serves every request")
}

func TestIBCRateLimitsBeforeV830(t *testing.T) {
	e := newSyncEnv(t)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	e.grpc.SetResponse("ratelimit", "AllRateLimits", "", nil) // the node does not have the module
	base := e.serveAPI(t)

	r := fetch(t, base+"/ibc/rate-limits")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.JSONEq(t, `{"supported":false,"items":[],"blacklisted_denoms":[]}`, string(r.body))
	r = fetch(t, base+"/tokens/ubze")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Empty(t, into[dto.Token](t, r).RateLimits)
}
