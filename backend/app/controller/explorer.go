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
	// Transfers returns the transfers of the transaction at (height,
	// txIndex), or of the block itself for repository.BlockTxIndex.
	Transfers(ctx context.Context, height, txIndex int64) ([]repository.Transfer, error)
	// BlockEvents returns up to limit stored events of the block at height
	// after seq after (from the first when negative).
	BlockEvents(ctx context.Context, height, after int64, limit int) ([]repository.BlockEvent, error)
	// TxPosition returns the height and the index in the block of a
	// transaction.
	TxPosition(ctx context.Context, hash string) (height, index int64, err error)
	BlockExists(ctx context.Context, height int64) (bool, error)
	TxExists(ctx context.Context, hash string) (bool, error)
	AccountIndexed(ctx context.Context, address string) (bool, error)
	ValidatorMoniker(ctx context.Context, operator string) (string, error)
	// Validators lists the validators of status (all when nil) in rank
	// order, after the list position after.
	Validators(ctx context.Context, status *string, after int64, limit int) ([]repository.ValidatorSummary, error)
	Validator(ctx context.Context, operator string) (*repository.ValidatorDetail, error)
	// ValidatorBlocks lists the blocks proposed by consensus address cons,
	// below before when it is set.
	ValidatorBlocks(ctx context.Context, cons string, before *int64, limit int) ([]repository.BlockSummary, error)
	ValidatorEvents(ctx context.Context, operator string, limit int) ([]repository.ValidatorEvent, error)
	VotesOf(ctx context.Context, account string, limit int) ([]repository.ValidatorVote, error)
	// SearchLabels returns the labels whose name contains q, any case.
	SearchLabels(ctx context.Context, q string, limit int) ([]repository.Label, error)
	// SearchValidators returns the validators whose moniker contains q, any
	// case.
	SearchValidators(ctx context.Context, q string, limit int) ([]repository.ValidatorMatch, error)
}

// Search by name: matches of each kind returned, and the shortest text
// searched.
const (
	SearchNameMatches = 5
	SearchNameMinLen  = 2
)

// BlockEventsInline is how many events the block route carries; the
// events route pages through the rest.
const BlockEventsInline = MaxLimit

// Sizes of the lists of the validator page.
const (
	ValidatorRecentBlocks = 10
	ValidatorRecentEvents = 20
	ValidatorRecentVotes  = 20
)

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

// Block serves GET /api/v1/blocks/{height}: every column of the block, its
// transactions, its own transfers and its first BlockEventsInline events.
func (h *ExplorerController) Block(c *echo.Context) error {
	height, err := parseHeight(c.Param("height"))
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	b, err := h.repo.Block(ctx, height)
	if errors.Is(err, repository.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("block %d is not indexed", height))
	}
	if err != nil {
		return err
	}
	transfers, err := h.repo.Transfers(ctx, height, repository.BlockTxIndex)
	if err != nil {
		return err
	}
	events, err := h.repo.BlockEvents(ctx, height, -1, BlockEventsInline+1)
	if err != nil {
		return err
	}
	var eventsNext *string
	if len(events) > BlockEventsInline {
		events = events[:BlockEventsInline]
		next := dto.EncodeCursor(events[len(events)-1].Seq)
		eventsNext = &next
	}
	// The proposer's name is the one field of a block that can still
	// change: it is null until the state sync knows the validator.
	cache := CacheImmutable
	if b.ProposerConsAddress != nil && b.Proposer == nil {
		cache = CacheNoStore
	}
	c.Response().Header().Set(echo.HeaderCacheControl, cache)
	return c.JSON(http.StatusOK, dto.NewBlock(b, transfers, events, eventsNext))
}

// BlockEvents serves GET /api/v1/blocks/{height}/events?cursor&limit: the
// block's stored events in order. An indexed block's events never change.
func (h *ExplorerController) BlockEvents(c *echo.Context) error {
	height, err := parseHeight(c.Param("height"))
	if err != nil {
		return err
	}
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	after := int64(-1)
	if keys, err := parseCursor(c, 1); err != nil {
		return err
	} else if keys != nil {
		after = keys[0]
	}
	ctx := c.Request().Context()
	ok, err := h.repo.BlockExists(ctx, height)
	if err != nil {
		return err
	}
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("block %d is not indexed", height))
	}

	rows, err := h.repo.BlockEvents(ctx, height, after, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.BlockEvent]{Items: make([]dto.BlockEvent, 0, min(len(rows), limit))}
	for i, e := range rows {
		if i == limit {
			next := dto.EncodeCursor(rows[i-1].Seq)
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewBlockEvent(e))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheImmutable)
	return c.JSON(http.StatusOK, resp)
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

// Tx serves GET /api/v1/txs/{hash}: every column of the transaction, its
// messages and its transfers. A well-formed hash that is not indexed is a 404, which the UI
// shows as pending.
func (h *ExplorerController) Tx(c *echo.Context) error {
	hash, ok := normaliseHash(c.Param("hash"))
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "hash must be 64 hexadecimal characters")
	}
	ctx := c.Request().Context()
	t, err := h.repo.Tx(ctx, hash)
	if errors.Is(err, repository.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("transaction %s is not indexed", hash))
	}
	if err != nil {
		return err
	}
	transfers, err := h.repo.Transfers(ctx, t.Height, t.TxIndex)
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheImmutable)
	return c.JSON(http.StatusOK, dto.NewTx(t, transfers))
}

// Validators serves GET
// /api/v1/validators?status=bonded|unbonding|unbonded|all&cursor&limit: the
// bonded validators by rank, then the others by tokens. status defaults to
// all.
func (h *ExplorerController) Validators(c *echo.Context) error {
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	var after int64
	if keys, err := parseCursor(c, 1); err != nil {
		return err
	} else if keys != nil {
		after = keys[0]
	}
	var status *string
	switch s := c.QueryParam("status"); s {
	case "", "all":
	case "bonded", "unbonding", "unbonded":
		status = &s
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "status must be bonded, unbonding, unbonded or all")
	}

	rows, err := h.repo.Validators(c.Request().Context(), status, after, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.Validator]{Items: make([]dto.Validator, 0, min(len(rows), limit))}
	for i, v := range rows {
		if i == limit {
			next := dto.EncodeCursor(rows[i-1].Position)
			resp.NextCursor = &next
			break
		}
		resp.Items = append(resp.Items, dto.NewValidator(v))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// Validator serves GET /api/v1/validators/{operator}: the validator, its
// last proposed blocks, its last events and its owner's last votes.
func (h *ExplorerController) Validator(c *echo.Context) error {
	ctx := c.Request().Context()
	v, err := h.validator(c)
	if err != nil {
		return err
	}
	var blocks []repository.BlockSummary
	if v.ConsensusAddress != nil {
		if blocks, err = h.repo.ValidatorBlocks(ctx, *v.ConsensusAddress, nil, ValidatorRecentBlocks); err != nil {
			return err
		}
	}
	events, err := h.repo.ValidatorEvents(ctx, v.OperatorAddress, ValidatorRecentEvents)
	if err != nil {
		return err
	}
	votes, err := h.repo.VotesOf(ctx, v.AccountAddress, ValidatorRecentVotes)
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.NewValidatorDetail(v, blocks, events, votes))
}

// ValidatorBlocks serves GET /api/v1/validators/{operator}/blocks?cursor&limit:
// the blocks the validator proposed, by height descending.
func (h *ExplorerController) ValidatorBlocks(c *echo.Context) error {
	v, err := h.validator(c)
	if err != nil {
		return err
	}
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
	resp := dto.List[dto.BlockSummary]{Items: []dto.BlockSummary{}}
	if v.ConsensusAddress != nil {
		rows, err := h.repo.ValidatorBlocks(c.Request().Context(), *v.ConsensusAddress, before, limit+1)
		if err != nil {
			return err
		}
		for i, b := range rows {
			if i == limit {
				next := dto.EncodeCursor(rows[i-1].Height)
				resp.NextCursor = &next
				break
			}
			resp.Items = append(resp.Items, dto.NewBlockSummary(b))
		}
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}

// validator loads the validator of the operator path parameter: 400 for
// anything but a bzevaloper1 address, 404 when it is not synced.
func (h *ExplorerController) validator(c *echo.Context) (*repository.ValidatorDetail, error) {
	hrp, operator, ok := bech32Address(c.Param("operator"))
	if !ok || hrp != validatorPrefix {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "operator must be a bzevaloper1 address")
	}
	v, err := h.repo.Validator(c.Request().Context(), operator)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("validator %s is not known", operator))
	}
	return v, err
}

// Search serves GET /api/v1/search?q=. The input decides what is looked
// up: digits a block, 64 hex characters a transaction, a bze1 address an
// account (always returned, with whether it is indexed), a bzevaloper1
// address a validator; any other text of at least SearchNameMinLen
// characters the validators by moniker and the labelled accounts by name.
// No match is an empty list, not an error.
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
	} else if len([]rune(q)) >= SearchNameMinLen {
		found, err := h.searchNames(ctx, q)
		if err != nil {
			return err
		}
		results = append(results, found...)
	}

	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.SearchResponse{Results: results})
}

// searchNames returns the validators whose moniker and the labelled
// accounts whose name contain q.
func (h *ExplorerController) searchNames(ctx context.Context, q string) ([]dto.SearchResult, error) {
	vals, err := h.repo.SearchValidators(ctx, q, SearchNameMatches)
	if err != nil {
		return nil, err
	}
	labels, err := h.repo.SearchLabels(ctx, q, SearchNameMatches)
	if err != nil {
		return nil, err
	}
	out := make([]dto.SearchResult, 0, len(vals)+len(labels))
	for _, v := range vals {
		out = append(out, dto.SearchResult{Type: dto.ResultValidator, ID: v.OperatorAddress, Label: v.Moniker})
	}
	for _, l := range labels {
		out = append(out, dto.SearchResult{Type: dto.ResultAccount, ID: l.Address, Label: l.Name})
	}
	return out, nil
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
