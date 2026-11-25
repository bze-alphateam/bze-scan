package transform_test

import (
	"testing"
	"time"

	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"
	tokenfactorytypes "github.com/bze-alphateam/bze/x/tokenfactory/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

const govModule = "bze10d07y265gmmuvt4z0w9aw880jnsr700j8xlwyy"

// Chain v8.3.0 is classified: no "not classified" warning for its events.
func transformerWithLog(t *testing.T) (*transform.Transformer, *logtest.Hook) {
	t.Helper()
	log, hook := logtest.NewNullLogger()
	return transform.New(chainCodec(t), log), hook
}

func TestV830MsgCreateDaoIsDaoCreateGovernanceActivity(t *testing.T) {
	in := baseInput()
	in.Block.Txs = []string{encodedTx(t, &daotypes.MsgCreateDao{Creator: creator, Name: "Hive"})}
	in.Results.TxsResults = []node.TxResult{{}}
	tr, hook := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	require.Len(t, ents.Activity, 1)
	a := ents.Activity[0]
	assert.Equal(t, creator, a.Address)
	assert.Equal(t, "dao_create", a.Kind)
	assert.Equal(t, "governance", a.Category)
	assert.True(t, a.IsSigner)
	assert.Equal(t, []string{"/bze.dao.MsgCreateDao"}, a.MsgTypes)
	assert.Empty(t, hook.AllEntries())
}

func TestV830GovExecutedGuardianEventsAreStored(t *testing.T) {
	in := baseInput()
	in.Block.Txs, in.Results.TxsResults = nil, nil
	in.Results.FinalizeBlockEvents = []node.Event{
		typed("bze.guardian.v1.TradingDenomsHaltedEvent", "", "signer", govModule),
		typed("bze.guardian.v1.DenomBlockedEvent", "", "signer", govModule, "denom", haltedIB),
		typed("bze.guardian.v1.ChainHaltScheduledEvent", "", "signer", govModule, "name", "halt"),
		typed("bze.tradebin.PoolFeesUpdatedEvent", "", "pool_id", "ubze_"+haltedIB, "old_fee", "0.01", "new_fee", "0.02"),
		typed("bze.dao.EventAdminChanged", "", "new_admin", newAdmin, "old_admin", creator),
	}
	tr, hook := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	var types []string
	for _, e := range ents.BlockEvents {
		types = append(types, e.Type)
	}
	assert.Equal(t, []string{
		"bze.guardian.v1.TradingDenomsHaltedEvent", "bze.guardian.v1.DenomBlockedEvent",
		"bze.guardian.v1.ChainHaltScheduledEvent", "bze.tradebin.PoolFeesUpdatedEvent", "bze.dao.EventAdminChanged",
	}, types)
	assert.Empty(t, hook.AllEntries(), "no unclassified bze.* block event")
	require.Len(t, ents.Activity, 1, "only the DAO's new admin is named")
	assert.Equal(t, newAdmin, ents.Activity[0].Address)
	assert.Equal(t, "dao_admin", ents.Activity[0].Kind)
	assert.Equal(t, "governance", ents.Activity[0].Category)
}

func TestV830CommunityPoolFeePaidGivesTheRecipientABlockRow(t *testing.T) {
	in := baseInput()
	in.Block.Txs, in.Results.TxsResults = nil, nil
	ev := typed("bze.txfeecollector.CommunityPoolFeePaidEvent", "", "denom", honey, "recipient", newAdmin, "amount", "12")
	in.Results.FinalizeBlockEvents = []node.Event{ev}
	tr, hook := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	require.Len(t, ents.Activity, 1)
	a := ents.Activity[0]
	assert.Equal(t, newAdmin, a.Address)
	assert.Equal(t, "community_pool_fee", a.Kind)
	assert.Equal(t, "tokens", a.Category)
	assert.Equal(t, transform.BlockTxIndex, a.TxIndex)
	require.Len(t, a.Details, 1)
	assert.Equal(t, "bze.txfeecollector.CommunityPoolFeePaidEvent", a.Details[0].Type)
	assert.Empty(t, hook.AllEntries())
}

func TestV830TokenEvents(t *testing.T) {
	in := baseInput()
	in.Block.Txs = []string{encodedTx(t,
		&tokenfactorytypes.MsgDisableMinting{Creator: creator, Denom: honey},
		&tokenfactorytypes.MsgSetDenomConfig{Creator: creator, Denom: honey, CommunityPool: newAdmin},
	)}
	in.Results.TxsResults = []node.TxResult{{Events: []node.Event{
		typed("bze.tokenfactory.DenomMintingDisabledEvent", "0", "denom", honey, "admin", creator),
		typed("bze.tokenfactory.DenomConfigChangeEvent", "1", "denom", honey, "community_pool", newAdmin,
			"transfer_fee", "0.01", "gas_surcharge", "0.02"),
	}}}
	// Governance's MsgSetDenomAdmin and MsgUpdatePools run in its EndBlocker.
	in.Results.FinalizeBlockEvents = []node.Event{
		typed("bze.tokenfactory.DenomAdminChangeEvent", "", "admin", creator, "new_admin", newAdmin, "denom", honey),
		typed("bze.tradebin.PoolFeesUpdatedEvent", "", "pool_id", "ubze_"+haltedIB, "old_fee", "0.01", "new_fee", "0.02"),
	}
	tr, hook := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	at := time.Unix(100, 0).UTC()
	pool := map[string]any{"pool_id": "ubze_" + haltedIB, "old_fee": "0.01", "new_fee": "0.02"}
	assert.Equal(t, []transform.TokenEvent{
		{Height: 10, TxIndex: -1, Seq: 0, Denom: honey, Kind: "admin_set_by_governance",
			Details: map[string]any{"from": creator, "to": newAdmin}, Time: at},
		{Height: 10, TxIndex: -1, Seq: 1, Denom: "ubze", Kind: "pool_fees_updated", Details: pool, Time: at},
		{Height: 10, TxIndex: -1, Seq: 2, Denom: haltedIB, Kind: "pool_fees_updated", Details: pool, Time: at},
		{Height: 10, TxIndex: 0, Seq: 0, Denom: honey, Kind: "minting_disabled", Actor: creator, Time: at},
		{Height: 10, TxIndex: 0, Seq: 1, Denom: honey, Kind: "config_changed", Actor: creator,
			Details: map[string]any{"community_pool": newAdmin, "transfer_fee": "0.01", "gas_surcharge": "0.02"}, Time: at},
	}, ents.TokenEvents)
	assert.Empty(t, hook.AllEntries())
}

func TestV830PoolFeesOfAPoolIDWithoutASplitAreSkippedWithAWarning(t *testing.T) {
	in := baseInput()
	in.Block.Txs, in.Results.TxsResults = nil, nil
	in.Results.FinalizeBlockEvents = []node.Event{
		typed("bze.tradebin.PoolFeesUpdatedEvent", "", "pool_id", "a_b_c", "old_fee", "0.01", "new_fee", "0.02"),
	}
	tr, hook := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	assert.Empty(t, ents.TokenEvents)
	require.Len(t, hook.AllEntries(), 1)
	assert.Contains(t, hook.LastEntry().Message, "does not split")
}
