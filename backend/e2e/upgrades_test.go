//go:build e2e

package e2e_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// The v8.1.0 upgrade, planned at 22551810: block 22551811's commit was
// signed on the new binary 12 minutes after the old one decided 22551810.
// Recorded: the set at 22551811, the blocks 22551811 and 22551812 and the
// commits 22551811..22551816.
const (
	upgradePlan     int64 = 22551810
	upgradeHalt     int64 = 22551811
	upgradeSetSize        = 23
	upgradeWindow         = "5" // 22551811..22551815, 22551816 for the last time
	upgradeLastRead int64 = 22551815
	// lateCons signed 22551812 first; neverCons are the six that signed
	// none of the window.
	lateCons = "2AD8E07AA7B80F9A5C7229AFE353F5C69F28AA67"
	// Seen by the watch in the synthetic halt: the set's index 1 and 16.
	watchedFirst  = "A4C8C1086A5A8C8C16689B91DF11B34B904F7329"
	watchedSecond = "213C087354635A43E10817601008CF683A82AA18"
)

var neverCons = []string{
	"18E08A311C5111C975C1E85D5B91AE167E79B1EF", "2217EC92A4E73BA79E86E90FA6C4180429EB7C41",
	"A1DB7908F8B108EC46E6C7B30C5AA2EE7E7F2B74", "A25583AAC5A99D5897C6DE4079E96A2F934E1993",
	"EFF58FAD4559319233C282F942C38A0BE8F50120", "FC6F1ADFA48A54E4A99F401DA96BDEDA1B34BE52",
}

// planUpgrades writes the passed v8.1.0 proposal (a MsgSoftwareUpgrade),
// the passed legacy v8.1.1 one (a SoftwareUpgradeProposal in a
// MsgExecLegacyContent) and a rejected one, and moves the live cursor to
// tip.
func (e *syncEnv) planUpgrades(t *testing.T, tip int64) {
	t.Helper()
	for _, p := range []struct {
		id       int64
		status   string
		messages string
	}{
		{40, "passed", `[{"@type":"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade","plan":{"name":"v8.1.0","height":"22551810","info":""}}]`},
		{41, "rejected", `[{"@type":"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade","plan":{"name":"v9","height":"30000000"}}]`},
		{42, "passed", `[{"@type":"/cosmos.gov.v1.MsgExecLegacyContent","content":{"@type":"/cosmos.upgrade.v1beta1.SoftwareUpgradeProposal",` +
			`"plan":{"name":"v8.1.1","height":"23855000"}}}]`},
	} {
		_, err := e.db.Exec(`INSERT INTO explorer.proposals (id, title, kind, status, submit_time, messages, updated_at)
			VALUES ($1, 'upgrade', 'software_upgrade', $2, now(), $3::jsonb, now())`, p.id, p.status, p.messages)
		require.NoError(t, err)
	}
	e.setCursor(t, tip)
}

func (e *syncEnv) setCursor(t *testing.T, tip int64) {
	t.Helper()
	_, err := e.db.Exec(`INSERT INTO explorer.indexer_state (key, value, updated_at) VALUES ('last_indexed_height', $1::bigint::text, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, tip)
	require.NoError(t, err)
}

// recoveryRows are the members of the upgrade as "cons source first needed
// seconds", "-" for what is unset, sorted.
func (e *syncEnv) recoveryRows(t *testing.T, where string) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', consensus_address, coalesce(back_source, '-'),
			coalesce(first_signed_height::text, '-'), coalesce(blocks_needed::text, '-'), coalesce(seconds_back::text, '-'))
		FROM explorer.upgrade_recovery WHERE upgrade_height = $1`+where, upgradePlan)
}

func TestUpgradeRecoveryOfAHistoricalUpgrade(t *testing.T) {
	e := newSyncEnv(t)
	t.Setenv("UPGRADE_RECOVERY_WINDOW", upgradeWindow)

	// The chain has not resumed in the indexed blocks: the upgrades are
	// found, nothing is read. (The validators first: they name the
	// members.)
	e.planUpgrades(t, upgradeHalt)
	code, out := e.syncState(t, "--job", "validators")
	require.Equal(t, 0, code, out)
	code, out = e.syncState(t, "--job", "upgrade_recovery")
	require.Equal(t, 0, code, out)
	assert.Equal(t, []string{"22551810 40 v8.1.0 pending", "23855000 42 v8.1.1 pending"},
		queryStrings(t, e.db, `SELECT concat_ws(' ', height, proposal_id, name, coalesce(computed_at::text, 'pending')) FROM explorer.upgrades`))
	assert.Zero(t, e.count(t, `SELECT count(*) FROM explorer.upgrade_recovery`))
	assert.Zero(t, e.node.RequestsAt(fakenode.RouteCommit, upgradeHalt))

	// The blocks of the halt and the resume indexed, the live cursor past
	// the window: every member is filled from the commits.
	e.index(t, upgradeHalt, upgradeHalt+1)
	e.setCursor(t, upgradeLastRead+1)
	e.node.ResetRequests()
	code, out = e.syncState(t, "--job", "upgrade_recovery")
	require.Equal(t, 0, code, out)
	assert.Equal(t, []string{"2026-04-28 15:52:31.683549+00 2026-04-28 16:04:39.802993+00 23 22551815 f computed"},
		queryStrings(t, e.db, `SELECT concat_ws(' ', halt_time, resume_time, active_set_size, window_end_height, watched,
			CASE WHEN computed_at IS NOT NULL THEN 'computed' END) FROM explorer.upgrades WHERE height = $1`, upgradePlan))
	assert.Equal(t, upgradeSetSize, e.count(t, `SELECT count(*) FROM explorer.upgrade_recovery WHERE upgrade_height = $1`, upgradePlan))
	assert.Equal(t, []string{"bzevaloper19fgph876c3rqxrn6xk5ch6wd73r3g05wzpsht0"}, queryStrings(t, e.db,
		`SELECT operator_address FROM explorer.upgrade_recovery WHERE consensus_address = 'A4C8C1086A5A8C8C16689B91DF11B34B904F7329'`),
		"named by the validators table")
	// Sixteen signed the first block after the halt: back at the resume,
	// 728 s after the halt.
	assert.Equal(t, 16, e.count(t, `SELECT count(*) FROM explorer.upgrade_recovery WHERE upgrade_height = $1
		AND back_source = 'commit' AND first_signed_height = $2 AND blocks_needed = 0 AND seconds_back = 728
		AND back_time = '2026-04-28T16:04:39.802993053Z'`, upgradePlan, upgradeHalt))
	assert.Equal(t, []string{lateCons + " commit 22551812 1 734"}, e.recoveryRows(t, ` AND consensus_address = '`+lateCons+`'`))
	assert.Equal(t, rowsOf(neverCons, " - - - -"), e.recoveryRows(t, ` AND back_time IS NULL`))
	// Each signature's own time is kept.
	assert.Equal(t, []string{"2026-04-28T16:04:45.776698Z"}, queryStrings(t, e.db,
		`SELECT vote_time FROM explorer.upgrade_recovery WHERE consensus_address = $1`, lateCons))
	for h := upgradeHalt; h <= upgradeLastRead+1; h++ {
		assert.Equal(t, 1, e.node.RequestsAt(fakenode.RouteCommit, h), "commit %d read once", h)
	}

	// The API: the upgrade with its members, back first, and the proposal's
	// summary.
	base := e.serveAPI(t)
	u := into[dto.Upgrade](t, fetch(t, base+"/upgrades/22551810"))
	assert.Equal(t, "v8.1.0", u.Name)
	assert.Equal(t, int64(40), u.ProposalID)
	require.NotNil(t, u.DowntimeSeconds)
	assert.Equal(t, int64(728), *u.DowntimeSeconds)
	require.Len(t, u.Members, upgradeSetSize)
	assert.Equal(t, lateCons, u.Members[16].ConsensusAddress, "after the sixteen back at the resume")
	for i, m := range u.Members {
		switch {
		case i < 16:
			assert.Equal(t, 728, *m.SecondsBack)
			if i > 0 {
				assert.GreaterOrEqual(t, u.Members[i-1].VotingPower, m.VotingPower, "then by power")
			}
		case i > 16:
			assert.Nil(t, m.BackTime)
			assert.Nil(t, m.BackSource)
		}
	}
	var pct float64
	for _, m := range u.Members {
		p, err := strconv.ParseFloat(m.PowerPct, 64)
		require.NoError(t, err)
		pct += p
	}
	assert.InDelta(t, 100, pct, 0.001)
	p := into[dto.Proposal](t, fetch(t, base+"/proposals/40"))
	require.NotNil(t, p.Upgrade)
	assert.Equal(t, upgradePlan, p.Upgrade.Height)
	assert.Equal(t, int64(728), *p.Upgrade.DowntimeSeconds)
	assert.False(t, p.Upgrade.Watched)
	assert.Equal(t, 404, fetch(t, base+"/upgrades/30000000").status, "a rejected upgrade is not one")

	// A forced recompute gives the same rows.
	before := e.recoveryRows(t, "")
	code, out = e.syncState(t, "--job", "upgrade_recovery", "--id", "22551810")
	require.Equal(t, 0, code, out)
	assert.Equal(t, before, e.recoveryRows(t, ""))
	code, _ = e.syncState(t, "--job", "upgrade_recovery", "--id", "23855000")
	assert.Equal(t, 1, code, "v8.1.1 has not resumed in the indexed blocks")
}

func TestHaltWatchDuringAnUpgrade(t *testing.T) {
	e := newSyncEnv(t)
	t.Setenv("UPGRADE_RECOVERY_WINDOW", upgradeWindow)
	e.planUpgrades(t, upgradePlan-1)
	code, out := e.syncState(t, "--job", "upgrade_recovery")
	require.Equal(t, 0, code, out)

	// The node stops at 22551809, its last block days old: halted.
	e.node.SetStatusHeight(upgradePlan - 1)
	e.node.SetConsensusState(fakenode.ConsensusStateBody(upgradeHalt, upgradeSetSize, map[int]string{
		1: fakenode.Vote(1, watchedFirst, upgradeHalt, time.Now()),
	}))
	stop := e.runWatch(t)
	require.Eventually(t, func() bool {
		return e.count(t, `SELECT count(*) FROM explorer.upgrade_recovery WHERE back_source = 'halt_vote'`) == 1
	}, 10*time.Second, 20*time.Millisecond)
	assert.Equal(t, upgradeSetSize, e.count(t, `SELECT count(*) FROM explorer.upgrade_recovery`), "the set at 22551811")
	assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM explorer.upgrades WHERE height = $1 AND watched`, upgradePlan))
	first := queryStrings(t, e.db, `SELECT back_time FROM explorer.upgrade_recovery WHERE consensus_address = $1`, watchedFirst)

	// Cosmovisor swaps the binary: the node does not answer for a while.
	e.node.GoDown(5)
	// Back on the new binary, block 22551810 replayed: another validator.
	e.node.SetStatusHeight(upgradePlan)
	e.node.SetConsensusState(fakenode.ConsensusStateBody(upgradeHalt, upgradeSetSize, map[int]string{
		1:  fakenode.Vote(1, watchedFirst, upgradeHalt, time.Now()),
		16: fakenode.Vote(16, watchedSecond, upgradeHalt, time.Now()),
	}))
	require.Eventually(t, func() bool {
		return e.count(t, `SELECT count(*) FROM explorer.upgrade_recovery WHERE back_source = 'halt_vote'`) == 2
	}, 10*time.Second, 20*time.Millisecond)

	// bze-scan restarts mid-halt: what was seen stays as seen.
	stop()
	stop = e.runWatch(t)
	e.waitPolls(t, fakenode.RouteConsensusState, 3)
	assert.Equal(t, first, queryStrings(t, e.db, `SELECT back_time FROM explorer.upgrade_recovery WHERE consensus_address = $1`, watchedFirst))

	// Block 22551811 decided: disarmed, nothing more is recorded.
	e.node.SetStatusHeight(upgradeHalt)
	e.node.SetConsensusState(fakenode.ConsensusStateBody(upgradeHalt, upgradeSetSize, map[int]string{
		0: fakenode.Vote(0, neverCons[1], upgradeHalt, time.Now()),
	}))
	e.waitPolls(t, fakenode.RouteStatus, 3)
	stop()
	assert.Equal(t, 2, e.count(t, `SELECT count(*) FROM explorer.upgrade_recovery WHERE back_source IS NOT NULL`))

	// The commits fill the others and never replace a halt vote, though
	// both watched validators signed 22551811.
	e.index(t, upgradeHalt, upgradeHalt+1)
	e.setCursor(t, upgradeLastRead+1)
	code, out = e.syncState(t, "--job", "upgrade_recovery")
	require.Equal(t, 0, code, out)
	assert.Equal(t, []string{watchedSecond, watchedFirst},
		queryStrings(t, e.db, `SELECT consensus_address FROM explorer.upgrade_recovery WHERE back_source = 'halt_vote'
			AND first_signed_height IS NULL AND seconds_back > 0`))
	assert.Equal(t, first, queryStrings(t, e.db, `SELECT back_time FROM explorer.upgrade_recovery WHERE consensus_address = $1`, watchedFirst))
	assert.Equal(t, 16-2, e.count(t, `SELECT count(*) FROM explorer.upgrade_recovery WHERE back_source = 'commit' AND first_signed_height = $1`, upgradeHalt))
	assert.Equal(t, rowsOf(neverCons, " - - - -"), e.recoveryRows(t, ` AND back_time IS NULL`))
	assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM explorer.upgrades WHERE height = $1 AND watched AND computed_at IS NOT NULL`, upgradePlan))
}

// runWatch runs the halt watch over the env's database and fake node,
// polling every 20 ms; the returned func stops it.
func (e *syncEnv) runWatch(t *testing.T) func() {
	t.Helper()
	cfg := &config.Config{DatabaseURL: e.url, NodeRPCURL: e.node.URL, HaltWatchEnabled: true, HaltWatchInterval: 20 * time.Millisecond}
	watch, closeFn, err := serve.NewHaltWatch(context.Background(), cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- watch.Run(ctx) }()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		assert.NoError(t, <-done)
		closeFn()
	}
	t.Cleanup(stop)
	return stop
}

// waitPolls waits until the watch called route n more times.
func (e *syncEnv) waitPolls(t *testing.T, route string, n int) {
	t.Helper()
	from := e.node.Requests(route)
	require.Eventually(t, func() bool { return e.node.Requests(route) >= from+n }, 10*time.Second, 5*time.Millisecond)
}

func rowsOf(cons []string, suffix string) []string {
	out := make([]string, len(cons))
	for i, c := range cons {
		out[i] = c + suffix
	}
	return out
}
