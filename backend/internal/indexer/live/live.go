// Package live is the live indexer: a dedicated PostgreSQL connection
// listens on the notification the sink's blocks trigger sends at the commit
// of each block, and a cursor against the node's height indexes every height
// the explorer is missing, in order. The notification is the fast path; the
// cursor is the guarantee.
package live

import (
	"context"
	"errors"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/migrations"
)

// Node is the part of the node client the indexer uses.
type Node interface {
	Status(ctx context.Context) (*node.Status, []byte, error)
	Block(ctx context.Context, height int64) (*node.Block, []byte, error)
	BlockResults(ctx context.Context, height int64) (*node.BlockResults, []byte, error)
	Commit(ctx context.Context, height int64) (*node.Commit, []byte, error)
}

// Listener opens subscriptions to the block notifications of the sink.
type Listener interface {
	// Listen connects and starts listening.
	Listen(ctx context.Context) (Subscription, error)
}

// Subscription is one listening connection.
type Subscription interface {
	// Wait blocks until the next notification and returns its height (0
	// when the payload is not a height). An error means the connection is
	// gone.
	Wait(ctx context.Context) (int64, error)
	// Close closes the connection.
	Close()
}

// Store is the part of the live writer the indexer uses.
type Store interface {
	Cursor(ctx context.Context) (int64, bool, error)
	LiveFloor(ctx context.Context) (int64, bool, error)
	WriteBlock(ctx context.Context, ents *transform.Entities) error
	RecordFailure(ctx context.Context, height int64, attempts int, cause error) error
}

// Transformer turns the node's answers for one height into entities.
type Transformer interface {
	Transform(in transform.Input) (*transform.Entities, error)
}

// Deps are the indexer's dependencies.
type Deps struct {
	Listener    Listener
	Node        Node
	Store       Store
	Transformer Transformer
}

// Defaults of Config.
var (
	// DefaultRetryDelays are the waits between the attempts at one height:
	// three attempts in all.
	DefaultRetryDelays = []time.Duration{500 * time.Millisecond, 2 * time.Second}
	// DefaultReconnectMin and DefaultReconnectMax bound the reconnect backoff.
	DefaultReconnectMin = time.Second
	DefaultReconnectMax = 30 * time.Second
	// DefaultShutdownGrace is how long the height in flight may run on after
	// shutdown is requested.
	DefaultShutdownGrace = 10 * time.Second
)

// Config of an Indexer. Zero values take the defaults above.
type Config struct {
	// RetryDelays are the waits between attempts at one height; the number of
	// attempts is len(RetryDelays)+1.
	RetryDelays   []time.Duration
	ReconnectMin  time.Duration
	ReconnectMax  time.Duration
	ShutdownGrace time.Duration
	// Sleep waits for d or until ctx is done (a fake clock in tests).
	Sleep func(ctx context.Context, d time.Duration) error
}

// Indexer is the live indexer. Run it once per database.
type Indexer struct {
	cfg         Config
	listener    Listener
	node        Node
	store       Store
	transformer Transformer
}

// New returns an indexer woken by deps.Listener, reading deps.Node,
// transforming with deps.Transformer and writing through deps.Store.
func New(cfg Config, deps Deps) *Indexer {
	if cfg.RetryDelays == nil {
		cfg.RetryDelays = DefaultRetryDelays
	}
	if cfg.ReconnectMin <= 0 {
		cfg.ReconnectMin = DefaultReconnectMin
	}
	if cfg.ReconnectMax <= 0 {
		cfg.ReconnectMax = DefaultReconnectMax
	}
	if cfg.ShutdownGrace <= 0 {
		cfg.ShutdownGrace = DefaultShutdownGrace
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleep
	}
	return &Indexer{cfg: cfg, listener: deps.Listener, node: deps.Node, store: deps.Store, transformer: deps.Transformer}
}

// Run listens and indexes until ctx is cancelled, reconnecting with backoff
// when the connection or a pass fails. It returns nil on a clean stop.
func (ix *Indexer) Run(ctx context.Context) error {
	backoff := ix.cfg.ReconnectMin
	for {
		err := ix.session(ctx, func() { backoff = ix.cfg.ReconnectMin })
		if ctx.Err() != nil {
			log.Info("live indexer stopped")
			return nil
		}
		log.WithError(err).WithField("retry_in", backoff.String()).Warn("live indexer: connection lost, reconnecting")
		if ix.cfg.Sleep(ctx, backoff) != nil {
			log.Info("live indexer stopped")
			return nil
		}
		backoff = min(backoff*2, ix.cfg.ReconnectMax)
	}
}

// session listens and runs a pass at once and at every notification, until
// the connection or a pass fails. connected is called once listening.
func (ix *Indexer) session(ctx context.Context, connected func()) error {
	sub, err := ix.listener.Listen(ctx)
	if err != nil {
		return err
	}
	defer sub.Close()
	log.WithField("channel", migrations.NotifyChannel).Info("live indexer listening")
	connected()

	// A notification sent while the connection was down is lost: the pass on
	// (re)connect catches up against the node.
	if err := ix.pass(ctx, 0); err != nil {
		return err
	}
	for {
		h, err := sub.Wait(ctx)
		if err != nil {
			return fmt.Errorf("wait for notification: %w", err)
		}
		if err := ix.pass(ctx, h); err != nil {
			return err
		}
	}
}

// heightRange returns the heights a pass indexes, [from, to], or ok false when
// there is nothing to do. cursor is the last indexed height (hasCursor false
// before the first write), floor the live floor (hasFloor false before the
// first write), notified the height of the notification that woke the pass
// (0 on a (re)connect) and tip the node's latest height.
//
// The target is the greater of notified and tip. A first start has no cursor
// and begins at the target: the live side starts at the chain's head and the
// backfill owns everything below. The live side never indexes below the
// floor.
func heightRange(cursor int64, hasCursor bool, floor int64, hasFloor bool, notified, tip int64) (from, to int64, ok bool) {
	to = max(notified, tip)
	if to <= 0 {
		return 0, 0, false
	}
	if hasCursor {
		from = cursor + 1
	} else {
		from = to
	}
	if hasFloor {
		from = max(from, floor)
	}
	return from, to, from <= to
}

// pass indexes every height between the cursor and the target, in order. It
// returns an error only for what a reconnect may fix (the database or the
// node unreachable); a height that keeps failing is recorded and skipped.
func (ix *Indexer) pass(ctx context.Context, notified int64) error {
	st, _, err := ix.node.Status(ctx)
	if err != nil {
		return fmt.Errorf("node status: %w", err)
	}
	cursor, hasCursor, err := ix.store.Cursor(ctx)
	if err != nil {
		return err
	}
	floor, hasFloor, err := ix.store.LiveFloor(ctx)
	if err != nil {
		return err
	}
	from, to, ok := heightRange(cursor, hasCursor, floor, hasFloor, notified, st.LatestBlockHeight)
	if !ok {
		return nil
	}
	if to > from {
		log.WithFields(log.Fields{"from": from, "to": to}).Info("live indexer catching up")
	}
	for h := from; h <= to; h++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := ix.indexHeight(ctx, h); err != nil {
			return err
		}
	}
	return nil
}

// indexHeight indexes one height with retries. Once started, it runs on for
// up to ShutdownGrace after ctx is cancelled so the height in flight is
// finished; the waits between attempts end at once on shutdown.
func (ix *Indexer) indexHeight(ctx context.Context, h int64) error {
	hctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	stop := context.AfterFunc(ctx, func() {
		t := time.AfterFunc(ix.cfg.ShutdownGrace, cancel)
		context.AfterFunc(hctx, func() { t.Stop() })
	})
	defer stop()

	attempts := len(ix.cfg.RetryDelays) + 1
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			if err := ix.cfg.Sleep(ctx, ix.cfg.RetryDelays[attempt-2]); err != nil {
				return err
			}
		}
		start := time.Now()
		lastErr = ix.indexOnce(hctx, h)
		if lastErr == nil {
			log.WithFields(log.Fields{"height": h, "took_ms": time.Since(start).Milliseconds()}).Debug("block indexed")
			return nil
		}
		if errors.Is(lastErr, node.ErrPruned) {
			// Retrying cannot bring a pruned height back; the backfill
			// pipeline reads it from an archive node.
			return ix.recordFailure(hctx, h, attempt, lastErr)
		}
		log.WithError(lastErr).WithFields(log.Fields{"height": h, "attempt": attempt}).Warn("live indexer: height failed")
	}
	return ix.recordFailure(hctx, h, attempts, lastErr)
}

func (ix *Indexer) recordFailure(ctx context.Context, h int64, attempts int, cause error) error {
	log.WithError(cause).WithFields(log.Fields{"height": h, "attempts": attempts}).Error("live indexer: giving up on height, recorded in index_failures")
	if err := ix.store.RecordFailure(ctx, h, attempts, cause); err != nil {
		return fmt.Errorf("height %d: %w", h, err)
	}
	return nil
}

func (ix *Indexer) indexOnce(ctx context.Context, h int64) error {
	b, _, err := ix.node.Block(ctx, h)
	if err != nil {
		return err
	}
	r, _, err := ix.node.BlockResults(ctx, h)
	if err != nil {
		return err
	}
	c, _, err := ix.node.Commit(ctx, h)
	if err != nil {
		return err
	}
	ents, err := ix.transformer.Transform(transform.Input{Block: b, Results: r, Commit: c})
	if err != nil {
		return err
	}
	return ix.store.WriteBlock(ctx, ents)
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
