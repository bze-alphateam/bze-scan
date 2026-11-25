package controller_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainstate"
	"github.com/bze-alphateam/bze-scan/backend/internal/daostate"
)

const daoAdmin = "bze19fgph876c3rqxrn6xk5ch6wd73r3g05w690uls"

type fakeDaos struct {
	daos   []repository.Dao
	events []repository.DaoEvent
	denoms map[string]repository.Denom
	labels map[string]repository.Label
	err    error

	gotFilter repository.DaoFilter
	gotBefore *int64
	gotKey    *repository.DaoEventKey
	gotLimit  int
}

func (f *fakeDaos) Daos(_ context.Context, filter repository.DaoFilter, before *int64, limit int) ([]repository.Dao, error) {
	f.gotFilter, f.gotBefore, f.gotLimit = filter, before, limit
	return f.daos[:min(limit, len(f.daos))], f.err
}

func (f *fakeDaos) Dao(_ context.Context, id int64) (*repository.Dao, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, d := range f.daos {
		if d.ID == id {
			return &d, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeDaos) DaoEvents(_ context.Context, _ int64, before *repository.DaoEventKey, limit int) ([]repository.DaoEvent, error) {
	f.gotKey, f.gotLimit = before, limit
	return f.events[:min(limit, len(f.events))], nil
}

func (f *fakeDaos) Denoms(context.Context, []string) (map[string]repository.Denom, error) {
	return f.denoms, nil
}

func (f *fakeDaos) Labels(_ context.Context, addrs []string) (map[string]repository.Label, error) {
	out := map[string]repository.Label{}
	for _, a := range addrs {
		if l, ok := f.labels[a]; ok {
			out[a] = l
		}
	}
	return out, nil
}

type fakeDaoState struct {
	live      map[uint64]*daostate.Live
	err       error
	supported bool
}

func (f *fakeDaoState) Live(_ context.Context, id uint64) (*daostate.Live, error) {
	if f.err != nil {
		return nil, f.err
	}
	l, ok := f.live[id]
	if !ok {
		return nil, daostate.ErrNotFound
	}
	return l, nil
}

func (f *fakeDaoState) Supported(context.Context) bool { return f.supported }

type fakeTreasury struct {
	coins []chainstate.Coin
	err   error
}

func (f *fakeTreasury) Balances(context.Context, string) ([]chainstate.Coin, error) {
	return f.coins, f.err
}

func daoServer(r controller.DaoReader, s controller.DaoState, tr controller.DaoTreasury) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	h := controller.NewDaoController(r, s, tr)
	e.GET("/api/v1/daos", h.Daos)
	e.GET("/api/v1/daos/:id", h.Dao)
	e.GET("/api/v1/daos/:id/history", h.DaoHistory)
	return e
}

func indexedDao(id int64, name, typ string) repository.Dao {
	created := time.Unix(100, 0)
	h := int64(50)
	return repository.Dao{ID: id, Address: "bze1dao" + name, Type: typ, Name: name, Admin: daoAdmin,
		AdminLabel: &repository.Label{Address: daoAdmin, Name: "Alpha", Kind: "known"}, CreatedHeight: &h, CreatedTime: &created}
}

func liveStake() *daostate.Live {
	return &daostate.Live{
		Dao: daostate.Dao{ID: 2, Address: "bze1daoStakers", Admin: daoAdmin, PendingAdmin: "bze1next", Type: "stake", Name: "Stakers",
			Description: "d", CreatedAt: 1700000000, Electorate: &daostate.Electorate{SrIDs: []string{"sr1"}, DrDenom: "factory/x/y"},
			Config: daostate.VotingConfig{MinQuorum: "0.4", Threshold: "0.5", VotingPeriodS: 3600,
				ProposalDeposit: &daostate.Coin{Denom: "ubze", Amount: "100"}}},
		Controller: daostate.Controller{Address: daoAdmin, EndReason: "account", Path: []string{"bze1daoStakers"}},
		Stake: &daostate.ElectorateStake{
			Sources: []daostate.Source{
				{SrID: "sr1", StakingDenom: "ubze", StakedAmount: daostate.Coin{Denom: "ubze", Amount: "1000"}},
				{DrDenom: "factory/x/y", StakingDenom: "ubze", StakedAmount: daostate.Coin{Denom: "ubze", Amount: "70"}},
			},
			VotingStake: daostate.Coin{Denom: "ubze", Amount: "1070"},
		},
	}
}

func TestDaosListNewestFirstWithFiltersAndPages(t *testing.T) {
	f := &fakeDaos{daos: []repository.Dao{indexedDao(3, "C", "stake"), indexedDao(2, "B", "static_list"), indexedDao(1, "A", "stake")}}
	e := daoServer(f, &fakeDaoState{supported: true}, nil)

	rec := get(t, e, "/api/v1/daos?limit=2&type=stake&admin="+daoAdmin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.Equal(t, "stake", *f.gotFilter.Type)
	assert.Equal(t, daoAdmin, *f.gotFilter.Admin)
	assert.Equal(t, 3, f.gotLimit)
	page := decode[dto.DaoList](t, rec)
	assert.True(t, page.Supported)
	require.Len(t, page.Items, 2)
	assert.Equal(t, int64(3), page.Items[0].ID)
	assert.Equal(t, &dto.Label{Name: "Alpha", Kind: "known"}, page.Items[0].AdminLabel)
	require.NotNil(t, page.NextCursor)

	rec = get(t, e, "/api/v1/daos?cursor="+*page.NextCursor)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, int64(2), *f.gotBefore)

	for _, path := range []string{"/api/v1/daos?type=other", "/api/v1/daos?admin=notanaddress", "/api/v1/daos?cursor=!!", "/api/v1/daos?limit=0"} {
		rec := get(t, e, path)
		assert.Equal(t, http.StatusBadRequest, rec.Code, path)
	}
}

// A node before v8.3.0: an empty list that says it is not supported yet.
func TestDaosListOfANodeWithoutDaosIsNotSupported(t *testing.T) {
	for name, state := range map[string]controller.DaoState{"unsupported": &fakeDaoState{}, "no node": nil} {
		t.Run(name, func(t *testing.T) {
			rec := get(t, daoServer(&fakeDaos{}, state, nil), "/api/v1/daos")
			require.Equal(t, http.StatusOK, rec.Code)
			page := decode[dto.DaoList](t, rec)
			assert.False(t, page.Supported)
			assert.Empty(t, page.Items)
			assert.Nil(t, page.NextCursor)
		})
	}
}

func TestStakeDaoPage(t *testing.T) {
	f := &fakeDaos{daos: []repository.Dao{indexedDao(2, "Stakers", "stake")},
		denoms: map[string]repository.Denom{"ubze": {Symbol: strp("BZE"), Exponent: 6}},
		labels: map[string]repository.Label{"bze1next": {Address: "bze1next", Name: "Next", Kind: "known"}}}
	e := daoServer(f, &fakeDaoState{live: map[uint64]*daostate.Live{2: liveStake()}},
		&fakeTreasury{coins: []chainstate.Coin{{Denom: "ubze", Amount: "5000"}}})

	rec := get(t, e, "/api/v1/daos/2")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	d := decode[dto.Dao](t, rec)
	assert.Equal(t, "Stakers", d.Name)
	assert.True(t, d.Live.Available)
	assert.Equal(t, 3600, int(d.Live.Config.VotingPeriodSeconds))
	assert.Equal(t, &dto.Balance{Denom: "ubze", Amount: "100", Symbol: strp("BZE"), Exponent: intp(6)}, d.Live.Config.ProposalDeposit)
	assert.Equal(t, "bze1next", *d.Live.PendingAdmin)
	assert.Equal(t, []string{"sr1"}, d.Live.Electorate.SrIDs)
	require.NotNil(t, d.Live.ElectorateStake)
	assert.Equal(t, "1070", d.Live.ElectorateStake.VotingStake.Amount)
	assert.Equal(t, "BZE", *d.Live.ElectorateStake.VotingStake.Symbol)
	require.Len(t, d.Live.ElectorateStake.Sources, 2)
	assert.Equal(t, "sr1", *d.Live.ElectorateStake.Sources[0].SrID)
	assert.Nil(t, d.Live.ElectorateStake.Sources[0].DrDenom)
	assert.Equal(t, "factory/x/y", *d.Live.ElectorateStake.Sources[1].DrDenom)
	require.NotNil(t, d.Controller)
	assert.Equal(t, daoAdmin, d.Controller.Address)
	assert.Equal(t, []dto.Balance{{Denom: "ubze", Amount: "5000", Symbol: strp("BZE"), Exponent: intp(6)}}, d.Treasury)
}

// A static-list DAO has no electorate stake.
func TestStaticListDaoPageOmitsTheElectorateStake(t *testing.T) {
	live := &daostate.Live{Dao: daostate.Dao{ID: 1, Address: "bze1daoA", Type: "static_list", Members: []string{"bze1a"}},
		Controller: daostate.Controller{Address: "bze1daoA", EndReason: "self_owned"}}
	e := daoServer(&fakeDaos{daos: []repository.Dao{indexedDao(1, "A", "static_list")}},
		&fakeDaoState{live: map[uint64]*daostate.Live{1: live}}, &fakeTreasury{})
	rec := get(t, e, "/api/v1/daos/1")
	require.Equal(t, http.StatusOK, rec.Code)
	d := decode[dto.Dao](t, rec)
	assert.Nil(t, d.Live.ElectorateStake)
	assert.Nil(t, d.Live.Electorate)
	assert.Equal(t, []string{"bze1a"}, d.Live.Members)
	assert.Equal(t, "self_owned", d.Controller.Reason)
	assert.Empty(t, d.Treasury)
	assert.NotContains(t, rec.Body.String(), "electorate_stake")
}

// A failed or unsupported live read, or a failed treasury read, keeps the
// indexed part and reports the live part unavailable.
func TestDaoPageWhenTheNodeFails(t *testing.T) {
	row := indexedDao(2, "Stakers", "stake")
	for name, tc := range map[string]struct {
		state controller.DaoState
		tr    controller.DaoTreasury
	}{
		"node down":   {&fakeDaoState{err: errors.New("down")}, &fakeTreasury{}},
		"unsupported": {&fakeDaoState{err: daostate.ErrUnsupported}, &fakeTreasury{}},
		"no node":     {nil, nil},
		"treasury":    {&fakeDaoState{live: map[uint64]*daostate.Live{2: liveStake()}}, &fakeTreasury{err: errors.New("down")}},
	} {
		t.Run(name, func(t *testing.T) {
			rec := get(t, daoServer(&fakeDaos{daos: []repository.Dao{row}}, tc.state, tc.tr), "/api/v1/daos/2")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			d := decode[dto.Dao](t, rec)
			assert.False(t, d.Live.Available)
			assert.Nil(t, d.Controller)
			assert.Empty(t, d.Treasury)
			assert.Equal(t, "Stakers", d.Name)
			assert.Equal(t, int64(50), *d.CreatedHeight)
		})
	}
}

// The chain has a DAO the state sync has not written yet; a DAO neither
// knows is a 404.
func TestDaoPageOfADaoOnlyTheChainHas(t *testing.T) {
	live := liveStake()
	e := daoServer(&fakeDaos{}, &fakeDaoState{live: map[uint64]*daostate.Live{2: live}}, &fakeTreasury{})
	rec := get(t, e, "/api/v1/daos/2")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	d := decode[dto.Dao](t, rec)
	assert.Equal(t, "Stakers", d.Name)
	assert.Equal(t, "stake", d.Type)
	assert.Nil(t, d.CreatedHeight)

	for _, path := range []string{"/api/v1/daos/9", "/api/v1/daos/9/history"} {
		rec = get(t, e, path)
		assert.Equal(t, http.StatusNotFound, rec.Code, path)
	}
	for _, path := range []string{"/api/v1/daos/0", "/api/v1/daos/x", "/api/v1/daos/x/history"} {
		rec = get(t, e, path)
		assert.Equal(t, http.StatusBadRequest, rec.Code, path)
	}
}

func TestDaoHistoryPagesNewestFirst(t *testing.T) {
	hash := "AB"
	f := &fakeDaos{daos: []repository.Dao{indexedDao(2, "Stakers", "stake")}, events: []repository.DaoEvent{
		{Height: 90, TxIndex: -1, MsgIndex: -1, Seq: 3, Type: "EventAdminChanged", Attrs: []byte(`{"id":"2"}`), Time: time.Unix(9, 0)},
		{Height: 80, TxIndex: 1, MsgIndex: 0, Seq: 0, Type: "EventDaoMetadataUpdated", Attrs: []byte(`{"id":"2"}`), TxHash: &hash, Time: time.Unix(8, 0)},
		{Height: 50, TxIndex: 0, MsgIndex: 0, Seq: 0, Type: "EventDaoCreated", Attrs: []byte(`{"id":"2"}`), Time: time.Unix(5, 0)},
	}}
	e := daoServer(f, nil, nil)
	rec := get(t, e, "/api/v1/daos/2/history?limit=2")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 3, f.gotLimit)
	page := decode[dto.List[dto.DaoEvent]](t, rec)
	require.Len(t, page.Items, 2)
	assert.Equal(t, "EventAdminChanged", page.Items[0].Type)
	assert.Nil(t, page.Items[0].TxHash, "a block-level change has no transaction")
	assert.Equal(t, "AB", *page.Items[1].TxHash)
	require.NotNil(t, page.NextCursor)

	rec = get(t, e, "/api/v1/daos/2/history?cursor="+*page.NextCursor)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, &repository.DaoEventKey{Height: 80, TxIndex: 1, MsgIndex: 0, Seq: 0}, f.gotKey)

	// A block-level key round-trips through the cursor.
	k := repository.DaoEventKey{Height: 90, TxIndex: -1, MsgIndex: -1, Seq: 3}
	back, err := dto.DecodeDaoEventCursor(dto.EncodeDaoEventCursor(k))
	require.NoError(t, err)
	assert.Equal(t, &k, back)

	assert.Equal(t, http.StatusBadRequest, get(t, e, "/api/v1/daos/2/history?cursor=!!").Code)
}

// A DAO's treasury account is labelled "DAO #<id> · <name>" (kind dao): a name
// search finds the DAO by its id, not the account.
func TestSearchFindsDaosByName(t *testing.T) {
	e := newAPI(&fakeReader{labels: []repository.Label{
		{Address: "bze1daoStakers", Name: "DAO #12 · Stakers club", Kind: "dao"},
		{Address: "bze1known", Name: "Stakers treasury", Kind: "known"},
	}})
	rec := get(t, e, "/search?q=stakers")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []dto.SearchResult{
		{Type: "dao", ID: "12", Label: "DAO #12 · Stakers club"},
		{Type: "account", ID: "bze1known", Label: "Stakers treasury"},
	}, decode[dto.SearchResponse](t, rec).Results)
}
