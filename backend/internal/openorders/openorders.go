// Package openorders answers what an account has locked in open DEX orders,
// per denom and per market.
//
// The explorer's orders table is built from events only, so it knows every
// open order only when the indexed history is complete from the DEX's first
// market onwards (package ordercoverage). Then the answer comes from the
// table, as of the live indexer's cursor. Otherwise it comes from the node:
// UserMarketOrders lists the account's order references and MarketOrder reads
// each one (1 + N calls, at most InFlight at a time), every read pinned to one
// height. The local node's gRPC is asked first and the archive's /abci_query
// when the local node cannot be reached. Answers are cached per address for a
// few seconds, and concurrent misses of one address share a single read.
package openorders

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

// Where an answer comes from.
const (
	SourceIndex = "index"
	SourceNode  = "node"
)

// Defaults of Config.
const (
	DefaultTTL        = 15 * time.Second
	DefaultMarketsTTL = 5 * time.Minute
	DefaultInFlight   = 8
)

// PageLimit is the page size of UserMarketOrders and AllMarkets; MaxPages
// bounds how many pages one read follows.
const (
	PageLimit = 100
	MaxPages  = 100
)

// ErrUpstream is a node path that failed: both nodes unreachable, or a node
// answering an error.
var ErrUpstream = errors.New("open orders: the nodes did not answer")

// Index reads the explorer's orders table.
type Index interface {
	// OpenOrders returns the open orders of owner, Amount being what is
	// still on the book, and the height the table describes (the live
	// indexer's cursor).
	OpenOrders(ctx context.Context, owner string) ([]Order, int64, error)
}

// Coverage says whether the orders table is complete;
// *ordercoverage.Coverage satisfies it.
type Coverage interface {
	Complete() bool
}

// Deps of the reader.
type Deps struct {
	Index    Index
	Coverage Coverage
	// Local is the local node, asked first.
	Local Source
	// Archive is asked when Local cannot be reached; nil disables it.
	Archive Source
	// Log defaults to the standard logger.
	Log log.FieldLogger
}

// Config tunes the reader; the zero value is production.
type Config struct {
	// TTL is how long an answer is cached; zero is DefaultTTL.
	TTL time.Duration
	// MarketsTTL is how long the market list is cached; zero is
	// DefaultMarketsTTL.
	MarketsTTL time.Duration
	// InFlight bounds the concurrent MarketOrder reads; zero is
	// DefaultInFlight.
	InFlight int
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Reader answers open orders per account.
type Reader struct {
	deps     Deps
	inFlight int
	cache    *cache
	markets  *markets
}

// New returns a reader over deps.
func New(cfg Config, deps Deps) *Reader {
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultTTL
	}
	if cfg.MarketsTTL <= 0 {
		cfg.MarketsTTL = DefaultMarketsTTL
	}
	if cfg.InFlight <= 0 {
		cfg.InFlight = DefaultInFlight
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	return &Reader{
		deps:     deps,
		inFlight: cfg.InFlight,
		cache:    newCache(cfg.TTL, cfg.Now),
		markets:  &markets{ttl: cfg.MarketsTTL, now: cfg.Now},
	}
}

// OpenOrders returns what address has locked in open orders. A failure of
// the node path wraps ErrUpstream.
func (r *Reader) OpenOrders(ctx context.Context, address string) (Summary, error) {
	return r.cache.get(ctx, address, func(ctx context.Context) (Summary, error) {
		if r.deps.Coverage.Complete() {
			s, err := r.fromIndex(ctx, address)
			if err == nil {
				return s, nil
			}
			r.deps.Log.WithError(err).Warn("open orders: index read failed, asking the node")
		}
		return r.fromNode(ctx, address)
	})
}

func (r *Reader) fromIndex(ctx context.Context, address string) (Summary, error) {
	orders, height, err := r.deps.Index.OpenOrders(ctx, address)
	if err != nil {
		return Summary{}, err
	}
	byID, err := r.marketsFor(ctx, orders)
	if err != nil {
		return Summary{}, err
	}
	s, err := Summarise(orders, byID)
	if err != nil {
		return Summary{}, err
	}
	s.Source, s.Height = SourceIndex, height
	return s, nil
}

// marketsFor resolves the markets of orders read from the index, through
// whichever node answers.
func (r *Reader) marketsFor(ctx context.Context, orders []Order) (map[string]Market, error) {
	ids := marketIDs(orders)
	if byID, ok := r.markets.cached(ids); ok {
		return byID, nil
	}
	var byID map[string]Market
	err := r.eachSource(ctx, func(src Source) error {
		h, err := src.Height(ctx)
		if err != nil {
			return err
		}
		byID, err = r.markets.resolve(ctx, src, h, ids)
		return err
	})
	return byID, err
}

func (r *Reader) fromNode(ctx context.Context, address string) (Summary, error) {
	var s Summary
	err := r.eachSource(ctx, func(src Source) (err error) {
		s, err = r.readNode(ctx, src, address)
		return err
	})
	return s, err
}

// eachSource runs read on the local node, then on the archive when the
// local node cannot be reached. Every failure wraps ErrUpstream.
func (r *Reader) eachSource(ctx context.Context, read func(Source) error) error {
	sources := []Source{r.deps.Local}
	if r.deps.Archive != nil {
		sources = append(sources, r.deps.Archive)
	}
	var errs []error
	for _, src := range sources {
		err := read(src)
		if err == nil {
			return nil
		}
		errs = append(errs, err)
		if !unavailable(ctx, err) {
			break
		}
	}
	return fmt.Errorf("%w: %w", ErrUpstream, errors.Join(errs...))
}

// readNode reads the open orders of address from src, every read at the
// same height.
func (r *Reader) readNode(ctx context.Context, src Source, address string) (Summary, error) {
	h, err := src.Height(ctx)
	if err != nil {
		return Summary{}, err
	}
	var refs []tradebintypes.OrderReference
	var key []byte
	for page := 0; ; page++ {
		if page == MaxPages {
			return Summary{}, fmt.Errorf("orders of %s: more than %d pages", address, MaxPages)
		}
		res, err := src.UserMarketOrders(ctx, h, &tradebintypes.QueryUserMarketOrdersRequest{
			Address: address, Pagination: &query.PageRequest{Key: key, Limit: PageLimit},
		})
		if err != nil {
			return Summary{}, fmt.Errorf("orders of %s: %w", address, err)
		}
		refs = append(refs, res.List...)
		if res.Pagination == nil || len(res.Pagination.NextKey) == 0 {
			break
		}
		key = res.Pagination.NextKey
	}

	found := make([]*Order, len(refs))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(r.inFlight)
	for i, ref := range refs {
		g.Go(func() error {
			res, err := src.MarketOrder(gctx, h, &tradebintypes.QueryMarketOrderRequest{
				Market: ref.MarketId, OrderType: ref.OrderType, OrderId: ref.Id,
			})
			if notFound(err) {
				// Filled or cancelled between the two reads.
				return nil
			}
			if err != nil {
				return fmt.Errorf("order %s: %w", ref.Id, err)
			}
			found[i] = &Order{
				ID: ref.Id, MarketID: ref.MarketId, Side: ref.OrderType,
				Amount: res.Order.Amount, Price: res.Order.Price,
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return Summary{}, err
	}
	orders := make([]Order, 0, len(found))
	for _, o := range found {
		if o != nil {
			orders = append(orders, *o)
		}
	}
	byID, err := r.markets.resolve(ctx, src, h, marketIDs(orders))
	if err != nil {
		return Summary{}, err
	}
	s, err := Summarise(orders, byID)
	if err != nil {
		return Summary{}, err
	}
	s.Source, s.Height = SourceNode, h
	return s, nil
}

func marketIDs(orders []Order) []string {
	seen := map[string]bool{}
	var ids []string
	for _, o := range orders {
		if !seen[o.MarketID] {
			seen[o.MarketID] = true
			ids = append(ids, o.MarketID)
		}
	}
	return ids
}

// markets caches the chain's market list. Market ids are looked up whole,
// never split on "/": factory denoms contain slashes.
type markets struct {
	ttl time.Duration
	now func() time.Time

	load sync.Mutex // one load at a time

	mu       sync.Mutex
	byID     map[string]Market
	loadedAt time.Time
}

// cached returns the markets of ids when the list is fresh and has them all.
func (m *markets) cached(ids []string) (map[string]Market, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byID == nil || m.now().Sub(m.loadedAt) >= m.ttl {
		return nil, false
	}
	out := make(map[string]Market, len(ids))
	for _, id := range ids {
		mk, ok := m.byID[id]
		if !ok {
			return nil, false
		}
		out[id] = mk
	}
	return out, true
}

// resolve returns the markets of ids, reloading the list from src at height
// once when it is stale or misses one of them. A market the fresh list does
// not have is an error.
func (m *markets) resolve(ctx context.Context, src Source, height int64, ids []string) (map[string]Market, error) {
	if out, ok := m.cached(ids); ok {
		return out, nil
	}
	m.load.Lock()
	defer m.load.Unlock()
	if out, ok := m.cached(ids); ok { // loaded while waiting
		return out, nil
	}
	byID := map[string]Market{}
	var key []byte
	for page := 0; ; page++ {
		if page == MaxPages {
			return nil, fmt.Errorf("markets: more than %d pages", MaxPages)
		}
		res, err := src.AllMarkets(ctx, height, &tradebintypes.QueryAllMarketsRequest{
			Pagination: &query.PageRequest{Key: key, Limit: PageLimit},
		})
		if err != nil {
			return nil, fmt.Errorf("markets: %w", err)
		}
		for _, mk := range res.Market {
			id := tradebintypes.CreateMarketId(mk.Base, mk.Quote)
			byID[id] = Market{ID: id, Base: mk.Base, Quote: mk.Quote}
		}
		if res.Pagination == nil || len(res.Pagination.NextKey) == 0 {
			break
		}
		key = res.Pagination.NextKey
	}
	m.mu.Lock()
	m.byID, m.loadedAt = byID, m.now()
	m.mu.Unlock()

	out := make(map[string]Market, len(ids))
	for _, id := range ids {
		mk, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("market %q is not listed by the node", id)
		}
		out[id] = mk
	}
	return out, nil
}

// cache holds answers by address until they expire. Errors are never
// cached.
type cache struct {
	ttl time.Duration
	now func() time.Time

	group singleflight.Group

	mu        sync.Mutex
	entries   map[string]entry
	lastSweep time.Time
}

type entry struct {
	value   Summary
	expires time.Time
}

func newCache(ttl time.Duration, now func() time.Time) *cache {
	return &cache{ttl: ttl, now: now, entries: map[string]entry{}}
}

// get returns the cached answer of key, or loads it once for every
// concurrent caller. The load outlives a caller that gives up (the node
// clients bound it), so one cancelled request does not fail the others.
func (c *cache) get(ctx context.Context, key string, load func(context.Context) (Summary, error)) (Summary, error) {
	c.mu.Lock()
	e, ok := c.entries[key]
	c.mu.Unlock()
	if ok && c.now().Before(e.expires) {
		return e.value, nil
	}
	ch := c.group.DoChan(key, func() (any, error) {
		v, err := load(context.WithoutCancel(ctx))
		if err != nil {
			return nil, err
		}
		c.put(key, v)
		return v, nil
	})
	select {
	case res := <-ch:
		if res.Err != nil {
			return Summary{}, res.Err
		}
		s, _ := res.Val.(Summary) // load returns a Summary
		return s, nil
	case <-ctx.Done():
		return Summary{}, ctx.Err()
	}
}

// put stores value and, at most once per TTL, drops the expired entries.
func (c *cache) put(key string, value Summary) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if now.Sub(c.lastSweep) >= c.ttl {
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
		c.lastSweep = now
	}
	c.entries[key] = entry{value: value, expires: now.Add(c.ttl)}
}
