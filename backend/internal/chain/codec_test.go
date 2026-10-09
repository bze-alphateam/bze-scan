package chain_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
)

var (
	codecOnce sync.Once
	codec     *chain.Codec
	codecErr  error
)

func newCodec(t *testing.T) *chain.Codec {
	t.Helper()
	codecOnce.Do(func() { codec, codecErr = chain.NewCodec() })
	require.NoError(t, codecErr)
	return codec
}

// fixtureTx returns the raw bytes of transaction i of a recorded fixture block.
func fixtureTx(t *testing.T, height string, i int) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "testutil", "fakenode", "testdata", height, "block.json"))
	require.NoError(t, err)
	var res struct {
		Result struct {
			Block struct {
				Data struct {
					Txs []string `json:"txs"`
				} `json:"data"`
			} `json:"block"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(b, &res))
	raw, err := base64.StdEncoding.DecodeString(res.Result.Block.Data.Txs[i])
	require.NoError(t, err)
	return raw
}

func TestDecodeMsgSend(t *testing.T) {
	c := newCodec(t)
	raw := fixtureTx(t, "25000894", 0)

	decoded, err := c.DecodeTx(raw)
	require.NoError(t, err)
	require.Len(t, decoded.GetMsgs(), 1)
	assert.Equal(t, "/cosmos.bank.v1beta1.MsgSend", chain.TypeURL(decoded.GetMsgs()[0]))

	tx, err := c.Decode(raw)
	require.NoError(t, err)
	assert.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("ubze", 2000)), tx.Fee)
	assert.Equal(t, []string{"bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw"}, tx.Signers)
	assert.Equal(t, "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw", tx.FeePayer)
	require.Len(t, tx.Msgs, 1)
	assert.Equal(t, "/cosmos.bank.v1beta1.MsgSend", tx.Msgs[0].TypeURL)
	assert.NoError(t, tx.Msgs[0].Err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(tx.Msgs[0].Body, &body))
	assert.Equal(t, "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw", body["from_address"])
	assert.NotContains(t, body, "@type", "the type URL has its own column")
}

func TestMsgExecBodyCarriesTheNestedMessages(t *testing.T) {
	tx, err := newCodec(t).Decode(fixtureTx(t, "25000440", 0))
	require.NoError(t, err)
	require.Len(t, tx.Msgs, 1)
	assert.Equal(t, "/cosmos.authz.v1beta1.MsgExec", tx.Msgs[0].TypeURL)

	var body struct {
		Msgs []map[string]any `json:"msgs"`
	}
	require.NoError(t, json.Unmarshal(tx.Msgs[0].Body, &body))
	require.NotEmpty(t, body.Msgs)
	for _, m := range body.Msgs {
		assert.NotEmpty(t, m["@type"])
	}
}

// rawTx builds transaction bytes whose messages are the given Anys, which
// the regular decoder may not resolve.
func rawTx(t *testing.T, memo string, msgs ...*codectypes.Any) []byte {
	t.Helper()
	body, err := (&tx.TxBody{Messages: msgs, Memo: memo}).Marshal()
	require.NoError(t, err)
	auth, err := (&tx.AuthInfo{Fee: &tx.Fee{
		Amount:  sdk.NewCoins(sdk.NewInt64Coin("ubze", 7)),
		Payer:   "bze1payer",
		Granter: "bze1granter",
	}}).Marshal()
	require.NoError(t, err)
	raw, err := (&tx.TxRaw{BodyBytes: body, AuthInfoBytes: auth}).Marshal()
	require.NoError(t, err)
	return raw
}

func TestUnknownMessageKeepsItsTypeURLAndLosesOnlyItsBody(t *testing.T) {
	c := newCodec(t)
	send, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{
		FromAddress: "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw",
		ToAddress:   "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw",
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("ubze", 1)),
	})
	require.NoError(t, err)
	unknown := &codectypes.Any{TypeUrl: "/bze.future.MsgSomething", Value: []byte{0x0a, 0x01, 0x61}}
	raw := rawTx(t, "hello", unknown, send)

	_, err = c.DecodeTx(raw)
	require.Error(t, err, "the strict decoder refuses an unknown type URL")

	decoded, err := c.Decode(raw)
	require.NoError(t, err)
	assert.Equal(t, "hello", decoded.Memo)
	assert.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("ubze", 7)), decoded.Fee)
	assert.Equal(t, "bze1granter", decoded.FeePayer, "a fee grant pays")
	require.Len(t, decoded.Msgs, 2)

	assert.Equal(t, "/bze.future.MsgSomething", decoded.Msgs[0].TypeURL)
	assert.Nil(t, decoded.Msgs[0].Body)
	assert.Error(t, decoded.Msgs[0].Err)

	assert.Equal(t, "/cosmos.bank.v1beta1.MsgSend", decoded.Msgs[1].TypeURL)
	assert.NoError(t, decoded.Msgs[1].Err)
	assert.Contains(t, string(decoded.Msgs[1].Body), "from_address")
}

func TestNotATransaction(t *testing.T) {
	_, err := newCodec(t).Decode([]byte("definitely not protobuf \xff\xff"))
	assert.Error(t, err)
}

func TestRegistryHasTheChainMessages(t *testing.T) {
	urls := newCodec(t).MsgTypeURLs()
	for _, want := range []string{
		"/bze.tradebin.MsgCreateOrder",
		"/bze.tradebin.MsgHaltDenoms",
		"/cosmos.bank.v1beta1.MsgSend",
		"/ibc.applications.transfer.v1.MsgTransfer",
		"/ibc.core.channel.v1.MsgRecvPacket",
	} {
		assert.Contains(t, urls, want)
	}
}

// A pre-v8 tradebin order: the old type URL decodes into today's message.
func TestDecodesAPreV8Message(t *testing.T) {
	tx, err := newCodec(t).Decode(fixtureTx(t, "20230045", 0))
	require.NoError(t, err)
	require.Len(t, tx.Msgs, 2)
	for _, m := range tx.Msgs {
		assert.Equal(t, "/bze.tradebin.MsgCreateOrder", m.TypeURL)
		assert.NoError(t, m.Err)
		assert.Equal(t, "bze10kw8lpqd9emyxn94gkm038t4jj90ark4x8ls0d", m.Signer)
		assert.Contains(t, string(m.Body), `"market_id":`, "current field names")
	}
	assert.Equal(t, []string{"bze10kw8lpqd9emyxn94gkm038t4jj90ark4x8ls0d"}, tx.Signers)
	assert.Equal(t, "bze10kw8lpqd9emyxn94gkm038t4jj90ark4x8ls0d", tx.FeePayer)
}

func TestLegacyTypeURLsMapToRegisteredMessages(t *testing.T) {
	urls := newCodec(t).MsgTypeURLs()
	require.NotEmpty(t, chain.LegacyTypeURLs())
	for _, old := range chain.LegacyTypeURLs() {
		current := chain.CanonicalTypeURL(old)
		assert.NotEqual(t, old, current)
		assert.Contains(t, urls, current)
		assert.NotContains(t, urls, old, "aliases are not listed as messages")
	}
	assert.Equal(t, "/cosmos.bank.v1beta1.MsgSend", chain.CanonicalTypeURL("/cosmos.bank.v1beta1.MsgSend"))
	assert.Equal(t, "/bze.scavenge.MsgSubmitScavenge", chain.CanonicalTypeURL("/bze.scavenge.MsgSubmitScavenge"), "no current type")
}

func TestNestedLegacyMessagesGetTheirCurrentType(t *testing.T) {
	c := newCodec(t)
	order, err := (&tradebintypes.MsgCreateOrder{Creator: "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw", OrderType: "buy", Amount: "1", Price: "2", MarketId: "a/b"}).Marshal()
	require.NoError(t, err)
	execBytes, err := (&authz.MsgExec{Grantee: "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw", Msgs: []*codectypes.Any{{TypeUrl: "/bze.tradebin.v1.MsgCreateOrder", Value: order}}}).Marshal()
	require.NoError(t, err)

	body, err := (&tx.TxBody{Messages: []*codectypes.Any{{TypeUrl: "/cosmos.authz.v1beta1.MsgExec", Value: execBytes}}}).Marshal()
	require.NoError(t, err)
	auth, err := (&tx.AuthInfo{Fee: &tx.Fee{}}).Marshal()
	require.NoError(t, err)
	raw, err := (&tx.TxRaw{BodyBytes: body, AuthInfoBytes: auth}).Marshal()
	require.NoError(t, err)

	decoded, err := c.Decode(raw)
	require.NoError(t, err)
	require.Len(t, decoded.Msgs, 1)
	require.NoError(t, decoded.Msgs[0].Err)
	assert.Contains(t, string(decoded.Msgs[0].Body), `"@type":"/bze.tradebin.MsgCreateOrder"`)
	assert.NotContains(t, string(decoded.Msgs[0].Body), "v1.MsgCreateOrder")
}

// The backfill decodes in several workers at once. The SDK's cached signer
// lookup (cosmossdk.io/x/tx signing.Context) writes a variable shared by
// every call for a message type, so concurrent decodes of one type race
// unless the codec serialises them; run with -race.
func TestDecodeIsSafeForConcurrentUse(t *testing.T) {
	c := newCodec(t)
	raw := fixtureTx(t, "25000894", 0)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				tx, err := c.Decode(raw)
				if assert.NoError(t, err) {
					assert.Equal(t, []string{"bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw"}, tx.Signers)
				}
			}
		})
	}
	wg.Wait()
}
