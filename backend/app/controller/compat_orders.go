package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/marketmerge"
	"github.com/bze-alphateam/bze-scan/backend/internal/orderbook"
)

// OrderBooks serves the DEX's live order books in display units;
// implemented by orderbook.Service.
type OrderBooks interface {
	Market(ctx context.Context, id string) (orderbook.Market, error)
	Book(ctx context.Context, id string) (*orderbook.Book, error)
}

// CompatOrdersController serves the aggregator-compatible GET
// /api/dex/orders.
type CompatOrdersController struct {
	books  OrderBooks
	merges marketmerge.Merges
	log    log.FieldLogger
}

// NewCompatOrdersController returns a controller over books; merges folds
// markets for the coingecko format. A nil lg is the standard logger.
func NewCompatOrdersController(books OrderBooks, merges marketmerge.Merges, lg log.FieldLogger) *CompatOrdersController {
	if lg == nil {
		lg = log.StandardLogger()
	}
	return &CompatOrdersController{books: books, merges: merges, log: lg}
}

// Orders serves GET /api/dex/orders?market_id=|ticker_id=&depth=&format= as
// the aggregator does: depth/2 price levels per side (the whole book for 0,
// the default), both sides by ascending price, in display units. ticker_id
// is a market id with "_" for "/". format=coingecko answers under the
// ticker id, with the book of the first market folded into it that has
// orders (COINGECKO_MERGED_MARKETS), else its own. Every error is a 400
// with the aggregator's messages; an unknown market is its "unknown error".
func (h *CompatOrdersController) Orders(c *echo.Context) error {
	q := c.Request().URL.Query()
	depth := 0
	if s := q.Get("depth"); s != "" {
		d, err := strconv.ParseInt(s, 10, 0)
		if err != nil {
			return c.JSON(http.StatusBadRequest, dto.CompatError{Message: dto.CompatInvalidRequest})
		}
		depth = int(d)
	}
	if depth < 0 {
		return c.JSON(http.StatusBadRequest, dto.CompatError{Message: "depth must be a positive number"})
	}
	// The aggregator's checks, lengths included: a one-character id is no id.
	marketID, tickerID := q.Get("market_id"), q.Get("ticker_id")
	if len(marketID) <= 1 && len(tickerID) <= 1 {
		return c.JSON(http.StatusBadRequest, dto.CompatError{Message: "please provide market_id or ticker_id"})
	}
	if marketID == "" {
		marketID = strings.ReplaceAll(tickerID, "_", "/")
	}

	ctx := c.Request().Context()
	market, err := h.books.Market(ctx, marketID)
	if err != nil {
		return h.fail(c, err, marketID)
	}
	var book *orderbook.Book
	if q.Get("format") == dto.CompatFormatCoinGecko {
		book, err = h.coingeckoBook(ctx, market.ID)
	} else {
		book, err = h.books.Book(ctx, market.ID)
	}
	if err != nil {
		return h.fail(c, err, marketID)
	}

	bids, asks := book.Depth(depth)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	if q.Get("format") == dto.CompatFormatCoinGecko {
		return c.JSON(http.StatusOK, dto.CompatCoinGeckoOrders{
			TickerID:  market.Base + "_" + market.Quote,
			Timestamp: timestamp,
			Bids:      dto.NewCompatCoinGeckoLevels(bids),
			Asks:      dto.NewCompatCoinGeckoLevels(asks),
		})
	}
	return c.JSON(http.StatusOK, dto.CompatOrders{
		MarketID:  market.ID,
		Timestamp: timestamp,
		Bids:      dto.NewCompatLevels(bids),
		Asks:      dto.NewCompatLevels(asks),
	})
}

// coingeckoBook is the book of the first market folded into reported that
// has orders, else reported's own. Books are never mixed: a mixed one could
// cross. A folded market the chain does not have counts as empty.
func (h *CompatOrdersController) coingeckoBook(ctx context.Context, reported string) (*orderbook.Book, error) {
	for _, folded := range h.merges.FoldedInto(reported) {
		book, err := h.books.Book(ctx, folded)
		if errors.Is(err, orderbook.ErrUnknownMarket) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !book.Empty() {
			return book, nil
		}
	}
	return h.books.Book(ctx, reported)
}

func (h *CompatOrdersController) fail(c *echo.Context, err error, marketID string) error {
	lg := h.log.WithError(err).WithField("market", marketID)
	if errors.Is(err, orderbook.ErrUnknownMarket) {
		lg.Info("dex orders: unknown market")
	} else {
		lg.Error("dex orders")
	}
	return c.JSON(http.StatusBadRequest, dto.CompatError{Message: dto.CompatUnknownError})
}
