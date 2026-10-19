// Package orders is the orders set of the state sync: the hourly
// reconciliation of the explorer's open orders with the node's order book.
// The chain removes a remainder below the market minimum without any event,
// so its orders row would stay open forever; the set closes, as filled, every
// open row the node no longer lists. Events remain the source of truth: the
// set never reopens a row, never touches a row the node still lists, and a
// later fill or cancel event of a row it closed still applies.
//
// The tradebin queries list an owner's orders, not a market's book, so a run
// reads the order references of each owner with open rows, every read pinned
// to one height: the live indexer's cursor (the node's latest height when
// the indexer has not written yet), so the rows compared are those the
// events up to that height left open.
//
// An order of chain v7.0 or v7.1 whose placing message the transformer did
// not find has no owner (those events name none): the set reads it from the
// node by its market, side and id, names its owner and the maker of its
// fills, and closes it when the node no longer has it.
package orders

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	"github.com/cosmos/cosmos-sdk/types/query"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultInterval is the hourly reconciliation.
const DefaultInterval = time.Hour

// PageLimit is the page size of UserMarketOrders.
const PageLimit = 1000

// Node is the local node's RPC; *node.Client satisfies it.
type Node interface {
	Status(ctx context.Context) (*node.Status, []byte, error)
}

// Tradebin is the part of the tradebin query client the set uses;
// tradebintypes.QueryClient satisfies it.
type Tradebin interface {
	UserMarketOrders(ctx context.Context, in *tradebintypes.QueryUserMarketOrdersRequest,
		opts ...grpc.CallOption) (*tradebintypes.QueryUserMarketOrdersResponse, error)
	MarketOrder(ctx context.Context, in *tradebintypes.QueryMarketOrderRequest,
		opts ...grpc.CallOption) (*tradebintypes.QueryMarketOrderResponse, error)
}

// Position is a height and its block time.
type Position struct {
	Height int64
	Time   time.Time
}

// OpenOrder is an orders row in status open.
type OpenOrder struct {
	OrderID string
	Owner   string
	// MarketID and Side locate an order without owner on the node.
	MarketID string
	Side     string
}

// Store reads, names and closes the orders rows.
type Store interface {
	// Cursor returns the live indexer's cursor and its block time; false
	// when the live indexer has not written yet.
	Cursor(ctx context.Context) (Position, bool, error)
	// Open returns the open orders created at or below height.
	Open(ctx context.Context, height int64) ([]OpenOrder, error)
	// Close closes the orders still open among ids as filled at pos and
	// returns how many it closed.
	Close(ctx context.Context, ids []string, pos Position) (int64, error)
	// Name sets the owner of the orders without one among owners (order id
	// to owner) and the maker of their fills without one, and returns how
	// many orders it named.
	Name(ctx context.Context, owners map[string]string) (int64, error)
}

// Deps of the set.
type Deps struct {
	Node     Node
	Tradebin Tradebin
	Store    Store
	// Log defaults to the standard logger.
	Log log.FieldLogger
}

// Set is the orders set.
type Set struct {
	deps     Deps
	interval time.Duration
}

// New returns the set; interval zero is DefaultInterval.
func New(deps Deps, interval time.Duration) *Set {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	return &Set{deps: deps, interval: interval}
}

var _ statesync.Set = (*Set)(nil)

// Name is statesync.Orders.
func (s *Set) Name() string { return statesync.Orders }

// Interval is the hourly reconciliation.
func (s *Set) Interval() time.Duration { return s.interval }

// ResyncOne reconciles everything: no event marks the set.
func (s *Set) ResyncOne(ctx context.Context, _ string) error { return s.FullResync(ctx) }

// FullResync compares every open orders row with the order book of its
// owner and closes the rows the node no longer lists. An owner whose read
// fails keeps its rows; the failures are returned joined.
func (s *Set) FullResync(ctx context.Context) error {
	st, _, err := s.deps.Node.Status(ctx)
	if err != nil {
		return fmt.Errorf("node status: %w", err)
	}
	pos := Position{Height: st.LatestBlockHeight, Time: st.LatestBlockTime.UTC()}
	cur, ok, err := s.deps.Store.Cursor(ctx)
	if err != nil {
		return err
	}
	if ok && cur.Height < pos.Height {
		pos = cur
	}
	open, err := s.deps.Store.Open(ctx, pos.Height)
	if err != nil {
		return err
	}
	byOwner := map[string][]string{}
	var unowned []OpenOrder
	for _, o := range open {
		if o.Owner == "" {
			unowned = append(unowned, o)
			continue
		}
		byOwner[o.Owner] = append(byOwner[o.Owner], o.OrderID)
	}
	owners := make([]string, 0, len(byOwner))
	for owner := range byOwner {
		owners = append(owners, owner)
	}
	slices.Sort(owners)

	at := metadata.AppendToOutgoingContext(ctx, grpctypes.GRPCBlockHeightHeader, strconv.FormatInt(pos.Height, 10))
	var gone []string
	var errs []error
	for _, owner := range owners {
		listed, err := s.listed(at, owner)
		if err != nil {
			errs = append(errs, fmt.Errorf("orders of %s: %w", owner, err))
			continue
		}
		for _, id := range byOwner[owner] {
			if !listed[id] {
				gone = append(gone, id)
			}
		}
	}
	named := map[string]string{}
	for _, o := range unowned {
		owner, found, err := s.owner(at, o)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("order %s: %w", o.OrderID, err))
		case found:
			named[o.OrderID] = owner
		default:
			gone = append(gone, o.OrderID)
		}
	}
	if len(named) > 0 {
		n, err := s.deps.Store.Name(ctx, named)
		if err != nil {
			errs = append(errs, err)
		} else if n > 0 {
			s.deps.Log.WithFields(log.Fields{"named": n, "height": pos.Height}).
				Info("orders: named the owners of open orders whose events named none")
		}
	}
	if len(gone) > 0 {
		slices.Sort(gone)
		closed, err := s.deps.Store.Close(ctx, gone, pos)
		if err != nil {
			errs = append(errs, err)
		} else if closed > 0 {
			s.deps.Log.WithFields(log.Fields{"closed": closed, "height": pos.Height}).
				Info("orders: closed open orders the node no longer lists")
		}
	}
	return errors.Join(errs...)
}

// owner reads an order without owner from the node: its owner, or false
// when the node no longer has it.
func (s *Set) owner(ctx context.Context, o OpenOrder) (string, bool, error) {
	res, err := s.deps.Tradebin.MarketOrder(ctx, &tradebintypes.QueryMarketOrderRequest{
		Market: o.MarketID, OrderType: o.Side, OrderId: o.OrderID,
	})
	if status.Code(err) == codes.NotFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if res.Order.Owner == "" {
		return "", false, errors.New("the node names no owner")
	}
	return res.Order.Owner, true, nil
}

// listed returns the order ids the node lists for owner, every page.
func (s *Set) listed(ctx context.Context, owner string) (map[string]bool, error) {
	out := map[string]bool{}
	var key []byte
	for {
		res, err := s.deps.Tradebin.UserMarketOrders(ctx, &tradebintypes.QueryUserMarketOrdersRequest{
			Address: owner, Pagination: &query.PageRequest{Key: key, Limit: PageLimit},
		})
		if err != nil {
			return nil, err
		}
		for _, ref := range res.List {
			out[ref.Id] = true
		}
		if res.Pagination == nil || len(res.Pagination.NextKey) == 0 {
			return out, nil
		}
		key = res.Pagination.NextKey
	}
}
