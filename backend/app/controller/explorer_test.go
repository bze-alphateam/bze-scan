package controller_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

const (
	hashUpper = "E580BFA56DE28886E51DDB9BE50DD610C1C832C579CA08FCF9177B04D2F7B7B9"
	account   = "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw"
)

var valoper = bech32Of("bzevaloper", account)

// bech32Of re-encodes the key bytes of addr under prefix.
func bech32Of(prefix, addr string) string {
	_, bz, err := bech32.DecodeAndConvert(addr)
	if err != nil {
		panic(err)
	}
	out, err := bech32.ConvertAndEncode(prefix, bz)
	if err != nil {
		panic(err)
	}
	return out
}

func zeroAddress(prefix string) string {
	out, err := bech32.ConvertAndEncode(prefix, make([]byte, 20))
	if err != nil {
		panic(err)
	}
	return out
}

// fakeReader is an in-memory ExplorerReader that records the arguments of
// the list calls.
type fakeReader struct {
	blocks   []repository.BlockSummary
	txs      []repository.TxSummary
	block    *repository.Block
	tx       *repository.Tx
	accounts map[string]bool
	monikers map[string]string
	vals     []repository.ValidatorSummary
	err      error

	gotBefore  *int64
	gotTxKey   *repository.TxKey
	gotSuccess *bool
	gotLimit   int
	gotHash    string
	gotStatus  *string
	gotAfter   int64
}

func (f *fakeReader) Validators(_ context.Context, status *string, after int64, limit int) ([]repository.ValidatorSummary, error) {
	f.gotStatus, f.gotAfter, f.gotLimit = status, after, limit
	var out []repository.ValidatorSummary
	for _, v := range f.vals {
		if v.Position > after && len(out) < limit {
			out = append(out, v)
		}
	}
	return out, f.err
}

func (f *fakeReader) Blocks(_ context.Context, before *int64, limit int) ([]repository.BlockSummary, error) {
	f.gotBefore, f.gotLimit = before, limit
	return f.blocks[:min(limit, len(f.blocks))], f.err
}

func (f *fakeReader) Block(_ context.Context, height int64) (*repository.Block, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.block == nil || f.block.Height != height {
		return nil, repository.ErrNotFound
	}
	return f.block, nil
}

func (f *fakeReader) Txs(_ context.Context, before *repository.TxKey, success *bool, limit int) ([]repository.TxSummary, error) {
	f.gotTxKey, f.gotSuccess, f.gotLimit = before, success, limit
	return f.txs[:min(limit, len(f.txs))], f.err
}

func (f *fakeReader) Tx(_ context.Context, hash string) (*repository.Tx, error) {
	f.gotHash = hash
	if f.err != nil {
		return nil, f.err
	}
	if f.tx == nil || f.tx.Hash != hash {
		return nil, repository.ErrNotFound
	}
	return f.tx, nil
}

func (f *fakeReader) TxPosition(_ context.Context, hash string) (int64, int64, error) {
	f.gotHash = hash
	if f.err != nil {
		return 0, 0, f.err
	}
	if f.tx == nil || f.tx.Hash != hash {
		return 0, 0, repository.ErrNotFound
	}
	return f.tx.Height, f.tx.TxIndex, nil
}

func (f *fakeReader) BlockExists(_ context.Context, height int64) (bool, error) {
	return f.block != nil && f.block.Height == height, f.err
}

func (f *fakeReader) TxExists(_ context.Context, hash string) (bool, error) {
	return f.tx != nil && f.tx.Hash == hash, f.err
}

func (f *fakeReader) AccountIndexed(_ context.Context, address string) (bool, error) {
	return f.accounts[address], f.err
}

func (f *fakeReader) ValidatorMoniker(_ context.Context, operator string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	m, ok := f.monikers[operator]
	if !ok {
		return "", repository.ErrNotFound
	}
	return m, nil
}

func newAPI(r controller.ExplorerReader) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	h := controller.NewExplorerController(r)
	e.GET("/blocks", h.Blocks)
	e.GET("/blocks/:height", h.Block)
	e.GET("/txs", h.Txs)
	e.GET("/txs/:hash", h.Tx)
	e.GET("/validators", h.Validators)
	e.GET("/search", h.Search)
	return e
}

func get(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v), rec.Body.String())
	return v
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decode[middleware.ErrorBody](t, rec).Error.Code
}

func blocksDown(from int64, n int) []repository.BlockSummary {
	var out []repository.BlockSummary
	for i := range n {
		out = append(out, repository.BlockSummary{Height: from - int64(i), Time: time.Unix(1700000000, 0), Hash: "H"})
	}
	return out
}

func TestBlocksPaginates(t *testing.T) {
	f := &fakeReader{blocks: blocksDown(100, 5)}
	e := newAPI(f)

	rec := get(t, e, "/blocks?limit=2")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.Equal(t, 3, f.gotLimit, "one extra row tells whether a next page exists")
	assert.Nil(t, f.gotBefore)
	page := decode[dto.List[dto.BlockSummary]](t, rec)
	require.Len(t, page.Items, 2)
	assert.Equal(t, []int64{100, 99}, []int64{page.Items[0].Height, page.Items[1].Height})
	require.NotNil(t, page.NextCursor)
	keys, err := dto.DecodeCursor(*page.NextCursor, 1)
	require.NoError(t, err)
	assert.Equal(t, []int64{99}, keys, "the cursor is the last item's key")

	get(t, e, "/blocks?limit=2&cursor="+*page.NextCursor)
	require.NotNil(t, f.gotBefore)
	assert.Equal(t, int64(99), *f.gotBefore)
}

func TestBlocksLastPageHasNullCursor(t *testing.T) {
	rec := get(t, newAPI(&fakeReader{blocks: blocksDown(10, 2)}), "/blocks")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `null`, string(decodeRaw(t, rec)["next_cursor"]))
	assert.Len(t, decode[dto.List[dto.BlockSummary]](t, rec).Items, 2)

	rec = get(t, newAPI(&fakeReader{}), "/blocks")
	assert.JSONEq(t, `{"items":[],"next_cursor":null}`, rec.Body.String(), "an empty list is [] not null")
}

func decodeRaw(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	return decode[map[string]json.RawMessage](t, rec)
}

func TestLimitBounds(t *testing.T) {
	f := &fakeReader{}
	e := newAPI(f)

	get(t, e, "/blocks")
	assert.Equal(t, controller.DefaultLimit+1, f.gotLimit)
	get(t, e, "/txs?limit=100")
	assert.Equal(t, controller.MaxLimit+1, f.gotLimit)
	get(t, e, "/txs?limit=1")
	assert.Equal(t, 2, f.gotLimit)

	for _, path := range []string{"/blocks?limit=101", "/blocks?limit=0", "/blocks?limit=-1", "/txs?limit=ten", "/txs?limit=2.5"} {
		rec := get(t, e, path)
		assert.Equal(t, http.StatusBadRequest, rec.Code, path)
		assert.Equal(t, "bad_request", errorCode(t, rec), path)
	}
}

func TestMalformedCursorIsBadRequest(t *testing.T) {
	e := newAPI(&fakeReader{})
	for _, path := range []string{
		"/blocks?cursor=!!!", "/blocks?cursor=" + dto.EncodeCursor(1, 2), "/txs?cursor=" + dto.EncodeCursor(5),
	} {
		rec := get(t, e, path)
		assert.Equal(t, http.StatusBadRequest, rec.Code, path)
		assert.Equal(t, "bad_request", errorCode(t, rec), path)
	}
}

func TestBlockByHeight(t *testing.T) {
	signer := account
	f := &fakeReader{block: &repository.Block{
		BlockSummary: repository.BlockSummary{Height: 25000894, Time: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), Hash: "BH", TxCount: 1},
		Transactions: []repository.BlockTx{{TxIndex: 0, Hash: hashUpper, Success: true,
			MsgTypes: []string{"/cosmos.bank.v1beta1.MsgSend"}, Fee: json.RawMessage(`[{"denom":"ubze","amount":"2000"}]`), Signer: &signer}},
	}}
	rec := get(t, newAPI(f), "/blocks/25000894")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, controller.CacheImmutable, rec.Header().Get(echo.HeaderCacheControl))
	assert.JSONEq(t, `{
		"height": 25000894, "time": "2026-10-07T12:00:00Z", "hash": "BH", "tx_count": 1, "tx_failed_count": 0,
		"proposer_cons_address": null, "block_time_ms": null, "size_bytes": null,
		"minted": null, "inflation": null, "fees_distributed": null, "signatures_count": null, "signatures_power_pct": null,
		"transactions": [{"height": 25000894, "tx_index": 0, "hash": "`+hashUpper+`", "success": true,
			"msg_types": ["/cosmos.bank.v1beta1.MsgSend"], "fee": [{"denom":"ubze","amount":"2000"}], "signer": "`+account+`"}]
	}`, rec.Body.String())
}

func TestBlockValidation(t *testing.T) {
	e := newAPI(&fakeReader{})
	for _, h := range []string{"0", "-1", "abc", "1.5", "+5", "99999999999999999999"} {
		rec := get(t, e, "/blocks/"+h)
		assert.Equal(t, http.StatusBadRequest, rec.Code, h)
		assert.Equal(t, "bad_request", errorCode(t, rec), h)
	}

	rec := get(t, e, "/blocks/42")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "not_found", errorCode(t, rec))
	assert.Equal(t, "no-store", rec.Header().Get(echo.HeaderCacheControl), "a missing block may be indexed soon")
}

func TestTxsFiltersByStatus(t *testing.T) {
	f := &fakeReader{txs: []repository.TxSummary{{Height: 5, TxIndex: 1, Hash: "A"}, {Height: 5, TxIndex: 0, Hash: "B"}}}
	e := newAPI(f)

	rec := get(t, e, "/txs?limit=1&status=failed")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, f.gotSuccess)
	assert.False(t, *f.gotSuccess)
	page := decode[dto.List[dto.TxSummary]](t, rec)
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.NextCursor)

	get(t, e, "/txs?status=success&cursor="+*page.NextCursor)
	assert.True(t, *f.gotSuccess)
	assert.Equal(t, &repository.TxKey{Height: 5, TxIndex: 1}, f.gotTxKey)

	get(t, e, "/txs")
	assert.Nil(t, f.gotSuccess)
	assert.Nil(t, f.gotTxKey)

	rec = get(t, e, "/txs?status=pending")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTxByHash(t *testing.T) {
	f := &fakeReader{tx: &repository.Tx{
		TxSummary: repository.TxSummary{Height: 25000894, Hash: hashUpper, Success: true, MsgCount: 1},
		Signers:   []string{account},
		Messages: []repository.Message{{TypeURL: "/cosmos.bank.v1beta1.MsgSend",
			Body: json.RawMessage(`{"amount":[]}`), Events: json.RawMessage(`[{"type":"message","attrs":{}}]`)}},
	}}
	e := newAPI(f)

	rec := get(t, e, "/txs/"+strings.ToLower(hashUpper))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, hashUpper, f.gotHash, "hashes are normalised to upper case")
	assert.Equal(t, controller.CacheImmutable, rec.Header().Get(echo.HeaderCacheControl))
	tx := decode[dto.Tx](t, rec)
	assert.Equal(t, []string{account}, tx.Signers)
	require.Len(t, tx.Messages, 1)
	assert.JSONEq(t, `{"amount":[]}`, string(tx.Messages[0].Body))

	for _, h := range []string{"abc", hashUpper + "0", strings.Replace(hashUpper, "E", "G", 1)} {
		rec := get(t, e, "/txs/"+h)
		assert.Equal(t, http.StatusBadRequest, rec.Code, h)
	}

	rec = get(t, e, "/txs/"+strings.Repeat("0", 64))
	assert.Equal(t, http.StatusNotFound, rec.Code, "well-formed but not indexed: the UI shows it as pending")
	assert.Equal(t, "not_found", errorCode(t, rec))
	assert.Equal(t, "no-store", rec.Header().Get(echo.HeaderCacheControl))
}

func TestRepositoryFailureIsInternal(t *testing.T) {
	e := newAPI(&fakeReader{err: errors.New("connection refused to 10.0.0.1")})
	for _, path := range []string{"/blocks", "/blocks/1", "/txs", "/txs/" + hashUpper, "/search?q=1", "/search?q=" + account} {
		rec := get(t, e, path)
		assert.Equal(t, http.StatusInternalServerError, rec.Code, path)
		assert.Equal(t, "internal", errorCode(t, rec), path)
		assert.NotContains(t, rec.Body.String(), "10.0.0.1", path)
	}
}

func TestSearch(t *testing.T) {
	f := &fakeReader{
		block:    &repository.Block{BlockSummary: repository.BlockSummary{Height: 25000894}},
		tx:       &repository.Tx{TxSummary: repository.TxSummary{Hash: hashUpper}},
		accounts: map[string]bool{account: true},
		monikers: map[string]string{valoper: "Vidulum"},
	}
	e := newAPI(f)
	search := func(q string) []dto.SearchResult {
		t.Helper()
		rec := get(t, e, "/search?q="+q)
		require.Equal(t, http.StatusOK, rec.Code, q)
		assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
		return decode[dto.SearchResponse](t, rec).Results
	}
	yes, no := true, false

	assert.Equal(t, []dto.SearchResult{{Type: "block", ID: "25000894", Label: "Block 25000894"}}, search("%2025000894%20"))
	assert.Equal(t, []dto.SearchResult{{Type: "transaction", ID: hashUpper, Label: "Transaction E580BFA5…D2F7B7B9"}},
		search(strings.ToLower(hashUpper)))
	assert.Equal(t, []dto.SearchResult{{Type: "account", ID: account, Label: account, Indexed: &yes}}, search(account))
	assert.Equal(t, []dto.SearchResult{{Type: "account", ID: account, Label: account, Indexed: &yes}},
		search(strings.ToUpper(account)), "bech32 is case-insensitive")
	other := zeroAddress("bze")
	assert.Equal(t, []dto.SearchResult{{Type: "account", ID: other, Label: other, Indexed: &no}}, search(other),
		"an account is always returned")
	assert.Equal(t, []dto.SearchResult{{Type: "validator", ID: valoper, Label: "Vidulum"}}, search(valoper))

	for _, q := range []string{"24", "0", strings.Repeat("A", 64), zeroAddress("bzevaloper"),
		bech32Of("cosmos", account), "bze1notanaddress", account[:len(account)-1] + "x", "hello", "-5"} {
		assert.Empty(t, search(q), q)
	}

	rec := get(t, e, "/search?q=%20%20")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, http.StatusBadRequest, get(t, e, "/search").Code)
	assert.JSONEq(t, `{"results":[]}`, get(t, e, "/search?q=hello").Body.String())
}

func validatorRows(n int) []repository.ValidatorSummary {
	out := make([]repository.ValidatorSummary, n)
	for i := range out {
		rank, missed, window := int64(i+1), int64(16), int64(10000)
		pct := "4.54545"
		out[i] = repository.ValidatorSummary{
			Position: int64(i + 1), Rank: &rank, Moniker: fmt.Sprintf("val %d", i+1),
			OperatorAddress: fmt.Sprintf("bzevaloper%d", i+1), Tokens: "1000", VotingPowerPct: &pct,
			CommissionRate: "0.050000000000000000", MissedBlocks: &missed, SignedBlocksWindow: &window, Status: "bonded",
		}
	}
	return out
}

func TestValidatorsPaginatesInRankOrder(t *testing.T) {
	f := &fakeReader{vals: validatorRows(3)}
	e := newAPI(f)

	rec := get(t, e, "/validators?limit=2")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	page := decode[dto.List[dto.Validator]](t, rec)
	require.Len(t, page.Items, 2)
	assert.Equal(t, "val 1", page.Items[0].Moniker)
	assert.Equal(t, "99.84000", *page.Items[0].Uptime)
	assert.Nil(t, f.gotStatus, "all statuses by default")
	assert.Equal(t, 3, f.gotLimit, "one more row than the page, to know there is a next one")
	require.NotNil(t, page.NextCursor)

	page = decode[dto.List[dto.Validator]](t, get(t, e, "/validators?limit=2&cursor="+*page.NextCursor))
	assert.Equal(t, int64(2), f.gotAfter)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "val 3", page.Items[0].Moniker)
	assert.Nil(t, page.NextCursor)

	body := decodeRaw(t, get(t, e, "/validators"))
	var items []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body["items"], &items))
	for _, k := range []string{"rank", "moniker", "operator_address", "tokens", "voting_power_pct", "commission_rate", "uptime", "jailed", "status"} {
		assert.Contains(t, items[0], k)
	}
}

func TestValidatorsStatusFilter(t *testing.T) {
	f := &fakeReader{}
	e := newAPI(f)
	for _, s := range []string{"bonded", "unbonding", "unbonded"} {
		require.Equal(t, http.StatusOK, get(t, e, "/validators?status="+s).Code)
		require.NotNil(t, f.gotStatus)
		assert.Equal(t, s, *f.gotStatus)
	}
	require.Equal(t, http.StatusOK, get(t, e, "/validators?status=all").Code)
	assert.Nil(t, f.gotStatus)

	rec := get(t, e, "/validators?status=jailed")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "bad_request", errorCode(t, rec))
	assert.Equal(t, http.StatusBadRequest, get(t, e, "/validators?cursor=nope").Code)

	page := decode[dto.List[dto.Validator]](t, get(t, e, "/validators"))
	assert.NotNil(t, page.Items, "an empty list is [], not null")
}
