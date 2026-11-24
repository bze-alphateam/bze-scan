package chain_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/x/nft"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/group"
	ratelimittypes "github.com/cosmos/ibc-apps/modules/rate-limiting/v10/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	channeltypesv2 "github.com/cosmos/ibc-go/v10/modules/core/04-channel/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"
	guardiantypes "github.com/bze-alphateam/bze/x/guardian/types"
	tokenfactorytypes "github.com/bze-alphateam/bze/x/tokenfactory/types"
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

const (
	addr1 = "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw"
)

// govAuthority is the governance module account, the signer of authority
// messages.
var govAuthority = chain.ModuleAddress(chain.Gov)

// signedTx is rawTx with a fee that names no payer or granter, which the
// strict decoder also accepts.
func signedTx(t *testing.T, msgs ...*codectypes.Any) []byte {
	t.Helper()
	body, err := (&tx.TxBody{Messages: msgs}).Marshal()
	require.NoError(t, err)
	auth, err := (&tx.AuthInfo{Fee: &tx.Fee{Amount: sdk.NewCoins(sdk.NewInt64Coin("ubze", 7))}}).Marshal()
	require.NoError(t, err)
	raw, err := (&tx.TxRaw{BodyBytes: body, AuthInfoBytes: auth}).Marshal()
	require.NoError(t, err)
	return raw
}

func anyOf(t *testing.T, msg sdk.Msg) *codectypes.Any {
	t.Helper()
	a, err := codectypes.NewAnyWithValue(msg)
	require.NoError(t, err)
	return a
}

// Messages of chain v8.3.0 decode with a body and their signer.
func TestV830MessagesDecodeWithBodiesAndSigners(t *testing.T) {
	c := newCodec(t)
	for _, tc := range []struct {
		msg    sdk.Msg
		typ    string
		signer string
	}{
		{&daotypes.MsgCreateDao{Creator: addr1, Name: "Team", Type: daotypes.DaoType_DAO_TYPE_STATIC_LIST, Members: []string{addr1}}, "/bze.dao.MsgCreateDao", addr1},
		{&guardiantypes.MsgHaltChain{Signer: addr1, Reason: "incident"}, "/bze.guardian.v1.MsgHaltChain", addr1},
		{&ratelimittypes.MsgAddRateLimit{
			Authority: govAuthority, Denom: "ubze", ChannelOrClientId: "channel-0",
			MaxPercentSend: sdkmath.NewInt(10), MaxPercentRecv: sdkmath.NewInt(10), DurationHours: 24,
		}, "/ratelimit.v1.MsgAddRateLimit", govAuthority},
		{&tradebintypes.MsgUpdatePools{Authority: govAuthority, Updates: []tradebintypes.PoolUpdate{{PoolId: "ulp_1"}}}, "/bze.tradebin.MsgUpdatePools", govAuthority},
		{&tokenfactorytypes.MsgSetDenomAdmin{Authority: govAuthority, Denom: "factory/" + addr1 + "/utok", NewAdmin: addr1}, "/bze.tokenfactory.MsgSetDenomAdmin", govAuthority},
		{&tokenfactorytypes.MsgDisableMinting{Creator: addr1, Denom: "factory/" + addr1 + "/utok"}, "/bze.tokenfactory.MsgDisableMinting", addr1},
		{&tokenfactorytypes.MsgSetDenomConfig{
			Creator: addr1, Denom: "factory/" + addr1 + "/utok",
			TransferFee: sdkmath.LegacyNewDecWithPrec(1, 2), GasSurcharge: sdkmath.LegacyZeroDec(),
		}, "/bze.tokenfactory.MsgSetDenomConfig", addr1},
		// Retired from the chain, still in the history.
		{&group.MsgCreateGroup{Admin: addr1, Members: []group.MemberRequest{{Address: addr1, Weight: "1"}}}, "/cosmos.group.v1.MsgCreateGroup", addr1},
		{&nft.MsgSend{ClassId: "class", Id: "1", Sender: addr1, Receiver: addr1}, "/cosmos.nft.v1beta1.MsgSend", addr1},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			decoded, err := c.Decode(signedTx(t, anyOf(t, tc.msg)))
			require.NoError(t, err)
			require.Len(t, decoded.Msgs, 1)
			m := decoded.Msgs[0]
			assert.Equal(t, tc.typ, m.TypeURL)
			assert.NoError(t, m.Err)
			assert.NotEmpty(t, m.Body, "a body")
			assert.Equal(t, tc.signer, m.Signer)
			assert.Equal(t, []string{tc.signer}, decoded.Signers)
		})
	}
}

// ICS-29 fee messages left ibc-go with v10 and are not carried: the type URL
// stays, the body is nil and the transaction is not dropped.
func TestARetiredFeeMessageKeepsItsTypeURL(t *testing.T) {
	c := newCodec(t)
	fee := &codectypes.Any{TypeUrl: "/ibc.applications.fee.v1.MsgRegisterPayee", Value: []byte{0x0a, 0x06, 'p', 'o', 'r', 't', '-', '1'}}
	decoded, err := c.Decode(rawTx(t, "m", fee, anyOf(t, &banktypes.MsgSend{
		FromAddress: addr1, ToAddress: addr1, Amount: sdk.NewCoins(sdk.NewInt64Coin("ubze", 1)),
	})))
	require.NoError(t, err)
	require.Len(t, decoded.Msgs, 2)
	assert.Equal(t, "/ibc.applications.fee.v1.MsgRegisterPayee", decoded.Msgs[0].TypeURL)
	assert.Nil(t, decoded.Msgs[0].Body)
	assert.Error(t, decoded.Msgs[0].Err)
	assert.NoError(t, decoded.Msgs[1].Err)
}

// ibc-go v10 renders an unset MsgTransfer encoding; the body keeps the shape
// it had before, also nested in an authz MsgExec, and keeps a set encoding.
func TestTransferBodiesDoNotCarryAnEmptyEncoding(t *testing.T) {
	c := newCodec(t)
	transfer := &ibctransfertypes.MsgTransfer{
		SourcePort: "transfer", SourceChannel: "channel-0", Token: sdk.NewInt64Coin("ubze", 5),
		Sender: addr1, Receiver: "osmo1x", TimeoutTimestamp: 1,
	}
	body, err := c.MsgJSON(transfer)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "encoding")

	exec := &authz.MsgExec{Grantee: addr1, Msgs: []*codectypes.Any{anyOf(t, transfer)}}
	body, err = c.MsgJSON(exec)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "encoding")
	assert.Contains(t, string(body), "channel-0")

	transfer.Encoding = "application/x-protobuf"
	body, err = c.MsgJSON(transfer)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"encoding":"application/x-protobuf"`)
}

// Only a MsgTransfer loses its empty encoding: a MsgExec holding a transfer
// next to a message whose payload has an (empty) encoding of its own keeps
// that one.
func TestOnlyTheTransferLosesItsEmptyEncoding(t *testing.T) {
	c := newCodec(t)
	transfer := &ibctransfertypes.MsgTransfer{
		SourcePort: "transfer", SourceChannel: "channel-0", Token: sdk.NewInt64Coin("ubze", 5),
		Sender: addr1, Receiver: "osmo1x", TimeoutTimestamp: 1,
	}
	send := &channeltypesv2.MsgSendPacket{
		SourceClient: "07-tendermint-0", Signer: addr1,
		Payloads: []channeltypesv2.Payload{{SourcePort: "transfer", DestinationPort: "transfer", Version: "v1"}},
	}
	setEncoding := *transfer
	setEncoding.Encoding = "application/x-protobuf"
	exec := &authz.MsgExec{Grantee: addr1, Msgs: []*codectypes.Any{anyOf(t, send), anyOf(t, transfer), anyOf(t, &setEncoding)}}

	body, err := c.MsgJSON(exec)
	require.NoError(t, err)
	var doc struct {
		Msgs []map[string]json.RawMessage `json:"msgs"`
	}
	require.NoError(t, json.Unmarshal(body, &doc))
	require.Len(t, doc.Msgs, 3)

	var payloads []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(doc.Msgs[0]["payloads"], &payloads))
	assert.Contains(t, payloads[0], "encoding", "another message's empty encoding stays")
	assert.NotContains(t, doc.Msgs[1], "encoding", "the transfer's empty encoding goes")
	assert.JSONEq(t, `"application/x-protobuf"`, string(doc.Msgs[2]["encoding"]), "a set encoding stays")
}
