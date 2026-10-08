// Package fakenode is a stand-in for a CometBFT RPC node in tests. It serves
// the URI form of the by-height routes the backend uses (GET /status,
// /block?height=N, /block_results?height=N, /commit?height=N) from recorded
// mainnet responses under testdata/, byte for byte.
//
// Layout of testdata/ (recorded by scripts/record-fixtures.sh):
//
//	status.json                   /status
//	above_tip.json                the JSON-RPC error a node answers for a height above its tip
//	<height>/block.json           /block?height=<height>
//	<height>/block_results.json   /block_results?height=<height>
//	<height>/commit.json          /commit?height=<height>
//
// A height without a fixture answers above_tip.json with HTTP 500, as a real
// node does. A request without a height resolves to the status height, as a
// real node resolves it to its latest height.
package fakenode

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"testing"
)

// Routes served by the fake node.
const (
	RouteStatus       = "/status"
	RouteBlock        = "/block"
	RouteBlockResults = "/block_results"
	RouteCommit       = "/commit"
)

var byHeightFiles = map[string]string{
	RouteBlock:        "block.json",
	RouteBlockResults: "block_results.json",
	RouteCommit:       "commit.json",
}

//go:embed testdata
var testdata embed.FS

var latestHeightRe = regexp.MustCompile(`"latest_block_height":"(\d+)"`)

// Node is a running fake CometBFT RPC node. Use URL for its base address.
type Node struct {
	*httptest.Server

	fixtures      fs.FS
	status        []byte
	aboveTip      []byte
	recordedTip   int64
	mu            sync.Mutex
	statusHeight  int64 // 0: answer status.json as recorded
	requestCounts map[string]int
}

// New starts a fake node serving the recorded fixtures and closes it when the
// test ends.
func New(t testing.TB) *Node {
	t.Helper()
	fixtures, err := fs.Sub(testdata, "testdata")
	if err != nil {
		t.Fatalf("fakenode: %v", err)
	}
	n, err := newNode(fixtures)
	if err != nil {
		t.Fatalf("fakenode: %v", err)
	}
	n.Server = httptest.NewServer(http.HandlerFunc(n.serveHTTP))
	t.Cleanup(n.Close)
	return n
}

func newNode(fixtures fs.FS) (*Node, error) {
	status, err := fs.ReadFile(fixtures, "status.json")
	if err != nil {
		return nil, err
	}
	aboveTip, err := fs.ReadFile(fixtures, "above_tip.json")
	if err != nil {
		return nil, err
	}
	m := latestHeightRe.FindSubmatch(status)
	if m == nil {
		return nil, fmt.Errorf("status.json has no latest_block_height")
	}
	tip, err := strconv.ParseInt(string(m[1]), 10, 64)
	if err != nil {
		return nil, err
	}
	return &Node{
		fixtures:      fixtures,
		status:        status,
		aboveTip:      aboveTip,
		recordedTip:   tip,
		requestCounts: map[string]int{},
	}, nil
}

// RecordedStatusHeight is latest_block_height of the recorded status.json.
func (n *Node) RecordedStatusHeight() int64 {
	return n.recordedTip
}

// StatusHeight is the latest_block_height /status currently answers.
func (n *Node) StatusHeight() int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.statusHeight > 0 {
		return n.statusHeight
	}
	return n.recordedTip
}

// SetStatusHeight makes /status answer h as latest_block_height; every other
// byte of the recorded response stays the same. h <= 0 restores the recorded
// height.
func (n *Node) SetStatusHeight(h int64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.statusHeight = h
}

// FixtureHeights lists the heights with recorded fixtures, ascending.
func (n *Node) FixtureHeights() []int64 {
	entries, err := fs.ReadDir(n.fixtures, ".")
	if err != nil {
		return nil
	}
	var heights []int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if h, err := strconv.ParseInt(e.Name(), 10, 64); err == nil {
			heights = append(heights, h)
		}
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	return heights
}

// Fixture returns the recorded body of route at height, for byte-for-byte
// assertions.
func (n *Node) Fixture(route string, height int64) ([]byte, error) {
	file, ok := byHeightFiles[route]
	if !ok {
		return nil, fmt.Errorf("fakenode: %s is not a by-height route", route)
	}
	return fs.ReadFile(n.fixtures, path.Join(strconv.FormatInt(height, 10), file))
}

// AboveTip returns the recorded above-tip error body.
func (n *Node) AboveTip() []byte {
	return n.aboveTip
}

// Requests returns how many requests route has received (any outcome).
func (n *Node) Requests(route string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.requestCounts[route]
}

// ResetRequests sets every request counter back to zero.
func (n *Node) ResetRequests() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.requestCounts = map[string]int{}
}

func (n *Node) serveHTTP(w http.ResponseWriter, r *http.Request) {
	route := r.URL.Path
	n.mu.Lock()
	n.requestCounts[route]++
	statusHeight := n.statusHeight
	n.mu.Unlock()

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if route == RouteStatus {
		body := n.status
		if statusHeight > 0 {
			body = latestHeightRe.ReplaceAll(n.status,
				[]byte(fmt.Sprintf(`"latest_block_height":"%d"`, statusHeight)))
		}
		writeJSON(w, http.StatusOK, body)
		return
	}

	file, ok := byHeightFiles[route]
	if !ok {
		http.NotFound(w, r)
		return
	}

	height := n.StatusHeight()
	if raw := r.URL.Query().Get("height"); raw != "" {
		h, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || h <= 0 {
			http.Error(w, "height must be a positive integer", http.StatusBadRequest)
			return
		}
		height = h
	}

	body, err := fs.ReadFile(n.fixtures, path.Join(strconv.FormatInt(height, 10), file))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, n.aboveTip)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func writeJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
