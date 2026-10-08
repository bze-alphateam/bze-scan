package controller

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// Page sizes of the list routes.
const (
	DefaultLimit = 25
	MaxLimit     = 100
)

// Cache-Control values. Lists, search and errors change with every block;
// a block by height and a found transaction never change (finality is
// instant).
const (
	CacheNoStore   = "no-store"
	CacheImmutable = "public, max-age=31536000, immutable"
)

// Bech32 prefixes of the chain.
const (
	accountPrefix   = "bze"
	validatorPrefix = "bzevaloper"
)

// ExplorerReader is the read side of the explorer tables the controller
// needs. Implemented by repository.Explorer; missing rows are
// repository.ErrNotFound.
type ExplorerReader interface {
	Blocks(ctx context.Context, before *int64, limit int) ([]repository.BlockSummary, error)
	Block(ctx context.Context, height int64) (*repository.Block, error)
	Txs(ctx context.Context, before *repository.TxKey, success *bool, limit int) ([]repository.TxSummary, error)
	Tx(ctx context.Context, hash string) (*repository.Tx, error)
	BlockExists(ctx context.Context, height int64) (bool, error)
	TxExists(ctx context.Context, hash string) (bool, error)
	AccountIndexed(ctx context.Context, address string) (bool, error)
	ValidatorMoniker(ctx context.Context, operator string) (string, error)
}

// ExplorerController serves the block, transaction and search routes under
// /api/v1.
type ExplorerController struct {
	repo ExplorerReader
}

// NewExplorerController returns a controller reading from repo.
func NewExplorerController(repo ExplorerReader) *ExplorerController {
	return &ExplorerController{repo: repo}
}

// Blocks serves GET /api/v1/blocks?cursor&limit: blocks by height
// descending.
func (h *ExplorerController) Blocks(c *echo.Context) error {
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	var before *int64
	if keys, err := parseCursor(c, 1); err != nil {
		return err
	} else if keys != nil {
		before = &keys[0]
	}

	rows, err := h.repo.Blocks(c.Request().Context(), before, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.BlockSummary]{Items: make([]dto.BlockSummary, 0, min(len(rows), limit))}
	for i, b := range rows {
		if i == limit {
			next := dto.EncodeCursor(rows[i-1].Height)
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewBlockSummary(b))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// Block serves GET /api/v1/blocks/{height}: every column of the block and
// its transactions.
func (h *ExplorerController) Block(c *echo.Context) error {
	height, err := parseHeight(c.Param("height"))
	if err != nil {
		return err
	}
	b, err := h.repo.Block(c.Request().Context(), height)
	if errors.Is(err, repository.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("block %d is not indexed", height))
	}
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheImmutable)
	return c.JSON(http.StatusOK, dto.NewBlock(b))
}

// Txs serves GET /api/v1/txs?cursor&limit&status=success|failed:
// transactions by height and index descending.
func (h *ExplorerController) Txs(c *echo.Context) error {
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	var before *repository.TxKey
	if keys, err := parseCursor(c, 2); err != nil {
		return err
	} else if keys != nil {
		before = &repository.TxKey{Height: keys[0], TxIndex: keys[1]}
	}
	var success *bool
	switch s := c.QueryParam("status"); s {
	case "":
	case "success", "failed":
		ok := s == "success"
		success = &ok
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "status must be success or failed")
	}

	rows, err := h.repo.Txs(c.Request().Context(), before, success, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.TxSummary]{Items: make([]dto.TxSummary, 0, min(len(rows), limit))}
	for i, t := range rows {
		if i == limit {
			next := dto.EncodeCursor(rows[i-1].Height, rows[i-1].TxIndex)
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewTxSummary(t))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// Tx serves GET /api/v1/txs/{hash}: every column of the transaction and its
// messages. A well-formed hash that is not indexed is a 404, which the UI
// shows as pending.
func (h *ExplorerController) Tx(c *echo.Context) error {
	hash, ok := normaliseHash(c.Param("hash"))
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "hash must be 64 hexadecimal characters")
	}
	t, err := h.repo.Tx(c.Request().Context(), hash)
	if errors.Is(err, repository.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("transaction %s is not indexed", hash))
	}
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheImmutable)
	return c.JSON(http.StatusOK, dto.NewTx(t))
}

// Search serves GET /api/v1/search?q=. The input decides what is looked
// up: digits a block, 64 hex characters a transaction, a bze1 address an
// account (always returned, with whether it is indexed), a bzevaloper1
// address a validator. No match is an empty list, not an error.
func (h *ExplorerController) Search(c *echo.Context) error {
	q := strings.TrimSpace(c.QueryParam("q"))
	if q == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "q is required")
	}
	ctx := c.Request().Context()
	results := []dto.SearchResult{}

	if hash, ok := normaliseHash(q); ok {
		found, err := h.repo.TxExists(ctx, hash)
		if err != nil {
			return err
		}
		if found {
			results = append(results, dto.SearchResult{Type: dto.ResultTransaction, ID: hash,
				Label: "Transaction " + hash[:8] + "…" + hash[len(hash)-8:]})
		}
	} else if height, err := strconv.ParseInt(q, 10, 64); err == nil && isDigits(q) && height > 0 {
		found, err := h.repo.BlockExists(ctx, height)
		if err != nil {
			return err
		}
		if found {
			id := strconv.FormatInt(height, 10)
			results = append(results, dto.SearchResult{Type: dto.ResultBlock, ID: id, Label: "Block " + id})
		}
	} else if hrp, addr, ok := bech32Address(q); ok {
		switch hrp {
		case accountPrefix:
			indexed, err := h.repo.AccountIndexed(ctx, addr)
			if err != nil {
				return err
			}
			results = append(results, dto.SearchResult{Type: dto.ResultAccount, ID: addr, Label: addr, Indexed: &indexed})
		case validatorPrefix:
			moniker, err := h.repo.ValidatorMoniker(ctx, addr)
			if err != nil && !errors.Is(err, repository.ErrNotFound) {
				return err
			}
			if err == nil {
				results = append(results, dto.SearchResult{Type: dto.ResultValidator, ID: addr, Label: moniker})
			}
		}
	}

	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.SearchResponse{Results: results})
}

func parseLimit(c *echo.Context) (int, error) {
	s := c.QueryParam("limit")
	if s == "" {
		return DefaultLimit, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > MaxLimit {
		return 0, echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("limit must be an integer from 1 to %d", MaxLimit))
	}
	return n, nil
}

// parseCursor returns the n keys of the cursor query parameter, or nil when
// there is none.
func parseCursor(c *echo.Context, n int) ([]int64, error) {
	s := c.QueryParam("cursor")
	if s == "" {
		return nil, nil
	}
	keys, err := dto.DecodeCursor(s, n)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "cursor is not valid")
	}
	return keys, nil
}

func parseHeight(s string) (int64, error) {
	h, err := strconv.ParseInt(s, 10, 64)
	if err != nil || !isDigits(s) || h < 1 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, "height must be a positive integer")
	}
	return h, nil
}

// normaliseHash accepts 64 hex characters in any case and returns them upper
// case, the form the explorer stores.
func normaliseHash(s string) (string, bool) {
	if len(s) != 64 {
		return "", false
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", false
	}
	return strings.ToUpper(s), true
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// bech32Address decodes a bech32 address and returns its prefix and its
// canonical lower-case form.
func bech32Address(s string) (string, string, bool) {
	hrp, _, err := bech32.DecodeAndConvert(s)
	if err != nil {
		return "", "", false
	}
	return hrp, strings.ToLower(s), true
}
