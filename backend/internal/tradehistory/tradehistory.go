// Package tradehistory serves the DEX's trades in display units, for the
// aggregator-compatible GET /aggregator/api/dex/history: order book fills
// (explorer.order_fills) and pool swaps (explorer.swaps), as the
// aggregator's one market_history table holds both. Trades are newest first
// by their stable trade id, which orders them by height and, within a
// block, as the chain executed them.
package tradehistory

import (
	"context"
	"slices"

	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/dexunits"
	"github.com/bze-alphateam/bze-scan/backend/internal/swaphistory"
)

// The aggregator's limits: DefaultLimit when none (or none above 0) is
// asked, MaxLimit at most.
const (
	DefaultLimit = 500
	MaxLimit     = 1000
)

// MaxPages bounds the store reads of one source: trades that cannot be
// converted are replaced by older ones, from at most MaxPages × limit rows.
const MaxPages = 10

// Filter selects trades. Every field is optional; the store applies those
// set.
type Filter struct {
	// MarketID is an order book market's id ("<base>/<quote>") or a pool's
	// ("<base>_<quote>"), matched exactly against both, as the aggregator
	// matches its market_history: IBC denoms put a "/" in pool ids too. ""
	// is every market and pool.
	MarketID string
	// Address is a fill's maker or taker, a swap's creator.
	Address string
	// Side is buy or sell; anything else is no filter.
	Side string
	// FromMs and ToMs bound the block time in unix milliseconds, both ends
	// inclusive. They apply only when both are above 0.
	FromMs, ToMs int64
	// FromHeight and ToHeight are the window's blocks, both inclusive:
	// Service.Trades looks them up once per request (Store.Heights) and the
	// store bounds its reads by them when both are above 0.
	FromHeight, ToHeight int64
}

// Window reports whether the time bounds apply, the aggregator's rule.
func (f Filter) Window() bool { return f.FromMs > 0 && f.ToMs > 0 }

// Sided reports whether Side filters.
func (f Filter) Sided() bool { return f.Side == "buy" || f.Side == "sell" }

// Fill is a stored order book fill in chain units.
type Fill struct {
	TradeID int64
	// TimeMs is the block time in unix milliseconds.
	TimeMs   int64
	MarketID string
	// Side is the resting order's, the chain's HistoryOrder.order_type.
	Side string
	// Price is the chain price, quote base units per base base unit.
	Price  string
	Amount string
	Maker  string
	Taker  string
}

// Store reads the stored trades; PGStore implements it. Fills and Swaps
// return at most limit rows matching f with a trade id below before (no
// bound when 0), newest first.
type Store interface {
	// Heights returns the lowest and highest height of the blocks whose
	// time, to the millisecond, is in [fromMs, toMs]; false when none is.
	Heights(ctx context.Context, fromMs, toMs int64) (lo, hi int64, ok bool, err error)
	Fills(ctx context.Context, f Filter, before int64, limit int) ([]Fill, error)
	Swaps(ctx context.Context, f Filter, before int64, limit int) ([]swaphistory.Swap, error)
}

// Trade is a fill or a swap in display units. A swap has no maker; its
// taker is the swap's creator.
type Trade struct {
	TradeID     int64
	TimeMs      int64
	Side        string
	Price       string
	BaseVolume  string
	QuoteVolume string
	Maker       string
	Taker       string
}

// Deps of the service; Log defaults to the standard logger.
type Deps struct {
	Store     Store
	Exponents dexunits.Exponents
	Log       log.FieldLogger
}

// Service converts the stored trades to display units.
type Service struct {
	deps Deps
}

// New returns a service over deps.
func New(deps Deps) *Service {
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	return &Service{deps: deps}
}

// Trades returns the last limit trades matching f, newest first, in display
// units: the fills and the swaps of f.MarketID and of every market of
// folded, merged. A trade that cannot be converted (a denom without a known
// exponent) is left out, as the aggregator never stores it, and older ones
// take its place. The time window is turned into heights once.
func (s *Service) Trades(ctx context.Context, f Filter, limit int, folded ...string) ([]Trade, error) {
	if f.Window() {
		if f.FromMs > f.ToMs {
			return []Trade{}, nil
		}
		lo, hi, ok, err := s.deps.Store.Heights(ctx, f.FromMs, f.ToMs)
		if err != nil {
			return nil, err
		}
		if !ok {
			return []Trade{}, nil
		}
		f.FromHeight, f.ToHeight = lo, hi
	}
	var lists [][]Trade
	for _, m := range append([]string{f.MarketID}, folded...) {
		fm := f
		fm.MarketID = m
		fills, err := collect(ctx, limit, func(before int64) ([]Fill, error) {
			return s.deps.Store.Fills(ctx, fm, before, limit)
		}, func(fl Fill) int64 { return fl.TradeID }, s.convertFills)
		if err != nil {
			return nil, err
		}
		swaps, err := collect(ctx, limit, func(before int64) ([]swaphistory.Swap, error) {
			return s.deps.Store.Swaps(ctx, fm, before, limit)
		}, func(sw swaphistory.Swap) int64 { return sw.TradeID }, s.convertSwaps)
		if err != nil {
			return nil, err
		}
		lists = append(lists, fills, swaps)
	}
	return Merge(limit, lists...), nil
}

// Merge merges lists of trades into one, newest first (by trade id, which
// holds the height), cut to limit. Empty is [], never nil.
func Merge(limit int, lists ...[]Trade) []Trade {
	out := []Trade{}
	for _, l := range lists {
		out = append(out, l...)
	}
	if len(lists) > 1 {
		slices.SortStableFunc(out, func(a, b Trade) int {
			switch {
			case a.TradeID > b.TradeID:
				return -1
			case a.TradeID < b.TradeID:
				return 1
			}
			return 0
		})
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// collect reads one source a page of limit rows at a time, at most MaxPages
// pages, until limit rows converted.
func collect[R any](ctx context.Context, limit int, read func(before int64) ([]R, error), id func(R) int64,
	convert func(context.Context, []R) ([]Trade, error)) ([]Trade, error) {
	out := []Trade{}
	before := int64(0)
	for range MaxPages {
		rows, err := read(before)
		if err != nil {
			return nil, err
		}
		got, err := convert(ctx, rows)
		if err != nil {
			return nil, err
		}
		for _, t := range got {
			if out = append(out, t); len(out) == limit {
				return out, nil
			}
		}
		if len(rows) < limit {
			break
		}
		before = id(rows[len(rows)-1])
	}
	return out, nil
}

func (s *Service) convertSwaps(ctx context.Context, swaps []swaphistory.Swap) ([]Trade, error) {
	var denoms []string
	for _, sw := range swaps {
		denoms = append(denoms, sw.Base, sw.Quote)
	}
	exps, err := s.exponents(ctx, denoms)
	if err != nil {
		return nil, err
	}
	out := make([]Trade, 0, len(swaps))
	for _, sw := range swaps {
		t, err := swaphistory.Convert(sw, exps)
		if err != nil {
			s.deps.Log.WithError(err).WithField("trade_id", sw.TradeID).Warn("dex history: swap left out")
			continue
		}
		out = append(out, Trade{TradeID: sw.TradeID, TimeMs: sw.TimeMs, Side: sw.Side, Price: t.Price,
			BaseVolume: t.BaseVolume, QuoteVolume: t.QuoteVolume, Maker: "", Taker: sw.Creator})
	}
	return out, nil
}

func (s *Service) convertFills(ctx context.Context, fills []Fill) ([]Trade, error) {
	var denoms []string
	seen := map[string]bool{}
	for _, fl := range fills {
		if !seen[fl.MarketID] {
			seen[fl.MarketID] = true
			for _, sp := range splits(fl.MarketID) {
				denoms = append(denoms, sp[0], sp[1])
			}
		}
	}
	exps, err := s.exponents(ctx, denoms)
	if err != nil {
		return nil, err
	}
	out := make([]Trade, 0, len(fills))
	for _, fl := range fills {
		t, err := convertFill(fl, exps)
		if err != nil {
			s.deps.Log.WithError(err).WithField("trade_id", fl.TradeID).Warn("dex history: fill left out")
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// convertFill computes the display values the aggregator stores for a
// fill: the price shifted by the exponents, the amount in base display
// units and the quote side as amount × price truncated to the quote's
// decimals.
func convertFill(fl Fill, exps map[string]int) (Trade, error) {
	base, quote, ok := market(fl.MarketID, exps)
	if !ok {
		return Trade{}, errUnknownMarket(fl.MarketID)
	}
	baseExp, quoteExp := exps[base], exps[quote]
	t := Trade{TradeID: fl.TradeID, TimeMs: fl.TimeMs, Side: fl.Side, Maker: fl.Maker, Taker: fl.Taker}
	var err error
	if t.Price, err = dexunits.TradePrice(fl.Price, baseExp, quoteExp); err != nil {
		return Trade{}, err
	}
	if t.BaseVolume, err = dexunits.Amount(fl.Amount, baseExp); err != nil {
		return Trade{}, err
	}
	if t.QuoteVolume, err = dexunits.QuoteAmount(t.BaseVolume, t.Price, quoteExp); err != nil {
		return Trade{}, err
	}
	return t, nil
}

type errUnknownMarket string

func (e errUnknownMarket) Error() string {
	return "market " + string(e) + ": no split into two denoms with known exponents"
}

// splits returns every way to cut a market id at a "/" into base and quote:
// denoms hold slashes themselves (factory/<creator>/<sub>, ibc/<hash>).
func splits(marketID string) [][2]string {
	var out [][2]string
	for i := range len(marketID) {
		if marketID[i] == '/' {
			out = append(out, [2]string{marketID[:i], marketID[i+1:]})
		}
	}
	return out
}

// market is the first cut of marketID whose two denoms both have a known
// exponent.
func market(marketID string, exps map[string]int) (base, quote string, ok bool) {
	for _, sp := range splits(marketID) {
		_, okB := exps[sp[0]]
		_, okQ := exps[sp[1]]
		if okB && okQ {
			return sp[0], sp[1], true
		}
	}
	return "", "", false
}

func (s *Service) exponents(ctx context.Context, denoms []string) (map[string]int, error) {
	slices.Sort(denoms)
	denoms = slices.Compact(denoms)
	if len(denoms) == 0 {
		return map[string]int{}, nil
	}
	return s.deps.Exponents.Exponents(ctx, denoms)
}
