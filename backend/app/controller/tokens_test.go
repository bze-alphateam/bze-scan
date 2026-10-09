package controller_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
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

const honeyDenom = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney"

func strp(s string) *string { return &s }

// fakeTokens answers the token reads from fixed rows and records the
// arguments it got.
type fakeTokens struct {
	tokens    []repository.TokenSummary
	token     *repository.Token
	events    []repository.TokenEvent
	transfers []repository.DenomTransfer
	err       error

	gotKind   *string
	gotAfter  *repository.TokenKey
	gotBefore *repository.EventKey
	gotDenom  string
	gotLimit  int
}

func (f *fakeTokens) Tokens(_ context.Context, kind *string, after *repository.TokenKey, limit int) ([]repository.TokenSummary, error) {
	f.gotKind, f.gotAfter, f.gotLimit = kind, after, limit
	return f.tokens[:min(limit, len(f.tokens))], f.err
}

func (f *fakeTokens) Token(_ context.Context, denom string) (*repository.Token, error) {
	f.gotDenom = denom
	if f.err != nil {
		return nil, f.err
	}
	if f.token == nil || f.token.Denom != denom {
		return nil, repository.ErrNotFound
	}
	return f.token, nil
}

func (f *fakeTokens) TokenEvents(_ context.Context, _ string, before *repository.EventKey, limit int) ([]repository.TokenEvent, error) {
	f.gotBefore, f.gotLimit = before, limit
	return f.events[:min(limit, len(f.events))], nil
}

func (f *fakeTokens) DenomTransfers(_ context.Context, _ string, before *repository.EventKey, limit int) ([]repository.DenomTransfer, error) {
	f.gotBefore, f.gotLimit = before, limit
	return f.transfers[:min(limit, len(f.transfers))], nil
}

func tokenServer(f *fakeTokens) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	h := controller.NewTokenController(f)
	e.GET("/api/v1/tokens", h.Tokens)
	e.GET("/api/v1/tokens/:denom", h.Token)
	e.GET("/api/v1/tokens/:denom/events", h.TokenEvents)
	e.GET("/api/v1/tokens/:denom/transfers", h.TokenTransfers)
	e.GET("/api/v1/token", h.Token)
	e.GET("/api/v1/token/events", h.TokenEvents)
	e.GET("/api/v1/token/transfers", h.TokenTransfers)
	return e
}

func honeyToken() *repository.Token {
	return &repository.Token{
		TokenSummary: repository.TokenSummary{Denom: honeyDenom, Symbol: strp("HONEY"), Kind: "factory", Exponent: 6,
			Supply: strp("1000"), KindRank: 1, SortKey: "honey"},
		Creator:   strp("bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk"),
		Markets:   []string{honeyDenom + "/ubze"},
		UpdatedAt: time.Unix(10, 0),
		Metadata:  []byte(`{"display":"HONEY"}`),
	}
}

func tokenEvents(n int) []repository.TokenEvent {
	out := make([]repository.TokenEvent, n)
	for i := range out {
		out[i] = repository.TokenEvent{Height: int64(100 - i), TxIndex: -1, Seq: 0, Kind: "halted", Time: time.Unix(5, 0)}
	}
	return out
}

func TestTokensListsByKindAndPages(t *testing.T) {
	f := &fakeTokens{tokens: []repository.TokenSummary{
		{Denom: "ubze", Symbol: strp("BZE"), Kind: "native", Exponent: 6, KindRank: 0, SortKey: "bze"},
		{Denom: honeyDenom, Symbol: strp("HONEY"), Kind: "factory", Exponent: 6, KindRank: 1, SortKey: "honey"},
		{Denom: "factory/x/uzz", Kind: "factory", KindRank: 1, SortKey: "factory/x/uzz"},
	}}
	e := tokenServer(f)

	rec := get(t, e, "/api/v1/tokens?limit=2")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	page := decode[dto.List[dto.TokenSummary]](t, rec)
	require.Len(t, page.Items, 2)
	assert.Equal(t, "BZE", *page.Items[0].Symbol)
	assert.Nil(t, page.Items[0].HoldersCount, "the holders story fills it")
	assert.Nil(t, page.Items[0].PriceUSD)
	require.NotNil(t, page.NextCursor)
	assert.Equal(t, 3, f.gotLimit, "one more row tells whether a next page exists")

	rec = get(t, e, "/api/v1/tokens?kind=factory&cursor="+*page.NextCursor)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "factory", *f.gotKind)
	assert.Equal(t, &repository.TokenKey{KindRank: 1, SortKey: "honey", Denom: honeyDenom}, f.gotAfter, "the cursor holds the text keyset")

	for _, path := range []string{"/api/v1/tokens?kind=meme", "/api/v1/tokens?cursor=!!", "/api/v1/tokens?cursor=" + dto.EncodeCursor(1), "/api/v1/tokens?limit=0"} {
		rec := get(t, e, path)
		assert.Equal(t, http.StatusBadRequest, rec.Code, path)
		assert.Equal(t, "bad_request", errorCode(t, rec), path)
	}
}

func TestTokenResolvesAnEncodedOrAQueryDenom(t *testing.T) {
	f := &fakeTokens{token: honeyToken(), events: tokenEvents(3)}
	e := tokenServer(f)
	for _, path := range []string{
		"/api/v1/tokens/" + url.PathEscape(honeyDenom),
		"/api/v1/token?denom=" + url.QueryEscape(honeyDenom),
	} {
		rec := get(t, e, path)
		require.Equal(t, http.StatusOK, rec.Code, path+": "+rec.Body.String())
		assert.Equal(t, honeyDenom, f.gotDenom, path)
		assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
		tok := decode[dto.Token](t, rec)
		assert.Equal(t, "HONEY", *tok.Symbol)
		assert.Equal(t, []string{honeyDenom + "/ubze"}, tok.Markets)
		assert.JSONEq(t, `{"display":"HONEY"}`, string(tok.Metadata))
		assert.Len(t, tok.Events, 3)
		assert.Nil(t, tok.EventsNextCursor)
		assert.Nil(t, tok.Events[0].TxHash, "a block-level event")
		assert.JSONEq(t, `null`, string(tok.Events[0].Details))
	}

	rec := get(t, e, "/api/v1/tokens/"+url.PathEscape("factory/nobody/uzz"))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "not_found", errorCode(t, rec))
	for _, path := range []string{"/api/v1/token", "/api/v1/token?denom=" + strings.Repeat("u", 129)} {
		rec := get(t, e, path)
		assert.Equal(t, http.StatusBadRequest, rec.Code, path)
	}
}

func TestTokenCarriesTheNewestEventsAndACursor(t *testing.T) {
	f := &fakeTokens{token: honeyToken(), events: tokenEvents(controller.TokenEventsInline + 5)}
	e := tokenServer(f)
	tok := decode[dto.Token](t, get(t, e, "/api/v1/tokens/"+url.PathEscape(honeyDenom)))
	require.Len(t, tok.Events, controller.TokenEventsInline)
	require.NotNil(t, tok.EventsNextCursor)
	key, err := dto.DecodeEventCursor(*tok.EventsNextCursor)
	require.NoError(t, err)
	assert.Equal(t, &repository.EventKey{Height: 100 - controller.TokenEventsInline + 1, TxIndex: -1, Seq: 0}, key,
		"the block-level tx_index -1 survives the cursor")

	rec := get(t, e, "/api/v1/tokens/"+url.PathEscape(honeyDenom)+"/events?limit=2&cursor="+*tok.EventsNextCursor)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, key, f.gotBefore)
	page := decode[dto.List[dto.TokenEvent]](t, rec)
	assert.Len(t, page.Items, 2)
	assert.NotNil(t, page.NextCursor)
}

func TestTokenTransfersPageNewestFirst(t *testing.T) {
	f := &fakeTokens{token: honeyToken(), transfers: []repository.DenomTransfer{
		{Height: 9, TxIndex: 0, TxHash: strp("AB"), Time: time.Unix(9, 0),
			Transfer: repository.Transfer{Seq: 1, Kind: "transfer", Sender: strp("bze1a"), Recipient: strp("bze1b"), Denom: honeyDenom, Amount: "5"}},
		{Height: 8, TxIndex: -1, Time: time.Unix(8, 0),
			Transfer: repository.Transfer{Seq: 0, Kind: "mint", Recipient: strp("bze1b"), Denom: honeyDenom, Amount: "7"}},
	}}
	e := tokenServer(f)
	rec := get(t, e, "/api/v1/token/transfers?limit=1&denom="+url.QueryEscape(honeyDenom))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	page := decode[dto.List[dto.TokenTransfer]](t, rec)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "AB", *page.Items[0].TxHash)
	assert.Equal(t, "5", page.Items[0].Amount)
	require.NotNil(t, page.NextCursor)

	rec = get(t, e, "/api/v1/tokens/"+url.PathEscape(honeyDenom)+"/transfers?cursor="+*page.NextCursor)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, &repository.EventKey{Height: 9, TxIndex: 0, Seq: 1}, f.gotBefore)
	page = decode[dto.List[dto.TokenTransfer]](t, rec)
	assert.Nil(t, page.Items[1].TxHash, "a block-level mint")

	assert.Equal(t, http.StatusNotFound, get(t, e, "/api/v1/tokens/ufoo/transfers").Code, "an unknown denom")
	assert.Equal(t, http.StatusBadRequest, get(t, e, "/api/v1/tokens/ubze/transfers?cursor=x").Code)
}

func TestTokenReadFailuresAreInternal(t *testing.T) {
	e := tokenServer(&fakeTokens{err: errors.New("db down")})
	for _, path := range []string{"/api/v1/tokens", "/api/v1/tokens/ubze", "/api/v1/tokens/ubze/events"} {
		rec := get(t, e, path)
		assert.Equal(t, http.StatusInternalServerError, rec.Code, path)
	}
}
