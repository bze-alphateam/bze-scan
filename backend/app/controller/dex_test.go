package controller_test

import (
	"errors"
	"net/http"
	"strings"
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

func newDexAPI(r controller.AccountReader) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	h := controller.NewAccountController(r, nil)
	e.GET("/accounts/:address/orders", h.Orders)
	e.GET("/accounts/:address/fills", h.Fills)
	return e
}

const (
	orderA = "000000000000000006704545"
	orderB = "000000000000000006704522"
)

var dexAt = time.Date(2026, 10, 11, 9, 0, 0, 0, time.FixedZone("x", 7200))

func orderRows() []repository.Order {
	hash := hashUpper
	msgID := "000000000000000000000004"
	closed := int64(31)
	return []repository.Order{
		{OrderID: orderA, MarketID: "ufoo/ubze", Owner: account, Side: "sell", Price: "223", Amount: "647259",
			FilledAmount: "0", Status: "open", CreatedHeight: 30, CreatedTime: dexAt, CreatedTxHash: &hash, MessageID: &msgID},
		{OrderID: orderB, MarketID: "ufoo/ubze", Owner: account, Side: "buy", Price: "0.0002", Amount: "5",
			FilledAmount: "5", Status: "filled", CreatedHeight: 30, CreatedTime: dexAt, ClosedHeight: &closed, ClosedTime: &dexAt},
		{OrderID: "000000000000000000000001", CreatedHeight: 2, CreatedTime: dexAt},
	}
}

func fillRows() []repository.OrderFill {
	hash := hashUpper
	return []repository.OrderFill{
		{Height: 31, Seq: 4, OrderID: orderB, MarketID: "ufoo/ubze", Side: "buy", Price: "0.0002", Amount: "5",
			Maker: account, Taker: "bze1taker", TakerTxHash: &hash, Time: dexAt},
		{Height: 30, Seq: 0, OrderID: orderA, Time: dexAt},
		{Height: 29, Seq: 9, OrderID: orderA, Time: dexAt},
	}
}

func TestAccountOrdersPage(t *testing.T) {
	repo := &fakeAccounts{orders: orderRows()}
	e := newDexAPI(repo)
	rec := get(t, e, "/accounts/"+account+"/orders?limit=2&status=open")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.Equal(t, 3, repo.gotLimit, "one more than the page")
	require.NotNil(t, repo.gotStatus)
	assert.Equal(t, "open", *repo.gotStatus)

	next := dto.EncodeOrderCursor(repository.Order{CreatedHeight: 30, OrderID: orderB})
	assert.JSONEq(t, `{"items":[
		{"order_id":"`+orderA+`","market_id":"ufoo/ubze","owner":"`+account+`","side":"sell","price":"223",
		 "amount":"647259","filled_amount":"0","status":"open","created_height":30,"created_time":"2026-10-11T07:00:00Z",
		 "created_tx_hash":"`+hashUpper+`","message_id":"000000000000000000000004","closed_height":null,"closed_time":null},
		{"order_id":"`+orderB+`","market_id":"ufoo/ubze","owner":"`+account+`","side":"buy","price":"0.0002",
		 "amount":"5","filled_amount":"5","status":"filled","created_height":30,"created_time":"2026-10-11T07:00:00Z",
		 "created_tx_hash":null,"message_id":null,"closed_height":31,"closed_time":"2026-10-11T07:00:00Z"}
	],"next_cursor":"`+next+`"}`, rec.Body.String())

	rec = get(t, e, "/accounts/"+account+"/orders?cursor="+next)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, &repository.OrderKey{Height: 30, OrderID: orderB}, repo.gotOrderBefore, "the id comes back zero-filled")
	assert.Nil(t, repo.gotStatus, "no filter means every status")
}

func TestAccountFillsPage(t *testing.T) {
	repo := &fakeAccounts{fills: fillRows()}
	e := newDexAPI(repo)
	rec := get(t, e, "/accounts/"+account+"/fills?limit=1")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	next := dto.EncodeFillCursor(repository.OrderFill{Height: 31, Seq: 4})
	assert.JSONEq(t, `{"items":[
		{"height":31,"time":"2026-10-11T07:00:00Z","order_id":"`+orderB+`","market_id":"ufoo/ubze","side":"buy",
		 "price":"0.0002","amount":"5","maker":"`+account+`","taker":"bze1taker","taker_tx_hash":"`+hashUpper+`",
		 "taker_message_id":null}
	],"next_cursor":"`+next+`"}`, rec.Body.String())

	rec = get(t, e, "/accounts/"+account+"/fills?cursor="+next)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, &repository.FillKey{Height: 31, Seq: 4}, repo.gotFillBefore)
	assert.NotContains(t, rec.Body.String(), `"next_cursor":"`, "three rows fit the default page")
}

func TestDexListsRejectBadInput(t *testing.T) {
	repo := &fakeAccounts{}
	e := newDexAPI(repo)
	for _, path := range []string{
		"/accounts/" + valoper + "/orders",
		"/accounts/bze1x/fills",
		"/accounts/" + account + "/orders?status=partial",
		"/accounts/" + account + "/orders?cursor=@@",
		"/accounts/" + account + "/fills?cursor=" + dto.EncodeCursor(1),
		"/accounts/" + account + "/fills?limit=0",
	} {
		assert.Equal(t, http.StatusBadRequest, get(t, e, path).Code, path)
	}
	repo.dexErr = errors.New("db down")
	for _, list := range []string{"orders", "fills"} {
		assert.Equal(t, http.StatusInternalServerError, get(t, e, "/accounts/"+account+"/"+list).Code, list)
	}
}

func TestOrderPage(t *testing.T) {
	o := orderRows()[1]
	f := &fakeReader{order: &o, fillCount: 250, fills: fillRows()[:1]}
	e := newAPI(f)
	rec := get(t, e, "/orders/6704522")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, orderB, f.gotOrder, "ids are zero-filled to 24 digits")
	assert.Equal(t, controller.OrderFillsInline, f.gotLimit)
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl), "an open order still changes")
	got := decode[dto.OrderDetail](t, rec)
	assert.Equal(t, "filled", got.Status)
	assert.Equal(t, int64(250), got.FillCount)
	require.Len(t, got.Fills, 1)
	assert.Equal(t, "bze1taker", got.Fills[0].Taker)

	assert.Equal(t, http.StatusNotFound, get(t, e, "/orders/1").Code)
	for _, id := range []string{"abc", strings.Repeat("1", 25), "-1"} {
		assert.Equal(t, http.StatusBadRequest, get(t, e, "/orders/"+id).Code, id)
	}
}

// The transaction carries its order book outcome; while an outcome is
// pending the answer must not be cached.
func TestTxCarriesItsOrderOutcome(t *testing.T) {
	tx := &repository.Tx{TxSummary: repository.TxSummary{Height: 30, TxIndex: 0, Hash: hashUpper, Success: true}}
	o := orderRows()[0]
	f := &fakeReader{tx: tx, dex: map[string]*repository.TxDex{hashUpper: {
		Placed: []repository.Order{o}, Fills: fillRows()[:1],
	}}}
	e := newAPI(f)
	rec := get(t, e, "/txs/"+hashUpper)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheIndexed, rec.Header().Get(echo.HeaderCacheControl))
	got := decode[dto.Tx](t, rec)
	require.NotNil(t, got.Dex)
	require.Len(t, got.Dex.Placed, 1)
	assert.Equal(t, orderA, got.Dex.Placed[0].OrderID)
	require.Len(t, got.Dex.Fills, 1)
	assert.Equal(t, []dto.Order{}, got.Dex.Cancelled)
	assert.False(t, got.Dex.Pending)

	f.dex[hashUpper] = &repository.TxDex{Pending: true}
	rec = get(t, e, "/txs/"+hashUpper)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.Contains(t, rec.Body.String(), `"dex":{"placed":[],"fills":[],"cancelled":[],"pending":true}`)

	f.dex[hashUpper] = &repository.TxDex{}
	rec = get(t, e, "/txs/"+hashUpper)
	assert.Contains(t, rec.Body.String(), `"dex":null`, "no order message, no outcome")

	f.dexErr = errors.New("db down")
	assert.Equal(t, http.StatusInternalServerError, get(t, e, "/txs/"+hashUpper).Code)
}

func TestOrderCursorsRoundTrip(t *testing.T) {
	k, err := dto.DecodeOrderCursor(dto.EncodeOrderCursor(repository.Order{CreatedHeight: 9, OrderID: orderA}))
	require.NoError(t, err)
	assert.Equal(t, &repository.OrderKey{Height: 9, OrderID: orderA}, k)
	fk, err := dto.DecodeFillCursor(dto.EncodeFillCursor(repository.OrderFill{Height: 9, Seq: 0}))
	require.NoError(t, err)
	assert.Equal(t, &repository.FillKey{Height: 9, Seq: 0}, fk)
	_, err = dto.DecodeOrderCursor(dto.EncodeCursor(1, 2, 3))
	assert.ErrorIs(t, err, dto.ErrInvalidCursor)
}
