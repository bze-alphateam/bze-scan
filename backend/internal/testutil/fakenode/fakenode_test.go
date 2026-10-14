package fakenode_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
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

func TestSetEarliestHeightPrunesBelowIt(t *testing.T) {
	n := fakenode.New(t)
	h := n.FixtureHeights()[0]
	n.SetEarliestHeight(h + 1)

	_, body := get(t, n.URL+fakenode.RouteStatus)
	assert.Contains(t, string(body), fmt.Sprintf(`"earliest_block_height":"%d"`, h+1))
	code, body := get(t, fmt.Sprintf("%s%s?height=%d", n.URL, fakenode.RouteBlock, h))
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Contains(t, string(body), fmt.Sprintf("height %d is not available, lowest height is %d", h, h+1))

	n.SetEarliestHeight(0)
	code, _ = get(t, fmt.Sprintf("%s%s?height=%d", n.URL, fakenode.RouteBlock, h))
	assert.Equal(t, http.StatusOK, code)
	_, body = get(t, n.URL+fakenode.RouteStatus)
	assert.Equal(t, readFile(t, "status.json"), body)
}

func TestRequestsAtCountsPerHeight(t *testing.T) {
	n := fakenode.New(t)
	h := n.FixtureHeights()[0]
	get(t, fmt.Sprintf("%s%s?height=%d", n.URL, fakenode.RouteBlock, h))
	get(t, fmt.Sprintf("%s%s?height=%d", n.URL, fakenode.RouteBlock, h))
	get(t, fmt.Sprintf("%s%s?height=%d", n.URL, fakenode.RouteCommit, h+1))

	assert.Equal(t, 2, n.RequestsAt(fakenode.RouteBlock, h))
	assert.Equal(t, 1, n.RequestsAt(fakenode.RouteCommit, h+1))
	assert.Equal(t, 0, n.RequestsAt(fakenode.RouteBlock, h+1))
	n.ResetRequests()
	assert.Equal(t, 0, n.RequestsAt(fakenode.RouteBlock, h))
}

func TestUnknownRouteIsNotFound(t *testing.T) {
	n := fakenode.New(t)
	resp, err := http.Get(n.URL + "/tx_search")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, 1, n.Requests("/tx_search"))
}

func TestABCIQueryAnswersTheRecordedState(t *testing.T) {
	n := fakenode.New(t)
	c := node.New(n.URL)
	req := &banktypes.QueryBalanceRequest{Address: "bze1fl48vsnmsdzcv85q5d2q4z5ajdha8yu3gl9ytq", Denom: "ubze"}
	var resp banktypes.QueryBalanceResponse
	require.NoError(t, c.Query(context.Background(), "/cosmos.bank.v1beta1.Query/Balance", req, &resp, 23821224))
	assert.Equal(t, "131537493190125", resp.Balance.Amount.String(), "the bonded tokens before proposal 47's tally")
	assert.Equal(t, 1, n.RequestsAt(fakenode.RouteABCIQuery, 23821224))

	// A query without a recording answers an application error.
	err := c.Query(context.Background(), "/cosmos.bank.v1beta1.Query/Balance", req, &resp, 23821225)
	abciErr, ok := errors.AsType[*node.ABCIError](err)
	require.True(t, ok, err)
	assert.Equal(t, uint32(1), abciErr.Code)
	assert.Contains(t, abciErr.Log, "no fixture")
}

func statusCode(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestGoDownAnswers503ForTheNextRequests(t *testing.T) {
	n := fakenode.New(t)
	h := n.FixtureHeights()[0]
	block := fmt.Sprintf("%s%s?height=%d", n.URL, fakenode.RouteBlock, h)

	n.GoDown(2)
	assert.Equal(t, http.StatusServiceUnavailable, statusCode(t, n.URL+fakenode.RouteStatus))
	assert.Equal(t, http.StatusServiceUnavailable, statusCode(t, block))
	assert.Equal(t, http.StatusOK, statusCode(t, block))
}

func TestGoDownAtStartsWithThatRequest(t *testing.T) {
	n := fakenode.New(t)
	h := n.FixtureHeights()[0]
	block := fmt.Sprintf("%s%s?height=%d", n.URL, fakenode.RouteBlock, h)

	n.GoDownAt(fakenode.RouteBlock, h, 2)
	assert.Equal(t, http.StatusOK, statusCode(t, n.URL+fakenode.RouteStatus))
	assert.Equal(t, http.StatusServiceUnavailable, statusCode(t, block))
	assert.Equal(t, http.StatusServiceUnavailable, statusCode(t, n.URL+fakenode.RouteStatus))
	assert.Equal(t, http.StatusOK, statusCode(t, block))
	assert.Equal(t, http.StatusOK, statusCode(t, block), "once only")
}

func TestHoldAtHangsThatRequestUntilTheClientGivesUp(t *testing.T) {
	n := fakenode.New(t)
	h := n.FixtureHeights()[0]
	block := fmt.Sprintf("%s%s?height=%d", n.URL, fakenode.RouteBlock, h)

	n.HoldAt(fakenode.RouteBlock, h)
	assert.Equal(t, http.StatusOK, statusCode(t, n.URL+fakenode.RouteStatus))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, block, nil)
	require.NoError(t, err)
	_, err = http.DefaultClient.Do(req) //nolint:bodyclose // no response
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, http.StatusOK, statusCode(t, block), "once only")
}

func TestValidatorsAnswersTheRecordedSet(t *testing.T) {
	n := fakenode.New(t)
	code, body := get(t, n.URL+"/validators?height=23821226&page=1&per_page=100")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, readFile(t, "23821226", "validators.1.json"), body)
	assert.Equal(t, 1, n.RequestsAt(fakenode.RouteValidators, 23821226))

	code, body = get(t, n.URL+"/validators?height=23821226&per_page=100")
	assert.Equal(t, http.StatusOK, code, "page 1 by default")
	assert.Equal(t, readFile(t, "23821226", "validators.1.json"), body)

	code, _ = get(t, n.URL+"/validators?height=23821226&page=2&per_page=100")
	assert.Equal(t, http.StatusInternalServerError, code, "a page not recorded answers like an unknown height")
	assert.Equal(t, http.StatusBadRequest, statusCode(t, n.URL+"/validators?height=23821226&page=1&per_page=30"),
		"only per_page=100 is recorded")
	assert.Equal(t, http.StatusBadRequest, statusCode(t, n.URL+"/validators?height=23821226&page=0&per_page=100"))

	n.SetEarliestHeight(23821227)
	code, body = get(t, n.URL+"/validators?height=23821226&page=1&per_page=100")
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Contains(t, string(body), "is not available, lowest height is 23821227")
}
