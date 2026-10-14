package upgrades

import (
	"context"
	"errors"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/activeset"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// Defaults of the watch.
const (
	// DefaultWatchInterval is how often the watch polls.
	DefaultWatchInterval = 5 * time.Second
	// DefaultHaltedAfter is how old the node's latest block must be for
	// the chain to count as halted.
	DefaultHaltedAfter = 30 * time.Second
)

// WatchNode is the local node; *node.Client satisfies it.
type WatchNode interface {
	Status(ctx context.Context) (*node.Status, []byte, error)
	ConsensusState(ctx context.Context) (*node.ConsensusState, []byte, error)
}

// WatchStore is the part of the Store the watch uses.
type WatchStore interface {
	Pending(ctx context.Context) ([]Upgrade, error)
	AddMembers(ctx context.Context, height int64, set []activeset.Member) error
	HaltVotes(ctx context.Context, height int64, votes []HaltVote) error
}

// WatchDeps of the watch.
type WatchDeps struct {
	Node WatchNode
	// Set reads the validator set at U+1 from the local node.
	Set   activeset.Reader
	Store WatchStore
	// Clock defaults to the wall clock.
	Clock statesync.Clock
	// Log defaults to the standard logger.
	Log log.FieldLogger
}

// WatchConfig of the watch; zero values take the defaults.
type WatchConfig struct {
	Interval    time.Duration
	HaltedAfter time.Duration
}

// Watch polls the local node during an upgrade's halt and records the
// validators that vote at U+1, the first poll that sees each.
//
// It is armed for a pending upgrade at U when the node's latest block is
// U−1 or U (the node has not replayed block U on the new binary yet, or
// has) and older than HaltedAfter, and disarmed once the node is past U.
// While the node is unavailable (Cosmovisor swapping the binaries) it keeps
// polling at the same pace and records nothing. It never fails: Run returns
// when ctx is done.
type Watch struct {
	deps WatchDeps
	cfg  WatchConfig

	armed   int64 // the plan height armed for, 0 when not
	members []activeset.Member
	seen    map[string]bool
	marked  bool // the armed upgrade is marked watched
	warned  map[string]bool
}

// NewWatch returns the watch.
func NewWatch(deps WatchDeps, cfg WatchConfig) *Watch {
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultWatchInterval
	}
	if cfg.HaltedAfter <= 0 {
		cfg.HaltedAfter = DefaultHaltedAfter
	}
	if deps.Clock == nil {
		deps.Clock = wallClock{}
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	return &Watch{deps: deps, cfg: cfg, warned: map[string]bool{}}
}

type wallClock struct{}

func (wallClock) Now() time.Time                         { return time.Now() }
func (wallClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Run polls every Interval until ctx is done.
func (w *Watch) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.deps.Clock.After(w.cfg.Interval):
		}
		w.poll(ctx)
	}
}

// poll is one tick: arm, disarm, or record the votes a poll sees.
func (w *Watch) poll(ctx context.Context) {
	pending, err := w.deps.Store.Pending(ctx)
	if err != nil {
		w.warnOnce("store", err, "halt watch: reading the pending upgrades")
		return
	}
	st, _, err := w.deps.Node.Status(ctx)
	if err != nil {
		w.nodeError("status", err)
		return
	}
	var u *Upgrade
	for i := range pending {
		if st.LatestBlockHeight == pending[i].Height-1 || st.LatestBlockHeight == pending[i].Height {
			u = &pending[i]
			break
		}
	}
	if u == nil || (u.Height != w.armed && (st.CatchingUp || w.deps.Clock.Now().Sub(st.LatestBlockTime) < w.cfg.HaltedAfter)) {
		if w.armed != 0 {
			w.deps.Log.WithField("upgrade", w.armed).Info("halt watch: disarmed")
			w.armed, w.members, w.seen, w.marked = 0, nil, nil, false
		}
		return
	}
	lg := w.deps.Log.WithFields(log.Fields{"upgrade": u.Height, "name": u.Name})
	if w.armed != u.Height {
		w.armed, w.members, w.seen, w.marked = u.Height, nil, map[string]bool{}, false
		lg.Info("halt watch: armed, the chain is halted for the upgrade")
	}
	if w.members == nil {
		set, err := w.deps.Set.At(ctx, u.HaltHeight())
		if err != nil {
			w.nodeError("validators", err)
			return
		}
		if err := w.deps.Store.AddMembers(ctx, u.Height, set); err != nil {
			w.warnOnce("store", err, "halt watch: writing the members")
			return
		}
		w.members = set
	}
	cs, _, err := w.deps.Node.ConsensusState(ctx)
	if err != nil {
		w.nodeError("consensus_state", err)
		return
	}
	at := w.deps.Clock.Now()
	if len(cs.Unparsed) > 0 && !w.warned["unparsed"] {
		w.warned["unparsed"] = true
		lg.WithField("example", cs.Unparsed[0]).Warn("halt watch: /consensus_state entries that are not votes, skipped (logged once)")
	}
	if cs.Height != u.HaltHeight() {
		return
	}
	earliest := map[int]node.RoundVote{}
	for _, v := range cs.Votes {
		if v.Height != cs.Height {
			continue
		}
		if e, ok := earliest[v.ValidatorIndex]; !ok || v.Timestamp.Before(e.Timestamp) {
			earliest[v.ValidatorIndex] = v
		}
	}
	var votes []HaltVote
	for index, v := range earliest {
		if index < 0 || index >= len(w.members) || !hasPrefix(w.members[index].ConsensusAddress, v.AddressPrefix) {
			if !w.warned["index"] {
				w.warned["index"] = true
				lg.WithFields(log.Fields{"index": index, "address_prefix": v.AddressPrefix}).
					Warn("halt watch: a vote does not match the set at U+1, skipped (logged once)")
			}
			continue
		}
		cons := w.members[index].ConsensusAddress
		if w.seen[cons] {
			continue
		}
		votes = append(votes, HaltVote{ConsensusAddress: cons, BackTime: at, VoteTime: v.Timestamp})
	}
	if len(votes) == 0 && w.marked {
		return
	}
	// Written at once, so a restart in the middle of the halt keeps them;
	// the first poll marks the upgrade watched, votes or not.
	if err := w.deps.Store.HaltVotes(ctx, u.Height, votes); err != nil {
		w.warnOnce("store", err, "halt watch: writing the votes")
		return
	}
	w.marked = true
	for _, v := range votes {
		w.seen[v.ConsensusAddress] = true
	}
	if len(votes) > 0 {
		lg.WithFields(log.Fields{"new": len(votes), "back": len(w.seen), "members": len(w.members)}).Info("halt watch: validators back")
	}
}

// hasPrefix reports whether the consensus address starts with the
// printed prefix (CometBFT prints the first six bytes).
func hasPrefix(cons, prefix string) bool {
	return prefix != "" && len(prefix) <= len(cons) && cons[:len(prefix)] == prefix
}

// nodeError logs a failed node call: quietly while the node is
// unavailable (the binary swap), once otherwise.
func (w *Watch) nodeError(route string, err error) {
	if errors.Is(err, node.ErrUnavailable) {
		w.deps.Log.WithError(err).Debug("halt watch: node unavailable, polling on")
		return
	}
	w.warnOnce(route, err, "halt watch: node call failed, polling on")
}

func (w *Watch) warnOnce(key string, err error, msg string) {
	if w.warned[key] {
		w.deps.Log.WithError(err).Debug(msg)
		return
	}
	w.warned[key] = true
	w.deps.Log.WithError(err).Warn(msg + " (logged once)")
}
