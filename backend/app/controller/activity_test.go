package controller_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

func newActivityAPI(r controller.AccountReader) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	e.GET("/accounts/:address/activity", controller.NewAccountController(r, nil).Activity)
	return e
}

func activityRows() []repository.Activity {
	hash := "ABCD"
	moniker := "Vidulum"
	at := time.Date(2026, 10, 8, 18, 0, 0, 0, time.FixedZone("x", 3600))
	return []repository.Activity{
		{Height: 30, TxIndex: 2, Time: at, TxHash: &hash, Kind: "delegate", Category: "staking", IsSigner: true,
			Success: true, Counterparty: &valoper, CounterpartyMoniker: &moniker,
			Deltas:   json.RawMessage(`[{"denom":"ubze","amount":"-1002000"}]`),
			MsgTypes: []string{"/cosmos.staking.v1beta1.MsgDelegate"},
			Fee:      json.RawMessage(`[{"denom":"ubze","amount":"2000"}]`)},
		{Height: 30, TxIndex: -1, Time: at, Kind: "dex_fill", Category: "dex", Success: true,
			Deltas:   json.RawMessage(`[{"denom":"factory/x/uvdl","amount":"5"}]`),
			MsgTypes: []string{},
			Details:  json.RawMessage(`[{"type":"bze.tradebin.OrderExecutedEvent","attrs":{"maker":"bze1x"}}]`)},
		{Height: 29, TxIndex: 0, Time: at, TxHash: &hash, Kind: "send", Category: "received", Success: true,
			Deltas: json.RawMessage(`[]`), MsgTypes: []string{"/cosmos.bank.v1beta1.MsgSend"}},
	}
}

func TestActivityPage(t *testing.T) {
	bze := "BZE"
	repo := &fakeAccounts{activity: activityRows(), denoms: map[string]repository.Denom{"ubze": {Symbol: &bze, Exponent: 6}}}
	rec := get(t, newActivityAPI(repo), "/accounts/"+account+"/activity?limit=2&category=staking")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.Equal(t, 3, repo.gotLimit, "one more than the page, to know whether a next page exists")
	require.NotNil(t, repo.gotCategory)
	assert.Equal(t, "staking", *repo.gotCategory)
	assert.Nil(t, repo.gotBefore)
	assert.ElementsMatch(t, []string{"ubze", "factory/x/uvdl"}, repo.gotDenoms, "only the page's denoms")

	next := dto.EncodeActivityCursor(repository.Activity{Height: 30, TxIndex: -1})
	assert.JSONEq(t, `{"items":[
		{"time":"2026-10-08T17:00:00Z","height":30,"tx_hash":"ABCD","kind":"delegate","category":"staking",
		 "is_signer":true,"success":true,
		 "counterparty":{"address":"`+valoper+`","label":null,"moniker":"Vidulum"},
		 "deltas":[{"denom":"ubze","amount":"-1002000","symbol":"BZE","exponent":6}],
		 "msg_types":["/cosmos.staking.v1beta1.MsgDelegate"],"details":null,
		 "fee":[{"denom":"ubze","amount":"2000","symbol":"BZE","exponent":6}]},
		{"time":"2026-10-08T17:00:00Z","height":30,"tx_hash":null,"kind":"dex_fill","category":"dex",
		 "is_signer":false,"success":true,"counterparty":null,
		 "deltas":[{"denom":"factory/x/uvdl","amount":"5","symbol":null,"exponent":null}],
		 "msg_types":[],"details":[{"type":"bze.tradebin.OrderExecutedEvent","attrs":{"maker":"bze1x"}}],
		 "fee":null}
	],"next_cursor":"`+next+`"}`, rec.Body.String())

	// The next page starts after the block-level row.
	rec = get(t, newActivityAPI(repo), "/accounts/"+account+"/activity?cursor="+next)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, &repository.ActivityKey{Height: 30, TxIndex: -1}, repo.gotBefore)
	assert.Nil(t, repo.gotCategory, "no filter means every category")
	assert.Equal(t, controller.DefaultLimit+1, repo.gotLimit)
}

func TestActivityLastPageAndErrors(t *testing.T) {
	repo := &fakeAccounts{activity: activityRows()}
	rec := get(t, newActivityAPI(repo), "/accounts/"+account+"/activity")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"next_cursor":null`)

	for _, path := range []string{
		"/accounts/bzevaloper1x/activity",
		"/accounts/" + valoper + "/activity",
		"/accounts/" + account + "/activity?category=fees",
		"/accounts/" + account + "/activity?cursor=@@",
		"/accounts/" + account + "/activity?limit=0",
	} {
		assert.Equal(t, http.StatusBadRequest, get(t, newActivityAPI(repo), path).Code, path)
	}

	repo.activityErr = errors.New("db down")
	assert.Equal(t, http.StatusInternalServerError, get(t, newActivityAPI(repo), "/accounts/"+account+"/activity").Code)
	repo.activityErr, repo.denomsErr = nil, errors.New("db down")
	assert.Equal(t, http.StatusInternalServerError, get(t, newActivityAPI(repo), "/accounts/"+account+"/activity").Code)
}

func TestActivityCursorRoundTrip(t *testing.T) {
	for _, txIndex := range []int64{-1, 0, 7} {
		k, err := dto.DecodeActivityCursor(dto.EncodeActivityCursor(repository.Activity{Height: 12, TxIndex: txIndex}))
		require.NoError(t, err)
		assert.Equal(t, &repository.ActivityKey{Height: 12, TxIndex: txIndex}, k)
	}
	_, err := dto.DecodeActivityCursor(dto.EncodeCursor(1, 2, 3))
	assert.ErrorIs(t, err, dto.ErrInvalidCursor)
}
