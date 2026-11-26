package transform_test

import (
	"encoding/json"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"
	guardiantypes "github.com/bze-alphateam/bze/x/guardian/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// The guardian's powers run through a grantee's MsgExec: one row per inner
// guardian message, the inner signer as the signer, the grantee in Via, at
// the wrapper's message and the inner message's position.
func TestAuthzExecOfGuardianMessagesGivesNestedEmergencyActions(t *testing.T) {
	in := baseInput()
	in.Block.Txs = []string{
		encodedTx(t, &banktypes.MsgSend{FromAddress: creator, ToAddress: granter, Amount: sdk.NewCoins(sdk.NewInt64Coin("ubze", 1))}),
		encodedTx(t, &authz.MsgExec{Grantee: creator, Msgs: anys(t,
			&banktypes.MsgSend{FromAddress: granter, ToAddress: creator, Amount: sdk.NewCoins(sdk.NewInt64Coin("ubze", 1))},
			&guardiantypes.MsgCloseChannel{Signer: granter, ChannelId: "channel-3"},
			&guardiantypes.MsgReopenChannel{Authority: granter, ChannelId: "channel-4"})}),
	}
	in.Results.TxsResults = []node.TxResult{{}, {}}
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)

	got := ents.NestedEmergencyActions
	require.Len(t, got, 2)
	at := time.Unix(100, 0).UTC()
	assert.Equal(t, transform.NestedEmergencyAction{Height: 10, TxIndex: 1, MsgIndex: 0, Seq: 1, TxHash: got[0].TxHash, Time: at,
		TypeURL: "/bze.guardian.v1.MsgCloseChannel", Signer: granter, Body: got[0].Body, Via: transform.ViaAuthz, Grantee: creator}, got[0])
	assert.NotEmpty(t, got[0].TxHash)
	assert.JSONEq(t, `{"@type":"/bze.guardian.v1.MsgCloseChannel","signer":"`+granter+`","channel_id":"channel-3"}`, string(got[0].Body))
	assert.Equal(t, 2, got[1].Seq)
	assert.Equal(t, "/bze.guardian.v1.MsgReopenChannel", got[1].TypeURL)
	assert.Equal(t, granter, got[1].Signer, "an undo's authority is its signer")
}

// A DAO admin's MsgAdminExecute is the DAO's: Via names the DAO, no proposal.
func TestAdminExecuteOfAGuardianMessageIsTheDaos(t *testing.T) {
	dao := daoAddress(t, 3)
	in := baseInput()
	in.Block.Txs = []string{encodedTx(t, &daotypes.MsgAdminExecute{Admin: creator, DaoId: 3,
		Msgs: anys(t, &guardiantypes.MsgBlockDenom{Signer: dao, Denom: "ibc/AAA"})})}
	in.Results.TxsResults = []node.TxResult{{}}
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)

	require.Len(t, ents.NestedEmergencyActions, 1)
	a := ents.NestedEmergencyActions[0]
	assert.Equal(t, transform.ViaDao, a.Via)
	assert.Equal(t, int64(3), a.DaoID)
	assert.Zero(t, a.ProposalID)
	assert.Empty(t, a.Grantee)
	assert.Equal(t, dao, a.Signer)
	assert.Equal(t, "/bze.guardian.v1.MsgBlockDenom", a.TypeURL)
}

// A wrapper inside a wrapper is unwrapped like the other nested rows; the
// outermost wrapper names the path.
func TestAWrapperInsideAWrapperIsNamedByTheOutermost(t *testing.T) {
	dao := daoAddress(t, 3)
	in := baseInput()
	admin := &daotypes.MsgAdminExecute{Admin: granter, DaoId: 3, Msgs: anys(t, &guardiantypes.MsgPauseAutomation{Signer: dao})}
	in.Block.Txs = []string{encodedTx(t, &authz.MsgExec{Grantee: creator, Msgs: anys(t, admin)})}
	in.Results.TxsResults = []node.TxResult{{}}
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)

	require.Len(t, ents.NestedEmergencyActions, 1)
	a := ents.NestedEmergencyActions[0]
	assert.Equal(t, transform.ViaAuthz, a.Via)
	assert.Equal(t, creator, a.Grantee)
	assert.Equal(t, dao, a.Signer)
	assert.Equal(t, 0, a.Seq)
}

// A failed transaction ran nothing, a top-level guardian message is a
// transaction message (no nested row), and a wrapper of other messages
// gives none.
func TestNoNestedEmergencyActionsOutsideASuccessfulWrapper(t *testing.T) {
	for name, tc := range map[string]struct {
		msg  sdk.Msg
		code uint32
	}{
		"failed exec": {&authz.MsgExec{Grantee: creator, Msgs: anys(t, &guardiantypes.MsgCloseChannel{Signer: granter, ChannelId: "channel-3"})}, 5},
		"top level":   {&guardiantypes.MsgCloseChannel{Signer: granter, ChannelId: "channel-3"}, 0},
		"other inner": {&authz.MsgExec{Grantee: creator, Msgs: anys(t, &banktypes.MsgSend{FromAddress: granter, ToAddress: creator})}, 0},
	} {
		in := baseInput()
		in.Block.Txs = []string{encodedTx(t, tc.msg)}
		in.Results.TxsResults = []node.TxResult{{Code: tc.code}}
		ents, err := realTransformer(t).Transform(in)
		require.NoError(t, err, name)
		assert.Empty(t, ents.NestedEmergencyActions, name)
	}
}

// An executed DAO proposal's guardian messages take the proposal's seq range
// in its settle and name the DAO and the proposal.
func TestSettledEmergencyActionsOfAnExecutedProposal(t *testing.T) {
	dao := daoAddress(t, 3)
	msgs := json.RawMessage(`[{"@type":"/cosmos.bank.v1beta1.MsgSend","from_address":"` + dao + `"},` +
		`{"@type":"/bze.guardian.v1.MsgHaltTradingDenoms","signer":"` + dao + `","denoms":["ubze"]},` +
		`{"@type":"/cosmos.authz.v1beta1.MsgExec","grantee":"` + dao + `","msgs":[` +
		`{"@type":"/bze.guardian.v1.MsgUnblockDenom","authority":"` + granter + `","denom":"ibc/AAA"}]}]`)
	at := time.Unix(300, 0).UTC()
	got := transform.SettledEmergencyActions(msgs, transform.SettledProposal{ID: 9, DaoID: 3, Height: 20, TxIndex: 2, MsgIndex: 1,
		TxHash: "SETTLE", Time: at, Ordinal: 1})

	require.Len(t, got, 2)
	assert.Equal(t, transform.NestedEmergencyAction{Height: 20, TxIndex: 2, MsgIndex: 1, Seq: 2*transform.NestedSeqBase + 1,
		TxHash: "SETTLE", Time: at, TypeURL: "/bze.guardian.v1.MsgHaltTradingDenoms", Signer: dao, Body: got[0].Body,
		Via: transform.ViaDao, DaoID: 3, ProposalID: 9}, got[0])
	assert.Equal(t, 2*transform.NestedSeqBase+2, got[1].Seq, "the exec's inner message, unwrapped in place")
	assert.Equal(t, granter, got[1].Signer)
	assert.Equal(t, transform.ViaDao, got[1].Via, "the proposal is the outermost wrapper")

	assert.Empty(t, transform.SettledEmergencyActions(nil, transform.SettledProposal{ID: 9}))
	assert.Empty(t, transform.SettledEmergencyActions(json.RawMessage(`[{"@type":"/cosmos.bank.v1beta1.MsgSend"}]`), transform.SettledProposal{ID: 9}))
}
