//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// The DAOs of the hand-written gRPC fixtures: #1 a static-list DAO that owns
// itself, #2 a stake DAO administered by an account.
const (
	daoOne   = "bze1fsv8555ywhurwvf60srv04656erguqs5z9e0x0a0x426kpnn445sgck0e5"
	daoTwo   = "bze1xv5cjjf4lmfn2akksfel9e5q3kuufd62qugncgswz75nr8mr3qysyz5k8w"
	daoAdmin = "bze19fgph876c3rqxrn6xk5ch6wd73r3g05w690uls"
	daoH1    = int64(25_100_000)
	daoH2    = int64(25_100_001)
)

func daoEvent(typ, msgIndex string, kv ...string) node.Event { return v8Event(typ, msgIndex, kv...) }

// indexDaoBlock writes block h with the transactions and block events.
func indexDaoBlock(t *testing.T, pool *pgxpool.Pool, h int64, txs []string, results []node.TxResult, finalize ...node.Event) *transform.Entities {
	t.Helper()
	in := transform.Input{
		Block: &node.Block{Height: h, Time: time.Date(2026, 12, 1, 9, 0, int(h%60), 0, time.UTC), Hash: fmt.Sprintf("%064X", h),
			ProposerAddress: "AA", Raw: json.RawMessage(`{}`), Txs: txs},
		Results: &node.BlockResults{Height: h, TxsResults: results, FinalizeBlockEvents: finalize},
		Commit:  &node.Commit{Height: h},
	}
	ents, err := newTransformer(t).Transform(in)
	require.NoError(t, err)
	require.NoError(t, writer.NewLiveWriter(pool).WriteBlock(context.Background(), ents))
	return ents
}

// indexTheDaos creates both DAOs in block 1, then in block 2 an admin
// execution that updates DAO 2, and a transfer to DAO 1's treasury.
func indexTheDaos(t *testing.T, e *syncEnv) []*transform.Entities {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	one := indexDaoBlock(t, pool, daoH1,
		[]string{
			v8RawTx(t, &daotypes.MsgCreateDao{Creator: daoAdmin, Name: "Founders", Type: daotypes.DaoType_DAO_TYPE_STATIC_LIST}),
			v8RawTx(t, &daotypes.MsgCreateDao{Creator: daoAdmin, Admin: daoAdmin, Name: "Stakers club", Type: daotypes.DaoType_DAO_TYPE_STAKE}),
		},
		[]node.TxResult{
			{Events: []node.Event{daoEvent("bze.dao.EventDaoCreated", "0", "id", "1", "address", daoOne, "admin", daoOne, "type", "DAO_TYPE_STATIC_LIST")}},
			{Events: []node.Event{daoEvent("bze.dao.EventDaoCreated", "0", "id", "2", "address", daoTwo, "admin", daoAdmin, "type", "DAO_TYPE_STAKE")}},
		})
	two := indexDaoBlock(t, pool, daoH2,
		[]string{v8RawTx(t, &daotypes.MsgAdminExecute{Admin: daoAdmin, DaoId: 2})},
		[]node.TxResult{{Events: []node.Event{
			daoEvent("bze.dao.EventDaoMetadataUpdated", "0", "id", "2"),
			daoEvent("bze.dao.EventAdminExecuted", "0", "id", "2", "admin", daoAdmin, "msg_count", "1"),
		}}},
		node.Event{Type: "transfer", Attributes: []node.Attribute{
			{Key: "sender", Value: daoAdmin}, {Key: "recipient", Value: daoOne}, {Key: "amount", Value: "5ubze"}}})
	return []*transform.Entities{one, two}
}

func (e *syncEnv) daoRows(t *testing.T) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', id, type, name, admin = address, self_governed,
		created_height, creator = $1, updated_height) FROM explorer.daos ORDER BY id`, daoAdmin)
}

func TestDaosAreIndexedSyncedAndServed(t *testing.T) {
	e := newSyncEnv(t)
	ents := indexTheDaos(t, e)

	// The events alone give the creations and the history, before any sync.
	assert.Equal(t, []string{
		"1 static_list Founders true true 25100000 true 25100000",
		"2 stake Stakers club false false 25100000 true 25100001",
	}, e.daoRows(t))
	events := queryStrings(t, e.db, `SELECT concat_ws(' ', dao_id, type, height, tx_index, msg_index, seq, tx_hash IS NOT NULL)
		FROM explorer.dao_events ORDER BY height, tx_index, msg_index, seq`)
	assert.Equal(t, []string{
		"1 EventDaoCreated 25100000 0 0 0 true",
		"2 EventDaoCreated 25100000 1 0 0 true",
		"2 EventDaoMetadataUpdated 25100001 0 0 0 true",
		"2 EventAdminExecuted 25100001 0 0 1 true",
	}, events)

	// Reindexing the same heights keeps the same rows.
	pool, err := pgxpool.New(context.Background(), e.url)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, writer.NewBatchWriter(pool).Write(context.Background(), ents, writer.ModeUpdate))
	assert.Equal(t, events, queryStrings(t, e.db, `SELECT concat_ws(' ', dao_id, type, height, tx_index, msg_index, seq, tx_hash IS NOT NULL)
		FROM explorer.dao_events ORDER BY height, tx_index, msg_index, seq`), "a reindex of the same heights")
	assert.Len(t, e.daoRows(t), 2)

	// The state sync names the DAOs and their treasury accounts.
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	assert.Equal(t, []string{"DAO #1 · Founders", "DAO #2 · Stakers club"},
		queryStrings(t, e.db, `SELECT name FROM explorer.labels WHERE kind = 'dao' ORDER BY name`))
	assert.Equal(t, "25100000", queryStrings(t, e.db, `SELECT created_height FROM explorer.daos WHERE id = 1`)[0],
		"the sync keeps the creation facts")

	base := e.serveAPI(t)

	list := into[dto.DaoList](t, fetch(t, base+"/daos"))
	assert.True(t, list.Supported)
	require.Len(t, list.Items, 2)
	assert.Equal(t, "Stakers club", list.Items[0].Name, "newest first")
	assert.Equal(t, "stake", list.Items[0].Type)
	assert.False(t, list.Items[0].SelfGoverned)
	assert.True(t, list.Items[1].SelfGoverned)
	assert.Nil(t, list.NextCursor)

	page := into[dto.DaoList](t, fetch(t, base+"/daos?limit=1"))
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.NextCursor)
	next := into[dto.DaoList](t, fetch(t, base+"/daos?limit=1&cursor="+*page.NextCursor))
	require.Len(t, next.Items, 1)
	assert.Equal(t, int64(1), next.Items[0].ID)

	byType := into[dto.DaoList](t, fetch(t, base+"/daos?type=static_list"))
	require.Len(t, byType.Items, 1)
	assert.Equal(t, int64(1), byType.Items[0].ID)

	// The DAOs an account administers: never the self-governed ones.
	byAdmin := into[dto.DaoList](t, fetch(t, base+"/daos?admin="+daoAdmin))
	require.Len(t, byAdmin.Items, 1)
	assert.Equal(t, int64(2), byAdmin.Items[0].ID)
	assert.Empty(t, into[dto.DaoList](t, fetch(t, base+"/daos?admin="+daoOne)).Items)
	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/daos?type=nope").status)

	// A stake DAO: the indexed row, the chain's DAO, its controller, its
	// treasury and the stake of its electorate (staking reward sr-1, the
	// vanished sr-gone, the ubze denom reward).
	r := fetch(t, base+"/daos/2")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
	d := into[dto.Dao](t, r)
	assert.Equal(t, "Stakers club", d.Name)
	assert.Equal(t, daoTwo, d.Address)
	assert.Equal(t, &dto.Label{Name: "DAO #2 · Stakers club", Kind: "dao"}, d.AddressLabel)
	require.NotNil(t, d.CreatedHeight)
	assert.Equal(t, daoH1, *d.CreatedHeight)
	assert.Equal(t, daoAdmin, *d.Creator)
	assert.True(t, d.Live.Available)
	assert.Equal(t, "0.400000000000000000", d.Live.Config.MinQuorum)
	assert.Equal(t, int64(259200), d.Live.Config.VotingPeriodSeconds)
	assert.Equal(t, "only_passed", d.Live.Config.DepositRefundPolicy)
	six := 6
	assert.Equal(t, &dto.Balance{Denom: "ubze", Amount: "100000000", Symbol: strp("BZE"), Exponent: &six}, d.Live.Config.ProposalDeposit)
	require.NotNil(t, d.Live.Electorate)
	assert.Equal(t, []string{"sr-1", "sr-gone"}, d.Live.Electorate.SrIDs)
	require.NotNil(t, d.Live.ElectorateStake)
	stake := d.Live.ElectorateStake
	assert.Equal(t, "1250000", stake.VotingStake.Amount)
	assert.Equal(t, "ubze", stake.VotingStake.Denom)
	require.Len(t, stake.Sources, 3)
	assert.Equal(t, "sr-1", *stake.Sources[0].SrID)
	assert.Equal(t, "1000000", stake.Sources[0].StakedAmount.Amount)
	assert.Equal(t, "0", stake.Sources[1].StakedAmount.Amount, "a staking reward the chain no longer has")
	assert.Equal(t, "ubze", *stake.Sources[2].DrDenom)
	assert.Equal(t, "250000", stake.Sources[2].StakedAmount.Amount)
	require.NotNil(t, d.Controller)
	assert.Equal(t, daoAdmin, d.Controller.Address)
	assert.Equal(t, "account", d.Controller.Reason)
	assert.Equal(t, []dto.Balance{{Denom: "ubze", Amount: "2500000", Symbol: strp("BZE"), Exponent: &six}}, d.Treasury)

	// A static-list DAO has no electorate stake.
	d1 := into[dto.Dao](t, fetch(t, base+"/daos/1"))
	assert.True(t, d1.Live.Available)
	assert.Nil(t, d1.Live.ElectorateStake)
	assert.Equal(t, []string{daoAdmin, "bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk"}, d1.Live.Members)
	assert.Equal(t, "self_owned", d1.Controller.Reason)

	assert.Equal(t, http.StatusNotFound, fetch(t, base+"/daos/99").status)
	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/daos/x").status)

	// The History tab, newest first, paged.
	hist := into[dto.List[dto.DaoEvent]](t, fetch(t, base+"/daos/2/history?limit=2"))
	require.Len(t, hist.Items, 2)
	assert.Equal(t, "EventAdminExecuted", hist.Items[0].Type)
	assert.Equal(t, "EventDaoMetadataUpdated", hist.Items[1].Type)
	require.NotNil(t, hist.NextCursor)
	rest := into[dto.List[dto.DaoEvent]](t, fetch(t, base+"/daos/2/history?limit=2&cursor="+*hist.NextCursor))
	require.Len(t, rest.Items, 1)
	assert.Equal(t, "EventDaoCreated", rest.Items[0].Type)
	assert.Nil(t, rest.NextCursor)
	assert.JSONEq(t, `"`+daoTwo+`"`, string(mustField(t, rest.Items[0].Attrs, "address")))

	// The DAO's treasury account is labelled where addresses are labelled:
	// on a transfer to it, and a name search finds the DAO.
	transfers := into[dto.List[json.RawMessage]](t, fetch(t, fmt.Sprintf("%s/blocks/%d/transfers", base, daoH2)))
	require.Len(t, transfers.Items, 1)
	assert.JSONEq(t, `{"name":"DAO #1 · Founders","kind":"dao"}`, string(mustField(t, transfers.Items[0], "recipient_label")))
	found := into[dto.SearchResponse](t, fetch(t, base+"/search?q=stakers"))
	assert.Contains(t, found.Results, dto.SearchResult{Type: dto.ResultDao, ID: "2", Label: "DAO #2 · Stakers club"})
}

func mustField(t *testing.T, raw json.RawMessage, key string) json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &m))
	require.Contains(t, m, key)
	return m[key]
}

// The node answers a DAO's live part with a failure: the indexed part still
// answers and the live part says it is unavailable.
func TestDaoPageWithTheNodeDown(t *testing.T) {
	e := newSyncEnv(t)
	indexTheDaos(t, e)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	e.grpc.Stop()
	d := into[dto.Dao](t, fetch(t, e.serveAPI(t)+"/daos/2"))
	assert.Equal(t, "Stakers club", d.Name)
	assert.False(t, d.Live.Available)
	assert.Nil(t, d.Controller)
	assert.Empty(t, d.Treasury)
}

// Against a node before chain v8.3.0 (the fake node answers Unimplemented for
// bze.dao): the set is quiet, the list is empty and not supported, a DAO's
// live part is unavailable.
func TestDaosAgainstANodeWithoutThem(t *testing.T) {
	e := newSyncEnv(t)
	for _, m := range []string{"Params", "Daos", "Dao", "DaoController"} {
		e.grpc.SetResponse("dao", m, "", nil)
	}
	for _, id := range []string{"1", "2"} {
		e.grpc.SetResponse("dao", "Dao", id, nil)
		e.grpc.SetResponse("dao", "DaoController", id, nil)
	}
	code, _ := e.syncState(t)
	require.Equal(t, 0, code, "an unsupported set is not a failed run")
	assert.Equal(t, []string{"0"}, queryStrings(t, e.db, `SELECT count(*)::text FROM explorer.daos`))
	assert.Empty(t, queryStrings(t, e.db, `SELECT last_error FROM explorer.sync_jobs WHERE job = 'daos' AND last_error IS NOT NULL`))

	base := e.serveAPI(t)
	list := into[dto.DaoList](t, fetch(t, base+"/daos"))
	assert.False(t, list.Supported)
	assert.Empty(t, list.Items)

	indexTheDaos(t, e)
	d := into[dto.Dao](t, fetch(t, base+"/daos/2"))
	assert.Equal(t, "Stakers club", d.Name, "the indexed part answers")
	assert.False(t, d.Live.Available)
}
