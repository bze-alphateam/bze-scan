// Package sink trims the CometBFT psql sink (public.blocks, tx_results,
// events and attributes), which has no retention of its own. The explorer
// reads nothing from it but the notification its trigger on public.blocks
// sends (everything else comes from the node's RPC), so the sink only has to
// keep the heights the live indexer may still be working on.
//
// The cut-off is the lower of two heights: the newest block logged into the
// sink longer than the retention ago, and the live indexer's cursor minus
// CursorMargin. Every height below the cut-off is deleted. Without a block
// that old, or before the live indexer has written, nothing is deleted.
//
// The rows go in batches of heights, from the oldest stored height up, each
// batch one short transaction deleting children first (attributes, events,
// tx_results, then blocks). The order works with and without ON DELETE
// CASCADE on the sink's foreign keys, and walking up keeps what is left
// contiguous when a run stops half way. Each batch starts at the oldest
// height still stored, so gaps in the sink cost nothing.
package sink

import (
	"context"
	"errors"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
)

// Defaults of the periodic job.
const (
	DefaultInterval = 24 * time.Hour
	DefaultBatch    = 1000
	// CursorMargin is how far below the live indexer's cursor the cut-off
	// stays at most.
	CursorMargin = 1000
)

// ErrOff is returned by Plan when no retention is configured.
var ErrOff = errors.New("sink cleanup is off: no retention set (SINK_RETENTION)")

// Counts are rows per sink table.
type Counts struct {
	Attributes int64
	Events     int64
	TxResults  int64
	Blocks     int64
}

// Add returns the sum of c and o.
func (c Counts) Add(o Counts) Counts {
	return Counts{
		Attributes: c.Attributes + o.Attributes,
		Events:     c.Events + o.Events,
		TxResults:  c.TxResults + o.TxResults,
		Blocks:     c.Blocks + o.Blocks,
	}
}

// String lists the counts per table.
func (c Counts) String() string {
	return fmt.Sprintf("attributes=%d events=%d tx_results=%d blocks=%d", c.Attributes, c.Events, c.TxResults, c.Blocks)
}

// Store reads and deletes the sink, and reads the live indexer's cursor.
type Store interface {
	// Oldest returns the lowest stored height; false on an empty sink.
	Oldest(ctx context.Context) (int64, bool, error)
	// NewestBefore returns the highest height logged into the sink before
	// t (blocks.created_at); false when there is none.
	NewestBefore(ctx context.Context, t time.Time) (int64, bool, error)
	// LastIndexed returns the live indexer's cursor; false when it has not
	// written yet.
	LastIndexed(ctx context.Context) (int64, bool, error)
	// Count returns the rows of the heights in [from, to).
	Count(ctx context.Context, from, to int64) (Counts, error)
	// Delete deletes the rows of the heights in [from, to), children
	// first, in one transaction, and returns what it deleted.
	Delete(ctx context.Context, from, to int64) (Counts, error)
}

// Config tunes the cleaner.
type Config struct {
	// Retention is how long a block stays in the sink; zero turns the
	// cleanup off.
	Retention time.Duration
	// Interval between the periodic job's runs; zero is DefaultInterval.
	Interval time.Duration
	// Batch is the heights deleted per transaction; zero is DefaultBatch.
	Batch int64
	// Now defaults to time.Now.
	Now func() time.Time
	// Log defaults to the standard logger.
	Log log.FieldLogger
}

// Plan is what one run deletes: the heights in [Oldest, Cutoff).
type Plan struct {
	// Oldest is the lowest stored height (0 on an empty sink).
	Oldest int64
	// Cutoff is the lowest height kept; 0 when nothing qualifies.
	Cutoff int64
	// ByAge is the newest height older than the retention and ByCursor the
	// cursor minus CursorMargin (0 when unknown); Cutoff is the lower.
	ByAge, ByCursor int64
}

// Empty reports whether the plan deletes nothing.
func (p Plan) Empty() bool {
	return p.Cutoff <= p.Oldest
}

// String describes the plan in one line.
func (p Plan) String() string {
	if p.Empty() {
		return fmt.Sprintf("nothing to delete (oldest %d, by age %d, by cursor %d)", p.Oldest, p.ByAge, p.ByCursor)
	}
	return fmt.Sprintf("cut-off %d: heights %d..%d (by age %d, by cursor %d)", p.Cutoff, p.Oldest, p.Cutoff-1, p.ByAge, p.ByCursor)
}

// Cleaner plans and runs the sink cleanup.
type Cleaner struct {
	cfg   Config
	store Store
}

// New returns a cleaner over store.
func New(cfg Config, store Store) *Cleaner {
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.Batch <= 0 {
		cfg.Batch = DefaultBatch
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = log.StandardLogger()
	}
	return &Cleaner{cfg: cfg, store: store}
}

// Plan computes the cut-off. It returns ErrOff without a retention.
func (c *Cleaner) Plan(ctx context.Context) (Plan, error) {
	if c.cfg.Retention <= 0 {
		return Plan{}, ErrOff
	}
	var p Plan
	oldest, ok, err := c.store.Oldest(ctx)
	if err != nil || !ok {
		return p, err
	}
	p.Oldest = oldest
	byAge, ok, err := c.store.NewestBefore(ctx, c.cfg.Now().Add(-c.cfg.Retention))
	if err != nil || !ok {
		return p, err
	}
	p.ByAge = byAge
	cursor, ok, err := c.store.LastIndexed(ctx)
	if err != nil || !ok {
		return p, err
	}
	p.ByCursor = max(cursor-CursorMargin, 0)
	p.Cutoff = min(p.ByAge, p.ByCursor)
	if p.Empty() {
		p.Cutoff = 0
	}
	return p, nil
}

// Count returns the rows the plan would delete.
func (c *Cleaner) Count(ctx context.Context, p Plan) (Counts, error) {
	if p.Empty() {
		return Counts{}, nil
	}
	return c.store.Count(ctx, p.Oldest, p.Cutoff)
}

// Clean deletes the plan's heights batch by batch and returns the rows
// deleted, also when it stops on an error or a cancelled ctx.
func (c *Cleaner) Clean(ctx context.Context, p Plan) (Counts, error) {
	var total Counts
	if p.Empty() {
		return total, nil
	}
	from := p.Oldest
	for from < p.Cutoff {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		to := min(from+c.cfg.Batch, p.Cutoff)
		n, err := c.store.Delete(ctx, from, to)
		if err != nil {
			return total, fmt.Errorf("delete heights %d..%d: %w", from, to-1, err)
		}
		total = total.Add(n)
		next, ok, err := c.store.Oldest(ctx)
		if err != nil {
			return total, err
		}
		if !ok {
			break
		}
		// Never go back: a height below `to` still stored was written
		// after this batch's delete and belongs to the next run.
		from = max(next, to)
	}
	return total, nil
}

// Once plans and cleans, then logs one line with the cut-off and the rows
// deleted per table.
func (c *Cleaner) Once(ctx context.Context) (Plan, Counts, error) {
	p, err := c.Plan(ctx)
	if err != nil {
		c.cfg.Log.WithError(err).Warn("sink cleanup: planning failed")
		return p, Counts{}, err
	}
	n, err := c.Clean(ctx, p)
	entry := c.cfg.Log.WithFields(log.Fields{
		"cutoff":     p.Cutoff,
		"oldest":     p.Oldest,
		"attributes": n.Attributes,
		"events":     n.Events,
		"tx_results": n.TxResults,
		"blocks":     n.Blocks,
	})
	if err != nil {
		entry.WithError(err).Warn("sink cleanup: stopped")
		return p, n, err
	}
	entry.Info("sink cleanup: done")
	return p, n, nil
}

// Run cleans at once, then every interval, until ctx is cancelled. Without
// a retention it returns at once and touches nothing. Errors are logged,
// never returned: the next run starts where this one stopped.
func (c *Cleaner) Run(ctx context.Context) error {
	if c.cfg.Retention <= 0 {
		c.cfg.Log.Info("sink cleanup: off (SINK_RETENTION is empty)")
		return nil
	}
	t := time.NewTicker(c.cfg.Interval)
	defer t.Stop()
	for {
		// Once logs its own errors.
		_, _, _ = c.Once(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}
