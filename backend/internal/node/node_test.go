package node_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

const fixtureHeight = 24998316

func TestStatus(t *testing.T) {
	n := fakenode.New(t)
	st, raw, err := node.New(n.URL).Status(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "beezee-1", st.Network)
	assert.Equal(t, n.RecordedStatusHeight(), st.LatestBlockHeight)
	assert.False(t, st.LatestBlockTime.IsZero())
	assert.Equal(t, int64(1), st.EarliestBlockHeight)
	assert.NotEmpty(t, raw)

	n.SetStatusHeight(fixtureHeight)
	st, _, err = node.New(n.URL + "/").Status(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(fixtureHeight), st.LatestBlockHeight)
}

func TestBlock(t *testing.T) {
	n := fakenode.New(t)
	b, raw, err := node.New(n.URL).Block(context.Background(), fixtureHeight)
	require.NoError(t, err)

	want, err := n.Fixture(fakenode.RouteBlock, fixtureHeight)
	require.NoError(t, err)
	assert.Equal(t, want, raw)

	assert.Equal(t, int64(fixtureHeight), b.Height)
	assert.Equal(t, time.Date(2026, 10, 8, 15, 33, 38, 155384191, time.UTC), b.Time.UTC())
	assert.Equal(t, "1B2325023D11ABFF1ACE8896C7E8BF1EFE608B809BFF7D79244FD3DB290A33DE", b.Hash)
	assert.Equal(t, "CAC6C644778E878843F5F51B90CD511CFF5B55F7", b.ProposerAddress)
	assert.Len(t, b.Txs, 3)
	assert.Contains(t, string(raw), string(b.Raw))
}

func TestBlockResults(t *testing.T) {
	n := fakenode.New(t)
	r, raw, err := node.New(n.URL).BlockResults(context.Background(), fixtureHeight)
	require.NoError(t, err)

	want, err := n.Fixture(fakenode.RouteBlockResults, fixtureHeight)
	require.NoError(t, err)
	assert.Equal(t, want, raw)

	assert.Equal(t, int64(fixtureHeight), r.Height)
	require.Len(t, r.TxsResults, 3)
	assert.Equal(t, []uint32{0, 0, 11}, []uint32{r.TxsResults[0].Code, r.TxsResults[1].Code, r.TxsResults[2].Code})
	assert.Equal(t, "sdk", r.TxsResults[2].Codespace)
	assert.Positive(t, r.TxsResults[0].GasWanted)
	assert.Positive(t, r.TxsResults[0].GasUsed)

	var mint *node.Event
	for i := range r.FinalizeBlockEvents {
		if r.FinalizeBlockEvents[i].Type == "mint" {
			mint = &r.FinalizeBlockEvents[i]
		}
	}
	require.NotNil(t, mint)
	amount, ok := mint.Get("amount")
	assert.True(t, ok)
	assert.Equal(t, "2541770", amount)
	_, ok = mint.Get("missing")
	assert.False(t, ok)
}

// A node older than CometBFT 0.38 answers begin- and end-block events in
// fields of their own; they are kept for the archive adapter to reject.
func TestBlockResultsOfAPreCometBFT038Node(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":-1,"result":{"height":"7","txs_results":null,` +
			`"begin_block_events":[{"type":"mint","attributes":[{"key":"amount","value":"1"}]}],` +
			`"end_block_events":[{"type":"burn","attributes":[]}]}}`))
	}))
	t.Cleanup(srv.Close)

	r, _, err := node.New(srv.URL).BlockResults(context.Background(), 7)
	require.NoError(t, err)
	assert.Empty(t, r.FinalizeBlockEvents)
	require.Len(t, r.BeginBlockEvents, 1)
	assert.Equal(t, "mint", r.BeginBlockEvents[0].Type)
	require.Len(t, r.EndBlockEvents, 1)
	assert.Equal(t, "burn", r.EndBlockEvents[0].Type)
}

func TestCommit(t *testing.T) {
	n := fakenode.New(t)
	c, raw, err := node.New(n.URL).Commit(context.Background(), fixtureHeight)
	require.NoError(t, err)

	want, err := n.Fixture(fakenode.RouteCommit, fixtureHeight)
	require.NoError(t, err)
	assert.Equal(t, want, raw)

	assert.Equal(t, int64(fixtureHeight), c.Height)
	require.Len(t, c.Signatures, 22)
	assert.Equal(t, node.BlockIDFlagCommit, c.Signatures[0].BlockIDFlag)
	assert.Equal(t, "101DF52F658F4EF69DA0333BC4FB519E36DC54CA", c.Signatures[0].ValidatorAddress)
}

func TestAboveTipError(t *testing.T) {
	n := fakenode.New(t)
	c := node.New(n.URL)
	ctx := context.Background()

	_, _, err := c.Block(ctx, 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, node.ErrAboveTip)
	assert.NotErrorIs(t, err, node.ErrPruned)

	var rpcErr *node.RPCError
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, -32603, rpcErr.Code)
	assert.Equal(t, "block", rpcErr.Route)

	_, _, err = c.BlockResults(ctx, 1)
	assert.ErrorIs(t, err, node.ErrAboveTip)
	_, _, err = c.Commit(ctx, 1)
	assert.ErrorIs(t, err, node.ErrAboveTip)
}

// The pruned answer of a CometBFT 0.38 node (rpc/core/env.go getHeight).
const prunedBody = `{"jsonrpc":"2.0","id":-1,"error":{"code":-32603,"message":"Internal error","data":"height 100 is not available, lowest height is 24900001"}}`

func TestPrunedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(prunedBody))
	}))
	t.Cleanup(srv.Close)

	_, _, err := node.New(srv.URL).Block(context.Background(), 100)
	require.Error(t, err)
	assert.ErrorIs(t, err, node.ErrPruned)
	assert.NotErrorIs(t, err, node.ErrAboveTip)
}

func TestOtherErrors(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"other rpc error": {http.StatusInternalServerError, `{"jsonrpc":"2.0","id":-1,"error":{"code":-32603,"message":"Internal error","data":"something else"}}`},
		"not json":        {http.StatusBadGateway, `<html>bad gateway</html>`},
		"no result":       {http.StatusOK, `{"jsonrpc":"2.0","id":-1}`},
		"wrong height":    {http.StatusOK, `{"jsonrpc":"2.0","id":-1,"result":{"block_id":{"hash":"AA"},"block":{"header":{"height":"7"}}}}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			_, _, err := node.New(srv.URL).Block(context.Background(), 100)
			require.Error(t, err)
			assert.False(t, errors.Is(err, node.ErrAboveTip) || errors.Is(err, node.ErrPruned))
		})
	}
}

func TestRequestsByHeightOnly(t *testing.T) {
	var paths []string
	n := fakenode.New(t)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		http.Redirect(w, r, n.URL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	}))
	t.Cleanup(proxy.Close)

	c := node.New(proxy.URL)
	ctx := context.Background()
	_, _, err := c.Status(ctx)
	require.NoError(t, err)
	_, _, err = c.Block(ctx, fixtureHeight)
	require.NoError(t, err)
	_, _, err = c.BlockResults(ctx, fixtureHeight)
	require.NoError(t, err)
	_, _, err = c.Commit(ctx, fixtureHeight)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"/status",
		"/block?height=24998316",
		"/block_results?height=24998316",
		"/commit?height=24998316",
	}, paths)
}

// mockDoer records requests and answers with a canned body.
type mockDoer struct {
	reqs []*http.Request
	body string
	err  error
}

func (d *mockDoer) Do(req *http.Request) (*http.Response, error) {
	d.reqs = append(d.reqs, req)
	if d.err != nil {
		return nil, d.err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(d.body))}, nil
}

func TestSendsThroughTheDoer(t *testing.T) {
	d := &mockDoer{body: `{"jsonrpc":"2.0","id":-1,"result":{"node_info":{"network":"beezee-1"},"sync_info":{"latest_block_height":"7","latest_block_time":"2026-10-08T00:00:00Z"}}}`}
	st, _, err := node.NewWithDoer("http://node:26657/", d).Status(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(7), st.LatestBlockHeight)
	require.Len(t, d.reqs, 1)
	assert.Equal(t, "http://node:26657/status", d.reqs[0].URL.String())
	assert.Equal(t, http.MethodGet, d.reqs[0].Method)
}

func TestTransportErrorIsReturned(t *testing.T) {
	d := &mockDoer{err: errors.New("dial tcp: connection refused")}
	_, _, err := node.NewWithDoer("http://node:26657", d).Block(context.Background(), 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
	assert.False(t, errors.Is(err, node.ErrAboveTip) || errors.Is(err, node.ErrPruned))
}

func TestPrunedHeightIsErrPruned(t *testing.T) {
	n := fakenode.New(t)
	n.SetEarliestHeight(fixtureHeight + 1)
	c := node.New(n.URL)

	st, _, err := c.Status(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(fixtureHeight+1), st.EarliestBlockHeight)

	_, _, err = c.BlockResults(context.Background(), fixtureHeight)
	require.ErrorIs(t, err, node.ErrPruned)
	assert.NotErrorIs(t, err, node.ErrAboveTip)
}
