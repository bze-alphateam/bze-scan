package transform_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// goldenTxs is every transactions and messages row of one height.
type goldenTxs struct {
	Transactions []transform.Transaction `json:"transactions"`
	Messages     []transform.Message     `json:"messages"`
}

func TestTransactionsGolden(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	for _, h := range n.FixtureHeights() {
		t.Run(fmt.Sprint(h), func(t *testing.T) {
			ents, err := tr.Transform(fetchInput(t, n, h))
			require.NoError(t, err)
			got, err := json.MarshalIndent(goldenTxs{Transactions: ents.Transactions, Messages: ents.Messages}, "", "  ")
			require.NoError(t, err)
			got = append(got, '\n')

			file := filepath.Join("testdata", fmt.Sprintf("txs_%d.golden.json", h))
			if *update {
				require.NoError(t, os.WriteFile(file, got, 0o644))
			}
			want, err := os.ReadFile(file)
			require.NoError(t, err, "run go test ./internal/transform -golden to create it")
			assert.Equal(t, string(want), string(got))
		})
	}
}

// Rows that the golden files would only show implicitly.
func TestFixtureTransactions(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	transformed := func(h int64) *transform.Entities {
		ents, err := tr.Transform(fetchInput(t, n, h))
		require.NoError(t, err)
		return ents
	}

	t.Run("send: hash as the chain computes it", func(t *testing.T) {
		ents := transformed(25000894)
		require.Len(t, ents.Transactions, 2)
		tx := ents.Transactions[0]
		assert.Equal(t, "E580BFA56DE28886E51DDB9BE50DD610C1C832C579CA08FCF9177B04D2F7B7B9", tx.Hash)
		assert.True(t, tx.Success)
		assert.Equal(t, []chain.Coin{{Denom: "ubze", Amount: "2000"}}, tx.Fee)
		assert.Equal(t, []string{"bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw"}, tx.Signers)
		assert.Equal(t, "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw", tx.FeePayer)
		assert.Equal(t, []string{"/cosmos.bank.v1beta1.MsgSend"}, tx.MsgTypes)
		assert.Equal(t, ents.Blocks[0].Time, tx.Time)

		var send transform.Message
		for _, m := range ents.Messages {
			if m.TxIndex == 0 {
				send = m
			}
		}
		assert.Equal(t, "bank", send.Module)
		assert.Equal(t, "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw", send.Sender)
		assert.NotEmpty(t, send.Body)
		types := []string{}
		for _, ev := range send.Events {
			types = append(types, ev.Type)
			assert.NotContains(t, ev.Attrs, "msg_index")
		}
		assert.Equal(t, "message", types[0], "the message event comes first")
		assert.Contains(t, types, "transfer")
	})

	t.Run("failed transaction: body only", func(t *testing.T) {
		ents := transformed(24999004)
		var failed *transform.Transaction
		for i := range ents.Transactions {
			if !ents.Transactions[i].Success {
				failed = &ents.Transactions[i]
			}
		}
		require.NotNil(t, failed)
		assert.NotZero(t, failed.Code)
		assert.NotEmpty(t, failed.ErrorLog)
		assert.NotEmpty(t, failed.Fee, "the ante handler charged the fee")
		assert.NotEmpty(t, failed.Signers)
		assert.Positive(t, failed.MsgCount)
		assert.Len(t, failed.MsgTypes, failed.MsgCount)
		n := 0
		for _, m := range ents.Messages {
			if m.TxIndex != failed.TxIndex {
				continue
			}
			n++
			assert.NotEmpty(t, m.Body)
			assert.Empty(t, m.Events)
			assert.Equal(t, failed.Signers[0], m.Sender, "no message event: the first signer")
		}
		assert.Equal(t, failed.MsgCount, n)
		assert.Equal(t, 1, ents.Blocks[0].TxFailedCount)
	})

	t.Run("successful transactions carry no error log", func(t *testing.T) {
		for _, tx := range transformed(24999134).Transactions {
			assert.True(t, tx.Success)
			assert.Empty(t, tx.ErrorLog)
		}
	})

	t.Run("multi-message: events grouped by msg_index", func(t *testing.T) {
		ents := transformed(24999134)
		perTx := map[int]int{}
		for _, m := range ents.Messages {
			assert.Equal(t, perTx[m.TxIndex], m.MsgIndex, "messages in order")
			perTx[m.TxIndex]++
			assert.Equal(t, "/bze.tradebin.MsgCreateOrder", m.TypeURL)
			assert.Equal(t, "tradebin", m.Module)
			typed := false
			for _, ev := range m.Events {
				if ev.Type == "bze.tradebin.OrderCreateMessageEvent" {
					typed = true
					_, isString := ev.Attrs["creator"].(string)
					assert.True(t, isString, "typed-event strings are unquoted")
				}
			}
			assert.True(t, typed, "message %d/%d has its order event", m.TxIndex, m.MsgIndex)
		}
		for _, tx := range ents.Transactions {
			assert.Equal(t, tx.MsgCount, perTx[tx.TxIndex])
		}
	})

	t.Run("authz exec: one row with nested messages", func(t *testing.T) {
		ents := transformed(25000440)
		require.Len(t, ents.Messages, 1)
		m := ents.Messages[0]
		assert.Equal(t, "/cosmos.authz.v1beta1.MsgExec", m.TypeURL)
		assert.Contains(t, string(m.Body), `"@type"`)
	})

	t.Run("relay transactions decode", func(t *testing.T) {
		ents := transformed(24999209)
		types := map[string]bool{}
		for _, m := range ents.Messages {
			types[m.TypeURL] = true
			assert.NotEmpty(t, m.Body, m.TypeURL)
		}
		assert.True(t, types["/ibc.core.client.v1.MsgUpdateClient"])
		assert.True(t, types["/ibc.core.channel.v1.MsgRecvPacket"])
	})
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// oneTx is a block with a single transaction whose raw bytes are "tx".
func oneTx(res node.TxResult) transform.Input {
	in := baseInput()
	in.Block.Txs = []string{b64("tx")}
	in.Results.TxsResults = []node.TxResult{res}
	return in
}

func attrs(kv ...string) []node.Attribute {
	out := []node.Attribute{}
	for i := 0; i < len(kv); i += 2 {
		out = append(out, node.Attribute{Key: kv[i], Value: kv[i+1]})
	}
	return out
}

func TestTxEventsWinOverTheDecodedTransaction(t *testing.T) {
	tr, _ := mockTransformer(mockDecoder{txs: map[string]*chain.Tx{"tx": {
		Fee: sdk.NewCoins(sdk.NewInt64Coin("ubze", 1)), FeePayer: "bze1decoded", Signers: []string{"bze1decoded"},
		Msgs: []chain.Msg{{TypeURL: "/x.MsgA", Body: json.RawMessage(`{"a":1}`)}},
	}}})
	ents, err := tr.Transform(oneTx(node.TxResult{Events: []node.Event{
		{Type: "tx", Attributes: attrs("fee", "5ubze,2ibc/AB", "fee_payer", "bze1granter")},
		{Type: "tx", Attributes: attrs("acc_seq", "bze1first/7")},
		{Type: "tx", Attributes: attrs("acc_seq", "bze1second/0")},
	}}))
	require.NoError(t, err)
	tx := ents.Transactions[0]
	assert.Equal(t, []chain.Coin{{Denom: "ubze", Amount: "5"}, {Denom: "ibc/AB", Amount: "2"}}, tx.Fee)
	assert.Equal(t, "bze1granter", tx.FeePayer)
	assert.Equal(t, []string{"bze1first", "bze1second"}, tx.Signers)
}

func TestDecodedTransactionFillsWhatTheEventsLack(t *testing.T) {
	tr, _ := mockTransformer(mockDecoder{txs: map[string]*chain.Tx{"tx": {
		Memo: "gm", Fee: sdk.NewCoins(sdk.NewInt64Coin("ubze", 9)), FeePayer: "bze1payer", Signers: []string{"bze1payer", "bze1other"},
		Msgs: []chain.Msg{{TypeURL: "/x.MsgA", Body: json.RawMessage(`{"a":1}`)}},
	}}})
	ents, err := tr.Transform(oneTx(node.TxResult{Code: 0}))
	require.NoError(t, err)
	tx := ents.Transactions[0]
	assert.Equal(t, "gm", tx.Memo)
	assert.Equal(t, []chain.Coin{{Denom: "ubze", Amount: "9"}}, tx.Fee)
	assert.Equal(t, "bze1payer", tx.FeePayer)
	assert.Equal(t, []string{"bze1payer", "bze1other"}, tx.Signers)
	assert.Equal(t, "bze1payer", ents.Messages[0].Sender, "no message event: the first signer")
	assert.Empty(t, ents.Messages[0].Module)
}

func TestAnEmptyFeeEventMeansNoFee(t *testing.T) {
	tr, _ := mockTransformer(mockDecoder{txs: map[string]*chain.Tx{"tx": {Fee: sdk.NewCoins(sdk.NewInt64Coin("ubze", 9))}}})
	ents, err := tr.Transform(oneTx(node.TxResult{Events: []node.Event{{Type: "tx", Attributes: attrs("fee", "")}}}))
	require.NoError(t, err)
	assert.Equal(t, []chain.Coin{}, ents.Transactions[0].Fee)
}

func TestMessageEvents(t *testing.T) {
	tr, _ := mockTransformer(mockDecoder{txs: map[string]*chain.Tx{"tx": {
		Signers: []string{"bze1signer"},
		Msgs:    []chain.Msg{{TypeURL: "/x.MsgA"}, {TypeURL: "/x.MsgB"}},
	}}})
	ents, err := tr.Transform(oneTx(node.TxResult{Events: []node.Event{
		{Type: "tx", Attributes: attrs("fee", "1ubze")},
		{Type: "message", Attributes: attrs("action", "/x.MsgA", "sender", "bze1a", "module", "x", "msg_index", "0")},
		{Type: "transfer", Attributes: attrs("amount", "1ubze", "msg_index", "0")},
		{Type: "message", Attributes: attrs("action", "/x.MsgB", "sender", "bze1b", "module", "y", "msg_index", "1")},
		{Type: "message", Attributes: attrs("sender", "bze1later", "msg_index", "1")},
		{Type: "bze.x.Typed", Attributes: attrs(
			"text", `"quoted"`, "num", `18446744073709551616000`, "obj", `{"k":[1,2]}`, "plain", `not json`,
			"twice", "1", "twice", "2", "msg_index", "1")},
	}}))
	require.NoError(t, err)
	require.Len(t, ents.Messages, 2)

	a, b := ents.Messages[0], ents.Messages[1]
	assert.Equal(t, "bze1a", a.Sender)
	assert.Equal(t, "x", a.Module)
	assert.Equal(t, []string{"message", "transfer"}, []string{a.Events[0].Type, a.Events[1].Type})
	assert.Equal(t, "bze1b", b.Sender, "the first message event names the sender")
	assert.Equal(t, "y", b.Module)
	require.Len(t, b.Events, 3)

	got, err := json.Marshal(b.Events[2])
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"bze.x.Typed","attrs":{
		"text":"quoted","num":18446744073709551616000,"obj":{"k":[1,2]},"plain":"not json","twice":1}}`, string(got))
	assert.Contains(t, string(got), "18446744073709551616000", "numbers keep every digit")

	sdkEvent, err := json.Marshal(a.Events[1])
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"transfer","attrs":{"amount":"1ubze"}}`, string(sdkEvent), "SDK values stay strings")
}

func TestUndecodableMessageIsStoredWithoutBody(t *testing.T) {
	tr, hook := mockTransformer(mockDecoder{txs: map[string]*chain.Tx{"tx": {
		Signers: []string{"bze1signer"},
		Msgs: []chain.Msg{
			{TypeURL: "/bze.future.MsgNew", Err: errors.New("no concrete type registered")},
			{TypeURL: "/x.MsgOld", Body: json.RawMessage(`{}`)},
		},
	}}})
	ents, err := tr.Transform(oneTx(node.TxResult{}))
	require.NoError(t, err, "one undecodable message never fails the height")

	require.Len(t, ents.Messages, 2)
	assert.Equal(t, "/bze.future.MsgNew", ents.Messages[0].TypeURL)
	assert.Nil(t, ents.Messages[0].Body)
	assert.NotNil(t, ents.Messages[1].Body)
	assert.Equal(t, []string{"/bze.future.MsgNew", "/x.MsgOld"}, ents.Transactions[0].MsgTypes)

	require.Len(t, hook.Entries, 1)
	assert.Equal(t, logrus.WarnLevel, hook.LastEntry().Level)
	assert.Equal(t, "/bze.future.MsgNew", hook.LastEntry().Data["type_url"])
}

func TestUndecodableTransactionIsStoredFromItsResults(t *testing.T) {
	tr, hook := mockTransformer(mockDecoder{err: errors.New("not a transaction")})
	ents, err := tr.Transform(oneTx(node.TxResult{Code: 2, Codespace: "sdk", Log: "tx parse error", GasWanted: 5, GasUsed: 3}))
	require.NoError(t, err)

	require.Len(t, ents.Transactions, 1)
	tx := ents.Transactions[0]
	assert.False(t, tx.Success)
	assert.Equal(t, uint32(2), tx.Code)
	assert.Equal(t, "sdk", tx.Codespace)
	assert.Equal(t, "tx parse error", tx.ErrorLog)
	assert.Equal(t, int64(5), tx.GasWanted)
	assert.Equal(t, int64(3), tx.GasUsed)
	assert.Zero(t, tx.MsgCount)
	assert.Equal(t, []string{}, tx.MsgTypes)
	assert.Equal(t, []chain.Coin{}, tx.Fee)
	assert.Empty(t, ents.Messages)

	require.Len(t, hook.Entries, 1)
	assert.Equal(t, logrus.WarnLevel, hook.LastEntry().Level)
	assert.Equal(t, tx.Hash, hook.LastEntry().Data["hash"])
}
