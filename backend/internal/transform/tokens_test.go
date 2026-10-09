package transform_test

import (
	"encoding/base64"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tokenfactorytypes "github.com/bze-alphateam/bze/x/tokenfactory/types"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/denoms"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// No recorded mainnet height carries tokenfactory or tradebin token
// activity (branding and halts only exist from chain v8.2.0), so these tests
// encode real messages with the chain codec and give them the events the
// chain emits: the decoded bodies are exactly what the indexer sees. The
// cases are verified live during the mainnet soak.
const (
	creator  = "bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk"
	newAdmin = "bze15pqjgk4la0mfphwddce00d05n3th3u66n3ptcv"
	honey    = "factory/" + creator + "/uhoney"
	lpDenom  = "ulp/2D1B"
	haltedIB = "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4"
)

// encodedTx is the base64 raw transaction carrying msgs.
func encodedTx(t *testing.T, msgs ...sdk.Msg) string {
	t.Helper()
	anys := make([]*codectypes.Any, len(msgs))
	for i, m := range msgs {
		a, err := codectypes.NewAnyWithValue(m)
		require.NoError(t, err)
		anys[i] = a
	}
	body, err := (&tx.TxBody{Messages: anys}).Marshal()
	require.NoError(t, err)
	auth, err := (&tx.AuthInfo{Fee: &tx.Fee{Amount: sdk.NewCoins(sdk.NewInt64Coin("ubze", 1))}}).Marshal()
	require.NoError(t, err)
	raw, err := (&tx.TxRaw{BodyBytes: body, AuthInfoBytes: auth}).Marshal()
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(raw)
}

// typed is a typed event: attribute values JSON-encoded, as the chain emits
// them, with the message index.
func typed(typ, msgIndex string, kv ...string) node.Event {
	ev := node.Event{Type: typ}
	for i := 0; i < len(kv); i += 2 {
		ev.Attributes = append(ev.Attributes, node.Attribute{Key: kv[i], Value: `"` + kv[i+1] + `"`})
	}
	if msgIndex != "" {
		ev = withIndex(ev, msgIndex)
	}
	return ev
}

func tokenInput(t *testing.T) transform.Input {
	in := baseInput()
	in.Block.Txs = []string{
		encodedTx(t,
			&tokenfactorytypes.MsgCreateDenom{Creator: creator, Subdenom: "uhoney"},
			&tokenfactorytypes.MsgMint{Creator: creator, Coins: "100" + honey},
			&tokenfactorytypes.MsgBurn{Creator: creator, Coins: "5" + honey},
			&tokenfactorytypes.MsgSetDenomMetadata{Creator: creator, Metadata: banktypes.Metadata{
				Base: honey, Display: "HONEY", Symbol: "HONEY", Name: "Honey",
				DenomUnits: []*banktypes.DenomUnit{{Denom: honey}, {Denom: "HONEY", Exponent: 6}},
			}},
			&tokenfactorytypes.MsgSetDenomBranding{Creator: creator, Denom: honey},
			&tokenfactorytypes.MsgChangeAdmin{Creator: creator, Denom: honey, NewAdmin: newAdmin},
			&tradebintypes.MsgCreateMarket{Creator: creator, Base: honey, Quote: "ubze"},
			&tradebintypes.MsgCreateLiquidityPool{Creator: creator, Base: honey, Quote: "ubze",
				InitialBase: sdkmath.NewInt(1), InitialQuote: sdkmath.NewInt(1)},
		),
		encodedTx(t, &tokenfactorytypes.MsgMint{Creator: creator, Coins: "7" + honey}),
	}
	in.Results.TxsResults[0].Events = []node.Event{
		typed("bze.tokenfactory.DenomMetadataChangeEvent", "3", "denom", honey),
		typed("bze.tokenfactory.DenomBrandingChangeEvent", "4", "denom", honey),
		typed("bze.tokenfactory.DenomAdminChangeEvent", "5", "admin", creator, "new_admin", newAdmin, "denom", honey),
		typed("bze.tradebin.MarketCreatedEvent", "6", "creator", creator, "base", honey, "quote", "ubze"),
		typed("bze.tradebin.PoolCreatedEvent", "7", "creator", creator, "base", honey, "quote", "ubze", "lp_denom", lpDenom),
	}
	// Governance enacts halts in EndBlock: block-level events.
	in.Results.FinalizeBlockEvents = []node.Event{
		typed("bze.tradebin.DenomHaltedEvent", "", "denom", haltedIB),
		typed("bze.tradebin.DenomUnhaltedEvent", "", "denom", "ibc/OLD"),
	}
	return in
}

func TestTokenEvents(t *testing.T) {
	ents, err := realTransformer(t).Transform(tokenInput(t))
	require.NoError(t, err)
	at := time.Unix(100, 0).UTC()
	market := map[string]any{"market_id": honey + "/ubze", "base": honey, "quote": "ubze"}
	pool := map[string]any{"base": honey, "quote": "ubze", "lp_denom": lpDenom}
	assert.Equal(t, []transform.TokenEvent{
		{Height: 10, TxIndex: -1, Seq: 0, Denom: haltedIB, Kind: "halted", Time: at},
		{Height: 10, TxIndex: -1, Seq: 1, Denom: "ibc/OLD", Kind: "unhalted", Time: at},
		{Height: 10, TxIndex: 0, Seq: 0, Denom: honey, Kind: "created", Actor: creator, Details: map[string]any{"subdenom": "uhoney"}, Time: at},
		{Height: 10, TxIndex: 0, Seq: 1, Denom: honey, Kind: "minted", Actor: creator, Amount: "100", Details: map[string]any{"recipient": creator}, Time: at},
		{Height: 10, TxIndex: 0, Seq: 2, Denom: honey, Kind: "burned", Actor: creator, Amount: "5", Time: at},
		{Height: 10, TxIndex: 0, Seq: 3, Denom: honey, Kind: "metadata_changed", Actor: creator,
			Details: map[string]any{"symbol": "HONEY", "name": "Honey", "display": "HONEY"}, Time: at},
		{Height: 10, TxIndex: 0, Seq: 4, Denom: honey, Kind: "branding_changed", Actor: creator, Time: at},
		{Height: 10, TxIndex: 0, Seq: 5, Denom: honey, Kind: "admin_changed", Actor: creator,
			Details: map[string]any{"from": creator, "to": newAdmin}, Time: at},
		{Height: 10, TxIndex: 0, Seq: 6, Denom: honey, Kind: "market_created", Actor: creator, Details: market, Time: at},
		{Height: 10, TxIndex: 0, Seq: 7, Denom: "ubze", Kind: "market_created", Actor: creator, Details: market, Time: at},
		{Height: 10, TxIndex: 0, Seq: 8, Denom: honey, Kind: "pool_created", Actor: creator, Details: pool, Time: at},
		{Height: 10, TxIndex: 0, Seq: 9, Denom: "ubze", Kind: "pool_created", Actor: creator, Details: pool, Time: at},
		{Height: 10, TxIndex: 0, Seq: 10, Denom: lpDenom, Kind: "pool_created", Actor: creator, Details: pool, Time: at},
	}, ents.TokenEvents, "the failed mint of the second transaction is no event")

	assert.Equal(t, []string{honey, haltedIB, "ibc/OLD", "ubze", lpDenom},
		ents.Dirty.Keys(statesync.Denoms), "every denom a token event names")
}

// A denom a transaction moves is "seen"; the denoms set resyncs it only when
// it does not hold it yet.
func TestMovedDenomsAreSeen(t *testing.T) {
	in := tokenInput(t)
	in.Results.TxsResults[1].Events = []node.Event{transfer("bze1a", "bze1b", "3"+haltedIB)}
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	assert.Contains(t, ents.Dirty.Keys(statesync.Denoms), denoms.SeenKey(haltedIB))
}

func TestAdminChangeWithoutItsEventKeepsTheNewAdmin(t *testing.T) {
	in := baseInput()
	in.Block.Txs = []string{encodedTx(t, &tokenfactorytypes.MsgChangeAdmin{Creator: creator, Denom: honey})}
	in.Results.TxsResults = []node.TxResult{{}}
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	require.Len(t, ents.TokenEvents, 1)
	assert.Equal(t, map[string]any{"to": ""}, ents.TokenEvents[0].Details, "renounced: the new admin is empty")
}

func TestTokenEventsRejectABadMintAmount(t *testing.T) {
	in := baseInput()
	in.Block.Txs = []string{encodedTx(t, &tokenfactorytypes.MsgMint{Creator: creator, Coins: "lots"})}
	in.Results.TxsResults = []node.TxResult{{}}
	_, err := realTransformer(t).Transform(in)
	require.ErrorContains(t, err, "MsgMint coins")
}
