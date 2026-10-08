package cli_test

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/cli"
)

func TestHelpListsCommands(t *testing.T) {
	root := cli.NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--help"})

	require.NoError(t, root.Execute())
	assert.Contains(t, out.String(), "serve")
	assert.Contains(t, out.String(), "migrate")
	assert.Contains(t, out.String(), "backfill")
	assert.Contains(t, out.String(), "reindex")
}

func TestBackfillRequiresDatabaseURL(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "")

	root := cli.NewRootCmd()
	root.SetArgs([]string{"backfill"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL is required")
}

func TestBackfillRejectsInvalidConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("BACKFILL_FLOOR", "yesterday")

	root := cli.NewRootCmd()
	root.SetArgs([]string{"backfill"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BACKFILL_FLOOR")
}

func TestMigrateCommandsRequireDatabaseURL(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "")

	for _, args := range [][]string{{"migrate"}, {"migrate", "up"}, {"migrate", "down", "1"}, {"migrate", "version"}} {
		root := cli.NewRootCmd()
		root.SetArgs(args)
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})

		err := root.Execute()
		require.Error(t, err, args)
		assert.Contains(t, err.Error(), "DATABASE_URL is required", args)
	}
}

func TestMigrateDownRejectsInvalidCount(t *testing.T) {
	for _, n := range []string{"0", "1.5", "all"} {
		root := cli.NewRootCmd()
		root.SetArgs([]string{"migrate", "down", n})
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})

		err := root.Execute()
		require.Error(t, err, n)
		assert.Contains(t, err.Error(), "positive number", n)
	}
}

func TestServeRejectsInvalidConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("LOG_FORMAT", "xml")

	root := cli.NewRootCmd()
	root.SetArgs([]string{"serve"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOG_FORMAT")
}

func TestServeRequiresDatabaseURL(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "")
	t.Setenv("INDEXER_ENABLED", "false")
	t.Setenv("LOG_LEVEL", "warn")

	root := cli.NewRootCmd()
	root.SetArgs([]string{"serve"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL is required")
}

// TestServeStopsCleanlyOnSIGTERM runs the real serve command, waits until it
// answers /health, sends SIGTERM to the test process (caught by serve's
// signal context) and expects a nil error, i.e. exit code 0.
func TestServeStopsCleanlyOnSIGTERM(t *testing.T) {
	t.Chdir(t.TempDir())
	addr := freeAddr(t)
	t.Setenv("HTTP_ADDR", addr)
	t.Setenv("LOG_LEVEL", "warn")
	t.Setenv("LOG_FORMAT", "text")
	t.Setenv("INDEXER_ENABLED", "false")
	// Never reached: the API pool connects on the first query.
	t.Setenv("DATABASE_URL", "postgres://bze@127.0.0.1:1/none?sslmode=disable")

	root := cli.NewRootCmd()
	root.SetArgs([]string{"serve"})
	done := make(chan error, 1)
	go func() { done <- root.Execute() }()

	waitHealthy(t, "http://"+addr+"/health", done)

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop after SIGTERM")
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

func waitHealthy(t *testing.T, url string, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("serve exited early: %v", err)
		default:
		}
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("serve did not become healthy")
}

func runReindex(args ...string) error {
	root := cli.NewRootCmd()
	root.SetArgs(append([]string{"reindex"}, args...))
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	return root.Execute()
}

// Selector mistakes fail before the configuration or the database is read:
// exit code 1.
func TestReindexRejectsBadSelectors(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "postgres://unreachable.invalid/db")
	for _, tc := range []struct {
		args []string
		err  string
	}{
		{nil, "is required"},
		{[]string{"--heights", "1", "--failed"}, "mutually exclusive"},
		{[]string{"--from", "5", "--to", "9", "--heights", "7"}, "mutually exclusive"},
		{[]string{"--from", "5"}, "go together"},
		{[]string{"--from", "9", "--to", "5"}, "above --to"},
		{[]string{"--heights", "a"}, `"a"`},
		{[]string{"--heights", "1", "--source", "live"}, "--failed only"},
		{[]string{"--failed", "--source", "elsewhere"}, "must be one of"},
		{[]string{"--heights", "1", "--workers", "-1"}, "--workers"},
	} {
		err := runReindex(tc.args...)
		require.ErrorContains(t, err, tc.err, "%v", tc.args)
		assert.Equal(t, 1, cli.ExitCode(err), "%v", tc.args)
	}
}

func TestReindexRequiresDatabaseURL(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "")
	err := runReindex("--heights", "1")
	require.ErrorContains(t, err, "DATABASE_URL is required")
	assert.Equal(t, 1, cli.ExitCode(err))
}

func TestExitCode(t *testing.T) {
	assert.Equal(t, 0, cli.ExitCode(nil))
	assert.Equal(t, 1, cli.ExitCode(errors.New("database unavailable")))
	failed := &cli.ExitError{Code: cli.ExitSomeFailed, Err: errors.New("1 of 3 heights failed")}
	assert.Equal(t, 2, cli.ExitCode(failed))
	assert.Equal(t, 2, cli.ExitCode(fmt.Errorf("reindex: %w", failed)), "through wrapping")
	assert.Equal(t, "1 of 3 heights failed", failed.Error())
}
