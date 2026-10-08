//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// statusInterval is the checker's tick in the test: a poll every 10 ms sees
// every snapshot.
const statusInterval = 200 * time.Millisecond

func getStatus(t *testing.T, url string) dto.Status {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var s dto.Status
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&s))
	return s
}

// waitStatus polls the endpoint until cond holds and returns that snapshot.
func waitStatus(t *testing.T, url string, what string, cond func(dto.Status) bool) dto.Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last dto.Status
	for time.Now().Before(deadline) {
		if last = getStatus(t, url); cond(last) {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; last status %+v", what, last)
	return last
}

func heightIs(h *int64, want int64) bool { return h != nil && *h == want }

// The checker follows the explorer and both nodes: healthy while the
// explorer moves within the tolerance, unhealthy when it stands still or a
// node cannot be read, and the endpoint answers 200 throughout.
func TestStatusEndpointFollowsTheExplorerAndTheNodes(t *testing.T) {
	ix := newIndexer(t)
	local, archive := fakenode.New(t), fakenode.New(t)
	setTips := func(h int64) {
		local.SetStatusHeight(h)
		archive.SetStatusHeight(h)
	}
	ix.index(t, h316)
	setTips(h316)

	cfg := &config.Config{
		HTTPAddr: "127.0.0.1:0", LogLevel: "warn", LogFormat: config.LogFormatText,
		DatabaseURL: ix.url, NodeRPCURL: local.URL, ChainID: "beezee-1",
		ArchiveRPCURL: archive.URL, StatusInterval: statusInterval, StatusHeightTolerance: 5,
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
	var url string
	select {
	case a := <-listening:
		url = "http://" + a.String() + "/api/v1/status"
	case err := <-done:
		t.Fatalf("serve exited early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not listen")
	}

	// First tick: only the spread is known, and it is zero.
	first := waitStatus(t, url, "the first tick", func(s dto.Status) bool { return s.LiveFill.CheckedAt != nil })
	assert.True(t, first.LiveFill.Healthy)
	assert.True(t, heightIs(first.LiveFill.DBHeight, h316))
	assert.True(t, heightIs(first.LiveFill.NodeHeight, h316))
	assert.True(t, heightIs(first.LiveFill.ArchiveHeight, h316))
	assert.Equal(t, "finished", first.BackFill.Status)
	assert.True(t, heightIs(first.BackFill.OldestHeight, h316))

	// The explorer moves to the top fixture height with the nodes.
	setTips(h320)
	for _, h := range []int64{h317, h318, h320} {
		ix.index(t, h)
	}
	moved := waitStatus(t, url, "a healthy tick at the top fixture height", func(s dto.Status) bool {
		return heightIs(s.LiveFill.DBHeight, h320) && s.LiveFill.Healthy
	})
	assert.True(t, heightIs(moved.LiveFill.NodeHeight, h320))
	assert.True(t, heightIs(moved.BackFill.OldestHeight, h316))

	// Nothing new: the explorer stands still and the next tick says so.
	waitStatus(t, url, "an unhealthy tick while the explorer stands still", func(s dto.Status) bool {
		return heightIs(s.LiveFill.DBHeight, h320) && !s.LiveFill.Healthy &&
			s.LiveFill.CheckedAt.After(*moved.LiveFill.CheckedAt)
	})

	// The local node goes away: no node height, unhealthy, still 200.
	local.Close()
	down := waitStatus(t, url, "a tick without the local node", func(s dto.Status) bool {
		return s.LiveFill.NodeHeight == nil
	})
	assert.False(t, down.LiveFill.Healthy)
	assert.True(t, heightIs(down.LiveFill.ArchiveHeight, h320))
	assert.True(t, heightIs(down.LiveFill.DBHeight, h320))
}
