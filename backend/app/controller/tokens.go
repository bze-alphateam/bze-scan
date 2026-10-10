package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// TokenEventsInline is how many of a denom's newest events the token page
// carries; the events route pages through the rest.
const TokenEventsInline = 20

// tokenKinds are the values of the kind filter.
var tokenKinds = []string{"native", "factory", "ibc", "lp", "unknown"}

// TokenReader is the read side of the explorer tables the token routes
// need. Implemented by repository.Explorer.
type TokenReader interface {
	Tokens(ctx context.Context, kind *string, after *repository.TokenKey, limit int) ([]repository.TokenSummary, error)
	Token(ctx context.Context, denom string) (*repository.Token, error)
	TokenEvents(ctx context.Context, denom string, before *repository.EventKey, limit int) ([]repository.TokenEvent, error)
	DenomTransfers(ctx context.Context, denom string, before *repository.EventKey, limit int) ([]repository.DenomTransfer, error)
	TokenHolders(ctx context.Context, denom string, after *repository.HolderKey, limit int) ([]repository.TokenHolder, error)
}

// TokenController serves the /api/v1/tokens routes. A denom contains "/"
// (factory/bze1…/uhoney, ibc/HASH), so the path routes take it URL-encoded
// (factory%2Fbze1…%2Fuhoney) and the /token routes take it as ?denom=, the
// chain's own convention.
type TokenController struct {
	repo TokenReader
}

// NewTokenController returns a controller over repo.
func NewTokenController(repo TokenReader) *TokenController {
	return &TokenController{repo: repo}
}

// Tokens serves GET /api/v1/tokens?kind&cursor&limit: every denom by kind
// (native, factory, ibc, lp, unknown) then symbol.
func (h *TokenController) Tokens(c *echo.Context) error {
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	var kind *string
	if k := c.QueryParam("kind"); k != "" {
		if !slices.Contains(tokenKinds, k) {
			return echo.NewHTTPError(http.StatusBadRequest, "kind must be one of "+strings.Join(tokenKinds, ", "))
		}
		kind = &k
	}
	var after *repository.TokenKey
	if s := c.QueryParam("cursor"); s != "" {
		if after, err = dto.DecodeTokenCursor(s); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "cursor is not valid")
		}
	}
	rows, err := h.repo.Tokens(c.Request().Context(), kind, after, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.TokenSummary]{Items: make([]dto.TokenSummary, 0, min(len(rows), limit))}
	for i, t := range rows {
		if i == limit {
			next := dto.EncodeTokenCursor(dto.TokenKeyOf(rows[i-1]))
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewTokenSummary(t))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// Token serves GET /api/v1/tokens/{denom} and /api/v1/token?denom=: the
// denom with its newest events. Supply and admin change, so it is never
// cached.
func (h *TokenController) Token(c *echo.Context) error {
	denom, err := denomParam(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	t, err := h.token(ctx, denom)
	if err != nil {
		return err
	}
	events, err := h.repo.TokenEvents(ctx, denom, nil, TokenEventsInline+1)
	if err != nil {
		return err
	}
	var next *string
	if len(events) > TokenEventsInline {
		events = events[:TokenEventsInline]
		n := dto.EncodeEventCursor(eventKey(events[TokenEventsInline-1]))
		next = &n
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.NewToken(t, events, next))
}

// TokenEvents serves GET /api/v1/tokens/{denom}/events?cursor&limit and
// /api/v1/token/events?denom=: the denom's history, newest first.
func (h *TokenController) TokenEvents(c *echo.Context) error {
	denom, limit, before, err := h.page(c)
	if err != nil {
		return err
	}
	rows, err := h.repo.TokenEvents(c.Request().Context(), denom, before, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.TokenEvent]{Items: make([]dto.TokenEvent, 0, min(len(rows), limit))}
	for i, e := range rows {
		if i == limit {
			next := dto.EncodeEventCursor(eventKey(rows[i-1]))
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewTokenEvent(e))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// TokenTransfers serves GET /api/v1/tokens/{denom}/transfers?cursor&limit
// and /api/v1/token/transfers?denom=: every move of the denom, newest first.
func (h *TokenController) TokenTransfers(c *echo.Context) error {
	denom, limit, before, err := h.page(c)
	if err != nil {
		return err
	}
	rows, err := h.repo.DenomTransfers(c.Request().Context(), denom, before, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.TokenTransfer]{Items: make([]dto.TokenTransfer, 0, min(len(rows), limit))}
	for i, x := range rows {
		if i == limit {
			prev := rows[i-1]
			next := dto.EncodeEventCursor(repository.EventKey{Height: prev.Height, TxIndex: prev.TxIndex, Seq: prev.Seq})
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewTokenTransfer(x))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// TokenHolders serves GET /api/v1/tokens/{denom}/holders?cursor&limit and
// /api/v1/token/holders?denom=: the last holders snapshot, largest balance
// first, with each one's rank and share of the supply.
func (h *TokenController) TokenHolders(c *echo.Context) error {
	denom, err := denomParam(c)
	if err != nil {
		return err
	}
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	var after *repository.HolderKey
	if s := c.QueryParam("cursor"); s != "" {
		if after, err = dto.DecodeHolderCursor(s); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "cursor is not valid")
		}
	}
	ctx := c.Request().Context()
	t, err := h.token(ctx, denom)
	if err != nil {
		return err
	}
	rows, err := h.repo.TokenHolders(ctx, denom, after, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.TokenHolder]{Items: make([]dto.TokenHolder, 0, min(len(rows), limit))}
	for i, row := range rows {
		if i == limit {
			next := dto.EncodeHolderCursor(rows[i-1])
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewTokenHolder(row, t.Supply))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// page parses the denom, the limit and the cursor of a token list route and
// checks the denom is known.
func (h *TokenController) page(c *echo.Context) (string, int, *repository.EventKey, error) {
	denom, err := denomParam(c)
	if err != nil {
		return "", 0, nil, err
	}
	limit, err := parseLimit(c)
	if err != nil {
		return "", 0, nil, err
	}
	var before *repository.EventKey
	if s := c.QueryParam("cursor"); s != "" {
		if before, err = dto.DecodeEventCursor(s); err != nil {
			return "", 0, nil, echo.NewHTTPError(http.StatusBadRequest, "cursor is not valid")
		}
	}
	if _, err := h.token(c.Request().Context(), denom); err != nil {
		return "", 0, nil, err
	}
	return denom, limit, before, nil
}

func (h *TokenController) token(ctx context.Context, denom string) (*repository.Token, error) {
	t, err := h.repo.Token(ctx, denom)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("denom %s is not known", denom))
	}
	return t, err
}

// denomParam is the denom of the path (URL-encoded) or, on the /token
// routes, of ?denom=.
func denomParam(c *echo.Context) (string, error) {
	raw := c.Param("denom")
	if raw == "" {
		raw = c.QueryParam("denom")
	} else if d, err := url.PathUnescape(raw); err == nil {
		raw = d
	} else {
		return "", echo.NewHTTPError(http.StatusBadRequest, "denom is not a valid URL-encoded value")
	}
	if raw == "" || len(raw) > 128 {
		return "", echo.NewHTTPError(http.StatusBadRequest, "denom is required, at most 128 characters")
	}
	return raw, nil
}

func eventKey(e repository.TokenEvent) repository.EventKey {
	return repository.EventKey{Height: e.Height, TxIndex: e.TxIndex, Seq: e.Seq}
}
