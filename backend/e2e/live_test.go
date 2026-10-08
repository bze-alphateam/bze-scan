//go:build e2e

package e2e_test

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/live"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
	"github.com/bze-alphateam/bze-scan/backend/migrations"
)

// Recorded heights: 24998316, 24998317, 24998318 and 24998320. The fake node
// answers the above-tip error for 24998319.
const (
	h316 int64 = 24998316
	h317 int64 = 24998317
	h318 int64 = 24998318
	h319 int64 = 24998319
	h320 int64 = 24998320
)

// liveEnv is a migrated database of its own, the fake node and a running
// live indexer with test-sized retries and backoff.
type liveEnv struct {
	url  string
	db   *sql.DB
	node *fakenode.Node
	stop func()
}

// newLiveEnv starts the indexer with the fake node's tip at tip; the first
// pass indexes that height, and the function returns once it is written.
func newLiveEnv(t *testing.T, tip int64) *liveEnv {
	t.Helper()
	url := freshDatabase(t, true)
	migrateUp(t, url)
	env := &liveEnv{url: url, db: connect(t, url), node: fakenode.New(t)}
	env.node.SetStatusHeight(tip)
	env.start(t)
	env.waitBlock(t, tip, 5*time.Second)
	return env
}

func (e *liveEnv) start(t *testing.T) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), e.url)
	require.NoError(t, err)

	ix := live.New(live.Config{
		RetryDelays:  []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
		ReconnectMin: 50 * time.Millisecond,
		ReconnectMax: 200 * time.Millisecond,
	}, live.Deps{
		Listener:    live.NewPGListener(e.url),
		Node:        node.New(e.node.URL),
		Store:       writer.NewLiveWriter(pool),
		Transformer: transform.New(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ix.Run(ctx) }()
	stopped := false
	e.stop = func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(15 * time.Second):
			t.Error("live indexer did not stop")
		}
		pool.Close()
	}
	t.Cleanup(e.stop)
}

func (e *liveEnv) waitBlock(t *testing.T, height int64, within time.Duration) {
	t.Helper()
	waitFor(t, within, func() bool { return e.hasBlock(t, height) }, "explorer.blocks %d", height)
}

func (e *liveEnv) hasBlock(t *testing.T, height int64) bool {
	var n int
	require.NoError(t, e.db.QueryRow(`SELECT count(*) FROM explorer.blocks WHERE height = $1`, height).Scan(&n))
	return n == 1
}

func (e *liveEnv) state(t *testing.T, key string) string {
	t.Helper()
	var v string
	require.NoError(t, e.db.QueryRow(`SELECT value FROM explorer.indexer_state WHERE key = $1`, key).Scan(&v))
	return v
}

// listenerPID is the backend of the connection listening on the notify
// channel in this database, 0 when there is none.
func listenerPID(t *testing.T, db *sql.DB) int {
	t.Helper()
	var pid int
	err := db.QueryRow(`SELECT pid FROM pg_stat_activity
		WHERE datname = current_database() AND query = $1`, "LISTEN "+migrations.NotifyChannel).Scan(&pid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0
	}
	require.NoError(t, err)
	return pid
}

func waitFor(t *testing.T, within time.Duration, cond func() bool, what string, args ...any) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for "+what, append([]any{within}, args...)...)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLiveIndexesANotifiedBlock(t *testing.T) {
	env := newLiveEnv(t, h316)

	// The node's tip stays at 316: only the notification can bring 317 in.
	start := time.Now()
	insertSinkBlock(t, env.db, h317)
	env.waitBlock(t, h317, 2*time.Second)
	t.Logf("indexed %s after the sink insert", time.Since(start))

	var (
		blockTime                         time.Time
		txCount, txFailed, sigs           int
		blockTimeMs                       sql.NullInt64
		hash, proposer, minted, inflation string
		size                              int
		fees                              string
		powerPct                          sql.NullString
	)
	require.NoError(t, env.db.QueryRow(`SELECT time, tx_count, tx_failed_count, block_time_ms, hash,
			proposer_cons_address, size_bytes, minted::text, inflation::text, fees_distributed::text,
			signatures_count, signatures_power_pct::text
		FROM explorer.blocks WHERE height = $1`, h317).Scan(
		&blockTime, &txCount, &txFailed, &blockTimeMs, &hash, &proposer, &size, &minted, &inflation,
		&fees, &sigs, &powerPct))

	assert.Equal(t, time.Date(2026, 10, 8, 15, 33, 43, 944298000, time.UTC), blockTime.UTC())
	assert.Equal(t, 0, txCount)
	assert.Equal(t, 0, txFailed)
	assert.Equal(t, sql.NullInt64{Int64: 5789, Valid: true}, blockTimeMs, "317 minus 316")
	assert.Len(t, hash, 64)
	assert.Len(t, proposer, 40)
	assert.Positive(t, size)
	assert.Equal(t, "2541770", minted)
	assert.Equal(t, "0.050000000000000000", inflation)
	assert.JSONEq(t, `[{"denom":"ubze","amount":"2704636"}]`, fees)
	assert.Equal(t, 22, sigs)
	assert.False(t, powerPct.Valid)

	var first sql.NullInt64
	require.NoError(t, env.db.QueryRow(`SELECT block_time_ms FROM explorer.blocks WHERE height = $1`, h316).Scan(&first))
	assert.False(t, first.Valid, "the first block has no indexed predecessor")
	assert.Equal(t, "24998317", env.state(t, writer.KeyLastIndexedHeight))
}

func TestLiveIndexesAGapInOrder(t *testing.T) {
	env := newLiveEnv(t, h316)

	insertSinkBlock(t, env.db, h318)
	env.waitBlock(t, h318, 2*time.Second)
	assert.True(t, env.hasBlock(t, h317), "the cursor indexed the gap")

	// 317 was written before 318: both have their predecessor's time.
	assert.Equal(t, []string{"5789", "5827"}, queryStrings(t, env.db,
		`SELECT block_time_ms::text FROM explorer.blocks WHERE height IN ($1, $2)`, h317, h318))

	// The late notification of 317 changes nothing.
	insertSinkBlock(t, env.db, h317)
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, []string{"24998316", "24998317", "24998318"},
		queryStrings(t, env.db, `SELECT height::text FROM explorer.blocks`))
	assert.Empty(t, queryStrings(t, env.db, `SELECT height::text FROM explorer.index_failures`))
}

func TestLiveReconnectsAndCatchesUp(t *testing.T) {
	env := newLiveEnv(t, h316)
	pid := listenerPID(t, env.db)
	require.NotZero(t, pid)

	// The node moves on without a notification; only the pass that follows
	// the reconnect can notice.
	env.node.SetStatusHeight(h318)
	_, err := env.db.Exec(`SELECT pg_terminate_backend($1)`, pid)
	require.NoError(t, err)

	env.waitBlock(t, h318, 5*time.Second)
	assert.True(t, env.hasBlock(t, h317))
	newPID := listenerPID(t, env.db)
	assert.NotZero(t, newPID)
	assert.NotEqual(t, pid, newPID)
}

func TestLiveRecordsAFailingHeightAndMovesOn(t *testing.T) {
	env := newLiveEnv(t, h316)

	env.node.SetStatusHeight(h320)
	insertSinkBlock(t, env.db, h320)
	env.waitBlock(t, h320, 5*time.Second)

	assert.Equal(t, []string{"24998316", "24998317", "24998318", "24998320"},
		queryStrings(t, env.db, `SELECT height::text FROM explorer.blocks`))

	var (
		source, msg string
		attempts    int
		resolved    sql.NullTime
	)
	require.NoError(t, env.db.QueryRow(`SELECT source, attempts, error, resolved_at
		FROM explorer.index_failures WHERE height = $1`, h319).Scan(&source, &attempts, &msg, &resolved))
	assert.Equal(t, "live", source)
	assert.Equal(t, 3, attempts)
	assert.Contains(t, msg, "must be less than or equal to the current blockchain height")
	assert.False(t, resolved.Valid)
	assert.Equal(t, "24998320", env.state(t, writer.KeyLastIndexedHeight))
}

func TestLiveFloorIsWrittenOnce(t *testing.T) {
	env := newLiveEnv(t, h316)
	assert.Equal(t, "24998316", env.state(t, writer.KeyLiveFloor))

	insertSinkBlock(t, env.db, h317)
	env.waitBlock(t, h317, 2*time.Second)
	assert.Equal(t, "24998316", env.state(t, writer.KeyLiveFloor))

	// A restart after an outage catches up from the cursor; the floor stays.
	env.stop()
	env.node.SetStatusHeight(h318)
	env.start(t)
	env.waitBlock(t, h318, 5*time.Second)
	assert.Equal(t, "24998316", env.state(t, writer.KeyLiveFloor))
	assert.Equal(t, "24998318", env.state(t, writer.KeyLastIndexedHeight))
}

func TestLiveWriterIsIdempotentAndTopsUpPartitions(t *testing.T) {
	url := freshDatabase(t, true)
	migrateUp(t, url)
	db := connect(t, url)
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	w := writer.NewLiveWriter(pool)

	block := func(h int64) *transform.Entities {
		return &transform.Entities{Blocks: []transform.Block{{
			Height: h, Time: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), Hash: strings.Repeat("A", 64),
		}}}
	}

	// 48,999,999 is in p000048: no top-up yet.
	require.NoError(t, w.WriteBlock(ctx, block(48_999_999)))
	require.NoError(t, w.WriteBlock(ctx, block(48_999_999)))
	assert.Equal(t, []string{"1"}, queryStrings(t, db, `SELECT count(*)::text FROM explorer.blocks`))
	assert.Len(t, partitionsOf(t, db, "blocks"), 50)

	// 49,000,000 enters the last partition: partitions up to 69,000,000.
	require.NoError(t, w.WriteBlock(ctx, block(49_000_000)))
	parts := partitionsOf(t, db, "blocks")
	assert.Len(t, parts, 70)
	assert.Contains(t, parts, "blocks_p000069")
	assert.Len(t, partitionsOf(t, db, "order_fills"), 70)

	assert.Equal(t, "48999999", queryStrings(t, db, `SELECT value FROM explorer.indexer_state WHERE key = 'live_floor'`)[0])
	assert.Equal(t, "49000000", queryStrings(t, db, `SELECT value FROM explorer.indexer_state WHERE key = 'last_indexed_height'`)[0])

	// A failure never moves the cursor back.
	require.NoError(t, w.RecordFailure(ctx, 10, 3, errors.New("boom")))
	assert.Equal(t, "49000000", queryStrings(t, db, `SELECT value FROM explorer.indexer_state WHERE key = 'last_indexed_height'`)[0])
}

// runServe runs the real serve wiring on a migrated database of its own and
// returns that database once the HTTP server listens.
func runServe(t *testing.T, indexer bool) *sql.DB {
	t.Helper()
	url := freshDatabase(t, true)
	migrateUp(t, url)
	n := fakenode.New(t)
	n.SetStatusHeight(h316)

	cfg := &config.Config{
		HTTPAddr: "127.0.0.1:0", LogLevel: "info", LogFormat: config.LogFormatText,
		DatabaseURL: url, NodeRPCURL: n.URL, ChainID: "beezee-1", IndexerEnabled: indexer,
	}
	ctx, cancel := context.WithCancel(context.Background())
	listening := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- serve.Run(ctx, cfg, serve.Options{OnListen: func(a net.Addr) { listening <- a }}) }()
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
	case <-listening:
	case err := <-done:
		t.Fatalf("serve exited early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not listen")
	}
	return connect(t, url)
}

func TestServeWithIndexerListensAndIndexes(t *testing.T) {
	db := runServe(t, true)
	waitFor(t, 5*time.Second, func() bool { return listenerPID(t, db) != 0 }, "the listener")
	waitFor(t, 5*time.Second, func() bool {
		return len(queryStrings(t, db, `SELECT height::text FROM explorer.blocks`)) == 1
	}, "the first block")
}

func TestServeWithoutIndexerStartsNoListener(t *testing.T) {
	db := runServe(t, false)
	time.Sleep(500 * time.Millisecond)
	assert.Zero(t, listenerPID(t, db))

	insertSinkBlock(t, db, h316)
	time.Sleep(500 * time.Millisecond)
	assert.Empty(t, queryStrings(t, db, `SELECT height::text FROM explorer.blocks`))
	assert.Empty(t, queryStrings(t, db, `SELECT key FROM explorer.indexer_state`))
}
