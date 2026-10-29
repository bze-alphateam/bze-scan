//go:build e2e

package e2e_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// fakeFeed stands in for Medium's RSS feed: it serves body until it is
// told to go down, and counts the reads.
type fakeFeed struct {
	*httptest.Server
	mu     sync.Mutex
	body   []byte
	down   bool
	reads  int
	agents []string
}

func newFakeFeed(t *testing.T, body []byte) *fakeFeed {
	t.Helper()
	f := &fakeFeed{body: body}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.reads++
		f.agents = append(f.agents, r.Header.Get("User-Agent"))
		if f.down || r.URL.Path != "/feed/bzedge-community" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/xml; charset=UTF-8")
		_, _ = w.Write(f.body)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeFeed) setDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
}

func (f *fakeFeed) answered() (int, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads, append([]string(nil), f.agents...)
}

// serveArticles runs the real serve wiring (no indexer) with the feed at
// feedURL and returns the server's base URL.
func serveArticles(t *testing.T, feedURL string) string {
	t.Helper()
	dbURL := freshDatabase(t, true)
	migrateUp(t, dbURL)
	cfg := &config.Config{
		HTTPAddr: "127.0.0.1:0", LogLevel: "warn", LogFormat: config.LogFormatText,
		DatabaseURL: dbURL, NodeRPCURL: fakenode.New(t).URL, ChainID: "beezee-1",
		CORSAllowedOrigins: []string{"https://explorer.getbze.com"},
		MediumFeedURL:      feedURL,
	}
	ctx, cancel := context.WithCancel(context.Background())
	listening := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- serve.Run(ctx, cfg, serve.Options{OnListen: func(a net.Addr) { listening <- a }}) }()
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-done)
	})
	select {
	case a := <-listening:
		return "http://" + a.String()
	case err := <-done:
		t.Fatalf("serve exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not listen")
	}
	return ""
}

func TestCompatArticlesFromTheMediumFeed(t *testing.T) {
	// The feed recorded from Medium and getbze.com's aggregator answer to it.
	feedXML, err := os.ReadFile("../app/server/testdata/medium/feed.xml")
	require.NoError(t, err)
	want, err := os.ReadFile("../app/server/testdata/medium/aggregator.json")
	require.NoError(t, err)

	feed := newFakeFeed(t, feedXML)
	base := serveArticles(t, feed.URL+"/feed/bzedge-community")

	r := fetch(t, base+"/aggregator/api/articles/medium", "Origin", "https://getbze.com")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, string(want), string(r.body), "the aggregator's answer")
	assert.Equal(t, "*", r.header.Get("Access-Control-Allow-Origin"))
	reads, agents := feed.answered()
	assert.Equal(t, 1, reads)
	assert.Equal(t, []string{"Gofeed/1.0"}, agents)

	// Read once per 600 s; the feed going down meanwhile changes nothing.
	feed.setDown(true)
	assert.Equal(t, r.body, fetch(t, base+"/aggregator/api/articles/medium").body)
	reads, _ = feed.answered()
	assert.Equal(t, 1, reads)
}

func TestCompatArticlesWithTheFeedDownAreAnEmptyList(t *testing.T) {
	feed := newFakeFeed(t, nil)
	feed.setDown(true)
	base := serveArticles(t, feed.URL+"/feed/bzedge-community")

	r := fetch(t, base+"/aggregator/api/articles/medium")
	assert.Equal(t, http.StatusOK, r.status)
	assert.Equal(t, "[]\n", string(r.body))
}
