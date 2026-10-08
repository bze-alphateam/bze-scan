//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"database/sql"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/cli"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/backfill"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// The backfill range: the recorded heights 24998316..24998330, a contiguous
// run except 24998319, which the fake node answers with the above-tip error.
// The seeded live floor is one above the range, so the ceiling is its top.
const (
	backfillFloor   = h316
	backfillCeiling = int64(24998330)
	seededLiveFloor = backfillCeiling + 1
)

// backfilled are the heights a complete backfill of the range writes.
func backfilled() []string {
	var out []string
	for h := backfillFloor; h <= backfillCeiling; h++ {
		if h != h319 {
			out = append(out, strconv.FormatInt(h, 10))
		}
	}
	return out
}

// quickRetries keep the failing height from slowing the tests down.
var quickRetries = serve.Options{BackfillRetryDelays: []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}}

type backfillEnv struct {
	url     string
	db      *sql.DB
	archive *fakenode.Node
}

// newBackfillEnv is a migrated database of its own with the live floor
// seeded, as the live indexer leaves it, and an archive node.
func newBackfillEnv(t *testing.T) *backfillEnv {
	t.Helper()
	url := freshDatabase(t, true)
	migrateUp(t, url)
	e := &backfillEnv{url: url, db: connect(t, url), archive: fakenode.New(t)}
	_, err := e.db.Exec(`INSERT INTO explorer.indexer_state (key, value, updated_at) VALUES ('live_floor', $1, now())`,
		strconv.FormatInt(seededLiveFloor, 10))
	require.NoError(t, err)
	return e
}

// config is the backfill of the range with X=3 and M=4.
func (e *backfillEnv) config(rateLimit float64) *config.Config {
	return &config.Config{
		LogLevel: "warn", LogFormat: config.LogFormatText, DatabaseURL: e.url,
		ArchiveRPCURL: e.archive.URL, BackfillFloorHeight: backfillFloor,
		BackfillWorkers: 3, BackfillBatch: 4, BackfillQuiet: time.Second, BackfillRateLimit: rateLimit,
	}
}

func (e *backfillEnv) heights(t *testing.T) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT height::text FROM explorer.blocks ORDER BY height`)
}

func (e *backfillEnv) checkpoint(t *testing.T) string {
	t.Helper()
	rows := queryStrings(t, e.db, `SELECT concat_ws(' ', status, ceiling_height, floor_height, lowest_dispatched, blocks_done)
		FROM explorer.backfill_checkpoints WHERE job = 'main'`)
	if len(rows) == 0 {
		return ""
	}
	return rows[0]
}

// assertComplete checks the outcome of a complete backfill of the range.
func (e *backfillEnv) assertComplete(t *testing.T) {
	t.Helper()
	assert.Equal(t, backfilled(), e.heights(t), "every height from the floor to the ceiling, nothing else")
	assert.Equal(t, []string{"24998319 backfill 4"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', height, source, attempts) FROM explorer.index_failures`))
	assert.Equal(t, "done 24998330 24998316 24998316 14", e.checkpoint(t))
	assert.Zero(t, e.archive.RequestsAt(fakenode.RouteBlock, seededLiveFloor), "never above the ceiling")
	assert.Zero(t, e.archive.RequestsAt(fakenode.RouteBlock, backfillFloor-1), "never below the floor")
}

func TestBackfillFillsTheRangeDownToTheFloor(t *testing.T) {
	e := newBackfillEnv(t)
	require.NoError(t, serve.RunBackfill(context.Background(), e.config(0), quickRetries))
	e.assertComplete(t)

	// The transactions of the heights that carry some, and the block
	// times wherever the previous block is indexed (the batches arrived
	// downward, so most were filled by a later flush).
	assert.Equal(t, []string{"24998316 3", "24998321 2", "24998326 2"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', height, count(*)) FROM explorer.transactions GROUP BY height ORDER BY height`))
	assert.Equal(t, []string{"24998316", "24998320"}, queryStrings(t, e.db,
		`SELECT height::text FROM explorer.blocks WHERE block_time_ms IS NULL ORDER BY height`),
		"only the floor and the block above the failed height lack a predecessor")
	assert.Empty(t, queryStrings(t, e.db, `SELECT key FROM explorer.indexer_state WHERE key <> 'live_floor'`),
		"the backfill never moves the live cursor")
	var sinkRows int
	require.NoError(t, e.db.QueryRow(`SELECT count(*) FROM public.blocks`).Scan(&sinkRows))
	assert.Zero(t, sinkRows, "the backfill never writes the sink")

	// A second run finds the job done and asks the archive nothing.
	e.archive.ResetRequests()
	require.NoError(t, serve.RunBackfill(context.Background(), e.config(0), quickRetries))
	assert.Zero(t, e.archive.Requests(fakenode.RouteBlock))
}

func TestBackfillResumesAfterACancel(t *testing.T) {
	e := newBackfillEnv(t)
	// 20 requests a second: the 45 requests of the range take about two
	// seconds, so the run is still going after its first flush.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve.RunBackfill(ctx, e.config(20), quickRetries) }()
	waitFor(t, 10*time.Second, func() bool { return len(e.heights(t)) >= 4 }, "the first flush")
	cancel()
	err := <-done
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	first := e.heights(t)
	assert.Less(t, len(first), len(backfilled()), "stopped before the end")
	assert.Contains(t, e.checkpoint(t), "running ")

	require.NoError(t, serve.RunBackfill(context.Background(), e.config(0), quickRetries))
	e.assertComplete(t)
	// Rows are keyed by height, so a duplicate would be a second row of a
	// key: compare the counts with the fixtures' content.
	assert.Equal(t, []string{"14"}, queryStrings(t, e.db, `SELECT count(*) FROM explorer.blocks`))
	assert.Equal(t, []string{"7"}, queryStrings(t, e.db, `SELECT count(*) FROM explorer.transactions`))
}

func TestASecondBackfillCannotTakeTheLock(t *testing.T) {
	e := newBackfillEnv(t)
	locker := backfill.NewPGLocker(e.url)
	release, ok, err := locker.TryLock(context.Background(), backfill.JobMain)
	require.NoError(t, err)
	require.True(t, ok)

	err = serve.RunBackfill(context.Background(), e.config(0), quickRetries)
	require.ErrorIs(t, err, backfill.ErrLocked)
	assert.Empty(t, e.heights(t))
	assert.Zero(t, e.archive.Requests(fakenode.RouteBlock))
	_, ok, err = locker.TryLock(context.Background(), backfill.JobMain)
	require.NoError(t, err)
	assert.False(t, ok, "still held")

	release()
	require.NoError(t, serve.RunBackfill(context.Background(), e.config(0), quickRetries))
	e.assertComplete(t)
}

func TestBackfillCommandExitsZeroWhenDone(t *testing.T) {
	e := newBackfillEnv(t)
	t.Chdir(t.TempDir())
	for k, v := range map[string]string{
		"DATABASE_URL": e.url, "ARCHIVE_RPC_URL": e.archive.URL, "LOG_LEVEL": "warn",
		"BACKFILL_FLOOR": strconv.FormatInt(backfillFloor, 10), "BACKFILL_WORKERS": "3", "BACKFILL_BATCH": "4",
		"BACKFILL_RATE_LIMIT": "1000",
	} {
		t.Setenv(k, v)
	}
	run := func() error {
		root := cli.NewRootCmd()
		root.SetArgs([]string{"backfill"})
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		return root.Execute()
	}

	require.NoError(t, run(), "exit 0")
	e.assertComplete(t)

	// Locked by another process: an error, so exit 1.
	release, ok, err := backfill.NewPGLocker(e.url).TryLock(context.Background(), backfill.JobMain)
	require.NoError(t, err)
	require.True(t, ok)
	defer release()
	assert.ErrorIs(t, run(), backfill.ErrLocked)
}

// runServeBackfill runs serve with the API, the status checker and the
// backfill, without the live indexer, and returns the status URL.
func runServeBackfill(t *testing.T, e *backfillEnv, enabled bool, rateLimit float64) string {
	t.Helper()
	cfg := e.config(rateLimit)
	cfg.HTTPAddr, cfg.NodeRPCURL, cfg.ChainID = "127.0.0.1:0", e.archive.URL, "beezee-1"
	cfg.StatusInterval, cfg.BackfillEnabled = 50*time.Millisecond, enabled
	opts := quickRetries
	listening := make(chan net.Addr, 1)
	opts.OnListen = func(a net.Addr) { listening <- a }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve.Run(ctx, cfg, opts) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(15 * time.Second):
			t.Error("serve did not stop")
		}
	})
	select {
	case a := <-listening:
		return "http://" + a.String() + "/api/v1/status"
	case err := <-done:
		t.Fatalf("serve exited early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not listen")
	}
	return ""
}

func TestServeRunsTheBackfillAndTheStatusFollowsIt(t *testing.T) {
	e := newBackfillEnv(t)
	// 10 requests a second: about four seconds for the range.
	url := runServeBackfill(t, e, true, 10)

	waitStatus(t, url, "back_fill in progress", func(s dto.Status) bool {
		return s.LiveFill.CheckedAt != nil && s.BackFill.Status == "in_progress"
	})
	assert.Less(t, len(e.heights(t)), len(backfilled()), "seen while the backfill runs")

	deadline := time.Now().Add(20 * time.Second)
	var last dto.Status
	for time.Now().Before(deadline) {
		if last = getStatus(t, url); last.BackFill.Status == "finished" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.Equal(t, "finished", last.BackFill.Status)
	assert.True(t, heightIs(last.BackFill.OldestHeight, backfillFloor))
	e.assertComplete(t)
}

func TestServeWithTheBackfillDisabledPausesAnUnfinishedJob(t *testing.T) {
	e := newBackfillEnv(t)
	_, err := e.db.Exec(`INSERT INTO explorer.backfill_checkpoints
		(job, ceiling_height, floor_height, lowest_dispatched, blocks_done, status, started_at, updated_at)
		VALUES ('main', $1, $2, $3, 5, 'running', now(), now())`, backfillCeiling, backfillFloor, backfillCeiling-5)
	require.NoError(t, err)

	url := runServeBackfill(t, e, false, 0)

	s := waitStatus(t, url, "the first tick", func(s dto.Status) bool { return s.LiveFill.CheckedAt != nil })
	assert.Equal(t, "in_progress", s.BackFill.Status, "older history is still owed")
	assert.Contains(t, e.checkpoint(t), "paused ")
	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, e.heights(t), "nothing runs")
}

// An outage longer than the local node's window: the live indexer reads the
// heights the node has pruned from the archive, through the catch-up job,
// and the rest from the node.
func TestLiveCatchesUpThroughTheArchive(t *testing.T) {
	url := freshDatabase(t, true)
	migrateUp(t, url)
	db := connect(t, url)
	local, archive := fakenode.New(t), fakenode.New(t)
	local.SetStatusHeight(h316)

	cfg := &config.Config{
		HTTPAddr: "127.0.0.1:0", LogLevel: "warn", LogFormat: config.LogFormatText,
		DatabaseURL: url, NodeRPCURL: local.URL, ChainID: "beezee-1", IndexerEnabled: true,
		ArchiveRPCURL: archive.URL, StatusInterval: time.Hour, BackfillWorkers: 3, BackfillBatch: 4,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve.Run(ctx, cfg, quickRetries) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(15 * time.Second):
			t.Error("serve did not stop")
		}
	})
	hasBlock := func(h int64) func() bool {
		return func() bool {
			return len(queryStrings(t, db, `SELECT 1 FROM explorer.blocks WHERE height = $1`, h)) == 1
		}
	}
	waitFor(t, 5*time.Second, hasBlock(h316), "the live floor %d", h316)

	// The node comes back at 24998330 having pruned everything below
	// 24998325.
	const earliest = int64(24998325)
	local.SetEarliestHeight(earliest)
	local.SetStatusHeight(backfillCeiling)
	local.ResetRequests()
	insertSinkBlock(t, db, backfillCeiling)

	waitFor(t, 10*time.Second, hasBlock(backfillCeiling), "block %d", backfillCeiling)
	assert.Equal(t, backfilled(), queryStrings(t, db, `SELECT height::text FROM explorer.blocks ORDER BY height`))
	assert.Equal(t, []string{"24998319 live 4"}, queryStrings(t, db,
		`SELECT concat_ws(' ', height, source, attempts) FROM explorer.index_failures`))
	assert.Equal(t, []string{strconv.FormatInt(backfillCeiling, 10)}, queryStrings(t, db,
		`SELECT value FROM explorer.indexer_state WHERE key = 'last_indexed_height'`))
	for h := h317; h < earliest; h++ {
		assert.Zero(t, local.RequestsAt(fakenode.RouteBlock, h), "pruned height %d is not asked of the node", h)
	}
	assert.Equal(t, 1, archive.RequestsAt(fakenode.RouteBlock, h317))
	for h := earliest; h <= backfillCeiling; h++ {
		assert.Zero(t, archive.RequestsAt(fakenode.RouteBlock, h), "height %d comes from the node", h)
		assert.Equal(t, 1, local.RequestsAt(fakenode.RouteBlock, h))
	}
}
