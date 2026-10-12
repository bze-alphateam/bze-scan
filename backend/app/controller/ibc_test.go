package controller_test

import (
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

func newIBCAPI(r controller.AccountReader) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	e.GET("/accounts/:address/ibc", controller.NewAccountController(r, nil).IBCTransfers)
	return e
}

var ibcAt = time.Date(2026, 10, 11, 9, 0, 0, 0, time.FixedZone("x", 7200))

func str(s string) *string { return &s }

func noble() repository.Chain {
	return repository.Chain{ChainID: str("noble-1"), PrettyName: str("Noble"), LogoURL: str("https://logo/noble.png"),
		ExplorerAccountURL: str("https://mintscan.io/noble/account/${accountAddress}"),
		ExplorerTxURL:      str("https://mintscan.io/noble/tx/${txHash}")}
}

func pendingTransfer() repository.IBCTransfer {
	timeout := ibcAt.Add(time.Hour)
	return repository.IBCTransfer{ChannelID: "channel-3", Sequence: 5437, Direction: "out", Status: "pending",
		Sender: account, Receiver: "noble1r", Denom: "ibc/6490", Amount: "957617", CreatedHeight: 24999205,
		CreatedTxIndex: 0, CreatedTxHash: hashUpper, CreatedTime: ibcAt, TimeoutTime: &timeout, Counterparty: noble()}
}

func TestTxCarriesItsCrossChainTransfers(t *testing.T) {
	pending := pendingTransfer()
	f := &fakeReader{tx: &repository.Tx{TxSummary: repository.TxSummary{Height: 24999205, Hash: hashUpper, Success: true}},
		ibc: map[string][]repository.IBCTransfer{hashUpper: {pending}}}
	e := newAPI(f)

	rec := get(t, e, "/txs/"+hashUpper)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl), "the outcome lands later")
	tx := decode[map[string]any](t, rec)
	assert.Equal(t, []any{map[string]any{
		"channel_id": "channel-3", "sequence": float64(5437), "direction": "out", "status": "pending", "expired": false,
		"sender": account, "receiver": "noble1r", "denom": "ibc/6490", "amount": "957617", "memo": nil,
		"counterparty": map[string]any{"chain_id": "noble-1", "pretty_name": "Noble", "logo_url": "https://logo/noble.png",
			"explorer_account_url": "https://mintscan.io/noble/account/${accountAddress}",
			"explorer_tx_url":      "https://mintscan.io/noble/tx/${txHash}"},
		"created_height": float64(24999205), "created_tx_hash": hashUpper, "created_time": "2026-10-11T07:00:00Z",
		"timeout_height": nil, "timeout_time": "2026-10-11T08:00:00Z",
		"result_height": nil, "result_tx_hash": nil, "result_time": nil, "ack_error": nil,
	}}, tx["ibc_transfers"])

	h := int64(24999209)
	delivered := pending
	delivered.Status, delivered.ResultHeight, delivered.ResultTxHash, delivered.ResultTime = "delivered", &h, str("ACK"), &ibcAt
	f.ibc[hashUpper] = []repository.IBCTransfer{delivered}
	rec = get(t, e, "/txs/"+hashUpper)
	assert.Equal(t, controller.CacheIndexed, rec.Header().Get(echo.HeaderCacheControl), "settled: cached like any transaction")

	delete(f.ibc, hashUpper)
	rec = get(t, e, "/txs/"+hashUpper)
	assert.Nil(t, decode[map[string]any](t, rec)["ibc_transfers"], "null without cross-chain transfers")

	f.ibcErr = errors.New("db down")
	assert.Equal(t, http.StatusInternalServerError, get(t, e, "/txs/"+hashUpper).Code)
}

func TestAccountIBCPage(t *testing.T) {
	first := pendingTransfer()
	first.Expired = true
	second := pendingTransfer()
	second.ChannelID, second.Sequence, second.Direction, second.Status = "channel-10", 4, "in", "rejected"
	second.AckError, second.Counterparty = str("blocked inbound denom"), repository.Chain{ChainID: str("lumen")}
	third := pendingTransfer()
	third.CreatedHeight = 7
	repo := &fakeAccounts{ibc: []repository.IBCTransfer{first, second, third}}
	e := newIBCAPI(repo)

	rec := get(t, e, "/accounts/"+account+"/ibc?limit=2")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.Equal(t, 3, repo.gotLimit, "one more than the page")
	assert.Empty(t, repo.gotDirection, "both directions")
	page := decode[dto.List[dto.IBCTransfer]](t, rec)
	require.Len(t, page.Items, 2)
	assert.True(t, page.Items[0].Expired)
	assert.Equal(t, "rejected", page.Items[1].Status)
	assert.Nil(t, page.Items[1].Counterparty.PrettyName, "a chain the registry does not know")
	require.NotNil(t, page.NextCursor)
	assert.Equal(t, dto.EncodeIBCCursor(second), *page.NextCursor)

	rec = get(t, e, "/accounts/"+account+"/ibc?direction=in&cursor="+*page.NextCursor)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "in", repo.gotDirection)
	assert.Equal(t, &repository.IBCKey{Height: 24999205, TxIndex: 0, Channel: 10, Sequence: 4}, repo.gotIBCBefore)

	for _, q := range []string{"?direction=both", "?cursor=nope"} {
		assert.Equal(t, http.StatusBadRequest, get(t, e, "/accounts/"+account+"/ibc"+q).Code, q)
	}
	assert.Equal(t, http.StatusBadRequest, get(t, e, "/accounts/bzevaloper1x/ibc").Code)
	repo.ibcErr = errors.New("db down")
	assert.Equal(t, http.StatusInternalServerError, get(t, e, "/accounts/"+account+"/ibc").Code)
}

func TestIBCChannelsList(t *testing.T) {
	f := &fakeReader{channels: []repository.IBCChannel{
		{ChannelID: "channel-0", PortID: "transfer", State: str("open"), ClientStatus: str("expired"),
			ClientID: str("07-tendermint-2"), ConnectionID: str("connection-0"), CounterpartyChannelID: str("channel-340"), UpdatedAt: ibcAt,
			Counterparty: repository.Chain{ChainID: str("osmosis-1")}},
	}}
	rec := get(t, newAPI(f), "/ibc/channels")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"items":[{"channel_id":"channel-0","port_id":"transfer","state":"open","client_status":"expired",
		"client_id":"07-tendermint-2","connection_id":"connection-0","counterparty_channel_id":"channel-340",
		"counterparty":{"chain_id":"osmosis-1","pretty_name":null,"logo_url":null,"explorer_account_url":null,
		"explorer_tx_url":null},
		"escrow_address":"bze1a53udazy8ayufvy0s434pfwjcedzqv34tgwwee","updated_at":"2026-10-11T07:00:00Z"}],
		"next_cursor":null}`, rec.Body.String())

	f.ibcErr = errors.New("db down")
	assert.Equal(t, http.StatusInternalServerError, get(t, newAPI(f), "/ibc/channels").Code)
}
