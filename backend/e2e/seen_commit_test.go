//go:build e2e

package e2e_test

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// A block indexed at the node's tip gets the node's seen commit, which may
// lack signatures that arrived late. Once the next block is indexed, the
// block's signatures and its raw commit are the canonical commit's.
func TestLiveCorrectsASeenCommitOnceTheNextBlockIsIndexed(t *testing.T) {
	url := freshDatabase(t, true)
	migrateUp(t, url)
	db := connect(t, url)
	local, archive := fakenode.New(t), fakenode.New(t)
	local.SetStatusHeight(h316)

	cfg := &config.Config{
		HTTPAddr: "127.0.0.1:0", LogLevel: "warn", LogFormat: config.LogFormatText,
		DatabaseURL: url, NodeRPCURL: local.URL, ChainID: "beezee-1", IndexerEnabled: true,
		ArchiveRPCURL: archive.URL, StatusInterval: time.Hour,
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
	var base string
	select {
	case a := <-listening:
		base = "http://" + a.String() + "/api/v1/raw"
	case err := <-done:
		t.Fatalf("serve exited early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not listen")
	}

	signatures := func(h int64) string {
		rows := queryStrings(t, db, `SELECT signatures_count::text FROM explorer.blocks WHERE height = $1`, h)
		if len(rows) == 0 {
			return ""
		}
		return rows[0]
	}
	waitFor(t, 5*time.Second, func() bool { return signatures(h316) != "" }, "block %d", h316)

	// 317 is the tip's block: the node answers its seen commit, three of
	// the 22 signatures missing.
	local.SetSeenCommit(h317, 3)
	insertSinkBlock(t, db, h317)
	waitFor(t, 5*time.Second, func() bool { return signatures(h317) != "" }, "block %d", h317)
	assert.Equal(t, "19", signatures(h317), "written with the seen commit")

	// 318 exists: the node now answers 317's canonical commit.
	local.ClearSeenCommit(h317)
	insertSinkBlock(t, db, h318)
	waitFor(t, 5*time.Second, func() bool { return signatures(h318) != "" }, "block %d", h318)
	waitFor(t, 5*time.Second, func() bool { return signatures(h317) == "22" }, "block %d with the canonical 22 signatures", h317)
	assert.Equal(t, "22", signatures(h318))

	archive.ResetRequests()
	res := fetch(t, base+fakenode.RouteCommit+"/"+strconv.FormatInt(h317, 10))
	require.Equal(t, http.StatusOK, res.status)
	want, err := local.Fixture(fakenode.RouteCommit, h317)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(res.body), "the canonical commit, verbatim")
	assert.Zero(t, archive.Requests(fakenode.RouteCommit), "served from the cache")
	assert.Empty(t, queryStrings(t, db, `SELECT height::text FROM explorer.index_failures`))
}
