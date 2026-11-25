//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/upgrades"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// The synthetic emergency halt: scheduled in block 22551809 for 22551810,
// the v8.1.0 plan height, so the recorded set at 22551811 is the halt's.
const (
	emergencyHeight int64 = upgradePlan
	emergencyReason       = "pool drain under way"
	emergencySigner       = "bze1guardian"
)

// The migration keeps the governance upgrades as they were, and its down
// forgets the emergency halts.
func TestMigrateEmergencyHaltsKeepsTheGovernanceUpgrades(t *testing.T) {
	dbURL := freshDatabase(t, true)
	db := connect(t, dbURL)
	migrateUp(t, dbURL)
	_, err := openMigrator(t, dbURL).Down(latestVersion - 31) // back to before 000032
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO explorer.upgrades (height, proposal_id, name, computed_at) VALUES (22551810, 40, 'v8.1.0', now())`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO explorer.upgrade_recovery (upgrade_height, consensus_address, voting_power) VALUES (22551810, 'AA', 1)`)
	require.NoError(t, err)
	migrateUp(t, dbURL)
	assert.Equal(t, []string{"22551810 40 v8.1.0 governance - computed"}, queryStrings(t, db,
		`SELECT concat_ws(' ', height, proposal_id, name, kind, coalesce(superseded_by_height::text, '-'),
			CASE WHEN computed_at IS NOT NULL THEN 'computed' END) FROM explorer.upgrades`))

	_, err = db.Exec(`INSERT INTO explorer.upgrades (height, name, kind) VALUES (30000001, 'emergency-halt-30000001', 'emergency')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO explorer.upgrade_recovery (upgrade_height, consensus_address, voting_power) VALUES (30000001, 'AA', 1)`)
	require.NoError(t, err)
	// An emergency halt that took over a governance row keeps its proposal.
	_, err = db.Exec(`INSERT INTO explorer.upgrades (height, proposal_id, name, kind) VALUES (30000002, 41, 'emergency-halt-30000002', 'emergency')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO explorer.upgrade_recovery (upgrade_height, consensus_address, voting_power) VALUES (30000002, 'AA', 1)`)
	require.NoError(t, err)
	_, err = openMigrator(t, dbURL).Down(latestVersion - 31) // back to before 000032
	require.NoError(t, err)
	assert.Equal(t, []string{"22551810 40", "30000002 41"}, queryStrings(t, db, `SELECT concat_ws(' ', height, proposal_id) FROM explorer.upgrades`))
	assert.Equal(t, []string{"22551810", "30000002"}, queryStrings(t, db, `SELECT upgrade_height::text FROM explorer.upgrade_recovery`))
}

// indexSynthetic indexes block h with the finalize events.
func indexSynthetic(t *testing.T, pool *pgxpool.Pool, h int64, events ...node.Event) {
	t.Helper()
	in := transform.Input{
		Block: &node.Block{Height: h, Time: time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC), Hash: fmt.Sprintf("%064X", h),
			ProposerAddress: "AA", Raw: json.RawMessage(`{}`)},
		Results: &node.BlockResults{Height: h, FinalizeBlockEvents: events},
		Commit:  &node.Commit{Height: h},
	}
	ents, err := newTransformer(t).Transform(in)
	require.NoError(t, err)
	require.NoError(t, writer.NewLiveWriter(pool).WriteBlock(context.Background(), ents))
}

// scheduleHalt indexes block h with a block-level ChainHaltScheduledEvent
// (governance ran MsgHaltChain) for h+1.
func scheduleHalt(t *testing.T, pool *pgxpool.Pool, h int64) {
	t.Helper()
	str := strconv.Quote
	indexSynthetic(t, pool, h, node.Event{Type: "bze.guardian.v1.ChainHaltScheduledEvent", Attributes: []node.Attribute{
		{Key: "signer", Value: str(emergencySigner)},
		{Key: "name", Value: str("emergency-halt-" + strconv.FormatInt(h+1, 10))},
		{Key: "height", Value: str(strconv.FormatInt(h+1, 10))},
		{Key: "reason", Value: str(emergencyReason)},
	}})
}

func (e *syncEnv) passProposal(t *testing.T, id int64, planHeight int64, resolved *int64) {
	t.Helper()
	msgs := `[{"@type":"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade","plan":{"name":"v` + strconv.FormatInt(id, 10) +
		`","height":"` + strconv.FormatInt(planHeight, 10) + `"}}]`
	_, err := e.db.Exec(`INSERT INTO explorer.proposals (id, title, kind, status, submit_time, messages, updated_at, resolved_height)
		VALUES ($1, 'upgrade', 'software_upgrade', 'passed', now(), $2::jsonb, now(), $3)`, id, msgs, resolved)
	require.NoError(t, err)
}

func TestEmergencyHaltIsWatchedAndSupersedesTheGovernancePlan(t *testing.T) {
	e := newSyncEnv(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := upgrades.NewPGStore(pool)

	// Passed before the halt: 22000000 (below it, halted for already) and
	// 30000000 (the plan the halt replaced); 31000000's execution height
	// is not known yet.
	before := emergencyHeight - 500
	e.passProposal(t, 50, 22_000_000, &before)
	e.passProposal(t, 51, 30_000_000, &before)
	e.passProposal(t, 52, 31_000_000, nil)
	require.NoError(t, store.Discover(ctx))

	scheduleHalt(t, pool, emergencyHeight-1)
	rows := func() []string {
		return queryStrings(t, e.db, `SELECT concat_ws(' ', height, coalesce(proposal_id::text, '-'), name, kind,
			coalesce(reason, '-'), coalesce(scheduled_by, '-'), coalesce(scheduled_tx_hash, '-'),
			coalesce(superseded_by_height::text, '-')) FROM explorer.upgrades`)
	}
	want := []string{
		"22000000 50 v50 governance - - - -",
		"22551810 - emergency-halt-22551810 emergency " + emergencyReason + " " + emergencySigner + " - -",
		"30000000 51 v51 governance - - - 22551810",
		"31000000 52 v52 governance - - - -",
	}
	assert.Equal(t, want, rows())
	require.NoError(t, store.Discover(ctx))
	assert.Equal(t, want, rows(), "Discover keeps the marks")

	pending, err := store.Pending(ctx)
	require.NoError(t, err)
	var heights []int64
	for _, u := range pending {
		heights = append(heights, u.Height)
	}
	assert.Equal(t, []int64{22_000_000, emergencyHeight, 31_000_000}, heights, "the superseded plan is not pending")
	assert.Equal(t, upgrades.KindEmergency, pending[1].Kind)
	assert.Nil(t, pending[1].ProposalID)
	assert.Equal(t, emergencyReason, pending[1].Reason)

	// The halt watch arms for it as for a governance upgrade.
	e.node.SetStatusHeight(emergencyHeight - 1)
	e.node.SetConsensusState(fakenode.ConsensusStateBody(upgradeHalt, upgradeSetSize, map[int]string{
		1: fakenode.Vote(1, watchedFirst, upgradeHalt, time.Now()),
	}))
	cfg := &config.Config{DatabaseURL: e.url, NodeRPCURL: e.node.URL, ChainID: "beezee-1", HaltWatchEnabled: true, HaltWatchInterval: 20 * time.Millisecond}
	watch, closeWatch, err := serve.NewHaltWatch(ctx, cfg)
	require.NoError(t, err)
	wctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- watch.Run(wctx) }()
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-done)
		closeWatch()
	})
	require.Eventually(t, func() bool {
		return e.count(t, `SELECT count(*) FROM explorer.upgrade_recovery WHERE upgrade_height = $1 AND back_source = 'halt_vote'`, emergencyHeight) == 1
	}, 10*time.Second, 20*time.Millisecond)

	// GET /upgrades/live over the watch.
	ec := echo.New()
	ec.GET("/upgrades/live", controller.NewHaltController(watch, nil).Live)
	rec := httptest.NewRecorder()
	ec.ServeHTTP(rec, httptest.NewRequest("GET", "/upgrades/live", nil))
	live := into[dto.LiveHalt](t, response{status: rec.Code, body: rec.Body.Bytes()})
	assert.True(t, live.Halted)
	assert.Equal(t, emergencyHeight, live.Height)
	assert.Equal(t, "emergency", live.Kind)
	assert.Nil(t, live.ProposalID)
	require.NotNil(t, live.Reason)
	assert.Equal(t, emergencyReason, *live.Reason)
	require.NotNil(t, live.ScheduledBy)
	assert.Equal(t, emergencySigner, *live.ScheduledBy)

	// GET /upgrades/{h} for both kinds, and the block of the halt.
	base := e.serveAPI(t)
	em := into[dto.Upgrade](t, fetch(t, base+"/upgrades/22551810"))
	assert.Equal(t, "emergency", em.Kind)
	assert.Nil(t, em.ProposalID)
	require.NotNil(t, em.Reason)
	assert.Equal(t, emergencyReason, *em.Reason)
	assert.Nil(t, em.ScheduledTxHash, "governance ran it at block level")
	assert.Len(t, em.Members, upgradeSetSize)
	gov := into[dto.Upgrade](t, fetch(t, base+"/upgrades/30000000"))
	assert.Equal(t, "governance", gov.Kind)
	require.NotNil(t, gov.ProposalID)
	assert.Equal(t, int64(51), *gov.ProposalID)
	require.NotNil(t, gov.SupersededByHeight)
	assert.Equal(t, emergencyHeight, *gov.SupersededByHeight)

	indexSynthetic(t, pool, emergencyHeight)
	b := into[dto.Block](t, fetch(t, base+"/blocks/22551810"))
	require.NotNil(t, b.Upgrade)
	assert.Equal(t, "emergency", b.Upgrade.Kind)
	require.NotNil(t, b.Upgrade.Reason)
	assert.Equal(t, emergencyReason, *b.Upgrade.Reason)

	// The block of a governance upgrade names it; the height of a superseded
	// plan names none.
	indexSynthetic(t, pool, 22_000_000)
	indexSynthetic(t, pool, 30_000_000)
	gb := into[dto.Block](t, fetch(t, base+"/blocks/22000000"))
	require.NotNil(t, gb.Upgrade)
	assert.Equal(t, "governance", gb.Upgrade.Kind)
	require.NotNil(t, gb.Upgrade.ProposalID)
	assert.Equal(t, int64(50), *gb.Upgrade.ProposalID)
	assert.Nil(t, into[dto.Block](t, fetch(t, base+"/blocks/30000000")).Upgrade)

	// A decided emergency halt is no upgrade of the Hall of Fame rule.
	_, err = e.db.Exec(`UPDATE explorer.upgrades SET computed_at = now() WHERE height = $1`, emergencyHeight)
	require.NoError(t, err)
	hof := into[dto.HallOfFame](t, fetch(t, base+"/validators/hall-of-fame"))
	assert.Nil(t, hof.Rules.Upgrade)

	// A later proposal plans 30000000 again: it is watched again.
	after := emergencyHeight + 100
	e.passProposal(t, 53, 30_000_000, &after)
	require.NoError(t, store.Discover(ctx))
	assert.Equal(t, []string{"53 -"}, queryStrings(t, e.db, `SELECT concat_ws(' ', proposal_id, coalesce(superseded_by_height::text, '-'))
		FROM explorer.upgrades WHERE height = 30000000`))
}

// The supersede rule's edges: a proposal executed in the halt's own
// scheduling block (H-1) planned after the halt and is kept; one executed at
// H-2 was replaced; a computed upgrade is never marked.
func TestSupersedeBoundaries(t *testing.T) {
	e := newSyncEnv(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := upgrades.NewPGStore(pool)

	sameBlock, before := emergencyHeight-1, emergencyHeight-2
	e.passProposal(t, 60, 33_000_000, &sameBlock)
	e.passProposal(t, 61, 34_000_000, &before)
	e.passProposal(t, 62, 35_000_000, &before)
	require.NoError(t, store.Discover(ctx))
	_, err = e.db.Exec(`UPDATE explorer.upgrades SET computed_at = now() WHERE height = 35000000`)
	require.NoError(t, err)

	scheduleHalt(t, pool, emergencyHeight-1)
	require.NoError(t, store.Discover(ctx))
	assert.Equal(t, []string{"33000000 -", "34000000 22551810", "35000000 -"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', height, coalesce(superseded_by_height::text, '-')) FROM explorer.upgrades WHERE kind = 'governance'`))
}

// What Discover never overwrites: an emergency halt (written over a
// governance row, it keeps that proposal), a computed upgrade, a row of a
// higher proposal.
func TestDiscoverKeepsEmergencyComputedAndNewerRows(t *testing.T) {
	e := newSyncEnv(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := upgrades.NewPGStore(pool)

	resolved := emergencyHeight - 500
	e.passProposal(t, 70, emergencyHeight, &resolved)
	e.passProposal(t, 71, 36_000_000, &resolved)
	require.NoError(t, store.Discover(ctx))
	_, err = e.db.Exec(`UPDATE explorer.upgrades SET computed_at = now() WHERE height = 36000000`)
	require.NoError(t, err)
	_, err = e.db.Exec(`INSERT INTO explorer.upgrades (height, proposal_id, name) VALUES (37000000, 90, 'v90')`)
	require.NoError(t, err)

	// The emergency halt lands at the governance plan's height: it takes
	// the row over and keeps the proposal.
	scheduleHalt(t, pool, emergencyHeight-1)
	rows := func() []string {
		return queryStrings(t, e.db, `SELECT concat_ws(' ', height, proposal_id, name, kind) FROM explorer.upgrades`)
	}
	want := []string{
		"22551810 70 emergency-halt-22551810 emergency",
		"36000000 71 v71 governance",
		"37000000 90 v90 governance",
	}
	assert.Equal(t, want, rows())

	// Newer proposals for the emergency and the computed heights, an older
	// one for 37000000: nothing changes.
	e.passProposal(t, 72, emergencyHeight, &resolved)
	e.passProposal(t, 73, 36_000_000, &resolved)
	e.passProposal(t, 74, 37_000_000, &resolved)
	require.NoError(t, store.Discover(ctx))
	assert.Equal(t, want, rows())
}
