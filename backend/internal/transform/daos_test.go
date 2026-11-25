package transform_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

const (
	daoAddr   = "bze1daoaddressdaoaddressdaoaddressdaoaddress0"
	otherAddr = "bze1otheradminotheradminotheradminotheradmin"
)

// typedRaw is a typed event whose attributes are already JSON (arrays,
// numbers), with the message index.
func typedRaw(typ, msgIndex string, kv ...string) node.Event {
	ev := node.Event{Type: typ}
	for i := 0; i < len(kv); i += 2 {
		ev.Attributes = append(ev.Attributes, node.Attribute{Key: kv[i], Value: kv[i+1]})
	}
	if msgIndex != "" {
		ev = withIndex(ev, msgIndex)
	}
	return ev
}

func daoCreatedEvent(msgIndex, id, admin, daoType string) node.Event {
	return typed("bze.dao.EventDaoCreated", msgIndex, "id", id, "address", daoAddr, "admin", admin, "type", daoType)
}

func transformDaoTx(t *testing.T, events []node.Event, tx ...string) *transform.Entities {
	t.Helper()
	in := baseInput()
	in.Block.Txs = tx
	in.Results.TxsResults = []node.TxResult{{Events: events}}
	tr, hook := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	assert.Empty(t, hook.AllEntries())
	return ents
}

// A DAO nobody administers (an empty admin in the message) owns itself: the
// chain's event carries the DAO's own address as its admin.
func TestStaticListDaoCreationOwnedByItself(t *testing.T) {
	raw := encodedTx(t, &daotypes.MsgCreateDao{Creator: creator, Name: "Founders", Type: daotypes.DaoType_DAO_TYPE_STATIC_LIST,
		Members: []string{creator}})
	ents := transformDaoTx(t, []node.Event{daoCreatedEvent("0", "7", daoAddr, "DAO_TYPE_STATIC_LIST")}, raw)
	require.Len(t, ents.Transactions, 1)
	hash := ents.Transactions[0].Hash

	assert.Equal(t, []transform.DaoCreated{{ID: 7, Address: daoAddr, Type: "static_list", Name: "Founders", Admin: daoAddr,
		Creator: creator, Height: 10, Time: ents.Blocks[0].Time, TxHash: hash}}, ents.Daos)
	require.Len(t, ents.DaoEvents, 1)
	e := ents.DaoEvents[0]
	assert.Equal(t, transform.DaoEvent{Height: 10, TxIndex: 0, MsgIndex: 0, Seq: 0, DaoID: 7, Type: "EventDaoCreated",
		Attrs: e.Attrs, TxHash: hash, Time: e.Time}, e)
	assert.Equal(t, daoAddr, e.Attrs["admin"])
	assert.Equal(t, []string{"7"}, ents.Dirty.Keys(statesync.Daos))
}

// A stake DAO created with an explicit admin account.
func TestStakeDaoCreationWithAnExplicitAdmin(t *testing.T) {
	raw := encodedTx(t, &daotypes.MsgCreateDao{Creator: creator, Admin: otherAddr, Name: "Stakers", Type: daotypes.DaoType_DAO_TYPE_STAKE})
	ents := transformDaoTx(t, []node.Event{daoCreatedEvent("0", "8", otherAddr, "DAO_TYPE_STAKE")}, raw)
	require.Len(t, ents.Daos, 1)
	assert.Equal(t, "stake", ents.Daos[0].Type)
	assert.Equal(t, otherAddr, ents.Daos[0].Admin)
	assert.NotEqual(t, ents.Daos[0].Address, ents.Daos[0].Admin, "an administered DAO is not self-governed")
	assert.Equal(t, "Stakers", ents.Daos[0].Name)
}

// The admin handover events and the updates inside a MsgAdminExecute: every
// event is a row of its DAO, seq counts them per message, the DAO is marked
// dirty once.
func TestAdminHandoverAndUpdatesInsideAnAdminExecute(t *testing.T) {
	raw := encodedTx(t,
		&daotypes.MsgNominateAdmin{Authority: creator, DaoId: 3, Nominee: otherAddr},
		&daotypes.MsgAcceptAdminNomination{Nominee: otherAddr, DaoId: 3},
		&daotypes.MsgWithdrawAdminNomination{Authority: creator, DaoId: 4},
		&daotypes.MsgAdminExecute{Admin: creator, DaoId: 3},
	)
	events := []node.Event{
		typed("bze.dao.EventAdminNominated", "0", "id", "3", "admin", creator, "nominee", otherAddr),
		typed("bze.dao.EventAdminChanged", "1", "id", "3", "old_admin", creator, "new_admin", otherAddr),
		typed("bze.dao.EventAdminNominationWithdrawn", "2", "id", "4", "nominee", otherAddr),
		typedRaw("bze.dao.EventDaoConfigUpdated", "3", "id", `"3"`, "fields", `["min_quorum","threshold"]`),
		typed("bze.dao.EventDaoMetadataUpdated", "3", "id", "3"),
		typedRaw("bze.dao.EventDaoElectorateUpdated", "3", "id", `"3"`, "sr_ids", `["1","2"]`, "dr_denom", `"factory/x/y"`),
		typedRaw("bze.dao.EventDaoMembersUpdated", "3", "id", `"3"`, "added", `["`+creator+`"]`, "removed", `[]`),
		typed("bze.dao.EventAdminExecuted", "3", "id", "3", "admin", creator, "msg_count", "4"),
		// A proposal event is the proposals story's.
		typed("bze.dao.EventProposalSubmitted", "3", "dao_id", "3", "proposal_id", "1"),
	}
	ents := transformDaoTx(t, events, raw)
	require.Len(t, ents.DaoEvents, 8)

	type key struct {
		msg, seq int
		typ      string
		dao      int64
	}
	var got []key
	for _, e := range ents.DaoEvents {
		got = append(got, key{e.MsgIndex, e.Seq, e.Type, e.DaoID})
	}
	assert.Equal(t, []key{
		{0, 0, "EventAdminNominated", 3}, {1, 0, "EventAdminChanged", 3}, {2, 0, "EventAdminNominationWithdrawn", 4},
		{3, 0, "EventDaoConfigUpdated", 3}, {3, 1, "EventDaoMetadataUpdated", 3}, {3, 2, "EventDaoElectorateUpdated", 3},
		{3, 3, "EventDaoMembersUpdated", 3}, {3, 4, "EventAdminExecuted", 3},
	}, got)
	assert.Equal(t, []any{"min_quorum", "threshold"}, ents.DaoEvents[3].Attrs["fields"])
	assert.Equal(t, []any{"1", "2"}, ents.DaoEvents[5].Attrs["sr_ids"])
	assert.Equal(t, []string{"3", "4"}, ents.Dirty.Keys(statesync.Daos))
	assert.Empty(t, ents.Daos)
}

// Governance running a DAO message in its EndBlocker: block-level rows with
// -1 indexes and the position among the block's events as seq; a creation
// there has no signer or name to read.
func TestBlockLevelDaoEvents(t *testing.T) {
	in := baseInput()
	in.Block.Txs, in.Results.TxsResults = nil, nil
	in.Results.FinalizeBlockEvents = []node.Event{
		typed("bze.dao.EventAdminChanged", "", "id", "5", "old_admin", daoAddr, "new_admin", otherAddr),
		daoCreatedEvent("", "9", otherAddr, "DAO_TYPE_STATIC_LIST"),
	}
	tr, _ := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	require.Len(t, ents.DaoEvents, 2)
	assert.Equal(t, -1, ents.DaoEvents[0].TxIndex)
	assert.Equal(t, -1, ents.DaoEvents[0].MsgIndex)
	assert.Equal(t, 0, ents.DaoEvents[0].Seq)
	assert.Equal(t, 1, ents.DaoEvents[1].Seq)
	assert.Empty(t, ents.DaoEvents[0].TxHash)
	require.Len(t, ents.Daos, 1)
	assert.Equal(t, transform.DaoCreated{ID: 9, Address: daoAddr, Type: "static_list", Admin: otherAddr, Height: 10,
		Time: ents.Blocks[0].Time}, ents.Daos[0])
	assert.Equal(t, []string{"5", "9"}, ents.Dirty.Keys(statesync.Daos))
}

// A failed transaction changes nothing; an event without a usable id is
// skipped.
func TestDaoEventsOfAFailedTxOrWithoutAnIdAreSkipped(t *testing.T) {
	in := baseInput()
	in.Block.Txs = []string{encodedTx(t, &daotypes.MsgNominateAdmin{Authority: creator, DaoId: 3, Nominee: otherAddr})}
	in.Results.TxsResults = []node.TxResult{{Code: 5, Events: []node.Event{
		typed("bze.dao.EventAdminNominated", "0", "id", "3", "admin", creator, "nominee", otherAddr),
	}}}
	in.Results.FinalizeBlockEvents = []node.Event{
		typed("bze.dao.EventAdminChanged", "", "old_admin", daoAddr),
		typed("bze.dao.EventAdminChanged", "", "id", "zero"),
		typed("bze.dao.EventAdminChanged", "", "id", "0"),
	}
	tr, _ := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	assert.Empty(t, ents.DaoEvents)
	assert.Empty(t, ents.Daos)
	assert.Empty(t, ents.Dirty.Keys(statesync.Daos))
}
