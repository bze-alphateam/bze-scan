package transform_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"
	tokenfactorytypes "github.com/bze-alphateam/bze/x/tokenfactory/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// The chain runs a DAO's messages (MsgAdminExecute, a settled proposal) and
// an authz grantee's (MsgExec) inside the wrapper: their events carry the
// wrapper's msg_index, their bodies are the wrapper's (or, for a settle, the
// proposal's stored msgs). No recorded height has any, so these messages
// are encoded with the chain codec and given the events the chain emits.

const (
	granter  = "bze1granter0granter0granter0granter0grant0"
	operator = "bzevaloper1operatoroperatoroperatoroperatorop"
)

var tokenFactory = chain.ModuleAddress(tokenfactorytypes.ModuleName)

// daoAddress is the account of DAO id, as the chain derives it.
func daoAddress(t *testing.T, id uint64) string {
	t.Helper()
	addr, err := bech32.ConvertAndEncode(chain.Bech32Prefix, daotypes.DaoAddress(id))
	require.NoError(t, err)
	return addr
}

func anys(t *testing.T, msgs ...sdk.Msg) []*codectypes.Any {
	t.Helper()
	out := make([]*codectypes.Any, len(msgs))
	for i, m := range msgs {
		a, err := codectypes.NewAnyWithValue(m)
		require.NoError(t, err)
		out[i] = a
	}
	return out
}

// mintEvents are the bank events of a tokenfactory mint of amount to admin,
// under message j.
func mintEvents(j, admin, amount string) []node.Event {
	return []node.Event{
		withIndex(coinReceived(tokenFactory, amount), j),
		withIndex(coinbase(tokenFactory, amount), j),
		withIndex(transfer(tokenFactory, admin, amount), j),
	}
}

// burnEvents are the bank events of a tokenfactory burn of amount by admin,
// under message j.
func burnEvents(j, admin, amount string) []node.Event {
	return []node.Event{
		withIndex(transfer(admin, tokenFactory, amount), j),
		withIndex(node.Event{Type: "burn", Attributes: []node.Attribute{
			{Key: "burner", Value: tokenFactory}, {Key: "amount", Value: amount}}}, j),
	}
}

func TestAdminExecuteMintIsMintedByTheDao(t *testing.T) {
	dao := daoAddress(t, 3)
	honeyOfDao := "factory/" + dao + "/uhoney"
	raw := encodedTx(t, &daotypes.MsgAdminExecute{Admin: creator, DaoId: 3,
		Msgs: anys(t, &tokenfactorytypes.MsgMint{Creator: dao, Coins: "40" + honeyOfDao})})
	ents := transformDaoTx(t, append(mintEvents("0", dao, "40"+honeyOfDao),
		typed("bze.dao.EventAdminExecuted", "0", "id", "3", "admin", creator, "msg_count", "1")), raw)
	at := time.Unix(100, 0).UTC()
	assert.Equal(t, []transform.TokenEvent{{Height: 10, TxIndex: 0, Seq: 0, Denom: honeyOfDao, Kind: "minted", Actor: dao,
		Amount: "40", Details: map[string]any{"recipient": dao}, Time: at}}, ents.TokenEvents)
	assert.Contains(t, ents.Dirty.Keys(statesync.Denoms), honeyOfDao)
}

// Regression: an authz grantee minting for the granter was never seen.
func TestAuthzExecMintIsMinted(t *testing.T) {
	raw := encodedTx(t, &authz.MsgExec{Grantee: creator,
		Msgs: anys(t, &tokenfactorytypes.MsgMint{Creator: granter, Coins: "9" + honey})})
	ents := transformDaoTx(t, mintEvents("0", granter, "9"+honey), raw)
	require.Len(t, ents.TokenEvents, 1)
	e := ents.TokenEvents[0]
	assert.Equal(t, "minted", e.Kind)
	assert.Equal(t, granter, e.Actor, "the granter received and holds the mint")
	assert.Equal(t, "9", e.Amount)
}

// The body-only facts of an admin execution come from its inner bodies, the
// denom's previous admin from the matching event; the burn from its events.
func TestAdminExecuteBodiesAndBurn(t *testing.T) {
	dao := daoAddress(t, 3)
	h := "factory/" + dao + "/uhoney"
	raw := encodedTx(t, &daotypes.MsgAdminExecute{Admin: creator, DaoId: 3, Msgs: anys(t,
		&tokenfactorytypes.MsgBurn{Creator: dao, Coins: "2" + h},
		&tokenfactorytypes.MsgSetDenomMetadata{Creator: dao, Metadata: banktypes.Metadata{Base: h, Symbol: "HNY", Name: "Honey", Display: "HNY"}},
		&tokenfactorytypes.MsgChangeAdmin{Creator: dao, Denom: h, NewAdmin: newAdmin},
		&stakingtypes.MsgEditValidator{ValidatorAddress: operator, Description: stakingtypes.Description{
			Moniker: "DAO node", Identity: "[do-not-modify]", Website: "[do-not-modify]",
			SecurityContact: "[do-not-modify]", Details: "[do-not-modify]"}},
	)})
	events := append(burnEvents("0", dao, "2"+h),
		typed("bze.tokenfactory.DenomMetadataChangeEvent", "0", "denom", h),
		typed("bze.tokenfactory.DenomAdminChangeEvent", "0", "admin", dao, "new_admin", newAdmin, "denom", h))
	ents := transformDaoTx(t, events, raw)
	kinds := map[string]transform.TokenEvent{}
	for _, e := range ents.TokenEvents {
		kinds[e.Kind] = e
	}
	require.Len(t, ents.TokenEvents, 3)
	assert.Equal(t, dao, kinds["metadata_changed"].Actor)
	assert.Equal(t, map[string]any{"from": dao, "to": newAdmin}, kinds["admin_changed"].Details)
	assert.Equal(t, dao, kinds["burned"].Actor)
	assert.Equal(t, "2", kinds["burned"].Amount)

	require.Len(t, ents.ValidatorEvents, 1)
	assert.Equal(t, transform.ValidatorDescriptionChanged, ents.ValidatorEvents[0].Kind)
	assert.Equal(t, operator, ents.ValidatorEvents[0].Operator)
	assert.Contains(t, ents.Dirty.Keys(statesync.Validators), operator)
}

// A settle's mints and burns come from its events (with the DAO, which the
// transfer names, as actor); the body-only rows from NestedActions, never a
// second burn.
func TestSettleBurnAndMetadata(t *testing.T) {
	dao := daoAddress(t, 4)
	h := "factory/" + dao + "/uhoney"
	raw := encodedTx(t, &daotypes.MsgSettleProposal{Sender: creator, ProposalId: 6})
	events := append(burnEvents("0", dao, "5"+h),
		typed("bze.tokenfactory.DenomMetadataChangeEvent", "0", "denom", h),
		executedEv("0", "6", true, ""),
		settled("0", "6", "EXECUTED", "700", "100", "50", false))
	ents := transformDaoTx(t, events, raw)
	require.Len(t, ents.TokenEvents, 1, "only the burn: the settle's body holds no messages")
	assert.Equal(t, "burned", ents.TokenEvents[0].Kind)
	assert.Equal(t, dao, ents.TokenEvents[0].Actor)

	msgs := proposalMsgs(t,
		&tokenfactorytypes.MsgBurn{Creator: dao, Coins: "5" + h},
		&tokenfactorytypes.MsgSetDenomMetadata{Creator: dao, Metadata: banktypes.Metadata{Base: h, Symbol: "HNY", Name: "Honey", Display: "HNY"}},
	)
	at := time.Unix(200, 0).UTC()
	nested, _ := transform.NestedActions(msgs, 10, 0, at, 1)
	assert.Equal(t, []transform.TokenEvent{{Height: 10, TxIndex: 0, Seq: 2000, Denom: h, Kind: "metadata_changed", Actor: dao,
		Details: map[string]any{"symbol": "HNY", "name": "Honey", "display": "HNY"}, Time: at}}, nested.TokenEvents,
		"the second proposal settled in the transaction: seqs from 2000, no burn")
	assert.Contains(t, nested.Dirty.Keys(statesync.Denoms), h)
}

func TestNestedActionsOfValidatorMessagesInsideAnAuthzExec(t *testing.T) {
	dao := daoAddress(t, 4)
	msgs := proposalMsgs(t, &authz.MsgExec{Grantee: dao, Msgs: anys(t,
		&stakingtypes.MsgEditValidator{ValidatorAddress: operator, Description: stakingtypes.Description{
			Moniker: "[do-not-modify]", Identity: "[do-not-modify]", Website: "https://dao.example",
			SecurityContact: "[do-not-modify]", Details: "[do-not-modify]"}},
	)})
	nested, _ := transform.NestedActions(msgs, 12, 3, time.Unix(1, 0).UTC(), 0)
	require.Len(t, nested.ValidatorEvents, 1)
	v := nested.ValidatorEvents[0]
	assert.Equal(t, 1000, v.Seq)
	assert.Equal(t, 3, v.TxIndex)
	assert.Equal(t, map[string]any{"to": map[string]any{"website": "https://dao.example"}}, v.Details)
	assert.Contains(t, nested.Dirty.Keys(statesync.Validators), operator)
	empty, dropped := transform.NestedActions(nil, 12, 3, time.Unix(1, 0), 0)
	assert.Empty(t, empty.TokenEvents, "no msgs, no rows")
	assert.Zero(t, dropped)
}

// proposalMsgs is a proposal's msgs as the explorer stores them: proto JSON
// with "@type", as the codec renders a MsgSubmitProposal body.
func proposalMsgs(t *testing.T, msgs ...sdk.Msg) json.RawMessage {
	t.Helper()
	body, err := chainCodec(t).MsgJSON(&daotypes.MsgSubmitProposal{Proposer: creator, DaoId: 4, Msgs: anys(t, msgs...)})
	require.NoError(t, err)
	var b struct {
		Msgs json.RawMessage `json:"msgs"`
	}
	require.NoError(t, json.Unmarshal(body, &b))
	return b.Msgs
}

// A DAO voting in governance through a settle votes as itself: the
// proposal_vote event names it.
func TestSettleGovVoteIsTheDaos(t *testing.T) {
	dao := daoAddress(t, 4)
	raw := encodedTx(t, &daotypes.MsgSettleProposal{Sender: creator, ProposalId: 6})
	ents := transformDaoTx(t, []node.Event{
		withIndex(node.Event{Type: "proposal_vote", Attributes: []node.Attribute{
			{Key: "voter", Value: dao}, {Key: "option", Value: `[{"option":1,"weight":"1.000000000000000000"}]`},
			{Key: "proposal_id", Value: "47"}}}, "0"),
		executedEv("0", "6", true, ""),
		settled("0", "6", "EXECUTED", "700", "100", "50", false),
	}, raw)
	require.Len(t, ents.ProposalVotes, 1)
	assert.Equal(t, dao, ents.ProposalVotes[0].Voter, "not the settler")
	assert.Equal(t, "yes", ents.ProposalVotes[0].Option)
}

// A settle running a bank send from the treasury: the DAO sent, the
// recipient received.
func TestSettleSendGivesTheDaoActivity(t *testing.T) {
	dao := daoAddress(t, 4)
	raw := encodedTx(t, &daotypes.MsgSettleProposal{Sender: creator, ProposalId: 6})
	ents := transformDaoTx(t, []node.Event{
		withIndex(node.Event{Type: "coin_spent", Attributes: []node.Attribute{{Key: "spender", Value: dao}, {Key: "amount", Value: "25ubze"}}}, "0"),
		withIndex(coinReceived(newAdmin, "25ubze"), "0"),
		withIndex(transfer(dao, newAdmin, "25ubze"), "0"),
		executedEv("0", "6", true, ""),
		settled("0", "6", "EXECUTED", "700", "100", "50", false),
	}, raw)
	byAddr := map[string]transform.Activity{}
	for _, a := range ents.Activity {
		byAddr[a.Address] = a
	}
	require.Contains(t, byAddr, dao)
	require.Contains(t, byAddr, newAdmin)
	assert.Equal(t, "sent", byAddr[dao].Category)
	assert.Equal(t, []chain.Coin{{Denom: "ubze", Amount: "-25"}}, byAddr[dao].Deltas)
	assert.Equal(t, "received", byAddr[newAdmin].Category)
	assert.Equal(t, []chain.Coin{{Denom: "ubze", Amount: "25"}}, byAddr[newAdmin].Deltas)
}

// A gov proposal an authz grantee submits for the granter gets its row from
// the inner body and the wrapper's submit_proposal event.
func TestAuthzExecGovSubmission(t *testing.T) {
	raw := encodedTx(t, &authz.MsgExec{Grantee: creator, Msgs: anys(t,
		&govv1.MsgSubmitProposal{Proposer: granter, Title: "Fund it", Summary: "Please"})})
	ents := transformDaoTx(t, []node.Event{
		withIndex(node.Event{Type: "submit_proposal", Attributes: []node.Attribute{
			{Key: "proposal_id", Value: "51"}, {Key: "proposal_proposer", Value: granter}}}, "0"),
		withIndex(node.Event{Type: "submit_proposal", Attributes: []node.Attribute{
			{Key: "voting_period_start", Value: "51"}}}, "0"),
	}, raw)
	require.Len(t, ents.Proposals, 1)
	p := ents.Proposals[0]
	assert.Equal(t, uint64(51), p.ID)
	assert.Equal(t, "Fund it", p.Title)
	assert.Equal(t, granter, p.Proposer)
	assert.Equal(t, "voting_period", p.Status)
	assert.Contains(t, ents.Dirty.Keys(statesync.Proposals), "51")
}

// Token and validator rows of one proposal each count from the proposal's
// base, like the transaction's own rows.
func TestNestedActionsNumberTokenAndValidatorRowsApart(t *testing.T) {
	dao := daoAddress(t, 4)
	h := "factory/" + dao + "/uhoney"
	msgs := proposalMsgs(t,
		&tokenfactorytypes.MsgSetDenomBranding{Creator: dao, Denom: h},
		&stakingtypes.MsgEditValidator{ValidatorAddress: operator, CommissionRate: nil, Description: stakingtypes.Description{
			Moniker: "DAO node", Identity: "[do-not-modify]", Website: "[do-not-modify]",
			SecurityContact: "[do-not-modify]", Details: "[do-not-modify]"}},
	)
	nested, _ := transform.NestedActions(msgs, 12, 0, time.Unix(1, 0).UTC(), 0)
	require.Len(t, nested.TokenEvents, 1)
	require.Len(t, nested.ValidatorEvents, 1)
	assert.Equal(t, 1000, nested.TokenEvents[0].Seq)
	assert.Equal(t, 1000, nested.ValidatorEvents[0].Seq)
}

// The typed token events of an admin execution name the DAO, derived from
// the body's dao_id, not the admin who signed.
func TestAdminExecuteTypedEventsNameTheDao(t *testing.T) {
	dao := daoAddress(t, 3)
	h := "factory/" + dao + "/uhoney"
	raw := encodedTx(t, &daotypes.MsgAdminExecute{Admin: creator, DaoId: 3,
		Msgs: anys(t, &tokenfactorytypes.MsgSetDenomConfig{Creator: dao, Denom: h})})
	ents := transformDaoTx(t, []node.Event{typedRaw("bze.tokenfactory.DenomConfigChangeEvent", "0",
		"denom", `"`+h+`"`, "community_pool", `"0"`, "transfer_fee", `"0"`, "gas_surcharge", `"0"`)}, raw)
	require.Len(t, ents.TokenEvents, 1)
	assert.Equal(t, "config_changed", ents.TokenEvents[0].Kind)
	assert.Equal(t, dao, ents.TokenEvents[0].Actor)
}

// A settle that refunds the proposer's deposit: the proposer received; the
// settler who signed keeps the wrapper's own category.
func TestSettleDepositRefundIsReceived(t *testing.T) {
	raw := encodedTx(t, &daotypes.MsgSettleProposal{Sender: creator, ProposalId: 6})
	ents := transformDaoTx(t, []node.Event{
		withIndex(coinReceived(newAdmin, "1000ubze"), "0"),
		withIndex(transfer(chain.ModuleAddress(daotypes.ModuleName), newAdmin, "1000ubze"), "0"),
		typedRaw("bze.dao.EventDepositReleased", "0", "id", `"6"`, "refunded", "true", "destination", `"`+newAdmin+`"`, "amount", `"1000ubze"`),
		settled("0", "6", "REJECTED", "1", "9", "0", false),
	}, raw)
	byAddr := map[string]transform.Activity{}
	for _, a := range ents.Activity {
		byAddr[a.Address] = a
	}
	assert.Equal(t, "received", byAddr[newAdmin].Category)
	assert.Equal(t, "governance", byAddr[creator].Category, "the settler signs: unchanged")
}

// branding is a stored proposal message that gives one token row.
const branding = `{"@type":"/bze.tokenfactory.MsgSetDenomBranding","creator":"` + creator + `","denom":"` + honey + `"}`

// wrapped puts msg inside n authz execs.
func wrapped(msg string, n int) string {
	for i := 0; i < n; i++ {
		msg = `{"@type":"/cosmos.authz.v1beta1.MsgExec","grantee":"` + creator + `","msgs":[` + msg + `]}`
	}
	return msg
}

func TestNestedActionsStopAtTheDepthBound(t *testing.T) {
	at := time.Unix(1, 0).UTC()
	deep, _ := transform.NestedActions(json.RawMessage(`[`+wrapped(branding, 15)+`]`), 1, 0, at, 0)
	assert.Len(t, deep.TokenEvents, 1, "fifteen wrappers deep: read")
	tooDeep, _ := transform.NestedActions(json.RawMessage(`[`+wrapped(branding, 16)+`]`), 1, 0, at, 0)
	assert.Empty(t, tooDeep.TokenEvents, "sixteen wrappers deep: not unwrapped")
}

func TestNestedActionsSkipMalformedMessages(t *testing.T) {
	at := time.Unix(1, 0).UTC()
	for _, raw := range []string{`{"msgs":[]}`, `"x"`, `[1,`, `null`} {
		ents, dropped := transform.NestedActions(json.RawMessage(raw), 1, 0, at, 0)
		assert.Empty(t, ents.TokenEvents, raw)
		assert.Zero(t, dropped, raw)
	}
	ents, _ := transform.NestedActions(json.RawMessage(`[{"creator":"x"},{"@type":5},"str",{"@type":""},`+
		`{"@type":"/cosmos.authz.v1beta1.MsgExec","msgs":{"not":"an array"}},`+branding+`]`), 1, 0, at, 0)
	assert.Len(t, ents.TokenEvents, 1, "only the well-formed message")
}

// The inner messages of an admin execution or an authz exec in a
// transaction's body, when malformed, give nothing and no panic.
func TestMalformedWrapperBodiesInATransaction(t *testing.T) {
	raw := encodedTx(t, &authz.MsgExec{Grantee: creator})
	ents := transformDaoTx(t, nil, raw)
	assert.Empty(t, ents.TokenEvents)
}

// A proposal's range holds NestedSeqBase rows per table; the next is
// dropped and counted.
func TestNestedActionsRangeBoundary(t *testing.T) {
	rows := func(n int) json.RawMessage {
		items := make([]string, n)
		for i := range items {
			items[i] = branding
		}
		return json.RawMessage("[" + strings.Join(items, ",") + "]")
	}
	at := time.Unix(1, 0).UTC()
	full, dropped := transform.NestedActions(rows(transform.NestedSeqBase), 1, 0, at, 0)
	assert.Len(t, full.TokenEvents, transform.NestedSeqBase)
	assert.Equal(t, 2*transform.NestedSeqBase-1, full.TokenEvents[len(full.TokenEvents)-1].Seq)
	assert.Zero(t, dropped)
	over, dropped := transform.NestedActions(rows(transform.NestedSeqBase+1), 1, 0, at, 0)
	assert.Len(t, over.TokenEvents, transform.NestedSeqBase)
	assert.Equal(t, 1, dropped)
}

// A failed transaction runs nothing: no rows through the wrappers.
func TestFailedWrapperTransactionsGiveNoRows(t *testing.T) {
	dao := daoAddress(t, 3)
	h := "factory/" + dao + "/uhoney"
	for name, msg := range map[string]sdk.Msg{
		"authz exec": &authz.MsgExec{Grantee: creator, Msgs: anys(t,
			&tokenfactorytypes.MsgMint{Creator: granter, Coins: "9" + honey},
			&tokenfactorytypes.MsgSetDenomBranding{Creator: granter, Denom: honey},
			&slashingtypes.MsgUnjail{ValidatorAddr: operator},
			&govv1.MsgSubmitProposal{Proposer: granter, Title: "x"})},
		"admin execute": &daotypes.MsgAdminExecute{Admin: creator, DaoId: 3, Msgs: anys(t,
			&tokenfactorytypes.MsgMint{Creator: dao, Coins: "9" + h},
			&tokenfactorytypes.MsgSetDenomBranding{Creator: dao, Denom: h},
			&slashingtypes.MsgUnjail{ValidatorAddr: operator})},
	} {
		in := baseInput()
		in.Block.Txs = []string{encodedTx(t, msg)}
		events := append(mintEvents("0", granter, "9"+honey), withIndex(node.Event{Type: "submit_proposal",
			Attributes: []node.Attribute{{Key: "proposal_id", Value: "51"}}}, "0"))
		in.Results.TxsResults = []node.TxResult{{Code: 5, Events: events}}
		ents, err := realTransformer(t).Transform(in)
		require.NoError(t, err, name)
		assert.Empty(t, ents.TokenEvents, name)
		assert.Empty(t, ents.ValidatorEvents, name)
		assert.Empty(t, ents.Proposals, name)
		assert.NotContains(t, ents.Dirty.Keys(statesync.Validators), operator, name)
	}
}

// The chain's Burn moves the coins from the creator to the module first:
// inside an authz exec the burner is the granter.
func TestAuthzExecBurnIsTheGranters(t *testing.T) {
	raw := encodedTx(t, &authz.MsgExec{Grantee: creator, Msgs: anys(t,
		&tokenfactorytypes.MsgMint{Creator: granter, Coins: "9" + honey},
		&tokenfactorytypes.MsgBurn{Creator: granter, Coins: "4" + honey})})
	ents := transformDaoTx(t, append(mintEvents("0", granter, "9"+honey), burnEvents("0", granter, "4"+honey)...), raw)
	require.Len(t, ents.TokenEvents, 2)
	assert.Equal(t, "minted", ents.TokenEvents[0].Kind)
	assert.Equal(t, granter, ents.TokenEvents[0].Actor)
	assert.Equal(t, "burned", ents.TokenEvents[1].Kind)
	assert.Equal(t, granter, ents.TokenEvents[1].Actor)
	assert.Equal(t, "4", ents.TokenEvents[1].Amount)
	assert.Equal(t, 1, ents.TokenEvents[1].Seq)
}
