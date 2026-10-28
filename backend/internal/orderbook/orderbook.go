// Package orderbook reads the DEX's live order books from the node, in
// display units, for the aggregator-compatible routes (GET /api/dex/orders,
// later the tickers' bid and ask). A book is the node's aggregated price
// levels (MarketAggregatedOrders), not single orders, read whole and kept
// for TTL per market, so polling never reaches the node more than once per
// market per TTL. The market list (AllMarkets) is kept for MarketsTTL.
package orderbook

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"

	"github.com/bze-alphateam/bze-scan/backend/internal/dexunits"
)

// Defaults of Config.
const (
	DefaultTTL        = 10 * time.Second
	DefaultMarketsTTL = 60 * time.Second
	// PageLimit is the page size of the node reads, the aggregator's.
	PageLimit = 10000
)

// ErrUnknownMarket is a market id the chain has no market for.
var ErrUnknownMarket = errors.New("market not found")

// Node is the part of the tradebin query client the service reads;
// tradebintypes.QueryClient satisfies it.
type Node interface {
	MarketAggregatedOrders(ctx context.Context, in *tradebintypes.QueryMarketAggregatedOrdersRequest,
		opts ...grpc.CallOption) (*tradebintypes.QueryMarketAggregatedOrdersResponse, error)
	AllMarkets(ctx context.Context, in *tradebintypes.QueryAllMarketsRequest,
		opts ...grpc.CallOption) (*tradebintypes.QueryAllMarketsResponse, error)
}

// Clock is the service's time source; tests swap it.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Config tunes the service; the zero value is production.
type Config struct {
	// TTL is how long a market's book is served before the node is asked
	// again; zero is DefaultTTL.
	TTL time.Duration
	// MarketsTTL is how long the market list is kept; zero is
	// DefaultMarketsTTL.
	MarketsTTL time.Duration
}

// Deps of the service. Log defaults to the standard logger, Clock to the
// wall clock.
type Deps struct {
	Node      Node
	Exponents dexunits.Exponents
	Log       log.FieldLogger
	Clock     Clock
}

// Market is a DEX market: id "<base>/<quote>".
type Market struct {
	ID    string
	Base  string
	Quote string
}

// Level is one aggregated price level in display units: the price in quote
// per base, the volume in base.
type Level struct {
	Price  string
	Volume string
}

// Book is a market's whole order book in display units: bids from the
// highest price down, asks from the lowest up.
type Book struct {
	Market Market
	Bids   []Level
	Asks   []Level
}

// Depth returns the book as the aggregator's depth parameter cuts it:
// depth/2 levels per side (all of them when that is 0), the bids nearest
// the spread read from the highest down and then reversed, so both sides
// come out by ascending price.
func (b *Book) Depth(depth int) (bids, asks []Level) {
	limit := depth / 2
	bids, asks = b.Bids, b.Asks
	if limit > 0 {
		bids, asks = bids[:min(limit, len(bids))], asks[:min(limit, len(asks))]
	}
	bids = slices.Clone(bids)
	slices.Reverse(bids)
	return bids, slices.Clone(asks)
}

// Empty reports whether neither side has a level.
func (b *Book) Empty() bool { return len(b.Bids) == 0 && len(b.Asks) == 0 }

// Service serves the books; safe for concurrent use.
type Service struct {
	cfg  Config
	deps Deps

	group singleflight.Group

	mu        sync.Mutex
	books     map[string]cached[*Book]
	markets   cached[map[string]Market]
	lastSweep time.Time
}

type cached[T any] struct {
	value   T
	expires time.Time
}

// New returns a service.
func New(cfg Config, deps Deps) *Service {
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultTTL
	}
	if cfg.MarketsTTL <= 0 {
		cfg.MarketsTTL = DefaultMarketsTTL
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}
	return &Service{cfg: cfg, deps: deps, books: map[string]cached[*Book]{}}
}

// Market returns the chain's market id, or ErrUnknownMarket.
func (s *Service) Market(ctx context.Context, id string) (Market, error) {
	markets, err := s.allMarkets(ctx)
	if err != nil {
		return Market{}, err
	}
	m, ok := markets[id]
	if !ok {
		return Market{}, fmt.Errorf("%w: %s", ErrUnknownMarket, id)
	}
	return m, nil
}

// Book returns market id's whole book, or ErrUnknownMarket. The caller must
// not modify it.
func (s *Service) Book(ctx context.Context, id string) (*Book, error) {
	now := s.deps.Clock.Now()
	s.mu.Lock()
	if c, ok := s.books[id]; ok && now.Before(c.expires) {
		s.mu.Unlock()
		return c.value, nil
	}
	s.mu.Unlock()

	v, err, _ := s.group.Do("book:"+id, func() (any, error) {
		m, err := s.Market(ctx, id)
		if err != nil {
			return nil, err
		}
		b, err := s.read(ctx, m)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		now := s.deps.Clock.Now()
		s.sweep(now)
		s.books[id] = cached[*Book]{value: b, expires: now.Add(s.cfg.TTL)}
		return b, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Book), nil
}

// sweep drops the expired books once per TTL, so ids nobody asks for again
// do not pile up. Called under mu.
func (s *Service) sweep(now time.Time) {
	if now.Sub(s.lastSweep) < s.cfg.TTL {
		return
	}
	s.lastSweep = now
	for id, c := range s.books {
		if !now.Before(c.expires) {
			delete(s.books, id)
		}
	}
}

func (s *Service) allMarkets(ctx context.Context) (map[string]Market, error) {
	now := s.deps.Clock.Now()
	s.mu.Lock()
	if s.markets.value != nil && now.Before(s.markets.expires) {
		defer s.mu.Unlock()
		return s.markets.value, nil
	}
	s.mu.Unlock()

	v, err, _ := s.group.Do("markets", func() (any, error) {
		markets := map[string]Market{}
		var key []byte
		for {
			res, err := s.deps.Node.AllMarkets(ctx, &tradebintypes.QueryAllMarketsRequest{
				Pagination: &query.PageRequest{Key: key, Limit: PageLimit},
			})
			if err != nil {
				return nil, fmt.Errorf("all markets: %w", err)
			}
			for _, m := range res.GetMarket() {
				id := m.GetBase() + "/" + m.GetQuote()
				markets[id] = Market{ID: id, Base: m.GetBase(), Quote: m.GetQuote()}
			}
			if key = res.GetPagination().GetNextKey(); len(key) == 0 {
				break
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.markets = cached[map[string]Market]{value: markets, expires: s.deps.Clock.Now().Add(s.cfg.MarketsTTL)}
		return markets, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(map[string]Market), nil
}

// read asks the node for m's whole book and converts it to display units.
func (s *Service) read(ctx context.Context, m Market) (*Book, error) {
	exps, err := s.deps.Exponents.Exponents(ctx, []string{m.Base, m.Quote})
	if err != nil {
		return nil, err
	}
	baseExp, okBase := exps[m.Base]
	quoteExp, okQuote := exps[m.Quote]
	if !okBase || !okQuote {
		return nil, fmt.Errorf("market %s: exponents of %s and %s not both known", m.ID, m.Base, m.Quote)
	}

	b := &Book{Market: m}
	for _, side := range []struct {
		orderType string
		reverse   bool
		into      *[]Level
	}{
		{tradebintypes.OrderTypeBuy, true, &b.Bids},
		{tradebintypes.OrderTypeSell, false, &b.Asks},
	} {
		orders, err := s.side(ctx, m.ID, side.orderType, side.reverse)
		if err != nil {
			return nil, err
		}
		// By chain price, whatever order the node answers in: bids from the
		// highest down, asks from the lowest up.
		slices.SortStableFunc(orders, func(a, b tradebintypes.AggregatedOrder) int {
			if side.reverse {
				return dexunits.Compare(b.GetPrice(), a.GetPrice())
			}
			return dexunits.Compare(a.GetPrice(), b.GetPrice())
		})
		levels := make([]Level, 0, len(orders))
		for _, o := range orders {
			price, err := dexunits.Price(o.GetPrice(), baseExp, quoteExp)
			if err == nil {
				var volume string
				if volume, err = dexunits.Amount(o.GetAmount(), baseExp); err == nil {
					levels = append(levels, Level{Price: price, Volume: volume})
					continue
				}
			}
			// The aggregator skips a level it cannot convert, too.
			s.deps.Log.WithError(err).WithField("market", m.ID).Warn("order book: level skipped")
		}
		*side.into = levels
	}
	return b, nil
}

// side reads one side of market's book, every page.
func (s *Service) side(ctx context.Context, market, orderType string, reverse bool) ([]tradebintypes.AggregatedOrder, error) {
	var (
		out []tradebintypes.AggregatedOrder
		key []byte
	)
	for {
		res, err := s.deps.Node.MarketAggregatedOrders(ctx, &tradebintypes.QueryMarketAggregatedOrdersRequest{
			Market: market, OrderType: orderType,
			Pagination: &query.PageRequest{Key: key, Limit: PageLimit, Reverse: reverse},
		})
		if err != nil {
			return nil, fmt.Errorf("%s orders of %s: %w", orderType, market, err)
		}
		out = append(out, res.GetList()...)
		if key = res.GetPagination().GetNextKey(); len(key) == 0 {
			return out, nil
		}
	}
}
