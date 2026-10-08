package node

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

const fixtureHeight = 24998316

func TestStatus(t *testing.T) {
	n := fakenode.New(t)
	st, raw, err := New(n.URL).Status(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "beezee-1", st.Network)
	assert.Equal(t, n.RecordedStatusHeight(), st.LatestBlockHeight)
	assert.False(t, st.LatestBlockTime.IsZero())
	assert.NotEmpty(t, raw)

	n.SetStatusHeight(fixtureHeight)
	st, _, err = New(n.URL + "/").Status(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(fixtureHeight), st.LatestBlockHeight)
}

func TestBlock(t *testing.T) {
	n := fakenode.New(t)
	b, raw, err := New(n.URL).Block(context.Background(), fixtureHeight)
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
	r, raw, err := New(n.URL).BlockResults(context.Background(), fixtureHeight)
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

	var mint *Event
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

func TestCommit(t *testing.T) {
	n := fakenode.New(t)
	c, raw, err := New(n.URL).Commit(context.Background(), fixtureHeight)
	require.NoError(t, err)

	want, err := n.Fixture(fakenode.RouteCommit, fixtureHeight)
	require.NoError(t, err)
	assert.Equal(t, want, raw)

	assert.Equal(t, int64(fixtureHeight), c.Height)
	require.Len(t, c.Signatures, 22)
	assert.Equal(t, BlockIDFlagCommit, c.Signatures[0].BlockIDFlag)
	assert.Equal(t, "101DF52F658F4EF69DA0333BC4FB519E36DC54CA", c.Signatures[0].ValidatorAddress)
}

func TestAboveTipError(t *testing.T) {
	n := fakenode.New(t)
	c := New(n.URL)
	ctx := context.Background()

	_, _, err := c.Block(ctx, 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAboveTip)
	assert.NotErrorIs(t, err, ErrPruned)

	var rpcErr *RPCError
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, -32603, rpcErr.Code)
	assert.Equal(t, "block", rpcErr.Route)

	_, _, err = c.BlockResults(ctx, 1)
	assert.ErrorIs(t, err, ErrAboveTip)
	_, _, err = c.Commit(ctx, 1)
	assert.ErrorIs(t, err, ErrAboveTip)
}

// The pruned answer of a CometBFT 0.38 node (rpc/core/env.go getHeight).
const prunedBody = `{"jsonrpc":"2.0","id":-1,"error":{"code":-32603,"message":"Internal error","data":"height 100 is not available, lowest height is 24900001"}}`

func TestPrunedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(prunedBody))
	}))
	t.Cleanup(srv.Close)

	_, _, err := New(srv.URL).Block(context.Background(), 100)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPruned)
	assert.NotErrorIs(t, err, ErrAboveTip)
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

			_, _, err := New(srv.URL).Block(context.Background(), 100)
			require.Error(t, err)
			assert.False(t, errors.Is(err, ErrAboveTip) || errors.Is(err, ErrPruned))
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

	c := New(proxy.URL)
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
