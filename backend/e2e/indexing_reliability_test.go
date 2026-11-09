//go:build e2e

package e2e_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/status"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// The safe height stays below a height the live indexer gave up on while the
// cursor moves on, and follows the cursor again once that height is repaired.
func TestSafeHeightHoldsBelowAnOpenLiveFailure(t *testing.T) {
	ctx := context.Background()
	ix := newIndexer(t)
	pool, err := pgxpool.New(ctx, ix.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := status.NewPGStore(pool)

	safe := func() int64 {
		t.Helper()
		h, ok, err := store.SafeHeight(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		return h
	}

	_, ok, err := store.SafeHeight(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "nothing indexed yet")

	for _, h := range []int64{h316, h317, h318} {
		ix.index(t, h)
	}
	assert.Equal(t, h318, safe(), "no failure: the safe height is the cursor")

	require.NoError(t, ix.w.RecordFailure(ctx, h319, 3, errors.New("gave up")))
	ix.index(t, h320)
	cursor, _, err := store.LastIndexedHeight(ctx)
	require.NoError(t, err)
	assert.Equal(t, h320, cursor, "the cursor has passed the failed height")
	assert.Equal(t, h318, safe(), "the safe height has not")

	exec := func(query string, args ...any) {
		t.Helper()
		_, err := ix.db.Exec(query, args...)
		require.NoError(t, err)
	}
	exec(`UPDATE explorer.index_failures SET resolved_at = now() WHERE height = $1`, h319)
	assert.Equal(t, h320, safe(), "a resolved failure no longer holds it")

	exec(`INSERT INTO explorer.index_failures (height, source, attempts, error) VALUES ($1, 'backfill', 3, 'x')`, h319)
	assert.Equal(t, h320, safe(), "only live failures hold it")

	exec(`INSERT INTO explorer.index_failures (height, source, attempts, error) VALUES ($1, 'live', 3, 'x')`, h317)
	assert.Equal(t, h320, safe(), "a height whose block exists holds nothing")

	exec(`UPDATE explorer.index_failures SET resolved_at = NULL WHERE height = $1 AND source = 'live'`, h319)
	assert.Equal(t, h318, safe(), "an open failure holds it again")
}

// A NUL in a memo, a message body or an event attribute costs the block
// nothing: PostgreSQL gets the replacement character instead.
func TestABlockWithNULTextIsIndexed(t *testing.T) {
	ctx := context.Background()
	ix := newIndexer(t)
	b, _, err := ix.client.Block(ctx, hSend)
	require.NoError(t, err)
	r, _, err := ix.client.BlockResults(ctx, hSend)
	require.NoError(t, err)
	c, _, err := ix.client.Commit(ctx, hSend)
	require.NoError(t, err)
	ents, err := ix.tr.Transform(transform.Input{Block: b, Results: r, Commit: c})
	require.NoError(t, err)
	require.NotEmpty(t, ents.Transactions)
	require.NotEmpty(t, ents.Messages)

	ents.Transactions[0].Memo = "pay\x00ment"
	ents.Messages[0].Body = json.RawMessage(`{"note":"a\u0000b","path":"c:\\u0000d"}`)
	ents.Messages[0].Events = append(ents.Messages[0].Events,
		transform.Event{Type: "custom", Attrs: map[string]any{"text": "x\x00y"}})

	require.NoError(t, ix.w.WriteBlock(ctx, ents))
	assert.Equal(t, []string{"1"}, queryStrings(t, ix.db, `SELECT count(*) FROM explorer.blocks WHERE height = $1`, hSend))
	assert.Equal(t, []string{"pay\uFFFDment"}, queryStrings(t, ix.db,
		`SELECT memo FROM explorer.transactions WHERE height = $1 AND tx_index = $2`, hSend, ents.Transactions[0].TxIndex))
	assert.Equal(t, []string{"a\uFFFDb c:\\u0000d x\uFFFDy"}, queryStrings(t, ix.db,
		`SELECT concat_ws(' ', body->>'note', body->>'path', (
		   SELECT e->'attrs'->>'text' FROM jsonb_array_elements(events) e WHERE e->>'type' = 'custom'))
		   FROM explorer.messages WHERE height = $1 AND tx_index = $2 AND msg_index = $3`,
		hSend, ents.Messages[0].TxIndex, ents.Messages[0].MsgIndex))
}

// incomingRow is one synthetic activity row of the incoming filter's test,
// with the single message of its transaction.
type incomingRow struct {
	name    string
	signer  bool
	failed  bool
	deltas  string
	types   []string
	typeURL string
	body    string
	events  string
	want    bool
}

const (
	msgSend      = "/cosmos.bank.v1beta1.MsgSend"
	msgMultiSend = "/cosmos.bank.v1beta1.MsgMultiSend"
	msgExec      = "/cosmos.authz.v1beta1.MsgExec"
	msgRecv      = "/ibc.core.channel.v1.MsgRecvPacket"
	msgArticle   = "/bze.cointrunk.MsgAddArticle"
	msgDelegate  = "/cosmos.staking.v1beta1.MsgDelegate"
	msgWithdraw  = "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward"
)

func incomingRows(address string) []incomingRow {
	const plus, minus = `[{"denom":"ubze","amount":"5"}]`, `[{"denom":"ubze","amount":"-5"}]`
	const other = "bze1xrw00qx3laxg7dkwx0aymxyqtwr9xsuf3k3pa7"
	to := func(a string) string { return fmt.Sprintf(`{"to_address":%q}`, a) }
	outputs := func(a string) string { return fmt.Sprintf(`{"outputs":[{"address":%q},{"address":"bze1x"}]}`, a) }
	inner := func(typeURL, fields string) string { return fmt.Sprintf(`{"msgs":[{"@type":%q,%s}]}`, typeURL, fields) }
	packet := func(a string) string {
		return fmt.Sprintf(`[{"type":"fungible_token_packet","attrs":{"receiver":%q,"success":"true"}}]`, a)
	}
	return []incomingRow{
		{name: "bank send", deltas: plus, types: []string{msgSend}, typeURL: msgSend, body: to(address), want: true},
		{name: "multi-send", deltas: plus, types: []string{msgMultiSend}, typeURL: msgMultiSend, body: outputs(address), want: true},
		{name: "authz send", deltas: plus, types: []string{msgExec}, typeURL: msgExec,
			body: inner(msgSend, fmt.Sprintf(`"to_address":%q`, address)), want: true},
		{name: "authz multi-send", deltas: plus, types: []string{msgExec}, typeURL: msgExec,
			body: inner(msgMultiSend, fmt.Sprintf(`"outputs":[{"address":%q}]`, address)), want: true},
		{name: "ibc receipt", deltas: plus, types: []string{msgRecv}, typeURL: msgRecv, events: packet(address), want: true},
		{name: "grantee paid by authz send", signer: true, deltas: plus, types: []string{msgExec}, typeURL: msgExec,
			body: inner(msgSend, fmt.Sprintf(`"to_address":%q`, address)), want: true},
		{name: "two denoms", deltas: `[{"denom":"ubze","amount":"-1"},{"denom":"ufoo","amount":"7"}]`,
			types: []string{msgSend}, typeURL: msgSend, body: to(address), want: true},

		{name: "named only", deltas: `[]`, types: []string{msgArticle}, typeURL: msgArticle, body: fmt.Sprintf(`{"title":%q}`, address)},
		{name: "sender", signer: true, deltas: minus, types: []string{msgSend}, typeURL: msgSend, body: to(other)},
		{name: "rewards beside a send to someone else", signer: true, deltas: plus, types: []string{msgSend, msgWithdraw},
			typeURL: msgSend, body: to(other)},
		{name: "restake through authz", deltas: plus, types: []string{msgExec}, typeURL: msgExec,
			body: inner(msgDelegate, fmt.Sprintf(`"delegator_address":%q`, address))},
		{name: "authz send to someone else", deltas: plus, types: []string{msgExec}, typeURL: msgExec,
			body: inner(msgSend, fmt.Sprintf(`"to_address":%q`, other))},
		{name: "ibc receipt for someone else", deltas: plus, types: []string{msgRecv}, typeURL: msgRecv, events: packet(other)},
		{name: "failed transaction", failed: true, deltas: plus, types: []string{msgSend}, typeURL: msgSend, body: to(address)},
	}
}

// ?incoming=true keeps exactly the rows of a transfer paying the address,
// pages without a gap and leaves ?category=received as it was.
func TestAccountActivityIncomingFilter(t *testing.T) {
	e := newSyncEnv(t)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	e.index(t, hRelay, hExec, hSend)

	const firstHeight int64 = 30_000_000
	rows := incomingRows(chainToolsOwner)
	var want []string
	height := func(i int) int64 { return firstHeight + int64(i) }
	for i, r := range rows {
		body := r.body
		if body == "" {
			body = "{}"
		}
		events := r.events
		if events == "" {
			events = "[]"
		}
		_, err := e.db.Exec(`INSERT INTO explorer.account_activity
			(address, height, tx_index, time, category, kind, is_signer, success, deltas, msg_types)
			VALUES ($1, $2, 0, now(), 'received', 'send', $3, $4, $5::jsonb, $6::text[])`,
			chainToolsOwner, height(i), r.signer, !r.failed, r.deltas, "{"+strings.Join(r.types, ",")+"}")
		require.NoError(t, err, r.name)
		_, err = e.db.Exec(`INSERT INTO explorer.messages (height, tx_index, msg_index, type_url, events, body)
			VALUES ($1, 0, 0, $2, $3::jsonb, $4::jsonb)`, height(i), r.typeURL, events, body)
		require.NoError(t, err, r.name)
		if r.want {
			want = append([]string{r.name}, want...)
		}
	}
	base := e.serveAPI(t)
	names := func(query string, limit int) []string {
		t.Helper()
		var out []string
		for _, page := range activityPages(t, base, chainToolsOwner, query, limit) {
			for _, a := range page {
				out = append(out, rows[a.Height-firstHeight].name)
			}
		}
		return out
	}

	for _, limit := range []int{1, 2, 25} {
		assert.Equal(t, want, names("&incoming=true", limit), "%d at a time", limit)
	}
	page := into[dto.PagedList[dto.Activity]](t, fetch(t, base+"/accounts/"+chainToolsOwner+"/activity?incoming=true&limit=1&page=2"))
	require.NotNil(t, page.Total)
	assert.Equal(t, int64(len(want)), *page.Total, "the total counts the filtered rows")
	require.Len(t, page.Items, 1)
	assert.Equal(t, want[1], rows[page.Items[0].Height-firstHeight].name)

	assert.Equal(t, []string{"two denoms"}, names("&incoming=true&denom=ufoo", 25), "the denom that rose")
	assert.NotContains(t, names("&incoming=true&denom=ubze", 25), "two denoms", "not a denom that only fell")
	assert.Empty(t, names("&incoming=true&category=dex", 25), "it combines with a category")
	assert.Len(t, names("&category=received", 25), len(rows), "a category alone still returns every row of it")

	// Rows of real blocks.
	const recipient, sender = "bze1xrw00qx3laxg7dkwx0aymxyqtwr9xsuf3k3pa7", "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw"
	const ibcReceiver, restaked = "bze1mjpllwcpca0t2dmnr3wez2ugkr5l07lf62a32y", "bze1qly96sy3yklkwv6cf4he5ves4qu8yn588vltqp"
	assert.Equal(t, []string{fmt.Sprintf("%d tx send received", hSend)}, positions(activityPages(t, base, recipient, "&incoming=true", 25)))
	assert.Equal(t, []string{fmt.Sprintf("%d tx ibc_in cross_chain", hRelay)}, positions(activityPages(t, base, ibcReceiver, "&incoming=true", 25)))
	assert.Equal(t, [][]dto.Activity{{}}, activityPages(t, base, sender, "&incoming=true", 25), "the sender of a send")
	assert.NotEmpty(t, positions(activityPages(t, base, restaked, "", 25)))
	assert.Equal(t, [][]dto.Activity{{}}, activityPages(t, base, restaked, "&incoming=true", 25), "a restake's rewards are no transfer")
	assert.Equal(t, []string{"1"}, queryStrings(t, e.db, `SELECT count(*) FROM explorer.messages
		WHERE height = $1 AND type_url = $2 AND body->'msgs' @> jsonb_build_array(jsonb_build_object('@type', $3::text))`,
		hExec, msgExec, msgDelegate), "the filter reads the inner messages of a recorded MsgExec by these keys")

	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/accounts/"+chainToolsOwner+"/activity?incoming=yes").status)
}

// authzSendBlock is a height with two authz MsgExec transactions, one running
// a bank send and one a multi-send, encoded and decoded by the chain's codec:
// no recorded height has an authz-executed send.
func authzSendBlock(t *testing.T, height int64, granter, grantee, sendTo, multiTo string) (*node.Block, *node.BlockResults, *node.Commit) {
	t.Helper()
	coins := sdk.NewCoins(sdk.NewInt64Coin("ubze", 7))
	rawTx := func(inner sdk.Msg) string {
		anyInner, err := codectypes.NewAnyWithValue(inner)
		require.NoError(t, err)
		anyExec, err := codectypes.NewAnyWithValue(&authz.MsgExec{Grantee: grantee, Msgs: []*codectypes.Any{anyInner}})
		require.NoError(t, err)
		body, err := (&sdktx.TxBody{Messages: []*codectypes.Any{anyExec}}).Marshal()
		require.NoError(t, err)
		auth, err := (&sdktx.AuthInfo{Fee: &sdktx.Fee{Amount: sdk.NewCoins(sdk.NewInt64Coin("ubze", 1))}}).Marshal()
		require.NoError(t, err)
		raw, err := (&sdktx.TxRaw{BodyBytes: body, AuthInfoBytes: auth}).Marshal()
		require.NoError(t, err)
		return base64.StdEncoding.EncodeToString(raw)
	}
	moved := func(to string) node.TxResult {
		attr := func(k, v string) node.Attribute { return node.Attribute{Key: k, Value: v} }
		return node.TxResult{Events: []node.Event{
			{Type: "coin_spent", Attributes: []node.Attribute{attr("spender", granter), attr("amount", "7ubze"), attr("msg_index", "0")}},
			{Type: "coin_received", Attributes: []node.Attribute{attr("receiver", to), attr("amount", "7ubze"), attr("msg_index", "0")}},
		}}
	}
	b := &node.Block{Height: height, Time: time.Unix(1_800_000_000, 0).UTC(), Hash: "C0FFEE", Raw: json.RawMessage(`{}`), Txs: []string{
		rawTx(&banktypes.MsgSend{FromAddress: granter, ToAddress: sendTo, Amount: coins}),
		rawTx(&banktypes.MsgMultiSend{
			Inputs:  []banktypes.Input{{Address: granter, Coins: coins}},
			Outputs: []banktypes.Output{{Address: multiTo, Coins: coins}},
		}),
	}}
	r := &node.BlockResults{Height: height, TxsResults: []node.TxResult{moved(sendTo), moved(multiTo)}}
	return b, r, &node.Commit{Height: height}
}

// An authz-executed send, stored the way the indexer stores one, is found
// for its recipient and for nobody else.
func TestIncomingFilterFindsAuthzSendsAsTheCodecStoresThem(t *testing.T) {
	const height int64 = 26_000_200
	granter, grantee, sendTo, multiTo := swapTrader, swapBurner, chainToolsOwner, swapMaker
	e := newSyncEnv(t)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	defer pool.Close()
	b, r, c := authzSendBlock(t, height, granter, grantee, sendTo, multiTo)
	ents, err := newTransformer(t).Transform(transform.Input{Block: b, Results: r, Commit: c})
	require.NoError(t, err)
	require.NoError(t, writer.NewLiveWriter(pool).WriteBlock(ctx, ents))

	assert.Equal(t, []string{msgSend + " " + sendTo}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', body->'msgs'->0->>'@type', body->'msgs'->0->>'to_address')
		   FROM explorer.messages WHERE height = $1 AND tx_index = 0`, height), "the stored shape the filter reads")

	base := e.serveAPI(t)
	want := []string{fmt.Sprintf("%d tx authz_exec other", height)}
	assert.Equal(t, want, positions(activityPages(t, base, sendTo, "&incoming=true", 25)), "the recipient of the send")
	assert.Equal(t, want, positions(activityPages(t, base, multiTo, "&incoming=true", 25)), "the recipient of the multi-send")
	assert.Equal(t, [][]dto.Activity{{}}, activityPages(t, base, granter, "&incoming=true", 25), "the granter paid")
	assert.Equal(t, [][]dto.Activity{{}}, activityPages(t, base, grantee, "&incoming=true", 25), "the grantee only signed")
}
