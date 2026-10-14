package upgrades_test

import (
	"context"
	"errors"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/upgrades"
)

type setEnv struct {
	store   *memStore
	members *fakeSet
	commits *fakeCommits
	limiter *countingLimiter
	set     *upgrades.Set
}

func newSetEnv(t *testing.T, tip int64, cfg upgrades.Config) *setEnv {
	t.Helper()
	logger, _ := logtest.NewNullLogger()
	e := &setEnv{
		store: newMemStore(tip), members: &fakeSet{members: set},
		commits: &fakeCommits{signed: synthetic}, limiter: &countingLimiter{},
	}
	if cfg.Window == 0 {
		cfg.Window = 10
	}
	e.set = upgrades.New(upgrades.Deps{
		Set: e.members, Node: e.commits, Limiter: e.limiter, Store: e.store, Log: logger,
	}, cfg)
	return e
}

// haltVotes records A and B as the halt watch would have.
func (e *setEnv) haltVotes(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, e.store.Discover(ctx))
	require.NoError(t, e.store.AddMembers(ctx, planHeight, set))
	require.NoError(t, e.store.HaltVotes(ctx, planHeight, []upgrades.HaltVote{
		{ConsensusAddress: consA, BackTime: base.Add(3 * time.Minute), VoteTime: base.Add(3*time.Minute - time.Second)},
		{ConsensusAddress: consB, BackTime: base.Add(7 * time.Minute), VoteTime: base.Add(7*time.Minute - time.Second)},
	}))
}

func TestSetIsTheUpgradeRecoverySet(t *testing.T) {
	e := newSetEnv(t, 0, upgrades.Config{})
	assert.Equal(t, statesync.UpgradeRecovery, e.set.Name())
	assert.Equal(t, upgrades.DefaultInterval, e.set.Interval())
}

func TestWatchedHaltIsCompletedFromTheCommits(t *testing.T) {
	// A and B were seen during the halt, C signed 105, D never: the window
	// (101..110) is read once the indexed tip is past it.
	e := newSetEnv(t, 111, upgrades.Config{})
	e.haltVotes(t)

	require.NoError(t, e.set.FullResync(context.Background()))
	assert.Equal(t, []string{
		"AAAA halt_vote 10:03:00 180s",
		"BBBB halt_vote 10:07:00 420s",
		"CCCC commit 10:10:30 105 4 630s",
		"DDDD -",
	}, e.store.recovery(planHeight))
	u, computed := e.store.upgrade(planHeight)
	assert.True(t, computed)
	assert.True(t, u.Watched)
	assert.Equal(t, blockTime(101), *u.HaltTime)
	assert.Equal(t, blockTime(102), *u.ResumeTime)
	assert.Equal(t, int64(110), u.ScannedTo)
	assert.Equal(t, 4, e.store.setSize[planHeight])
	assert.Empty(t, e.members.calls, "the members the watch wrote are kept")
	// 101..110 and the time of 111, each once, each paced.
	assert.Equal(t, []int64{101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111}, e.commits.reads)
	assert.Equal(t, 11, e.limiter.n)

	// Computed: later runs leave it alone.
	require.NoError(t, e.set.FullResync(context.Background()))
	assert.Len(t, e.commits.reads, 11)
}

func TestUnwatchedHistoricalUpgrade(t *testing.T) {
	// Nobody watched: A and B signed the first block after the halt, so
	// they are back at the resume; the set is read from the node.
	e := newSetEnv(t, 200, upgrades.Config{})

	require.NoError(t, e.set.FullResync(context.Background()))
	assert.Equal(t, []string{
		"AAAA commit 10:10:06 101 0 606s",
		"BBBB commit 10:10:06 101 0 606s",
		"CCCC commit 10:10:30 105 4 630s",
		"DDDD -",
	}, e.store.recovery(planHeight))
	u, computed := e.store.upgrade(planHeight)
	assert.True(t, computed)
	assert.False(t, u.Watched)
	assert.Equal(t, []int64{haltHeight}, e.members.calls)
}

func TestEveryMemberBackEndsTheScanEarly(t *testing.T) {
	e := newSetEnv(t, 20_000, upgrades.Config{Window: 10_000})
	e.commits.signed = func(h int64) []string {
		if h >= 103 {
			return []string{consA, consB, consC, consD}
		}
		return synthetic(h)
	}
	require.NoError(t, e.set.FullResync(context.Background()))
	_, computed := e.store.upgrade(planHeight)
	assert.True(t, computed)
	assert.Equal(t, []int64{101, 102, 103, 104}, e.commits.reads)
}

func TestEveryMemberBackDuringTheHaltStillReadsTheTimes(t *testing.T) {
	e := newSetEnv(t, 111, upgrades.Config{})
	e.haltVotes(t)
	require.NoError(t, e.store.HaltVotes(context.Background(), planHeight, []upgrades.HaltVote{
		{ConsensusAddress: consC, BackTime: base.Add(8 * time.Minute), VoteTime: base.Add(8 * time.Minute)},
		{ConsensusAddress: consD, BackTime: base.Add(9 * time.Minute), VoteTime: base.Add(9 * time.Minute)},
	}))
	require.NoError(t, e.set.FullResync(context.Background()))
	u, computed := e.store.upgrade(planHeight)
	assert.True(t, computed)
	assert.Equal(t, blockTime(101), *u.HaltTime)
	assert.Equal(t, blockTime(102), *u.ResumeTime)
	assert.Equal(t, []int64{101, 102}, e.commits.reads)
	assert.Contains(t, e.store.recovery(planHeight), "DDDD halt_vote 10:09:00 540s")
}

func TestWaitsForTheResume(t *testing.T) {
	// The indexed tip at 101: block 102, whose time is the resume, is not
	// indexed yet.
	e := newSetEnv(t, haltHeight, upgrades.Config{})
	require.NoError(t, e.set.FullResync(context.Background()))
	assert.Empty(t, e.commits.reads)
	assert.Empty(t, e.store.scans)
	assert.Empty(t, e.members.calls)
	_, ok := e.store.upgrade(planHeight)
	assert.False(t, ok)
}

func TestWaitsForTheResumeBlocksToBeIndexed(t *testing.T) {
	// The live cursor is far past, but the backfill has not reached the
	// upgrade: block 102 is missing.
	e := newSetEnv(t, 50_000, upgrades.Config{})
	e.store.gaps = map[int64]bool{102: true}
	require.NoError(t, e.set.FullResync(context.Background()))
	assert.Empty(t, e.commits.reads)
	assert.ErrorContains(t, e.set.ResyncOne(context.Background(), "100"), "blocks 101 and 102")

	e.store.gaps = nil
	require.NoError(t, e.set.FullResync(context.Background()))
	_, computed := e.store.upgrade(planHeight)
	assert.True(t, computed)
}

func TestOneBudgetForEveryUpgrade(t *testing.T) {
	// Two upgrades and 8 heights a run: the lower one takes the budget
	// first, the other starts once it is done.
	e := newSetEnv(t, 50_000, upgrades.Config{MaxPerRun: 8})
	e.store.planned = append(e.store.planned, upgrades.Upgrade{Height: 1000, ProposalID: 49, Name: "v10"})
	e.commits.signed = func(int64) []string { return []string{consA, consB} } // C and D never: the whole window is read
	ctx := context.Background()

	require.NoError(t, e.set.FullResync(ctx))
	u, _ := e.store.upgrade(planHeight)
	assert.Equal(t, int64(108), u.ScannedTo)
	assert.Empty(t, e.store.rows[1000], "the second upgrade waits")

	require.NoError(t, e.set.FullResync(ctx))
	u, computed := e.store.upgrade(planHeight)
	assert.True(t, computed)
	assert.Equal(t, int64(110), u.ScannedTo)
	second, _ := e.store.upgrade(1000)
	assert.Equal(t, int64(1006), second.ScannedTo, "the 6 heights left of the run")
}

func TestReadsUpToTheIndexedTipAndContinues(t *testing.T) {
	// MaxPerRun 3, and the tip moves: each run reads what it may and the
	// next continues where it stopped.
	e := newSetEnv(t, 104, upgrades.Config{MaxPerRun: 3})
	ctx := context.Background()

	require.NoError(t, e.set.FullResync(ctx))
	u, computed := e.store.upgrade(planHeight)
	assert.False(t, computed)
	assert.Equal(t, int64(103), u.ScannedTo, "each height needs the next one's time: 103 is the last below the tip")
	assert.Equal(t, []string{"AAAA commit 10:10:06 101 0 606s", "BBBB commit 10:10:06 101 0 606s", "CCCC -", "DDDD -"},
		e.store.recovery(planHeight))

	e.store.tip = 200
	require.NoError(t, e.set.FullResync(ctx))
	u, computed = e.store.upgrade(planHeight)
	assert.False(t, computed)
	assert.Equal(t, int64(106), u.ScannedTo)
	assert.NotNil(t, u.HaltTime, "the times of the first run stay")
	assert.Contains(t, e.store.recovery(planHeight), "CCCC commit 10:10:30 105 4 630s")

	require.NoError(t, e.set.FullResync(ctx))
	require.NoError(t, e.set.FullResync(ctx))
	u, computed = e.store.upgrade(planHeight)
	assert.True(t, computed)
	assert.Equal(t, int64(110), u.ScannedTo)
}

func TestAFailedReadKeepsWhatWasRead(t *testing.T) {
	e := newSetEnv(t, 200, upgrades.Config{})
	e.commits.failAt = map[int64]error{104: errDown}
	ctx := context.Background()

	err := e.set.FullResync(ctx)
	require.ErrorIs(t, err, errDown)
	assert.ErrorContains(t, err, "upgrade 100")
	u, computed := e.store.upgrade(planHeight)
	assert.False(t, computed)
	assert.Equal(t, int64(102), u.ScannedTo)
	assert.Equal(t, blockTime(101), *u.HaltTime)

	e.commits.failAt = nil
	e.commits.reads = nil
	require.NoError(t, e.set.FullResync(ctx))
	_, computed = e.store.upgrade(planHeight)
	assert.True(t, computed)
	assert.Equal(t, int64(103), e.commits.reads[0], "continues after the last height read")
}

func TestAFailedSetReadIsReturned(t *testing.T) {
	e := newSetEnv(t, 200, upgrades.Config{})
	e.members.err = errDown
	require.ErrorIs(t, e.set.FullResync(context.Background()), errDown)
	assert.Empty(t, e.commits.reads)
}

func TestAFailedSaveIsReturned(t *testing.T) {
	e := newSetEnv(t, 200, upgrades.Config{})
	e.store.failSave = errors.New("database gone")
	assert.ErrorContains(t, e.set.FullResync(context.Background()), "database gone")
}

func TestResyncOneRecomputesFromTheCommits(t *testing.T) {
	e := newSetEnv(t, 111, upgrades.Config{})
	e.haltVotes(t)
	ctx := context.Background()
	require.NoError(t, e.set.FullResync(ctx))
	before := e.store.recovery(planHeight)

	// C signed earlier than first recorded (a fixed archive): a forced
	// run reads again, the halt votes stay.
	e.commits.signed = func(h int64) []string {
		if h >= 103 {
			return []string{consA, consB, consC}
		}
		return synthetic(h)
	}
	require.NoError(t, e.set.ResyncOne(ctx, "100"))
	assert.Equal(t, []string{
		before[0], before[1],
		"CCCC commit 10:10:18 103 2 618s",
		"DDDD -",
	}, e.store.recovery(planHeight))
	u, computed := e.store.upgrade(planHeight)
	assert.True(t, computed)
	assert.True(t, u.Watched)
}

func TestResyncOneRefusals(t *testing.T) {
	ctx := context.Background()
	e := newSetEnv(t, haltHeight, upgrades.Config{})
	assert.ErrorContains(t, e.set.ResyncOne(ctx, "x"), "not an upgrade height")
	assert.ErrorContains(t, e.set.ResyncOne(ctx, "0"), "not an upgrade height")
	assert.ErrorContains(t, e.set.ResyncOne(ctx, "99"), "no passed software upgrade planned at this height")
	assert.ErrorContains(t, e.set.ResyncOne(ctx, "100"), "has not resumed")
	assert.Empty(t, e.commits.reads)
}

func TestLogsTheComputedUpgrade(t *testing.T) {
	logger, hook := logtest.NewNullLogger()
	store := newMemStore(111)
	set := upgrades.New(upgrades.Deps{
		Set: &fakeSet{members: set}, Node: &fakeCommits{signed: synthetic}, Limiter: &countingLimiter{}, Store: store, Log: logger,
	}, upgrades.Config{Window: 10})
	require.NoError(t, set.FullResync(context.Background()))
	require.NotNil(t, hook.LastEntry())
	assert.Equal(t, log.InfoLevel, hook.LastEntry().Level)
	assert.Equal(t, "upgrade recovery: computed", hook.LastEntry().Message)
	assert.Equal(t, 3, hook.LastEntry().Data["back"])
}
