//go:build e2e

package e2e_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
)

// serveEmergency runs the real serve wiring (indexer off) over the env's fake
// node and fake gRPC server and returns the API's base URL.
func (e *syncEnv) serveEmergency(t *testing.T) string {
	t.Helper()
	cfg := &config.Config{
		HTTPAddr: "127.0.0.1:0", LogLevel: "warn", LogFormat: config.LogFormatText,
		DatabaseURL: e.url, NodeRPCURL: e.node.URL, ChainID: "beezee-1", NodeGRPCAddr: e.grpc.Addr,
		ArchiveRPCURL: e.node.URL,
	}
	ctx, cancel := context.WithCancel(context.Background())
	listening := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- serve.Run(ctx, cfg, serve.Options{OnListen: func(a net.Addr) { listening <- a }})
	}()
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-done)
	})
	select {
	case a := <-listening:
		return "http://" + a.String() + "/api/v1"
	case err := <-done:
		t.Fatalf("serve exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not listen")
	}
	return ""
}

// The emergency status over the fake node: every switch pulled, named from
// the denoms and channels tables.
func TestEmergencyState(t *testing.T) {
	e := newSyncEnv(t)
	g := e.grpc
	g.SetResponse("guardian", "AutomationPaused", "", []byte(`{"paused":true}`))
	g.SetResponse("guardian", "ClosedChannels", "", []byte(`{"channel_ids":["channel-3"]}`))
	g.SetResponse("guardian", "BlockedDenoms", "", []byte(`{"denoms":["ibc/AAA"]}`))
	g.SetResponse("circuit", "DisabledList", "", []byte(`{"disabled_list":["/bze.tradebin.MsgCreateOrder"]}`))
	g.SetResponse("tradebin", "HaltedDenoms", "", []byte(`{"denoms":["factory/bze1x/utok"],"pagination":{"next_key":null,"total":"1"}}`))
	g.SetResponse("upgrade", "CurrentPlan", "", []byte(`{"plan":{"name":"emergency-halt-25000000","time":"0001-01-01T00:00:00Z","height":"25000000","info":"stop","upgraded_client_state":null}}`))
	_, err := e.db.Exec(`INSERT INTO explorer.denoms (denom, symbol, exponent, kind, updated_at)
		VALUES ('ibc/AAA', 'AAA', 6, 'ibc', now())`)
	require.NoError(t, err)
	_, err = e.db.Exec(`INSERT INTO explorer.chains (chain_id, pretty_name, updated_at) VALUES ('cosmoshub-4', 'Cosmos Hub', now())`)
	require.NoError(t, err)
	_, err = e.db.Exec(`INSERT INTO explorer.ibc_channels (channel_id, counterparty_chain_id, updated_at) VALUES ('channel-3', 'cosmoshub-4', now())`)
	require.NoError(t, err)

	base := e.serveEmergency(t)
	r := fetch(t, base+"/emergency")

	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
	got := into[dto.Emergency](t, r)
	assert.True(t, got.Supported)
	assert.True(t, got.Active)
	assert.Equal(t, int64(24998377), got.Height)
	assert.True(t, got.AutomationPaused)
	assert.Equal(t, "bze1jx4x3kn8mlz2s03zdpf2a38x9gl66llvjqdd55", got.Guardian.Address)
	require.Len(t, got.ClosedChannels, 1)
	assert.Equal(t, "Cosmos Hub", *got.ClosedChannels[0].CounterpartyChainName)
	require.Len(t, got.BlockedDenoms, 1)
	assert.Equal(t, "AAA", *got.BlockedDenoms[0].Symbol)
	assert.Equal(t, []string{"/bze.tradebin.MsgCreateOrder"}, got.DisabledMessages)
	require.Len(t, got.HaltedDenoms, 1)
	assert.Equal(t, "factory/bze1x/utok", got.HaltedDenoms[0].Denom)
	assert.Equal(t, &dto.EmergencyPlan{Name: "emergency-halt-25000000", Height: 25000000, Info: "stop", Emergency: true}, got.Plan)
}

// A node answering the guardian query Unimplemented (before v8.3.0) is
// reported as unsupported; one that fails is a 502.
func TestEmergencyStateOfOlderAndBrokenNodes(t *testing.T) {
	e := newSyncEnv(t)
	e.grpc.SetResponse("guardian", "Params", "", []byte(`{"code":12,"message":"unknown service","details":[]}`))
	base := e.serveEmergency(t)

	got := into[dto.Emergency](t, fetch(t, base+"/emergency"))
	assert.False(t, got.Supported)
	assert.False(t, got.Active)
	assert.Empty(t, got.BlockedDenoms)

	e2 := newSyncEnv(t)
	e2.grpc.SetResponse("guardian", "AutomationPaused", "", []byte(`{"code":13,"message":"boom","details":[]}`))
	r := fetch(t, e2.serveEmergency(t)+"/emergency")
	assert.Equal(t, http.StatusBadGateway, r.status)
}

// The actions list merges messages and block events, newest first, skips
// failed transactions and other message types, and pages.
func TestEmergencyActions(t *testing.T) {
	e := newSyncEnv(t)
	gov := chain.ModuleAddress("gov")
	for _, h := range []int64{100, 150, 160, 200, 250, 300} {
		_, err := e.db.Exec(`INSERT INTO explorer.blocks (height, time, hash) VALUES ($1, '2026-11-25T10:00:00Z'::timestamptz + $1::bigint * interval '1 second', 'H')`, h)
		require.NoError(t, err)
	}
	tx := func(h int64, idx int, hash string, success bool, typeURL, sender, body string) {
		t.Helper()
		_, err := e.db.Exec(`INSERT INTO explorer.transactions (height, tx_index, hash, time, success, code)
			VALUES ($1, $2, $3, '2026-11-25T10:00:00Z', $4, 0)`, h, idx, hash, success)
		require.NoError(t, err)
		_, err = e.db.Exec(`INSERT INTO explorer.messages (height, tx_index, msg_index, type_url, sender, body)
			VALUES ($1, $2, 0, $3, $4, $5::jsonb)`, h, idx, typeURL, sender, body)
		require.NoError(t, err)
	}
	tx(100, 0, "T100", true, "/bze.guardian.v1.MsgCloseChannel", "bze1guardian", `{"channel_id":"channel-3"}`)
	tx(150, 1, "T150", true, "/bze.tradebin.MsgUnhaltDenoms", gov, `{"denoms":["factory/bze1x/utok"]}`)
	tx(160, 0, "T160", false, "/bze.guardian.v1.MsgPauseAutomation", "bze1guardian", `{}`)
	tx(200, 0, "T200", true, "/bze.guardian.v1.MsgBlockDenom", "bze1guardian", `{"denom":"ibc/AAA"}`)
	tx(200, 1, "T200B", true, "/cosmos.bank.v1beta1.MsgSend", "bze1guardian", `{}`)
	_, err := e.db.Exec(`INSERT INTO explorer.block_events (height, seq, type, attrs)
		VALUES (250, 3, 'bze.guardian.v1.DenomUnblockedEvent', jsonb_build_object('authority', $1::text, 'denom', 'ibc/AAA')),
		       (250, 4, 'bze.burner.v1.SomethingElse', '{}'::jsonb)`, gov)
	require.NoError(t, err)

	base := e.serveEmergency(t)
	first := fetch(t, base+"/emergency/actions?limit=2")
	require.Equal(t, http.StatusOK, first.status, string(first.body))
	assert.Equal(t, "no-store", first.header.Get("Cache-Control"))
	page := into[dto.List[dto.EmergencyAction]](t, first)
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.NextCursor)
	// A gov-executed block-level action, then the guardian's transaction.
	assert.Equal(t, int64(250), page.Items[0].Height)
	assert.Equal(t, "unblock_denom", page.Items[0].Action)
	assert.Equal(t, "governance", page.Items[0].By)
	assert.Nil(t, page.Items[0].TxHash)
	assert.Equal(t, gov, *page.Items[0].Signer)
	assert.Equal(t, int64(200), page.Items[1].Height)
	assert.Equal(t, "block_denom", page.Items[1].Action)
	assert.Equal(t, "guardian", page.Items[1].By)
	assert.Equal(t, "T200", *page.Items[1].TxHash)

	second := into[dto.List[dto.EmergencyAction]](t, fetch(t, base+"/emergency/actions?limit=2&cursor="+*page.NextCursor))
	require.Len(t, second.Items, 2, "the failed transaction at 160 is skipped")
	assert.Equal(t, "unhalt_trading", second.Items[0].Action)
	assert.Equal(t, "governance", second.Items[0].By)
	assert.Equal(t, "close_channel", second.Items[1].Action)
	assert.Nil(t, second.NextCursor)
}
