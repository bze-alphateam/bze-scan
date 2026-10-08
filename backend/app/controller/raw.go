package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/rawcache"
)

// RawReader serves the node's raw by-height JSON; implemented by
// rawcache.Cache. A height no archive node serves is rawcache.ErrUpstream.
type RawReader interface {
	Get(ctx context.Context, route rawcache.Route, height int64) ([]byte, error)
	Tx(ctx context.Context, height int64, index int) (*rawcache.Tx, error)
}

// RawController serves the raw-JSON routes under /api/v1/raw. Heights are
// proxied whether the explorer has indexed them or not.
type RawController struct {
	raw  RawReader
	repo ExplorerReader
}

// NewRawController returns a controller serving raw from raw and resolving
// transaction hashes through repo.
func NewRawController(raw RawReader, repo ExplorerReader) *RawController {
	return &RawController{raw: raw, repo: repo}
}

// Block serves GET /api/v1/raw/block/{height}: the node's /block response.
func (h *RawController) Block(c *echo.Context) error {
	return h.route(c, rawcache.RouteBlock)
}

// BlockResults serves GET /api/v1/raw/block_results/{height}.
func (h *RawController) BlockResults(c *echo.Context) error {
	return h.route(c, rawcache.RouteBlockResults)
}

// Commit serves GET /api/v1/raw/commit/{height}.
func (h *RawController) Commit(c *echo.Context) error {
	return h.route(c, rawcache.RouteCommit)
}

// route answers the verbatim JSON-RPC response body of route at the height.
func (h *RawController) route(c *echo.Context, route rawcache.Route) error {
	height, err := parseHeight(c.Param("height"))
	if err != nil {
		return err
	}
	body, err := h.raw.Get(c.Request().Context(), route, height)
	if err != nil {
		return rawError(err)
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheImmutable)
	return c.Blob(http.StatusOK, echo.MIMEApplicationJSON, body)
}

// Tx serves GET /api/v1/raw/tx/{hash}: the raw transaction and its result,
// at the height and index the explorer indexed it.
func (h *RawController) Tx(c *echo.Context) error {
	hash, ok := normaliseHash(c.Param("hash"))
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "hash must be 64 hexadecimal characters")
	}
	ctx := c.Request().Context()
	height, index, err := h.repo.TxPosition(ctx, hash)
	if errors.Is(err, repository.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("transaction %s is not indexed", hash))
	}
	if err != nil {
		return err
	}
	tx, err := h.raw.Tx(ctx, height, int(index))
	if err != nil {
		return rawError(err)
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheImmutable)
	return c.JSON(http.StatusOK, dto.RawTx{Height: height, Index: index, Tx: tx.Tx, TxResult: tx.Result})
}

// rawError maps an archive failure to 502 upstream_error; anything else
// stays an internal error.
func rawError(err error) error {
	if errors.Is(err, rawcache.ErrUpstream) {
		return echo.NewHTTPError(http.StatusBadGateway, "the archive node did not serve the height").Wrap(err)
	}
	return err
}
