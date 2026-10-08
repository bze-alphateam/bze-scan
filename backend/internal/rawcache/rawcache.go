// Package rawcache serves the node's raw by-height JSON (the /block,
// /block_results and /commit response bodies) for the explorer's "More
// details" view. Entries live in memory, one LRU per route; a miss asks an
// archive node, never the local one, whose pruning the backend does not know.
// The live indexer puts the bodies it already fetched, so recent heights never
// reach the archive.
//
// By-height data is immutable: an entry is never refreshed, and expiry exists
// only to bound memory. Raw payloads are never written to the database.
package rawcache

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// Route is a by-height node route the cache serves.
type Route string

// The cached routes.
const (
	RouteBlock        Route = "block"
	RouteBlockResults Route = "block_results"
	RouteCommit       Route = "commit"
)

// Defaults applied to the zero fields of Config.
const (
	DefaultMaxEntries   = 300
	DefaultTTL          = 20 * time.Minute
	DefaultFetchTimeout = 15 * time.Second
)

var (
	// ErrUpstream: no archive node answered the height (unreachable, an
	// error, or a height above its tip). Nothing is cached.
	ErrUpstream = errors.New("archive node did not serve the height")
	// ErrUnknownRoute: the route is not one of the cached routes.
	ErrUnknownRoute = errors.New("not a cached route")
	// ErrTxNotInBlock: the block has no transaction at the index asked for.
	ErrTxNotInBlock = errors.New("no transaction at that index in the block")
)

// Node is the part of the node client the cache fetches through;
// *node.Client satisfies it.
type Node interface {
	Block(ctx context.Context, height int64) (*node.Block, []byte, error)
	BlockResults(ctx context.Context, height int64) (*node.BlockResults, []byte, error)
	Commit(ctx context.Context, height int64) (*node.Commit, []byte, error)
}

// Config tunes the cache; zero fields take the defaults.
type Config struct {
	// MaxEntries bounds each route's LRU.
	MaxEntries int
	// TTL is how long an entry lives after it was stored.
	TTL time.Duration
	// FetchTimeout bounds one archive fetch, retry included. A fetch outlives
	// the request that started it, so a request that gives up never fails
	// the others waiting for the same height.
	FetchTimeout time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Deps are the cache's collaborators, built by the composition root.
type Deps struct {
	// Archive answers misses; ArchiveRetry is tried once when it fails (nil
	// means Archive again).
	Archive      Node
	ArchiveRetry Node
	// Log receives the fetch failures; nil is the standard logger.
	Log log.FieldLogger
}

// Cache is safe for concurrent use.
type Cache struct {
	cfg   Config
	deps  Deps
	mu    sync.Mutex
	lrus  map[Route]*lru
	group singleflight.Group
}

// New returns an empty cache.
func New(cfg Config, deps Deps) *Cache {
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = DefaultMaxEntries
	}
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultTTL
	}
	if cfg.FetchTimeout <= 0 {
		cfg.FetchTimeout = DefaultFetchTimeout
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	if deps.ArchiveRetry == nil {
		deps.ArchiveRetry = deps.Archive
	}
	c := &Cache{cfg: cfg, deps: deps, lrus: map[Route]*lru{}}
	for _, r := range []Route{RouteBlock, RouteBlockResults, RouteCommit} {
		c.lrus[r] = newLRU(cfg.MaxEntries)
	}
	return c
}

// Get returns the node's response body for route at height: from the cache,
// or from the archive on a miss. Concurrent misses for one height share one
// fetch. Only a successful body is stored; a failed fetch is ErrUpstream.
func (c *Cache) Get(ctx context.Context, route Route, height int64) ([]byte, error) {
	if _, ok := c.lrus[route]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownRoute, route)
	}
	if body, ok := c.get(route, height); ok {
		return body, nil
	}
	ch := c.group.DoChan(string(route)+"/"+strconv.FormatInt(height, 10), func() (any, error) {
		// A Put may have landed since the miss.
		if body, ok := c.get(route, height); ok {
			return body, nil
		}
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.cfg.FetchTimeout)
		defer cancel()
		body, err := c.fetch(fctx, route, height)
		if err != nil {
			return nil, err
		}
		c.put(route, height, body)
		return body, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.([]byte), nil
	}
}

// PutHeight stores the three bodies of a height the live indexer has
// written. A height already cached keeps its entries.
func (c *Cache) PutHeight(height int64, block, blockResults, commit []byte) {
	c.put(RouteBlock, height, block)
	c.put(RouteBlockResults, height, blockResults)
	c.put(RouteCommit, height, commit)
}

// Tx is one transaction of a block as the node encodes it.
type Tx struct {
	// Tx is the base64 raw transaction, /block data.txs[index].
	Tx string
	// Result is /block_results txs_results[index], verbatim.
	Result json.RawMessage
}

// Tx returns the transaction at index of the block at height, sliced from
// the cached (or fetched) /block and /block_results bodies.
func (c *Cache) Tx(ctx context.Context, height int64, index int) (*Tx, error) {
	blockBody, err := c.Get(ctx, RouteBlock, height)
	if err != nil {
		return nil, err
	}
	resultsBody, err := c.Get(ctx, RouteBlockResults, height)
	if err != nil {
		return nil, err
	}
	var b struct {
		Result struct {
			Block struct {
				Data struct {
					Txs []string `json:"txs"`
				} `json:"data"`
			} `json:"block"`
		} `json:"result"`
	}
	if err := json.Unmarshal(blockBody, &b); err != nil {
		return nil, fmt.Errorf("block %d: %w", height, err)
	}
	var r struct {
		Result struct {
			TxsResults []json.RawMessage `json:"txs_results"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resultsBody, &r); err != nil {
		return nil, fmt.Errorf("block_results %d: %w", height, err)
	}
	txs, results := b.Result.Block.Data.Txs, r.Result.TxsResults
	if index < 0 || index >= len(txs) || index >= len(results) {
		return nil, fmt.Errorf("%w: height %d index %d (%d transactions, %d results)",
			ErrTxNotInBlock, height, index, len(txs), len(results))
	}
	return &Tx{Tx: txs[index], Result: results[index]}, nil
}

// fetch asks the archive, then the retry node once.
func (c *Cache) fetch(ctx context.Context, route Route, height int64) ([]byte, error) {
	body, err := call(ctx, c.deps.Archive, route, height)
	if err == nil {
		return body, nil
	}
	c.deps.Log.WithError(err).WithField("height", height).Warnf("raw cache: archive %s failed, retrying", route)
	body, err = call(ctx, c.deps.ArchiveRetry, route, height)
	if err != nil {
		c.deps.Log.WithError(err).WithField("height", height).Warnf("raw cache: archive retry %s failed", route)
		return nil, fmt.Errorf("%w: %s %d: %w", ErrUpstream, route, height, err)
	}
	return body, nil
}

func call(ctx context.Context, n Node, route Route, height int64) ([]byte, error) {
	var body []byte
	var err error
	switch route {
	case RouteBlock:
		_, body, err = n.Block(ctx, height)
	case RouteBlockResults:
		_, body, err = n.BlockResults(ctx, height)
	case RouteCommit:
		_, body, err = n.Commit(ctx, height)
	}
	return body, err
}

func (c *Cache) get(route Route, height int64) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lrus[route].get(height, c.cfg.Now())
}

func (c *Cache) put(route Route, height int64, body []byte) {
	if len(body) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lrus[route].put(height, body, c.cfg.Now(), c.cfg.TTL)
}

// lru is one route's entries, most recently used at the front.
type lru struct {
	max   int
	order *list.List
	items map[int64]*list.Element
}

type entry struct {
	height  int64
	body    []byte
	expires time.Time
}

func newLRU(maxEntries int) *lru {
	return &lru{max: maxEntries, order: list.New(), items: map[int64]*list.Element{}}
}

func (l *lru) get(height int64, now time.Time) ([]byte, bool) {
	el, ok := l.items[height]
	if !ok {
		return nil, false
	}
	e := el.Value.(*entry)
	if !now.Before(e.expires) {
		l.remove(el)
		return nil, false
	}
	l.order.MoveToFront(el)
	return e.body, true
}

// put stores body unless a live entry exists: by-height data never changes.
// An expired entry is replaced.
func (l *lru) put(height int64, body []byte, now time.Time, ttl time.Duration) {
	if el, ok := l.items[height]; ok {
		if now.Before(el.Value.(*entry).expires) {
			return
		}
		l.remove(el)
	}
	l.items[height] = l.order.PushFront(&entry{height: height, body: body, expires: now.Add(ttl)})
	for l.order.Len() > l.max {
		l.remove(l.order.Back())
	}
}

func (l *lru) remove(el *list.Element) {
	l.order.Remove(el)
	delete(l.items, el.Value.(*entry).height)
}
