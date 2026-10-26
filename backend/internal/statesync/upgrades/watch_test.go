package upgrades_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/upgrades"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// stepClock lets a test run the watch one poll at a time: every After
// hands the test a channel through waits, and the test fires it.
type stepClock struct {
	mu    sync.Mutex
	now   time.Time
	waits chan chan time.Time
}

func newStepClock(now time.Time) *stepClock {
	return &stepClock{now: now, waits: make(chan chan time.Time)}
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) After(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	c.waits <- ch
	return ch
}

// fakeWatchNode is the local node: the status and consensus state the
// test sets, or an error.
type fakeWatchNode struct {
	mu          sync.Mutex
	status      node.Status
	statusErr   error
	consensus   node.ConsensusState
	consErr     error
	consensusOK int // successful /consensus_state calls
}

func (n *fakeWatchNode) Status(context.Context) (*node.Status, []byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.statusErr != nil {
		return nil, nil, n.statusErr
	}
	st := n.status
	return &st, nil, nil
}

func (n *fakeWatchNode) ConsensusState(context.Context) (*node.ConsensusState, []byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.consErr != nil {
		return nil, nil, n.consErr
	}
	n.consensusOK++
	cs := n.consensus
	return &cs, nil, nil
}

// halted sets the node at height latest, whose block is ten minutes old.
func (n *fakeWatchNode) halted(latest int64, now time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.status = node.Status{LatestBlockHeight: latest, LatestBlockTime: now.Add(-10 * time.Minute)}
	n.statusErr = nil
}

// votes sets the consensus state at 101 with a prevote of each index, cast
// at the given times.
func (n *fakeWatchNode) votes(at map[int]time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.consErr = nil
	n.consensus = node.ConsensusState{Height: haltHeight}
	for i, t := range at {
		v, ok := node.ParseVote(fakenode.Vote(i, set[i].ConsensusAddress, haltHeight, t))
		if !ok {
			panic("bad vote")
		}
		n.consensus.Votes = append(n.consensus.Votes, v)
	}
}

type watchEnv struct {
	watch   *upgrades.Watch
	clock   *stepClock
	node    *fakeWatchNode
	members *fakeSet
	store   *memStore
	hook    *logtest.Hook
	cancel  context.CancelFunc
	done    chan error
	pending chan time.Time // the watch's next wait, once taken
}

// startWatch runs a watch over store; the first poll waits for step.
func startWatch(t *testing.T, store *memStore, n *fakeWatchNode) *watchEnv {
	t.Helper()
	logger, hook := logtest.NewNullLogger()
	logger.SetLevel(log.DebugLevel)
	e := &watchEnv{clock: newStepClock(base), node: n, members: &fakeSet{members: set}, store: store, hook: hook,
		done: make(chan error, 1)}
	w := upgrades.NewWatch(upgrades.WatchDeps{Node: n, Set: e.members, Store: store, Clock: e.clock, Log: logger},
		upgrades.WatchConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.watch = w
	go func() { e.done <- w.Run(ctx) }()
	t.Cleanup(func() { e.stop(t) })
	return e
}

// step moves the clock to at and runs one poll to its end: the watch's
// next wait.
func (e *watchEnv) step(t *testing.T, at time.Time) {
	t.Helper()
	tick := e.pending
	if tick == nil {
		tick = <-e.clock.waits
	}
	e.clock.mu.Lock()
	e.clock.now = at
	e.clock.mu.Unlock()
	tick <- at
	e.pending = <-e.clock.waits
}

func (e *watchEnv) stop(t *testing.T) {
	t.Helper()
	if e.cancel == nil {
		return
	}
	e.cancel()
	e.cancel = nil
	select {
	case err := <-e.done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the watch did not stop")
	}
}

func pendingStore() *memStore {
	s := newMemStore(99)
	_ = s.Discover(context.Background())
	return s
}

func TestWatchRecordsTheValidatorsBackDuringTheHalt(t *testing.T) {
	store := pendingStore()
	n := &fakeWatchNode{}
	e := startWatch(t, store, n)

	// 10:01 the node is at 99, its block ten minutes old: armed. A votes.
	n.halted(99, base.Add(time.Minute))
	n.votes(map[int]time.Time{0: base.Add(55 * time.Second)})
	e.step(t, base.Add(time.Minute))
	assert.Equal(t, []int64{haltHeight}, e.members.calls)
	assert.Equal(t, []string{"AAAA halt_vote 10:01:00", "BBBB -", "CCCC -", "DDDD -"}, store.recovery(planHeight))
	u, _ := store.upgrade(planHeight)
	assert.True(t, u.Watched)

	// 10:02 A again (a later round) and B.
	n.votes(map[int]time.Time{0: base.Add(110 * time.Second), 1: base.Add(118 * time.Second)})
	e.step(t, base.Add(2*time.Minute))
	assert.Equal(t, []string{"AAAA halt_vote 10:01:00", "BBBB halt_vote 10:02:00", "CCCC -", "DDDD -"}, store.recovery(planHeight))
	assert.Equal(t, base.Add(55*time.Second), *store.rows[planHeight][consA].voteTime, "the first vote's own time")

	// Cosmovisor swaps the binary: nothing answers for two polls.
	n.mu.Lock()
	n.statusErr = errDown
	n.mu.Unlock()
	e.step(t, base.Add(3*time.Minute))
	n.halted(99, base.Add(4*time.Minute))
	n.mu.Lock()
	n.consErr = errDown
	n.mu.Unlock()
	e.step(t, base.Add(4*time.Minute))

	// Back on the new binary, block 100 replayed: still armed. C votes.
	n.halted(planHeight, base.Add(5*time.Minute))
	n.votes(map[int]time.Time{0: base.Add(290 * time.Second), 1: base.Add(291 * time.Second), 2: base.Add(292 * time.Second)})
	e.step(t, base.Add(5*time.Minute))
	assert.Equal(t, []string{"AAAA halt_vote 10:01:00", "BBBB halt_vote 10:02:00", "CCCC halt_vote 10:05:00", "DDDD -"},
		store.recovery(planHeight))

	// Two thirds back: block 101 is decided, the watch disarms and
	// records nothing more.
	n.halted(haltHeight, base.Add(6*time.Minute))
	n.votes(map[int]time.Time{3: base.Add(359 * time.Second)})
	calls := n.consensusOK
	e.step(t, base.Add(6*time.Minute))
	assert.Equal(t, calls, n.consensusOK)
	assert.Contains(t, store.recovery(planHeight), "DDDD -")

	e.stop(t)
	for _, entry := range e.hook.AllEntries() {
		if entry.Level <= log.WarnLevel {
			t.Errorf("unexpected %s: %s", entry.Level, entry.Message)
		}
	}
	assert.Equal(t, []int64{haltHeight}, e.members.calls, "the set is read once")
}

func TestWatchRestartKeepsTheRecordedVotes(t *testing.T) {
	store := pendingStore()
	n := &fakeWatchNode{}
	n.halted(99, base.Add(time.Minute))
	n.votes(map[int]time.Time{0: base.Add(55 * time.Second)})
	e := startWatch(t, store, n)
	e.step(t, base.Add(time.Minute))
	e.stop(t)

	// bze-scan restarts in the middle of the halt: A is seen again, B for
	// the first time.
	n.votes(map[int]time.Time{0: base.Add(170 * time.Second), 1: base.Add(175 * time.Second)})
	e = startWatch(t, store, n)
	e.step(t, base.Add(3*time.Minute))
	assert.Equal(t, []string{"AAAA halt_vote 10:01:00", "BBBB halt_vote 10:03:00", "CCCC -", "DDDD -"}, store.recovery(planHeight))
}

func TestWatchIsNotArmedWhileBlocksComeIn(t *testing.T) {
	store := pendingStore()
	n := &fakeWatchNode{}
	n.status = node.Status{LatestBlockHeight: 99, LatestBlockTime: base.Add(-5 * time.Second)}
	n.votes(map[int]time.Time{0: base})
	e := startWatch(t, store, n)
	e.step(t, base)

	// Halted, but the node is catching up: not the halt's votes.
	n.status = node.Status{LatestBlockHeight: 99, LatestBlockTime: base.Add(-time.Hour), CatchingUp: true}
	e.step(t, base)

	// Far from any upgrade.
	n.halted(50, base)
	e.step(t, base)
	assert.Empty(t, e.members.calls)
	assert.Zero(t, n.consensusOK)
	u, _ := store.upgrade(planHeight)
	assert.False(t, u.Watched)
}

func TestWatchIgnoresVotesOfAnotherHeightOrSet(t *testing.T) {
	store := pendingStore()
	n := &fakeWatchNode{}
	n.halted(99, base)
	n.consensus = node.ConsensusState{Height: planHeight} // the node still deciding 100
	e := startWatch(t, store, n)
	e.step(t, base)
	assert.Equal(t, []string{"AAAA -", "BBBB -", "CCCC -", "DDDD -"}, store.recovery(planHeight))
	u, _ := store.upgrade(planHeight)
	assert.False(t, u.Watched)

	// Index 1 printed with D's address, index 9 outside the set, a vote
	// at another height, and an entry that is not a vote: only A counts.
	vA, _ := node.ParseVote(fakenode.Vote(0, consA, haltHeight, base))
	vWrong, _ := node.ParseVote(fakenode.Vote(1, consD, haltHeight, base))
	vOut, _ := node.ParseVote(fakenode.Vote(9, consB, haltHeight, base))
	vOld, _ := node.ParseVote(fakenode.Vote(2, consC, planHeight, base))
	n.consensus = node.ConsensusState{Height: haltHeight, Votes: []node.RoundVote{vA, vWrong, vOut, vOld}, Unparsed: []string{"???"}}
	e.step(t, base.Add(time.Minute))
	e.step(t, base.Add(2*time.Minute))
	assert.Equal(t, []string{"AAAA halt_vote 10:01:00", "BBBB -", "CCCC -", "DDDD -"}, store.recovery(planHeight))

	e.stop(t)
	warnings := 0
	for _, entry := range e.hook.AllEntries() {
		if entry.Level == log.WarnLevel {
			warnings++
		}
	}
	assert.Equal(t, 2, warnings, "the unparsed entry and the mismatch are each logged once")
}

func TestWatchKeepsPollingWhenTheSetCannotBeRead(t *testing.T) {
	store := pendingStore()
	n := &fakeWatchNode{}
	n.halted(99, base)
	n.votes(map[int]time.Time{0: base})
	e := startWatch(t, store, n)
	e.members.err = errors.New("validators: rpc error")
	e.step(t, base)
	assert.Zero(t, n.consensusOK)

	e.members.mu.Lock()
	e.members.err = nil
	e.members.mu.Unlock()
	e.step(t, base.Add(5*time.Second))
	assert.Equal(t, []string{"AAAA halt_vote 10:00:05", "BBBB -", "CCCC -", "DDDD -"}, store.recovery(planHeight))
}

func TestWatchLiveShowsTheVotesOfTheCurrentRound(t *testing.T) {
	store := pendingStore()
	n := &fakeWatchNode{}
	e := startWatch(t, store, n)

	// Blocks come in: no halt to show.
	n.status = node.Status{LatestBlockHeight: 99, LatestBlockTime: base.Add(-5 * time.Second)}
	n.votes(map[int]time.Time{0: base})
	e.step(t, base)
	_, ok := e.watch.Live()
	assert.False(t, ok)

	// Halted at round 1: A prevoted and precommitted in round 1, B only
	// voted in round 0, C and D not at all.
	n.halted(99, base.Add(time.Minute))
	vote := func(index int, cons string, round int32, typ node.VoteType) node.RoundVote {
		return node.RoundVote{ValidatorIndex: index, AddressPrefix: cons[:12], Height: haltHeight, Round: round, Type: typ,
			Timestamp: base}
	}
	n.mu.Lock()
	n.consensus = node.ConsensusState{Height: haltHeight, Round: 1, Votes: []node.RoundVote{
		vote(0, consA, 1, node.Prevote), vote(0, consA, 1, node.Precommit), vote(1, consB, 0, node.Prevote),
	}}
	n.mu.Unlock()
	e.step(t, base.Add(time.Minute))

	l, ok := e.watch.Live()
	require.True(t, ok)
	assert.Equal(t, planHeight, l.Height)
	assert.Equal(t, int32(1), l.Round)
	assert.Equal(t, base.Add(time.Minute), l.CheckedAt)
	assert.Equal(t, base.Add(-9*time.Minute), l.HaltedSince, "the time of the node's latest block")
	got := make([]string, len(l.Members))
	for i, m := range l.Members {
		got[i] = fmt.Sprintf("%s back=%t prevote=%t precommit=%t", m.ConsensusAddress[:4], m.Back, m.Prevote, m.Precommit)
	}
	assert.Equal(t, []string{
		"AAAA back=true prevote=true precommit=true",
		"BBBB back=true prevote=false precommit=false",
		"CCCC back=false prevote=false precommit=false",
		"DDDD back=false prevote=false precommit=false",
	}, got)

	// Block 101 decided: the halt is over, nothing to show.
	n.halted(haltHeight, base.Add(2*time.Minute))
	e.step(t, base.Add(2*time.Minute))
	_, ok = e.watch.Live()
	assert.False(t, ok)
}
