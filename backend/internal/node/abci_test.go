package node_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// abciNode answers /abci_query with code, log and value, and records the
// query string it received.
func abciNode(t *testing.T, code int, log string, value []byte, height int64) (*httptest.Server, *url.Values) {
	t.Helper()
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/abci_query", r.URL.Path)
		got = r.URL.Query()
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":-1,"result":{"response":{"code":%d,"log":%q,"info":"","index":"0","key":null,"value":%q,"proofOps":null,"height":"%d","codespace":"sdk"}}}`,
			code, log, base64.StdEncoding.EncodeToString(value), height)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestQueryEncodesTheRequestAndDecodesTheResponse(t *testing.T) {
	want := &banktypes.QueryBalanceResponse{Balance: &sdk.Coin{Denom: "ubze", Amount: sdkmath.NewInt(131537493190125)}}
	value, err := gogoproto.Marshal(want)
	require.NoError(t, err)
	srv, got := abciNode(t, 0, "", value, 23821224)

	req := &banktypes.QueryBalanceRequest{Address: "bze1fl48vsnmsdzcv85q5d2q4z5ajdha8yu3gl9ytq", Denom: "ubze"}
	var resp banktypes.QueryBalanceResponse
	require.NoError(t, node.New(srv.URL).Query(context.Background(), "/cosmos.bank.v1beta1.Query/Balance", req, &resp, 23821224))
	assert.Equal(t, "131537493190125", resp.Balance.Amount.String())

	data, err := gogoproto.Marshal(req)
	require.NoError(t, err)
	assert.Equal(t, `"/cosmos.bank.v1beta1.Query/Balance"`, got.Get("path"), "the path is quoted, as the URI form wants")
	assert.Equal(t, fmt.Sprintf("0x%x", data), got.Get("data"))
	assert.Equal(t, "23821224", got.Get("height"))
}

func TestQueryWithANonZeroCodeIsAnError(t *testing.T) {
	srv, _ := abciNode(t, 18, "invalid denom", nil, 17131436)
	var resp banktypes.QueryBalanceResponse
	err := node.New(srv.URL).Query(context.Background(), "/cosmos.staking.v1beta1.Query/Pool", &banktypes.QueryBalanceRequest{}, &resp, 17131436)
	require.Error(t, err)
	abciErr, ok := errors.AsType[*node.ABCIError](err)
	require.True(t, ok, err)
	assert.Equal(t, uint32(18), abciErr.Code)
	assert.Equal(t, "invalid denom", abciErr.Log)
	assert.Contains(t, err.Error(), "invalid denom")
}

func TestABCIQueryChecksTheHeight(t *testing.T) {
	srv, _ := abciNode(t, 0, "", nil, 99)
	_, err := node.New(srv.URL).ABCIQuery(context.Background(), "/x/Y", nil, 100)
	require.ErrorContains(t, err, "node answered height 99")

	_, err = node.New(srv.URL).ABCIQuery(context.Background(), "/x/Y", nil, 0)
	require.ErrorContains(t, err, "height must be positive", "height 0 would be the latest state")
}
