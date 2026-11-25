// Package daos is the DAOs set of the state sync: it writes the name, admin
// and type of every DAO of chain v8.3.0 (x/dao) into explorer.daos, and the
// "DAO #<id> · <name>" label of its treasury address into explorer.labels. A
// full resync lists every DAO (also the ones the index missed, created below
// the live floor); the transformer marks a DAO dirty on each of its events.
//
// A node that has not upgraded to v8.3.0 answers Unimplemented: that is
// "not supported", not a failure: no error, no rows, retried at the next
// interval.
package daos

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultInterval is the safety-net period of the full resync.
const DefaultInterval = time.Hour

// PageLimit is the page size of the DAOs list.
const PageLimit = 100

// Types of DAO, as the type column stores them.
const (
	TypeStaticList = "static_list"
	TypeStake      = "stake"
)

// Client is the part of the x/dao query client the set uses;
// daotypes.QueryClient satisfies it.
type Client interface {
	Dao(ctx context.Context, in *daotypes.QueryDaoRequest, opts ...grpc.CallOption) (*daotypes.QueryDaoResponse, error)
	Daos(ctx context.Context, in *daotypes.QueryDaosRequest, opts ...grpc.CallOption) (*daotypes.QueryDaosResponse, error)
}

// Dao is the part of a DAO the explorer stores.
type Dao struct {
	ID      uint64
	Address string
	Type    string
	Name    string
	Admin   string
}

// SelfGoverned reports whether the DAO is its own admin.
func (d Dao) SelfGoverned() bool { return d.Admin == d.Address }

// Store persists the set.
type Store interface {
	// Save upserts DAOs, and their labels, in one transaction.
	Save(ctx context.Context, daos []Dao) error
}

// Deps of the set.
type Deps struct {
	Client Client
	Store  Store
}

// Set is the daos set.
type Set struct {
	deps     Deps
	interval time.Duration
}

// New returns the set; interval zero is DefaultInterval.
func New(deps Deps, interval time.Duration) *Set {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Set{deps: deps, interval: interval}
}

var _ statesync.Set = (*Set)(nil)

// Name is statesync.Daos.
func (s *Set) Name() string { return statesync.Daos }

// Interval is the safety-net period.
func (s *Set) Interval() time.Duration { return s.interval }

// FullResync lists every DAO, a page of PageLimit at a time, and saves them.
func (s *Set) FullResync(ctx context.Context) error {
	var out []Dao
	var next []byte
	for {
		resp, err := s.deps.Client.Daos(ctx, &daotypes.QueryDaosRequest{Pagination: &query.PageRequest{Key: next, Limit: PageLimit}})
		if status.Code(err) == codes.Unimplemented {
			return nil
		}
		if err != nil {
			return fmt.Errorf("daos: list: %w", err)
		}
		for _, d := range resp.Daos {
			out = append(out, of(d))
		}
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			break
		}
		next = resp.Pagination.NextKey
	}
	if len(out) == 0 {
		return nil
	}
	return s.deps.Store.Save(ctx, out)
}

// ResyncOne rewrites DAO key (its id). A DAO the node does not have keeps the
// row its events wrote.
func (s *Set) ResyncOne(ctx context.Context, key string) error {
	id, err := strconv.ParseUint(key, 10, 64)
	if err != nil {
		return fmt.Errorf("daos: key %q is not a DAO id", key)
	}
	resp, err := s.deps.Client.Dao(ctx, &daotypes.QueryDaoRequest{Id: id})
	switch status.Code(err) {
	case codes.OK:
	case codes.NotFound, codes.Unimplemented:
		return nil
	default:
		return fmt.Errorf("daos: dao %d: %w", id, err)
	}
	return s.deps.Store.Save(ctx, []Dao{of(resp.Dao)})
}

func of(d daotypes.Dao) Dao {
	t := TypeStaticList
	if d.Type == daotypes.DaoType_DAO_TYPE_STAKE {
		t = TypeStake
	}
	return Dao{ID: d.Id, Address: d.Address, Type: t, Name: d.Name, Admin: d.Admin}
}
