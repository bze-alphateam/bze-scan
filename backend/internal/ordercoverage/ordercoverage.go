// Package ordercoverage says whether the explorer's orders table is complete
// for an account's open orders: the table is built from events only, so an
// order placed below the indexed history that is still open is missing from
// it. The table is complete when every height from the DEX's first market up
// to the live indexer's cursor is indexed and none of them has an open index
// failure.
//
// The check is cheap: a verified watermark is kept in memory. The first check
// counts the blocks rows over [start, last] once; every later one counts only
// (watermark, last]. The explorer never deletes a blocks row (a reindex
// rewrites rows, it never removes them), so the watermark only grows. A gap
// or an open failure in range makes the table incomplete and the next check
// starts again from the DEX's start height. Callers never wait for a check:
// before the first one has finished, and after a database error, the table
// is reported incomplete.
package ordercoverage

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// DefaultInterval is how often the coverage is checked again.
const DefaultInterval = time.Minute

// DEXStart is the height of the first MarketCreatedEvent of each chain. A
// chain without an entry is never complete, so its open orders are always
// read from the node (testnet: its history starts at a floor set by hand).
var DEXStart = map[string]int64{
	// Tx 7681D87EA9B3E8AD8747532CE05A57AB15F396B24ADA5E2A3C6694044A077D6E,
	// 2024-08-02.
	"beezee-1": 12_940_553,
}

// Store reads the indexed range.
type Store interface {
	// LastIndexed returns the live indexer's cursor; false when the live
	// indexer has not written yet.
	LastIndexed(ctx context.Context) (int64, bool, error)
	// CountBlocks returns how many blocks rows lie in [lo, hi].
	CountBlocks(ctx context.Context, lo, hi int64) (int64, error)
	// OpenFailures reports whether an unresolved index failure lies in
	// [lo, hi].
	OpenFailures(ctx context.Context, lo, hi int64) (bool, error)
}

// Config tunes the checker; the zero value is production.
type Config struct {
	// ChainID selects the DEX start height.
	ChainID string
	// Starts replaces DEXStart; nil is DEXStart.
	Starts map[string]int64
	// Interval between checks; zero is DefaultInterval.
	Interval time.Duration
	// Log defaults to the standard logger.
	Log log.FieldLogger
}

// Coverage is the checker. Its zero state is incomplete.
type Coverage struct {
	store    Store
	start    int64
	hasStart bool
	interval time.Duration
	log      log.FieldLogger

	mu sync.Mutex
	// watermark is the highest height up to which [start, watermark] is
	// verified complete; 0 when nothing is verified.
	watermark int64
	complete  bool
}

// New returns a checker over store; nothing is checked until Check or Run.
func New(cfg Config, store Store) *Coverage {
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.Log == nil {
		cfg.Log = log.StandardLogger()
	}
	if cfg.Starts == nil {
		cfg.Starts = DEXStart
	}
	start, ok := cfg.Starts[cfg.ChainID]
	return &Coverage{store: store, start: start, hasStart: ok, interval: cfg.Interval, log: cfg.Log}
}

// Complete reports whether the last check found the orders table complete.
// It never blocks on a check.
func (c *Coverage) Complete() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.complete
}

// Run checks at once, then every interval, until ctx is cancelled. A chain
// without a DEX start height is never checked. Errors are logged, never
// returned: the reader then falls back to the node.
func (c *Coverage) Run(ctx context.Context) error {
	if !c.hasStart {
		c.log.Info("order coverage: no DEX start height for this chain, open orders are read from the node")
		return nil
	}
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		if err := c.Check(ctx); err != nil && ctx.Err() == nil {
			c.log.WithError(err).Warn("order coverage: check failed, open orders are read from the node")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// Check runs one check and updates the verdict. A database error leaves the
// table incomplete until the next successful check, keeping the watermark
// (rows are never deleted, so what was verified stays verified).
func (c *Coverage) Check(ctx context.Context) error {
	if !c.hasStart {
		return nil
	}
	complete, watermark, err := c.check(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.complete = false
		return err
	}
	if c.complete && !complete {
		c.log.WithField("watermark", c.watermark).Warn("order coverage: the indexed history has a gap or an open failure")
	}
	c.complete, c.watermark = complete, watermark
	return nil
}

// check returns the verdict and the new watermark.
func (c *Coverage) check(ctx context.Context) (bool, int64, error) {
	c.mu.Lock()
	from := c.watermark
	c.mu.Unlock()

	last, ok, err := c.store.LastIndexed(ctx)
	if err != nil {
		return false, from, err
	}
	if !ok || last < c.start {
		return false, 0, nil
	}
	failed, err := c.store.OpenFailures(ctx, c.start, last)
	if err != nil {
		return false, from, err
	}
	if failed {
		return false, 0, nil
	}
	lo := c.start
	if from >= c.start {
		lo = from + 1
	}
	if lo <= last {
		n, err := c.store.CountBlocks(ctx, lo, last)
		if err != nil {
			return false, from, err
		}
		if n != last-lo+1 {
			return false, 0, nil
		}
	}
	return true, last, nil
}
