package daos_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/daos"
)

type fakeClient struct {
	all []daotypes.Dao
	// pageSize splits the list; 0 is one page.
	pageSize int
	err      error
	pages    []*query.PageRequest
}

func (f *fakeClient) Daos(_ context.Context, in *daotypes.QueryDaosRequest, _ ...grpc.CallOption) (*daotypes.QueryDaosResponse, error) {
	f.pages = append(f.pages, in.Pagination)
	if f.err != nil {
		return nil, f.err
	}
	start := 0
	if len(in.Pagination.Key) > 0 {
		start = int(in.Pagination.Key[0])
	}
	end := len(f.all)
	if f.pageSize > 0 {
		end = min(start+f.pageSize, end)
	}
	resp := &daotypes.QueryDaosResponse{Daos: f.all[start:end], Pagination: &query.PageResponse{}}
	if end < len(f.all) {
		resp.Pagination.NextKey = []byte{byte(end)}
	}
	return resp, nil
}

func (f *fakeClient) Dao(_ context.Context, in *daotypes.QueryDaoRequest, _ ...grpc.CallOption) (*daotypes.QueryDaoResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, d := range f.all {
		if d.Id == in.Id {
			return &daotypes.QueryDaoResponse{Dao: d}, nil
		}
	}
	return nil, status.Error(codes.NotFound, "dao not found")
}

type memStore struct {
	saved [][]daos.Dao
	err   error
}

func (m *memStore) Save(_ context.Context, d []daos.Dao) error {
	if m.err != nil {
		return m.err
	}
	m.saved = append(m.saved, d)
	return nil
}

var chainDaos = []daotypes.Dao{
	{Id: 1, Address: "bze1dao1", Admin: "bze1dao1", Type: daotypes.DaoType_DAO_TYPE_STATIC_LIST, Name: "Founders"},
	{Id: 2, Address: "bze1dao2", Admin: "bze1admin", Type: daotypes.DaoType_DAO_TYPE_STAKE, Name: "Stakers"},
	{Id: 3, Address: "bze1dao3", Admin: "bze1dao3", Type: daotypes.DaoType_DAO_TYPE_STAKE},
}

func newSet(c *fakeClient, s *memStore) *daos.Set {
	return daos.New(daos.Deps{Client: c, Store: s}, 0)
}

func TestSetIdentity(t *testing.T) {
	set := newSet(&fakeClient{}, &memStore{})
	assert.Equal(t, statesync.Daos, set.Name())
	assert.Equal(t, daos.DefaultInterval, set.Interval())
	assert.Equal(t, "42", daos.Key(42))
}

// A full resync reads every page and saves the DAOs with their type and
// self-governed flag.
func TestFullResyncPagesThroughEveryDao(t *testing.T) {
	client := &fakeClient{all: chainDaos, pageSize: 2}
	store := &memStore{}
	require.NoError(t, newSet(client, store).FullResync(context.Background()))

	require.Len(t, client.pages, 2)
	assert.EqualValues(t, daos.PageLimit, client.pages[0].Limit)
	assert.Equal(t, []byte{2}, client.pages[1].Key, "the second page continues from the first's next key")
	require.Len(t, store.saved, 1)
	assert.Equal(t, []daos.Dao{
		{ID: 1, Address: "bze1dao1", Type: "static_list", Name: "Founders", Admin: "bze1dao1"},
		{ID: 2, Address: "bze1dao2", Type: "stake", Name: "Stakers", Admin: "bze1admin"},
		{ID: 3, Address: "bze1dao3", Type: "stake", Admin: "bze1dao3"},
	}, store.saved[0])
	assert.True(t, store.saved[0][0].SelfGoverned())
	assert.False(t, store.saved[0][1].SelfGoverned())
}

// A node without x/dao answers Unimplemented: not an error, nothing saved.
func TestUnimplementedIsNotSupportedNotAFailure(t *testing.T) {
	client := &fakeClient{err: status.Error(codes.Unimplemented, "unknown service bze.dao.Query")}
	store := &memStore{}
	set := newSet(client, store)
	require.NoError(t, set.FullResync(context.Background()))
	require.NoError(t, set.ResyncOne(context.Background(), "3"))
	assert.Empty(t, store.saved)
}

func TestNoDaosSavesNothing(t *testing.T) {
	store := &memStore{}
	require.NoError(t, newSet(&fakeClient{}, store).FullResync(context.Background()))
	assert.Empty(t, store.saved)
}

func TestResyncOne(t *testing.T) {
	client := &fakeClient{all: chainDaos}
	store := &memStore{}
	set := newSet(client, store)
	require.NoError(t, set.ResyncOne(context.Background(), "2"))
	assert.Equal(t, [][]daos.Dao{{{ID: 2, Address: "bze1dao2", Type: "stake", Name: "Stakers", Admin: "bze1admin"}}}, store.saved)

	// A DAO the node does not have keeps the row its events wrote.
	require.NoError(t, set.ResyncOne(context.Background(), "99"))
	assert.Len(t, store.saved, 1)

	assert.ErrorContains(t, set.ResyncOne(context.Background(), "x"), "not a DAO id")
}

func TestNodeAndStoreFailuresAreReturned(t *testing.T) {
	down := &fakeClient{err: status.Error(codes.Unavailable, "down")}
	set := newSet(down, &memStore{})
	assert.ErrorContains(t, set.FullResync(context.Background()), "daos: list")
	assert.ErrorContains(t, set.ResyncOne(context.Background(), "1"), "daos: dao 1")

	boom := errors.New("db down")
	set = newSet(&fakeClient{all: chainDaos}, &memStore{err: boom})
	assert.ErrorIs(t, set.FullResync(context.Background()), boom)
	assert.ErrorIs(t, set.ResyncOne(context.Background(), "1"), boom)
}
