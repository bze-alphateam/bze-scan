package fakenode_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

func get(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	return resp.StatusCode, body
}

func readFile(t *testing.T, parts ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"testdata"}, parts...)...))
	require.NoError(t, err)
	return b
}

// byHeightFiles is the documented fixture layout: route -> file name.
var byHeightFiles = map[string]string{
	fakenode.RouteBlock:        "block.json",
	fakenode.RouteBlockResults: "block_results.json",
	fakenode.RouteCommit:       "commit.json",
}

func TestHasAtLeastOneFixtureHeight(t *testing.T) {
	n := fakenode.New(t)
	require.NotEmpty(t, n.FixtureHeights())
}

func TestServesByHeightFixturesByteForByte(t *testing.T) {
	n := fakenode.New(t)
	for _, h := range n.FixtureHeights() {
		for route, file := range byHeightFiles {
			t.Run(fmt.Sprintf("%d%s", h, route), func(t *testing.T) {
				code, body := get(t, fmt.Sprintf("%s%s?height=%d", n.URL, route, h))
				assert.Equal(t, http.StatusOK, code)
				assert.Equal(t, readFile(t, strconv.FormatInt(h, 10), file), body)
			})
		}
	}
}

func TestServesStatusByteForByte(t *testing.T) {
	n := fakenode.New(t)
	code, body := get(t, n.URL+fakenode.RouteStatus)

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, readFile(t, "status.json"), body)
}

func TestFixturesAreJSONRPCEnvelopes(t *testing.T) {
	n := fakenode.New(t)
	h := n.FixtureHeights()[0]
	for route := range byHeightFiles {
		body, err := n.Fixture(route, h)
		require.NoError(t, err)
		var env struct {
			JSONRPC string          `json:"jsonrpc"`
			Result  json.RawMessage `json:"result"`
		}
		require.NoError(t, json.Unmarshal(body, &env), route)
		assert.Equal(t, "2.0", env.JSONRPC, route)
		assert.NotEmpty(t, env.Result, route)
	}
}

func TestUnknownHeightAnswersAboveTipError(t *testing.T) {
	n := fakenode.New(t)
	for route := range byHeightFiles {
		code, body := get(t, fmt.Sprintf("%s%s?height=%d", n.URL, route, n.RecordedStatusHeight()+1000))
		assert.Equal(t, http.StatusInternalServerError, code, route)
		assert.Equal(t, readFile(t, "above_tip.json"), body, route)
	}

	var env struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(n.AboveTip(), &env))
	assert.Equal(t, -32603, env.Error.Code)
}

func TestMissingHeightResolvesToStatusHeight(t *testing.T) {
	n := fakenode.New(t)
	h := n.FixtureHeights()[0]
	n.SetStatusHeight(h)

	code, body := get(t, n.URL+fakenode.RouteBlock)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, readFile(t, strconv.FormatInt(h, 10), "block.json"), body)
}

func TestInvalidHeightIsRejected(t *testing.T) {
	n := fakenode.New(t)
	resp, err := http.Get(n.URL + fakenode.RouteBlock + "?height=abc")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestSetStatusHeight(t *testing.T) {
	n := fakenode.New(t)
	recorded := readFile(t, "status.json")

	n.SetStatusHeight(42)
	assert.Equal(t, int64(42), n.StatusHeight())
	_, body := get(t, n.URL+fakenode.RouteStatus)
	assert.Contains(t, string(body), `"latest_block_height":"42"`)
	assert.Equal(t, len(recorded)-len(strconv.FormatInt(n.RecordedStatusHeight(), 10))+2, len(body),
		"only the height changes")

	n.SetStatusHeight(0)
	_, body = get(t, n.URL+fakenode.RouteStatus)
	assert.Equal(t, recorded, body)
}

func TestRequestCounters(t *testing.T) {
	n := fakenode.New(t)
	h := n.FixtureHeights()[0]

	get(t, n.URL+fakenode.RouteStatus)
	get(t, fmt.Sprintf("%s%s?height=%d", n.URL, fakenode.RouteBlock, h))
	get(t, fmt.Sprintf("%s%s?height=%d", n.URL, fakenode.RouteBlock, h+1))
	get(t, fmt.Sprintf("%s%s?height=%d", n.URL, fakenode.RouteCommit, h))

	assert.Equal(t, 1, n.Requests(fakenode.RouteStatus))
	assert.Equal(t, 2, n.Requests(fakenode.RouteBlock), "failed lookups count too")
	assert.Equal(t, 0, n.Requests(fakenode.RouteBlockResults))
	assert.Equal(t, 1, n.Requests(fakenode.RouteCommit))

	n.ResetRequests()
	assert.Equal(t, 0, n.Requests(fakenode.RouteBlock))
}

func TestUnknownRouteIsNotFound(t *testing.T) {
	n := fakenode.New(t)
	resp, err := http.Get(n.URL + "/tx_search")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, 1, n.Requests("/tx_search"))
}
