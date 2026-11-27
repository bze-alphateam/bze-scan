//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"
	guardiantypes "github.com/bze-alphateam/bze/x/guardian/types"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
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

	_, err = e.db.Exec(`INSERT INTO explorer.labels (address, name, kind, source, updated_at)
		VALUES ('bze1jx4x3kn8mlz2s03zdpf2a38x9gl66llvjqdd55', 'DAO #3 · Ops', 'dao', 'sync', now())`)
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
	assert.Equal(t, "DAO #3 · Ops", *got.Guardian.Label)
	assert.Equal(t, "dao", *got.Guardian.LabelKind)
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

// The actions list merges transactions' messages, block events and the
// messages of passed proposals, newest first, skips failed transactions,
// other message types and proposals that did not pass, and pages without
// skipping or repeating a row, also across rows of one height.
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
	proposal := func(id int64, status string, resolved int64, typeURL string) {
		t.Helper()
		_, err := e.db.Exec(`INSERT INTO explorer.proposals (id, title, kind, message_types, messages, status, submit_time, resolved_height, updated_at)
			VALUES ($1, 'p', 'other', ARRAY[$3::text], jsonb_build_array(jsonb_build_object('@type', $3::text, 'authority', $4::text)),
			        $2, '2026-11-25T09:00:00Z', $5, now())`, id, status, typeURL, gov, resolved)
		require.NoError(t, err)
	}
	tx(100, 0, "T100", true, "/bze.guardian.v1.MsgCloseChannel", "bze1guardian", `{"channel_id":"channel-3"}`)
	tx(160, 0, "T160", false, "/bze.guardian.v1.MsgPauseAutomation", "bze1guardian", `{}`)
	tx(200, 0, "T200", true, "/bze.guardian.v1.MsgBlockDenom", "bze1guardian", `{"denom":"ibc/AAA"}`)
	tx(200, 1, "T200B", true, "/cosmos.bank.v1beta1.MsgSend", "bze1guardian", `{}`)
	proposal(7, "passed", 150, "/bze.tradebin.MsgUnhaltDenoms")
	proposal(8, "failed", 160, "/bze.tradebin.MsgUnhaltDenoms") // did not execute
	proposal(9, "passed", 250, "/cosmos.circuit.v1.MsgResetCircuitBreaker")
	proposal(10, "passed", 300, "/cosmos.bank.v1beta1.MsgSend") // not an emergency message
	_, err := e.db.Exec(`INSERT INTO explorer.block_events (height, seq, type, attrs)
		VALUES (250, 3, 'bze.guardian.v1.DenomUnblockedEvent', jsonb_build_object('authority', $1::text, 'denom', 'ibc/AAA')),
		       (250, 4, 'bze.burner.v1.SomethingElse', '{}'::jsonb)`, gov)
	require.NoError(t, err)

	base := e.serveEmergency(t)
	all := fetch(t, base+"/emergency/actions")
	require.Equal(t, http.StatusOK, all.status, string(all.body))
	assert.Equal(t, "no-store", all.header.Get("Cache-Control"))
	list := into[dto.List[dto.EmergencyAction]](t, all)
	require.Nil(t, list.NextCursor)
	type row struct {
		height     int64
		action, by string
		tx         string
	}
	var got []row
	for _, a := range list.Items {
		tx := ""
		if a.TxHash != nil {
			tx = *a.TxHash
		}
		got = append(got, row{a.Height, a.Action, a.By, tx})
	}
	// Two rows at height 250: the proposal's message, then the block event.
	assert.Equal(t, []row{
		{250, "enable_messages", "governance", ""},
		{250, "unblock_denom", "governance", ""},
		{200, "block_denom", "guardian", "T200"},
		{150, "unhalt_trading", "governance", ""},
		{100, "close_channel", "guardian", "T100"},
	}, got)
	assert.Equal(t, gov, *list.Items[0].Signer)
	require.NotNil(t, list.Items[0].ProposalID)
	assert.Equal(t, int64(9), *list.Items[0].ProposalID)
	assert.JSONEq(t, `{"@type":"/cosmos.circuit.v1.MsgResetCircuitBreaker","authority":"`+gov+`","proposal_id":9}`, string(list.Items[0].Details))
	assert.Nil(t, list.Items[1].ProposalID)
	assert.Equal(t, int64(7), *list.Items[3].ProposalID)

	// One row per page: every page boundary, the one inside height 250 too.
	var paged []dto.EmergencyAction
	url := base + "/emergency/actions?limit=1"
	for range 10 {
		page := into[dto.List[dto.EmergencyAction]](t, fetch(t, url))
		require.LessOrEqual(t, len(page.Items), 1)
		paged = append(paged, page.Items...)
		if page.NextCursor == nil {
			break
		}
		url = base + "/emergency/actions?limit=1&cursor=" + *page.NextCursor
	}
	assert.Equal(t, list.Items, paged)
}

// Guardian actions run through authz (a grantee's MsgExec) and through a DAO
// (an executed proposal of the DAO that is the guardian) are indexed by the
// real transformer and settle hook and listed with the guardian's own
// transaction message, a governance block event and a passed proposal's
// message, newest first, paged across all of them. A proposal whose
// execution failed lists nothing (a failed transaction runs nothing: see the
// transformer's tests); the old rows have no via.
func TestEmergencyActionsThroughAuthzAndADao(t *testing.T) {
	e := newSyncEnv(t)
	pool, err := pgxpool.New(context.Background(), e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	gov := chain.ModuleAddress("gov")
	const h = int64(25_400_000)
	guardian, hot := daoAdmin, daoMember
	anyOf := func(m sdk.Msg) []*codectypes.Any {
		a, err := codectypes.NewAnyWithValue(m)
		require.NoError(t, err)
		return []*codectypes.Any{a}
	}
	closeChannel := &authz.MsgExec{Grantee: hot, Msgs: anyOf(&guardiantypes.MsgCloseChannel{Signer: guardian, ChannelId: "channel-3"})}
	blocks := []*transform.Entities{
		// The guardian's own transaction message.
		daoTxBlock(t, h, []sdk.Msg{&guardiantypes.MsgBlockDenom{Signer: guardian, Denom: "ibc/AAA"}}, [][]node.Event{{}}),
		// Through authz: the hot key runs the guardian's grant.
		daoTxBlock(t, h+1, []sdk.Msg{closeChannel}, [][]node.Event{{}}),
		// Proposals #11 and #12 of DAO #2 (the guardian DAO), each halting trading.
		daoTxBlock(t, h+2,
			[]sdk.Msg{
				&daotypes.MsgSubmitProposal{Proposer: daoAdmin, DaoId: 2, Title: "Halt", Msgs: anyOf(&guardiantypes.MsgHaltTradingDenoms{Signer: daoTwo, Denoms: []string{"ubze"}})},
				&daotypes.MsgSubmitProposal{Proposer: daoAdmin, DaoId: 2, Title: "Halt too", Msgs: anyOf(&guardiantypes.MsgHaltTradingDenoms{Signer: daoTwo, Denoms: []string{"uvdl"}})},
			},
			[][]node.Event{{daoSubmittedEvent("11", "2", "100", "1764666002", false)}, {daoSubmittedEvent("12", "2", "100", "1764666002", false)}}),
		// #11 executed, #12's execution failed.
		daoTxBlock(t, h+3,
			[]sdk.Msg{&daotypes.MsgSettleProposal{Sender: daoMember, ProposalId: 11}, &daotypes.MsgSettleProposal{Sender: daoMember, ProposalId: 12}},
			[][]node.Event{daoSettledEvents("11", "EXECUTED", "60", "100", boolPtr(true), false),
				daoSettledEvents("12", "EXECUTION_FAILED", "60", "100", boolPtr(false), false)}),
	}
	w := writer.NewBatchWriter(pool, writer.SettledProposalActions(nullLog()))
	require.NoError(t, w.Write(context.Background(), blocks, writer.ModeInsert))

	// Governance's block event and passed proposal, between them.
	_, err = e.db.Exec(`INSERT INTO explorer.blocks (height, time, hash) VALUES ($1, '2026-12-01T08:00:00Z', 'H')`, h-1)
	require.NoError(t, err)
	_, err = e.db.Exec(`INSERT INTO explorer.block_events (height, seq, type, attrs)
		VALUES ($1, 3, 'bze.guardian.v1.DenomUnblockedEvent', jsonb_build_object('authority', $2::text, 'denom', 'ibc/BBB'))`, h-1, gov)
	require.NoError(t, err)
	_, err = e.db.Exec(`INSERT INTO explorer.proposals (id, title, kind, message_types, messages, status, submit_time, resolved_height, updated_at)
		VALUES (7, 'p', 'other', ARRAY['/bze.tradebin.MsgUnhaltDenoms'],
		        jsonb_build_array(jsonb_build_object('@type', '/bze.tradebin.MsgUnhaltDenoms', 'authority', $1::text)),
		        'passed', '2026-12-01T07:00:00Z', $2, now())`, gov, h-1)
	require.NoError(t, err)

	base := e.serveEmergency(t)
	list := into[dto.List[dto.EmergencyAction]](t, fetch(t, base+"/emergency/actions"))
	require.Nil(t, list.NextCursor)
	type row struct {
		height       int64
		action, by   string
		signer, via  string
		viaID, viaPr int64
	}
	var got []row
	for _, a := range list.Items {
		r := row{height: a.Height, action: a.Action, by: a.By}
		if a.Signer != nil {
			r.signer = *a.Signer
		}
		if a.Via != nil {
			r.via = a.Via.Kind
			if a.Via.Grantee != nil {
				r.via += " " + *a.Via.Grantee
			}
			if a.Via.DaoID != nil {
				r.viaID = *a.Via.DaoID
			}
			if a.Via.ProposalID != nil {
				r.viaPr = *a.Via.ProposalID
			}
		}
		got = append(got, r)
	}
	assert.Equal(t, []row{
		{h + 3, "halt_trading", "guardian", daoTwo, "dao", 2, 11},
		{h + 1, "close_channel", "guardian", guardian, "authz " + hot, 0, 0},
		{h, "block_denom", "guardian", guardian, "", 0, 0},
		{h - 1, "unhalt_trading", "governance", gov, "", 0, 0},
		{h - 1, "unblock_denom", "governance", gov, "", 0, 0},
	}, got)
	require.NotNil(t, list.Items[0].TxHash, "the settle's transaction")
	assert.Equal(t, blocks[3].Transactions[0].Hash, *list.Items[0].TxHash)
	assert.Equal(t, blocks[1].Transactions[0].Hash, *list.Items[1].TxHash)
	assert.JSONEq(t, `{"@type":"/bze.guardian.v1.MsgCloseChannel","signer":"`+guardian+`","channel_id":"channel-3"}`, string(list.Items[1].Details))
	for _, a := range list.Items[2:] {
		assert.Nil(t, a.Via)
	}
	raw := fetch(t, base+"/emergency/actions")
	assert.Contains(t, string(raw.body), `"via":{"kind":"dao","dao_id":2,"proposal_id":11}`)
	assert.Contains(t, string(raw.body), `"via":null`)

	// One row per page across the five sources.
	var paged []dto.EmergencyAction
	url := base + "/emergency/actions?limit=1"
	for range 10 {
		page := into[dto.List[dto.EmergencyAction]](t, fetch(t, url))
		require.LessOrEqual(t, len(page.Items), 1)
		paged = append(paged, page.Items...)
		if page.NextCursor == nil {
			break
		}
		url = base + "/emergency/actions?limit=1&cursor=" + *page.NextCursor
	}
	assert.Equal(t, list.Items, paged)

	// A reindex numbers the nested rows the same: nothing moves.
	require.NoError(t, w.Write(context.Background(), blocks, writer.ModeUpdate))
	assert.Equal(t, list.Items, into[dto.List[dto.EmergencyAction]](t, fetch(t, base+"/emergency/actions")).Items)
}

// A settle that is not its transaction's first message: the proposal's
// nested emergency row sits at the settle message's own index, read from the
// stored transaction message (not the fallback 0).
func TestSettledEmergencyActionsSitAtTheSettleMessage(t *testing.T) {
	e := newSyncEnv(t)
	pool, err := pgxpool.New(context.Background(), e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	const h = int64(25_500_000)
	halt, err := codectypes.NewAnyWithValue(&guardiantypes.MsgHaltTradingDenoms{Signer: daoTwo, Denoms: []string{"ubze"}})
	require.NoError(t, err)
	submit := daoTxBlock(t, h,
		[]sdk.Msg{&daotypes.MsgSubmitProposal{Proposer: daoAdmin, DaoId: 2, Title: "Halt", Msgs: []*codectypes.Any{halt}}},
		[][]node.Event{{daoSubmittedEvent("21", "2", "100", "1764666002", false)}})

	// One transaction: a send (message 0), then the settle (message 1).
	raw := v8RawTx(t,
		&banktypes.MsgSend{FromAddress: daoMember, ToAddress: daoAdmin, Amount: sdk.NewCoins(sdk.NewInt64Coin("ubze", 1))},
		&daotypes.MsgSettleProposal{Sender: daoMember, ProposalId: 21})
	events := []node.Event{
		rawDaoEvent("bze.dao.EventProposalExecuted", "1", "id", q("21"), "success", "true", "error", q("")),
		rawDaoEvent("bze.dao.EventProposalSettled", "1", "id", q("21"), "status", q("EXECUTED"), "yes", q("60"),
			"no", q("0"), "abstain", q("0"), "denominator", q("100"), "expedited_early", "false"),
	}
	settle, err := newTransformer(t).Transform(transform.Input{
		Block: &node.Block{Height: h + 1, Time: time.Date(2026, 12, 1, 9, 0, 1, 0, time.UTC), Hash: fmt.Sprintf("%064X", h+1),
			ProposerAddress: "AA", Raw: json.RawMessage(`{}`), Txs: []string{raw}},
		Results: &node.BlockResults{Height: h + 1, TxsResults: []node.TxResult{{Events: events}}},
		Commit:  &node.Commit{Height: h + 1},
	})
	require.NoError(t, err)
	require.Len(t, settle.Messages, 2)

	w := writer.NewBatchWriter(pool, writer.SettledProposalActions(nullLog()))
	require.NoError(t, w.Write(context.Background(), []*transform.Entities{submit, settle}, writer.ModeInsert))
	assert.Equal(t, []string{fmt.Sprintf("%d 0 1 1000 dao 2 21 /bze.guardian.v1.MsgHaltTradingDenoms", h+1)},
		queryStrings(t, e.db, `SELECT concat_ws(' ', height, tx_index, msg_index, seq, via_kind, dao_id, proposal_id, type_url)
			FROM explorer.emergency_nested_actions`))
}
