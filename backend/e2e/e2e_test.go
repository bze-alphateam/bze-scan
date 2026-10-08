//go:build e2e

// Package e2e holds the acceptance tests. They run against the PostgreSQL of
// docker/compose.yml and the fake node (make e2e), behind the e2e build tag so
// a plain `go test ./...` never runs them.
package e2e

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/server"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// defaultDatabaseURL matches the postgres service of docker/compose.yml.
const defaultDatabaseURL = "postgres://bze:bze@127.0.0.1:15432/bze_index?sslmode=disable"

// openDB connects to E2E_DATABASE_URL (or the compose default) and waits up
// to 60 s for the database to accept connections.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("E2E_DATABASE_URL")
	if url == "" {
		url = defaultDatabaseURL
	}
	db, err := sql.Open("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	deadline := time.Now().Add(60 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = db.PingContext(ctx)
		cancel()
		if err == nil {
			return db
		}
		if time.Now().After(deadline) {
			t.Fatalf("database not reachable: %v", err)
		}
		time.Sleep(time.Second)
	}
}

func TestHealthOnInProcessServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addrCh := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, server.New(), "127.0.0.1:0", func(a net.Addr) { addrCh <- a }) }()

	var addr net.Addr
	select {
	case addr = <-addrCh:
	case err := <-done:
		t.Fatalf("server exited before listening: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start listening")
	}

	resp, err := http.Get("http://" + addr.String() + "/health")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, body)

	cancel()
	assert.NoError(t, <-done)
}

func TestSinkSchemaIsInstalled(t *testing.T) {
	db := openDB(t)

	rows, err := db.Query(`SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	tables := map[string]bool{}
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables[name] = true
	}
	require.NoError(t, rows.Err())

	for _, want := range []string{"blocks", "tx_results", "events", "attributes"} {
		assert.True(t, tables[want], "sink table %q is missing", want)
	}
}

func TestFakeNodeAnswersStatus(t *testing.T) {
	n := fakenode.New(t)

	resp, err := http.Get(n.URL + fakenode.RouteStatus)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var status struct {
		Result struct {
			SyncInfo struct {
				LatestBlockHeight string `json:"latest_block_height"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&status))

	height, err := strconv.ParseInt(status.Result.SyncInfo.LatestBlockHeight, 10, 64)
	require.NoError(t, err)
	assert.Positive(t, height)
	assert.Equal(t, n.RecordedStatusHeight(), height)
	assert.Equal(t, 1, n.Requests(fakenode.RouteStatus))
}
