// Package status keeps the explorer's data verdict: a checker goroutine reads
// the explorer's height and the tips of the local node and an archive node on
// every tick and stores a snapshot that GET /api/v1/status serves as is. A
// request never touches a node or the database.
//
// This is deliberately separate from GET /health, the deploy's blue/green
// probe: a halted chain must never block a release.
package status

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// Defaults applied to the zero fields of Config.
const (
	DefaultInterval       = 60 * time.Second
	DefaultTolerance      = 5
	DefaultRequestTimeout = 5 * time.Second
)

// Back-fill states reported in back_fill.status.
const (
	BackFillFinished   = "finished"
	BackFillInProgress = "in_progress"
)

// Store reads the explorer's heights.
type Store interface {
	// LastIndexedHeight is indexer_state.last_indexed_height; false when the
	// live indexer has not written anything yet.
	LastIndexedHeight(ctx context.Context) (int64, bool, error)
	// OldestHeight is the lowest height in explorer.blocks; false when the
	// table is empty.
	OldestHeight(ctx context.Context) (int64, bool, error)
}

// Node answers CometBFT's /status; *node.Client satisfies it.
type Node interface {
	Status(ctx context.Context) (*node.Status, []byte, error)
}

// Config tunes the checker; zero fields take the defaults.
type Config struct {
	// Interval is the period between ticks; the first tick runs at once.
	Interval time.Duration
	// Tolerance is the largest healthy spread between the three heights.
	// Negative means DefaultTolerance; 0 is a valid tolerance.
	Tolerance int64
	// RequestTimeout bounds each node call.
	RequestTimeout time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Deps are the checker's collaborators, built by the composition root.
type Deps struct {
	Store Store
	// Local is the node whose sink feeds the explorer.
	Local Node
	// Archive is the archive node; ArchiveRetry is tried when it fails (nil
	// means Archive again). A nil Archive reports no archive height.
	Archive      Node
	ArchiveRetry Node
	// Log receives the read failures; nil is the standard logger.
	Log log.FieldLogger
}

// Snapshot is the result of one tick. Nil heights could not be read.
type Snapshot struct {
	// CheckedAt is when the tick ran; zero before the first tick completed.
	CheckedAt     time.Time
	Healthy       bool
	DBHeight      *int64
	NodeHeight    *int64
	ArchiveHeight *int64
	// BackFill is BackFillFinished or BackFillInProgress.
	BackFill     string
	OldestHeight *int64
}

// Checker runs the ticks and holds the latest snapshot.
type Checker struct {
	cfg  Config
	deps Deps
	last atomic.Pointer[Snapshot]
	// prevDB is the explorer's height at the previous tick; only the tick
	// loop touches it.
	prevDB *int64
}

// New returns a checker; its snapshot is the pre-first-tick form until Check
// or Run completes a tick.
func New(cfg Config, deps Deps) *Checker {
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.Tolerance < 0 {
		cfg.Tolerance = DefaultTolerance
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
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
	c := &Checker{cfg: cfg, deps: deps}
	c.last.Store(&Snapshot{BackFill: BackFillFinished})
	return c
}

// Snapshot returns the latest snapshot without any I/O.
func (c *Checker) Snapshot() Snapshot {
	return *c.last.Load()
}

// Run ticks at once and then every Interval until ctx is cancelled; it
// always returns nil. Ticks never overlap: Check must not be called while Run
// runs.
func (c *Checker) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()
	for {
		c.Check(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Check runs one tick: it reads the three heights, decides health and stores
// the snapshot, which it returns. A tick cut short by ctx stores nothing.
func (c *Checker) Check(ctx context.Context) Snapshot {
	s := Snapshot{BackFill: BackFillFinished}
	s.DBHeight = c.read(ctx, "explorer height", c.deps.Store.LastIndexedHeight)
	s.OldestHeight = c.read(ctx, "oldest height", c.deps.Store.OldestHeight)
	s.NodeHeight = c.tip(ctx, "local node", c.deps.Local)
	if c.deps.Archive != nil {
		if s.ArchiveHeight = c.tip(ctx, "archive node", c.deps.Archive); s.ArchiveHeight == nil {
			s.ArchiveHeight = c.tip(ctx, "archive retry node", c.deps.ArchiveRetry)
		}
	}
	if ctx.Err() != nil {
		return c.Snapshot()
	}

	s.Healthy = healthy(c.prevDB, s.DBHeight, s.NodeHeight, s.ArchiveHeight, c.cfg.Tolerance)
	s.CheckedAt = c.cfg.Now().UTC()
	if s.DBHeight != nil {
		c.prevDB = s.DBHeight
	}
	c.last.Store(&s)
	return s
}

// healthy: every height is known, their spread is within tolerance, and the
// explorer moved since the previous known height (no previous height, as on
// the first tick after a start, leaves only the spread test).
func healthy(prev, db, local, archive *int64, tolerance int64) bool {
	if db == nil || local == nil || archive == nil {
		return false
	}
	if prev != nil && *db <= *prev {
		return false
	}
	lo := min(*db, *local, *archive)
	hi := max(*db, *local, *archive)
	return hi-lo <= tolerance
}

func (c *Checker) read(ctx context.Context, what string, f func(context.Context) (int64, bool, error)) *int64 {
	h, ok, err := f(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			c.deps.Log.WithError(err).Warnf("status: reading the %s", what)
		}
		return nil
	}
	if !ok {
		return nil
	}
	return &h
}

func (c *Checker) tip(ctx context.Context, what string, n Node) *int64 {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()
	st, _, err := n.Status(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			c.deps.Log.WithError(err).Warnf("status: reading the %s's /status", what)
		}
		return nil
	}
	return &st.LatestBlockHeight
}
