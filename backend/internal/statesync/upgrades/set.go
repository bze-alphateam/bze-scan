package upgrades

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/activeset"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// Defaults of the set.
const (
	// DefaultInterval is how often the set looks for upgrades to read.
	DefaultInterval = time.Minute
	// DefaultWindow is how many heights from U+1 the commits are read: a
	// member not back by then keeps no back_time.
	DefaultWindow = 10_000
	// DefaultMaxPerRun bounds the commits one run reads, every upgrade
	// together; the next run continues.
	DefaultMaxPerRun = 1_000
)

// Node reads commits by height; *activeset.Routed satisfies it.
type Node interface {
	Commit(ctx context.Context, height int64) (*node.Commit, []byte, error)
}

// Limiter paces the commit reads; *rate.Limiter satisfies it.
type Limiter interface {
	Wait(ctx context.Context) error
}

// Deps of the set.
type Deps struct {
	// Set reads the validator set at U+1.
	Set     activeset.Reader
	Node    Node
	Limiter Limiter
	Store   Store
	// Log defaults to the standard logger.
	Log log.FieldLogger
}

// Config of the set; zero values take the defaults.
type Config struct {
	Interval  time.Duration
	Window    int64
	MaxPerRun int64
}

// Set is the upgrade_recovery state-sync set.
type Set struct {
	deps Deps
	cfg  Config
}

var _ statesync.Set = (*Set)(nil)

// New returns the set.
func New(deps Deps, cfg Config) *Set {
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.Window <= 0 {
		cfg.Window = DefaultWindow
	}
	if cfg.MaxPerRun <= 0 {
		cfg.MaxPerRun = DefaultMaxPerRun
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	return &Set{deps: deps, cfg: cfg}
}

// Name is statesync.UpgradeRecovery.
func (s *Set) Name() string { return statesync.UpgradeRecovery }

// Interval is how often the set looks for upgrades to read.
func (s *Set) Interval() time.Duration { return s.cfg.Interval }

// FullResync adds the new upgrades and reads up to MaxPerRun more commits
// of the pending ones whose resume the explorer indexed (blocks U+1 and
// U+2: a historical upgrade waits for the backfill), lowest first. The
// failures are returned joined; an upgrade that fails continues on the
// next run.
func (s *Set) FullResync(ctx context.Context) error {
	if err := s.deps.Store.Discover(ctx); err != nil {
		return err
	}
	tip, ok, err := s.deps.Store.Tip(ctx)
	if err != nil || !ok {
		return err
	}
	pending, err := s.deps.Store.Pending(ctx)
	if err != nil {
		return err
	}
	var errs []error
	budget := s.cfg.MaxPerRun
	for _, u := range pending {
		if !u.Resumed || tip < u.Height+2 {
			continue
		}
		if budget <= 0 {
			break
		}
		read, err := s.scan(ctx, u, tip, budget)
		budget -= read
		if err != nil {
			errs = append(errs, fmt.Errorf("upgrade %d: %w", u.Height, err))
		}
	}
	return errors.Join(errs...)
}

// ResyncOne computes the upgrade planned at key (its height) again: what
// the commits gave is read again, the halt votes stay. It reads as far as
// the indexed tip allows in one go.
func (s *Set) ResyncOne(ctx context.Context, key string) error {
	height, err := strconv.ParseInt(key, 10, 64)
	if err != nil || height <= 0 {
		return fmt.Errorf("upgrade recovery: %q is not an upgrade height", key)
	}
	if err := s.deps.Store.Discover(ctx); err != nil {
		return err
	}
	u, ok, err := s.deps.Store.Upgrade(ctx, height)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("upgrade %d: no passed software upgrade planned at this height", height)
	}
	tip, ok, err := s.deps.Store.Tip(ctx)
	if err != nil {
		return err
	}
	if !ok || !u.Resumed || tip < u.Height+2 {
		return fmt.Errorf("upgrade %d: the chain has not resumed after it in the indexed blocks yet (blocks %d and %d)",
			height, height+1, height+2)
	}
	if err := s.deps.Store.Reset(ctx, height); err != nil {
		return err
	}
	u.HaltTime, u.ResumeTime, u.ScannedTo = nil, nil, 0
	if _, err := s.scan(ctx, u, tip, 0); err != nil {
		return fmt.Errorf("upgrade %d: %w", height, err)
	}
	return nil
}

// scan reads the commits of u from where the last run stopped, the heights
// of up to limit of them (0: no limit) and never above tip−1 (each height
// needs the time of the next). It saves what it read, the error included,
// and returns how many heights it read.
func (s *Set) scan(ctx context.Context, u Upgrade, tip, limit int64) (int64, error) {
	lg := s.deps.Log.WithFields(log.Fields{"upgrade": u.Height, "name": u.Name})
	members, err := s.members(ctx, u)
	if err != nil {
		return 0, err
	}
	isMember := make(map[string]bool, len(members))
	back := make(map[string]bool, len(members))
	for _, m := range members {
		isMember[m.ConsensusAddress] = true
		if m.Back {
			back[m.ConsensusAddress] = true
		}
	}

	halt := u.HaltHeight()
	end := u.Height + s.cfg.Window
	from := max(halt, u.ScannedTo+1)
	last := min(end, tip-1)
	if limit > 0 {
		last = min(last, from+limit-1)
	}
	sc := Scan{Height: u.Height, SetSize: len(members), ScannedTo: u.ScannedTo}

	var readErr error
	var next *node.Commit
	// U+1 is always read: it gives the times, even when every member is
	// back by its halt vote.
	for h := from; h <= last && (h == halt || len(back) < len(members)); h++ {
		cur := next
		if cur == nil {
			if cur, readErr = s.commit(ctx, h); readErr != nil {
				break
			}
		}
		if next, readErr = s.commit(ctx, h+1); readErr != nil {
			break
		}
		if h == halt {
			sc.HaltTime, sc.ResumeTime = &cur.Time, &next.Time
		}
		for _, sig := range cur.Signatures {
			cons := strings.ToUpper(sig.ValidatorAddress)
			if sig.BlockIDFlag != node.BlockIDFlagCommit || !isMember[cons] || back[cons] {
				continue
			}
			back[cons] = true
			sc.Backs = append(sc.Backs, CommitBack{
				ConsensusAddress: cons, BackTime: next.Time, VoteTime: sig.Timestamp,
				FirstSignedHeight: h, BlocksNeeded: h - halt,
			})
		}
		sc.ScannedTo = h
	}
	// The times come from the commits of U+1 and U+2: a recorded halt is
	// complete only once they were read.
	timed := sc.ScannedTo >= halt
	sc.Complete = readErr == nil && timed && (sc.ScannedTo >= end || len(back) == len(members))
	read := max(0, sc.ScannedTo-max(u.ScannedTo, halt-1))
	if err := s.deps.Store.SaveScan(ctx, sc); err != nil {
		return read, errors.Join(readErr, err)
	}
	if readErr != nil {
		return read, readErr
	}
	if sc.Complete {
		lg.WithFields(log.Fields{"members": len(members), "back": len(back), "scanned_to": sc.ScannedTo, "watched": u.Watched}).
			Info("upgrade recovery: computed")
	}
	return read, nil
}

// members returns the upgrade's members, reading the set at U+1 the first
// time.
func (s *Set) members(ctx context.Context, u Upgrade) ([]Member, error) {
	members, err := s.deps.Store.Members(ctx, u.Height)
	if err != nil || len(members) > 0 {
		return members, err
	}
	set, err := s.deps.Set.At(ctx, u.HaltHeight())
	if err != nil {
		return nil, err
	}
	if err := s.deps.Store.AddMembers(ctx, u.Height, set); err != nil {
		return nil, err
	}
	return s.deps.Store.Members(ctx, u.Height)
}

func (s *Set) commit(ctx context.Context, h int64) (*node.Commit, error) {
	if err := s.deps.Limiter.Wait(ctx); err != nil {
		return nil, err
	}
	c, _, err := s.deps.Node.Commit(ctx, h)
	return c, err
}
