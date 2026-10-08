//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

var rawRoutes = []string{fakenode.RouteBlock, fakenode.RouteBlockResults, fakenode.RouteCommit}

// The raw-JSON routes serve what the live indexer wrote without asking the
// archive, fetch a miss from the archive exactly once, and never cache an
// archive failure.
func TestRawJSONProxy(t *testing.T) {
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

	// The first pass writes 316. The indexer puts a height's bodies before
	// it moves on, so once 317 is written, 316 is cached.
	hasBlock := func(h int64) func() bool {
		return func() bool {
			return len(queryStrings(t, db, `SELECT height::text FROM explorer.blocks WHERE height = $1`, h)) == 1
		}
	}
	waitFor(t, 5*time.Second, hasBlock(h316), "block %d", h316)
	insertSinkBlock(t, db, h317)
	waitFor(t, 5*time.Second, hasBlock(h317), "block %d", h317)
	archive.ResetRequests()

	upstream := func() map[string]int {
		counts := map[string]int{}
		for _, r := range rawRoutes {
			counts[r] = archive.Requests(r)
		}
		return counts
	}
	none := map[string]int{fakenode.RouteBlock: 0, fakenode.RouteBlockResults: 0, fakenode.RouteCommit: 0}

	t.Run("an indexed height is served without the archive", func(t *testing.T) {
		for _, r := range rawRoutes {
			res := fetch(t, base+r+"/"+strconv.FormatInt(h316, 10))
			require.Equal(t, http.StatusOK, res.status, r)
			assert.Contains(t, res.header.Get("Content-Type"), "application/json")
			want, err := archive.Fixture(r, h316)
			require.NoError(t, err)
			assert.Equal(t, string(want), string(res.body), "verbatim %s", r)
		}
		assert.Equal(t, none, upstream())
	})

	t.Run("a transaction is sliced from the cached block", func(t *testing.T) {
		row := queryStrings(t, db, `SELECT hash || ' ' || tx_index FROM explorer.transactions
			WHERE height = $1 ORDER BY tx_index DESC LIMIT 1`, h316)
		require.Len(t, row, 1)
		hash, index, _ := strings.Cut(row[0], " ")

		res := fetch(t, base+"/tx/"+strings.ToLower(hash))
		require.Equal(t, http.StatusOK, res.status, string(res.body))
		got := into[dto.RawTx](t, res)
		assert.Equal(t, h316, got.Height)
		assert.Equal(t, index, strconv.FormatInt(got.Index, 10))

		b, _, err := node.New(archive.URL).Block(context.Background(), h316)
		require.NoError(t, err)
		assert.Equal(t, b.Txs[got.Index], got.Tx)
		var result struct {
			Code uint32 `json:"code"`
		}
		require.NoError(t, json.Unmarshal(got.TxResult, &result))
		assert.Equal(t, uint32(11), result.Code, "the third transaction of the block failed")
		archive.ResetRequests() // the Block call above
		assert.Equal(t, none, upstream())
	})

	t.Run("a miss hits the archive once", func(t *testing.T) {
		path := base + fakenode.RouteBlock + "/" + strconv.FormatInt(h318, 10)
		want, err := archive.Fixture(fakenode.RouteBlock, h318)
		require.NoError(t, err)
		for range 3 {
			res := fetch(t, path)
			require.Equal(t, http.StatusOK, res.status)
			assert.Equal(t, string(want), string(res.body))
		}
		assert.Equal(t, 1, archive.Requests(fakenode.RouteBlock))
	})

	t.Run("a height above the archive's tip is 502 and not cached", func(t *testing.T) {
		archive.ResetRequests()
		path := base + fakenode.RouteCommit + "/" + strconv.FormatInt(h319, 10)
		for i := 1; i <= 2; i++ {
			res := fetch(t, path)
			require.Equal(t, http.StatusBadGateway, res.status)
			assert.Equal(t, "upstream_error", into[middleware.ErrorBody](t, res).Error.Code)
			assert.Equal(t, "no-store", res.header.Get("Cache-Control"))
			// Without a retry URL the archive is asked twice per request.
			assert.Equal(t, 2*i, archive.Requests(fakenode.RouteCommit), "request %d", i)
		}
	})

	t.Run("bad input", func(t *testing.T) {
		res := fetch(t, base+"/tx/"+strings.Repeat("AB", 32))
		assert.Equal(t, http.StatusNotFound, res.status)
		assert.Equal(t, "not_found", into[middleware.ErrorBody](t, res).Error.Code)

		res = fetch(t, base+fakenode.RouteBlock+"/0")
		assert.Equal(t, http.StatusBadRequest, res.status)
		assert.Equal(t, "bad_request", into[middleware.ErrorBody](t, res).Error.Code)
	})
}
