package params_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/params"
)

// fakeConn answers every Params call with an empty response, or an error
// for the methods in fail, and HaltedDenoms with halted or haltErr; it
// records the methods called and the halted denoms request.
type fakeConn struct {
	mu        sync.Mutex
	methods   []string
	fail      map[string]bool
	halted    []string
	haltErr   error
	haltedReq *tradebintypes.QueryHaltedDenomsRequest
}

func (c *fakeConn) Invoke(_ context.Context, method string, args, reply any, _ ...grpc.CallOption) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.methods = append(c.methods, method)
	if c.fail[method] {
		return errors.New("unavailable")
	}
	if method == "/bze.tradebin.Query/HaltedDenoms" {
		c.haltedReq, _ = args.(*tradebintypes.QueryHaltedDenomsRequest)
		if c.haltErr != nil {
			return c.haltErr
		}
		if r, ok := reply.(*tradebintypes.QueryHaltedDenomsResponse); ok {
			r.Denoms = c.halted
		}
	}
	return nil
}

func (c *fakeConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("no streams")
}

// fakeJSON renders a message as its proto name.
type fakeJSON struct{}

func (fakeJSON) ProtoJSON(msg gogoproto.Message) ([]byte, error) {
	return json.Marshal(map[string]string{"type": gogoproto.MessageName(msg)})
}

func TestEveryModuleIsReadAndAFailureLeavesOnlyItOut(t *testing.T) {
	conn := &fakeConn{fail: map[string]bool{"/bze.burner.Query/Params": true}}
	got, err := params.NewGRPC(conn, fakeJSON{}).Params(context.Background())
	require.ErrorContains(t, err, "burner params: unavailable")

	assert.ElementsMatch(t, []string{
		"/cosmos.staking.v1beta1.Query/Params", "/cosmos.mint.v1beta1.Query/Params",
		"/cosmos.distribution.v1beta1.Query/Params", "/cosmos.slashing.v1beta1.Query/Params",
		"/cosmos.gov.v1.Query/Params", "/bze.tradebin.Query/Params", "/bze.tokenfactory.Query/Params",
		"/bze.rewards.Query/Params", "/bze.burner.Query/Params", "/bze.cointrunk.Query/Params",
		"/bze.txfeecollector.Query/Params",
	}, conn.methods)
	assert.Len(t, got, len(params.Modules)-2, "burner failed; gov's empty answer carries no params")
	assert.NotContains(t, got, "burner")
	assert.JSONEq(t, `{"type":"cosmos.staking.v1beta1.Params"}`, string(got["staking"]))
	assert.JSONEq(t, `{"type":"bze.txfeecollector.Params"}`, string(got["txfeecollector"]))
}

func TestGovWithoutParamsIsAFailure(t *testing.T) {
	got, err := params.NewGRPC(&fakeConn{}, fakeJSON{}).Params(context.Background())
	require.ErrorContains(t, err, "gov params: no params in the answer", "an empty answer carries no params")
	assert.NotContains(t, got, "gov")
}

func TestHaltedDenomsAreReadInOnePage(t *testing.T) {
	conn := &fakeConn{halted: []string{"ibc/AAA", "factory/bze1x/usdc"}}
	got, err := params.NewGRPC(conn, fakeJSON{}).HaltedDenoms(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"ibc/AAA", "factory/bze1x/usdc"}, got)
	assert.Equal(t, []string{"/bze.tradebin.Query/HaltedDenoms"}, conn.methods)
	require.NotNil(t, conn.haltedReq.Pagination)
	assert.Equal(t, uint64(params.HaltedLimit), conn.haltedReq.Pagination.Limit)
	assert.Empty(t, conn.haltedReq.Pagination.Key)
}

func TestHaltedDenomsNoneIsAnEmptyList(t *testing.T) {
	got, err := params.NewGRPC(&fakeConn{}, fakeJSON{}).HaltedDenoms(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{}, got)

	old := &fakeConn{haltErr: status.Error(codes.Unimplemented, "unknown method")}
	got, err = params.NewGRPC(old, fakeJSON{}).HaltedDenoms(context.Background())
	require.NoError(t, err, "a node from before the halt has none")
	assert.Equal(t, []string{}, got)
}

func TestHaltedDenomsFailure(t *testing.T) {
	conn := &fakeConn{haltErr: status.Error(codes.Unavailable, "down")}
	_, err := params.NewGRPC(conn, fakeJSON{}).HaltedDenoms(context.Background())
	require.ErrorContains(t, err, "tradebin halted denoms")
}

type countingReader struct {
	mu          sync.Mutex
	calls       int
	val         map[string]json.RawMessage
	err         error
	haltedCalls int
	halted      []string
	haltErr     error
}

func (r *countingReader) HaltedDenoms(context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.haltedCalls++
	return r.halted, r.haltErr
}

func (r *countingReader) Params(context.Context) (map[string]json.RawMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.val, r.err
}

func TestCachedServesACompleteAnswerForItsTTL(t *testing.T) {
	now := time.Unix(0, 0)
	r := &countingReader{val: map[string]json.RawMessage{"staking": json.RawMessage(`{}`)}}
	c := params.NewCached(r, 0, func() time.Time { return now })

	for range 3 {
		got, err := c.Params(context.Background())
		require.NoError(t, err)
		assert.Contains(t, got, "staking")
	}
	assert.Equal(t, 1, r.calls)

	now = now.Add(params.DefaultTTL)
	_, err := c.Params(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, r.calls, "read again once the TTL passed")
}

func TestCachedReadsAPartialAnswerAgain(t *testing.T) {
	r := &countingReader{val: map[string]json.RawMessage{"staking": json.RawMessage(`{}`)}, err: errors.New("gov down")}
	c := params.NewCached(r, time.Minute, nil)
	got, err := c.Params(context.Background())
	require.Error(t, err)
	assert.Contains(t, got, "staking", "the modules that answered are served")
	_, _ = c.Params(context.Background())
	assert.Equal(t, 2, r.calls)
}

func TestCachedServesTheHaltedDenomsForItsTTL(t *testing.T) {
	now := time.Unix(0, 0)
	r := &countingReader{halted: []string{"ibc/AAA"}}
	c := params.NewCached(r, 0, func() time.Time { return now })

	for range 3 {
		got, err := c.HaltedDenoms(context.Background())
		require.NoError(t, err)
		assert.Equal(t, []string{"ibc/AAA"}, got)
	}
	assert.Equal(t, 1, r.haltedCalls)
	assert.Equal(t, 0, r.calls, "the parameters are cached apart")

	now = now.Add(params.DefaultTTL)
	_, err := c.HaltedDenoms(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, r.haltedCalls, "read again once the TTL passed")
}

func TestCachedReadsTheHaltedDenomsAgainAfterAFailure(t *testing.T) {
	r := &countingReader{haltErr: errors.New("down")}
	c := params.NewCached(r, time.Minute, nil)
	_, err := c.HaltedDenoms(context.Background())
	require.Error(t, err)
	_, _ = c.HaltedDenoms(context.Background())
	assert.Equal(t, 2, r.haltedCalls)
}
