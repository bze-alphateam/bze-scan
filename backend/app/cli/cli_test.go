package cli_test

import (
	"bytes"
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
