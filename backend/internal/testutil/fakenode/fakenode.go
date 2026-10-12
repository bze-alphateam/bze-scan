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
//	                              (a height may hold only some of the three)
//	abci/<height>/<file>          /abci_query?path="/<svc>/<Method>"&data=0x<hex>&height=<height>,
//	                              <file> = <svc>.<Method>.<hex>.json (scripts/record-fixtures.sh, ABCI)
//
// An /abci_query without a recorded answer answers code 1 ("no fixture"), so
// a test sees which query it lacks.
// A height without a fixture answers above_tip.json with HTTP 500, as a real
// node does. SetEarliestHeight makes the node behave as a pruned one: /status
// reports that earliest_block_height and every height below it answers the
// node's "not available" error. A request without a height resolves to the status height, as a
// real node resolves it to its latest height.
package fakenode

import (
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Routes served by the fake node.
const (
	RouteStatus       = "/status"
	RouteBlock        = "/block"
	RouteBlockResults = "/block_results"
	RouteCommit       = "/commit"
	RouteABCIQuery    = "/abci_query"
)

var byHeightFiles = map[string]string{
	RouteBlock:        "block.json",
	RouteBlockResults: "block_results.json",
	RouteCommit:       "commit.json",
}

//go:embed testdata
var testdata embed.FS

var (
	latestHeightRe   = regexp.MustCompile(`"latest_block_height":"(\d+)"`)
	earliestHeightRe = regexp.MustCompile(`"earliest_block_height":"(\d+)"`)
)

// Node is a running fake CometBFT RPC node. Use URL for its base address.
type Node struct {
	*httptest.Server

	fixtures      fs.FS
	status        []byte
	aboveTip      []byte
	recordedTip   int64
	mu            sync.Mutex
	statusHeight  int64 // 0: answer status.json as recorded
	earliest      int64 // 0: the recorded earliest height, nothing pruned
	requestCounts map[string]int
	heightCounts  map[string]int
	down          int    // requests left to answer 503
	downRoute     string // with downHeight: the request that starts an outage
	downHeight    int64
	downFor       int
	holdRoute     string // with holdHeight: the request that never answers
	holdHeight    int64
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
		heightCounts:  map[string]int{},
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

// SetEarliestHeight makes /status answer h as earliest_block_height and every
// by-height route below h answer the pruned error. h <= 0 restores the
// recorded status and serves every height again.
func (n *Node) SetEarliestHeight(h int64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.earliest = h
}

// GoDown makes the next requests requests, of any route, answer HTTP 503 with
// a proxy's HTML page, as an archive behind a load balancer does while its
// node restarts.
func (n *Node) GoDown(requests int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.down = requests
}

// GoDownAt is GoDown starting with the first request of route at height:
// that request and the requests-1 after it answer 503.
func (n *Node) GoDownAt(route string, height int64, requests int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.downRoute, n.downHeight, n.downFor = route, height, requests
}

// HoldAt makes the next request of route at height hang until the client
// gives up (its ctx is cancelled), as a block too slow to serve does. Later
// requests answer normally.
func (n *Node) HoldAt(route string, height int64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.holdRoute, n.holdHeight = route, height
}

// RequestsAt returns how many requests route has received for height.
func (n *Node) RequestsAt(route string, height int64) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.heightCounts[route+"?"+strconv.FormatInt(height, 10)]
}

// FixtureHeights lists the heights recorded in full (block, block_results
// and commit), ascending. Heights with only some of them (the snapshots
// command's block times and resolutions) are served but not listed.
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
		h, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil || !n.complete(e.Name()) {
			continue
		}
		heights = append(heights, h)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	return heights
}

func (n *Node) complete(dir string) bool {
	for _, file := range byHeightFiles {
		if _, err := fs.Stat(n.fixtures, path.Join(dir, file)); err != nil {
			return false
		}
	}
	return true
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
	n.heightCounts = map[string]int{}
}

func (n *Node) serveHTTP(w http.ResponseWriter, r *http.Request) {
	route := r.URL.Path
	n.mu.Lock()
	n.requestCounts[route]++
	statusHeight, earliest := n.statusHeight, n.earliest
	if n.downRoute == route && r.URL.Query().Get("height") == strconv.FormatInt(n.downHeight, 10) {
		n.down, n.downRoute = n.downFor, ""
	}
	down := n.down > 0
	if down {
		n.down--
	}
	hold := n.holdRoute == route && r.URL.Query().Get("height") == strconv.FormatInt(n.holdHeight, 10)
	if hold {
		n.holdRoute = ""
	}
	n.mu.Unlock()

	if hold {
		<-r.Context().Done()
		return
	}

	if down {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("<html><body>503 Service Unavailable</body></html>"))
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if route == RouteStatus {
		body := n.status
		if statusHeight > 0 {
			body = latestHeightRe.ReplaceAll(body,
				[]byte(fmt.Sprintf(`"latest_block_height":"%d"`, statusHeight)))
		}
		if earliest > 0 {
			body = earliestHeightRe.ReplaceAll(body,
				[]byte(fmt.Sprintf(`"earliest_block_height":"%d"`, earliest)))
		}
		writeJSON(w, http.StatusOK, body)
		return
	}

	if route == RouteABCIQuery {
		n.serveABCI(w, r)
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
	n.mu.Lock()
	n.heightCounts[route+"?"+strconv.FormatInt(height, 10)]++
	n.mu.Unlock()

	if height < earliest {
		writeJSON(w, http.StatusInternalServerError, []byte(fmt.Sprintf(
			`{"jsonrpc":"2.0","id":-1,"error":{"code":-32603,"message":"Internal error","data":"height %d is not available, lowest height is %d"}}`,
			height, earliest)))
		return
	}

	body, err := fs.ReadFile(n.fixtures, path.Join(strconv.FormatInt(height, 10), file))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, n.aboveTip)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// ABCIFile is the fixture file of an abci_query: abci/<height>/<svc>.<Method>.<hex>.json.
func ABCIFile(queryPath string, data []byte, height int64) string {
	return path.Join("abci", strconv.FormatInt(height, 10),
		strings.ReplaceAll(strings.TrimPrefix(queryPath, "/"), "/", ".")+"."+hex.EncodeToString(data)+".json")
}

func (n *Node) serveABCI(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	queryPath, err := strconv.Unquote(q.Get("path"))
	if err != nil {
		http.Error(w, "path must be a quoted string", http.StatusBadRequest)
		return
	}
	data, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(q.Get("data")), "0x"))
	if err != nil {
		http.Error(w, "data must be 0x-prefixed hex", http.StatusBadRequest)
		return
	}
	height, err := strconv.ParseInt(q.Get("height"), 10, 64)
	if err != nil || height <= 0 {
		http.Error(w, "height must be a positive integer", http.StatusBadRequest)
		return
	}
	n.mu.Lock()
	n.heightCounts[RouteABCIQuery+"?"+strconv.FormatInt(height, 10)]++
	n.mu.Unlock()

	body, err := fs.ReadFile(n.fixtures, ABCIFile(queryPath, data, height))
	if err != nil {
		writeJSON(w, http.StatusOK, []byte(fmt.Sprintf(
			`{"jsonrpc":"2.0","id":-1,"result":{"response":{"code":1,"log":"no fixture for %s at %d","info":"","index":"0","key":null,"value":null,"proofOps":null,"height":"%d","codespace":"fakenode"}}}`,
			queryPath, height, height)))
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func writeJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
