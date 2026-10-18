package node_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	assert.Equal(t, "a_team_pub_xl", st.Moniker)
	assert.False(t, st.CatchingUp)
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
	assert.True(t, c.Canonical, "recorded below the tip")
	require.Len(t, c.Signatures, 22)
	assert.Equal(t, node.BlockIDFlagCommit, c.Signatures[0].BlockIDFlag)
	assert.Equal(t, "101DF52F658F4EF69DA0333BC4FB519E36DC54CA", c.Signatures[0].ValidatorAddress)
	assert.Equal(t, time.Date(2026, 10, 8, 15, 33, 38, 155384191, time.UTC), c.Time.UTC())
	assert.Equal(t, time.Date(2026, 10, 8, 15, 33, 43, 964380559, time.UTC), c.Signatures[0].Timestamp.UTC())
}

func TestSeenCommitIsNotCanonical(t *testing.T) {
	n := fakenode.New(t)
	n.SetSeenCommit(fixtureHeight, 2)
	c, _, err := node.New(n.URL).Commit(context.Background(), fixtureHeight)
	require.NoError(t, err)
	assert.False(t, c.Canonical)
	signed := 0
	for _, s := range c.Signatures {
		if s.BlockIDFlag == node.BlockIDFlagCommit {
			signed++
		}
	}
	assert.Equal(t, 20, signed)
}

func TestCommitAbsentSlot(t *testing.T) {
	// The first block after the v8.1.0 upgrade's halt: seven validators
	// were not back.
	c, _, err := node.New(fakenode.New(t).URL).Commit(context.Background(), 22551811)
	require.NoError(t, err)
	assert.Equal(t, 1, c.Signatures[0].BlockIDFlag)
	assert.Empty(t, c.Signatures[0].ValidatorAddress)
	assert.True(t, c.Signatures[0].Timestamp.IsZero())
	signed := 0
	for _, s := range c.Signatures {
		if s.BlockIDFlag == node.BlockIDFlagCommit {
			signed++
		}
	}
	assert.Equal(t, 16, signed)
}

func TestConsensusState(t *testing.T) {
	// Recorded on mainnet mid-round: 19 prevotes and 13 precommits for the
	// block at 25085046, round 0; round 1 is all nil-Vote.
	n := fakenode.New(t)
	cs, raw, err := node.New(n.URL).ConsensusState(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, raw)
	assert.Equal(t, int64(25085046), cs.Height)
	assert.Equal(t, int32(0), cs.Round)
	assert.Empty(t, cs.Unparsed)
	require.Len(t, cs.Votes, 32)
	prevotes, precommits := 0, 0
	for _, v := range cs.Votes {
		assert.Equal(t, int64(25085046), v.Height)
		assert.False(t, v.Nil)
		switch v.Type {
		case node.Prevote:
			prevotes++
		case node.Precommit:
			precommits++
		}
	}
	assert.Equal(t, 19, prevotes)
	assert.Equal(t, 13, precommits)
	assert.Equal(t, node.RoundVote{
		ValidatorIndex: 0, AddressPrefix: "2217EC92A4E7", Height: 25085046, Round: 0, Type: node.Prevote,
		Timestamp: time.Date(2026, 10, 14, 12, 11, 37, 668402387, time.UTC),
	}, cs.Votes[0])
}

func TestConsensusStateSkipsWhatIsNotAVote(t *testing.T) {
	n := fakenode.New(t)
	n.SetConsensusState([]byte(`{"jsonrpc":"2.0","id":-1,"result":{"round_state":{"height/round/step":"7/2/6",` +
		`"height_vote_set":[{"round":0,"prevotes":["nil-Vote","Vote{1:AB12 7/00/SIGNED_MSG_TYPE_PREVOTE(Prevote)  0A 00 @ 2026-10-13T10:00:00.5Z}"],` +
		`"precommits":["something else","nil-Vote"]},` +
		`{"round":2,"prevotes":["nil-Vote","nil-Vote"],"precommits":["Vote{0:CD34 7/02/SIGNED_MSG_TYPE_PRECOMMIT(Precommit) FFEE 0B 00 @ 2026-10-13T10:01:00Z}","nil-Vote"]}]}}}`))
	cs, _, err := node.New(n.URL).ConsensusState(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(7), cs.Height)
	assert.Equal(t, int32(2), cs.Round)
	assert.Equal(t, []string{"something else"}, cs.Unparsed)
	assert.Equal(t, []node.RoundVote{
		{ValidatorIndex: 1, AddressPrefix: "AB12", Height: 7, Round: 0, Type: node.Prevote, Nil: true,
			Timestamp: time.Date(2026, 10, 13, 10, 0, 0, 500_000_000, time.UTC)},
		{ValidatorIndex: 0, AddressPrefix: "CD34", Height: 7, Round: 2, Type: node.Precommit,
			Timestamp: time.Date(2026, 10, 13, 10, 1, 0, 0, time.UTC)},
	}, cs.Votes)
}

func TestParseVote(t *testing.T) {
	at := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	v, ok := node.ParseVote(fakenode.Vote(4, "2217EC92A4E73BA79E86E90FA6C4180429EB7C41", 22551811, at))
	require.True(t, ok)
	assert.Equal(t, node.RoundVote{ValidatorIndex: 4, AddressPrefix: "2217EC92A4E7", Height: 22551811, Type: node.Prevote,
		Nil: true, Timestamp: at}, v)
	for _, s := range []string{"nil-Vote", "", "Vote{x:AB 1/00/SIGNED_MSG_TYPE_PREVOTE(Prevote)  0A 00 @ 2026-10-13T10:00:00Z}",
		"Vote{1:AB 1/00/SIGNED_MSG_TYPE_PROPOSAL(Proposal)  0A 00 @ 2026-10-13T10:00:00Z}",
		"Vote{1:AB 1/00/SIGNED_MSG_TYPE_PREVOTE(Prevote)  0A 00 @ yesterday}"} {
		_, ok := node.ParseVote(s)
		assert.False(t, ok, s)
	}
}

func TestConsensusStateRejectsABadPosition(t *testing.T) {
	n := fakenode.New(t)
	n.SetConsensusState([]byte(`{"jsonrpc":"2.0","id":-1,"result":{"round_state":{"height/round/step":"7","height_vote_set":[]}}}`))
	_, _, err := node.New(n.URL).ConsensusState(context.Background())
	assert.ErrorContains(t, err, "height/round/step")
}

func TestValidators(t *testing.T) {
	n := fakenode.New(t)
	v, raw, err := node.New(n.URL).Validators(context.Background(), 23821226, 1, node.ValidatorsPerPage)
	require.NoError(t, err)

	want, err := os.ReadFile("../testutil/fakenode/testdata/23821226/validators.1.json")
	require.NoError(t, err)
	assert.Equal(t, want, raw)

	assert.Equal(t, int64(23821226), v.Height)
	assert.Equal(t, 23, v.Total)
	require.Len(t, v.Validators, 23)
	assert.Equal(t, node.Validator{
		Address:     "908785D2A4D25B062E669AAF65D3AB0727C92494",
		PubKey:      node.PubKey{Type: "tendermint/PubKeyEd25519", Value: "a8fkGHNvPGZI6Q2P+n+6IDAIZ5UpPH+/5HzWbaFjsW8="},
		VotingPower: 9656470,
	}, v.Validators[0])
}

func TestValidatorsRejectsAnotherHeight(t *testing.T) {
	d := &mockDoer{body: `{"jsonrpc":"2.0","id":-1,"result":{"block_height":"7","validators":[],"count":"0","total":"0"}}`}
	_, _, err := node.NewWithDoer("http://node", d).Validators(context.Background(), 8, 2, 50)
	require.ErrorContains(t, err, "validators 8: node answered height 7")
	require.Len(t, d.reqs, 1)
	assert.Equal(t, "/validators?height=8&page=2&per_page=50", d.reqs[0].URL.RequestURI())
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
	_, _, err = c.Validators(ctx, 23821226, 1, node.ValidatorsPerPage)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"/status",
		"/block?height=24998316",
		"/block_results?height=24998316",
		"/commit?height=24998316",
		"/validators?height=23821226&page=1&per_page=100",
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

func TestMissingValidatorSetIsErrPruned(t *testing.T) {
	d := &mockDoer{body: `{"jsonrpc":"2.0","id":-1,"error":{"code":-32603,"message":"Internal error","data":"could not find validator set for height #12"}}`}
	_, _, err := node.NewWithDoer("http://node", d).Validators(context.Background(), 12, 1, node.ValidatorsPerPage)
	require.ErrorIs(t, err, node.ErrPruned)
}

func TestUnreachableNodeIsErrUnavailable(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	_, _, err := node.New(closed.URL).Block(context.Background(), 5)
	require.ErrorIs(t, err, node.ErrUnavailable, "connection refused")

	d := &mockDoer{err: errors.New("read: connection reset by peer")}
	_, _, err = node.NewWithDoer("http://node:26657", d).Status(context.Background())
	require.ErrorIs(t, err, node.ErrUnavailable)
	assert.Contains(t, err.Error(), "connection reset by peer")
}

func TestProxyStatusesAreErrUnavailable(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"429":                 {http.StatusTooManyRequests, `rate limited`},
		"502":                 {http.StatusBadGateway, `<html>bad gateway</html>`},
		"503":                 {http.StatusServiceUnavailable, ``},
		"504":                 {http.StatusGatewayTimeout, `<html>gateway timeout</html>`},
		"503 json, no result": {http.StatusServiceUnavailable, `{"message":"no healthy upstream"}`},
		"520 cloudflare":      {520, `error code: 520`},
		"521 cloudflare":      {521, `error code: 521`},
		"522 cloudflare":      {522, `error code: 522`},
		"523 cloudflare":      {523, `error code: 523`},
		"524 cloudflare":      {524, `error code: 524`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			_, _, err := node.New(srv.URL).Block(context.Background(), 100)
			require.ErrorIs(t, err, node.ErrUnavailable)
			assert.NotErrorIs(t, err, node.ErrPruned)
		})
	}
}

func TestHeightErrorsAreNotErrUnavailable(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"above tip":              {http.StatusInternalServerError, `{"jsonrpc":"2.0","id":-1,"error":{"code":-32603,"message":"Internal error","data":"height 200 must be less than or equal to the current blockchain height 100"}}`},
		"rpc error behind a 503": {http.StatusServiceUnavailable, `{"jsonrpc":"2.0","id":-1,"error":{"code":-32603,"message":"Internal error","data":"something else"}}`},
		"not json":               {http.StatusInternalServerError, `<html>oops</html>`},
		"525 not an outage":      {525, `error code: 525`},
		"bad result":             {http.StatusOK, `{"jsonrpc":"2.0","id":-1,"result":{"block":{"header":{"height":"x"}}}}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			_, _, err := node.New(srv.URL).Block(context.Background(), 200)
			require.Error(t, err)
			assert.NotErrorIs(t, err, node.ErrUnavailable)
		})
	}
}

func TestCancelledCallIsNotErrUnavailable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &mockDoer{err: context.Canceled}
	_, _, err := node.NewWithDoer("http://node:26657", d).Block(ctx, 5)
	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, node.ErrUnavailable)
}
