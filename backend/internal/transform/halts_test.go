package transform_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	guardiantypes "github.com/bze-alphateam/bze/x/guardian/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// The guardian's MsgHaltChain (chain v8.3.0) schedules an emergency halt at
// the next block: the transaction's ChainHaltScheduledEvent becomes an
// upgrade row of kind emergency. No network has one yet; the event is the
// chain's typed-event encoding (height, an int64, as a JSON string).
func TestChainHaltScheduledInATransactionIsAnEmergencyHalt(t *testing.T) {
	in := baseInput()
	in.Block.Txs = []string{encodedTx(t, &guardiantypes.MsgHaltChain{Signer: creator, Reason: "exploit"})}
	in.Results.TxsResults = []node.TxResult{{Events: []node.Event{
		typed("bze.guardian.v1.ChainHaltScheduledEvent", "0",
			"signer", creator, "name", "emergency-halt-11", "height", "11", "reason", "exploit"),
	}}}
	tr, hook := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	assert.Empty(t, hook.AllEntries())
	require.Len(t, ents.Transactions, 1)
	assert.Equal(t, []transform.EmergencyHalt{{Height: 11, Name: "emergency-halt-11", Reason: "exploit",
		ScheduledBy: creator, ScheduledHeight: 10, TxHash: ents.Transactions[0].Hash}}, ents.EmergencyHalts)
}

// Governance runs MsgHaltChain in its EndBlocker: the event is a block
// event, the row has no transaction. A plain JSON number is read too.
func TestChainHaltScheduledAtBlockLevelIsAnEmergencyHalt(t *testing.T) {
	in := baseInput()
	in.Block.Txs, in.Results.TxsResults = nil, nil
	in.Results.FinalizeBlockEvents = []node.Event{{Type: "bze.guardian.v1.ChainHaltScheduledEvent", Attributes: []node.Attribute{
		{Key: "signer", Value: `"` + govModule + `"`}, {Key: "height", Value: `11`}, {Key: "reason", Value: `"proposal 9"`},
	}}}
	tr, hook := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	assert.Empty(t, hook.AllEntries())
	assert.Equal(t, []transform.EmergencyHalt{{Height: 11, Name: "emergency-halt-11", Reason: "proposal 9",
		ScheduledBy: govModule, ScheduledHeight: 10}}, ents.EmergencyHalts, "the plan name from the height when missing")
	require.Len(t, ents.BlockEvents, 1, "still a block event")
}

// A failed transaction schedules nothing; an event without a usable
// height is skipped.
func TestChainHaltScheduledWithoutAHeightOrInAFailedTxIsSkipped(t *testing.T) {
	in := baseInput()
	in.Block.Txs = []string{encodedTx(t, &guardiantypes.MsgHaltChain{Signer: creator, Reason: "x"})}
	in.Results.TxsResults = []node.TxResult{{Code: 5, Events: []node.Event{
		typed("bze.guardian.v1.ChainHaltScheduledEvent", "0", "signer", creator, "height", "11"),
	}}}
	in.Results.FinalizeBlockEvents = []node.Event{
		typed("bze.guardian.v1.ChainHaltScheduledEvent", "", "signer", govModule, "name", "halt"),
		typed("bze.guardian.v1.ChainHaltScheduledEvent", "", "signer", govModule, "height", "eleven"),
	}
	tr, _ := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	assert.Empty(t, ents.EmergencyHalts)
}

// A halt run through authz (the guardian's key granted to another) emits the
// same event inside the MsgExec: it is recorded too.
func TestChainHaltScheduledInsideAnAuthzExecIsAnEmergencyHalt(t *testing.T) {
	exec := authz.NewMsgExec(sdk.MustAccAddressFromBech32(newAdmin), []sdk.Msg{&guardiantypes.MsgHaltChain{Signer: creator, Reason: "exploit"}})
	in := baseInput()
	in.Block.Txs = []string{encodedTx(t, &exec)}
	in.Results.TxsResults = []node.TxResult{{Events: []node.Event{
		typed("bze.guardian.v1.ChainHaltScheduledEvent", "0",
			"signer", creator, "name", "emergency-halt-11", "height", "11", "reason", "exploit"),
	}}}
	tr, _ := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	require.Len(t, ents.Transactions, 1)
	assert.Equal(t, []string{"/cosmos.authz.v1beta1.MsgExec"}, ents.Transactions[0].MsgTypes)
	assert.Equal(t, []transform.EmergencyHalt{{Height: 11, Name: "emergency-halt-11", Reason: "exploit",
		ScheduledBy: creator, ScheduledHeight: 10, TxHash: ents.Transactions[0].Hash}}, ents.EmergencyHalts)
}
