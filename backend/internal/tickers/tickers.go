// Package tickers serves the DEX's 24-hour tickers for the
// aggregator-compatible GET /aggregator/api/dex/tickers, and BZE's 24-hour
// change to the prices job: one ticker per order book market ("base/quote")
// and per liquidity pool ("base_quote"), in display units.
//
// A ticker is built the way the aggregator builds it: the bid and ask are
// the best levels of the live book (orderbook.Service), the volumes, high,
// low and open price come from the market's 5-minute candles starting in the
// last 24 hours (explorer.candles), the last price is the market's newest
// trade of the last 24 hours (explorer.order_fills and explorer.swaps,
// through tradehistory), and a pool's liquidity in USD is its LP token's
// internal USD price times the LP supply (see Service.USD). Unlike the
// aggregator, a market whose data fails keeps its ticker with the parts
// that answered, and prices keep every digit (BZE-133).
package tickers

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strings"
	"sync"
	"time"

	sdkmath "cosmossdk.io/math"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
	"github.com/bze-alphateam/bze-scan/backend/internal/dexunits"
	"github.com/bze-alphateam/bze-scan/backend/internal/orderbook"
	"github.com/bze-alphateam/bze-scan/backend/internal/tradehistory"
)

// Window is the tickers' period.
const Window = 24 * time.Hour

// CandleMinutes is the length of the candles the tickers sum, the
// aggregator's.
const CandleMinutes = 5

// candleLimit holds every 5-minute bucket starting in the window.
const candleLimit = int(Window/(CandleMinutes*time.Minute)) + 1

// Defaults of Config.
const (
	// DefaultTTL is how long the built tickers are served.
	DefaultTTL = 5 * time.Second
	// DefaultPriceTTL is how long an internal USD price is kept, the
	// aggregator's.
	DefaultPriceTTL = 60 * time.Second
	// DefaultWorkers bounds the markets built at once.
	DefaultWorkers = 8
	// DefaultBuildTimeout bounds one build, which runs whatever happens to
	// the request that started it.
	DefaultBuildTimeout = 30 * time.Second
	// DefaultNativeCoinGeckoID is the native denom's CoinGecko id, the
	// last fallback of its USD price.
	DefaultNativeCoinGeckoID = "bzedge"
)

// LPPrefix starts the older pools' LP token denoms, the rest being the pool
// id; newer pools name theirs ulp/<hash> (Pool.LPDenom gives either).
const LPPrefix = "ulp_"

// LPExponent is the LP tokens' decimals.
const LPExponent = 12

// Books reads the chain's order book markets and their live books;
// orderbook.Service implements it.
type Books interface {
	Markets(ctx context.Context) ([]orderbook.Market, error)
	Book(ctx context.Context, id string) (*orderbook.Book, error)
}

// Candles reads the stored candles; candles.PGReader implements it.
type Candles interface {
	// Candles returns market's candles of minutes starting at from or
	// later, the newest limit, newest first.
	Candles(ctx context.Context, market string, minutes int, from time.Time, limit int) ([]candles.Candle, error)
}

// Trades reads the stored trades in display units, newest first;
// tradehistory.Service implements it.
type Trades interface {
	Trades(ctx context.Context, f tradehistory.Filter, limit int, folded ...string) ([]tradehistory.Trade, error)
}

// Heights turns a time window into the heights of its blocks;
// tradehistory.PGStore implements it.
type Heights interface {
	Heights(ctx context.Context, fromMs, toMs int64) (lo, hi int64, ok bool, err error)
}

// Pools reads the chain's liquidity pools and the LP supplies; NodePools
// implements it.
type Pools interface {
	Pools(ctx context.Context) ([]Pool, error)
	// Supply is denom's total supply in base units.
	Supply(ctx context.Context, denom string) (sdkmath.Int, error)
}

// Pool is a liquidity pool: id "<base>_<quote>".
type Pool struct {
	ID, Base, Quote, LPDenom  string
	ReserveBase, ReserveQuote sdkmath.Int
}

// USDPrices reads CoinGecko's USD prices by id; coingecko.Feed implements
// it.
type USDPrices interface {
	USD(ctx context.Context) (map[string]string, error)
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Config tunes the service; the zero value is production.
type Config struct {
	// TTL is how long built tickers are served; zero is DefaultTTL.
	TTL time.Duration
	// PriceTTL is how long an internal USD price is kept; zero is
	// DefaultPriceTTL.
	PriceTTL time.Duration
	// Workers bounds the markets built at once; zero is DefaultWorkers.
	Workers int
	// BuildTimeout bounds one build; zero is DefaultBuildTimeout.
	BuildTimeout time.Duration
	// NativeDenom is the chain's own denom (ubze).
	NativeDenom string
	// USDCDenom is priced at $1 and anchors the other USD prices; empty
	// anchors none.
	USDCDenom string
	// NativeCoinGeckoID is the native denom's CoinGecko id; empty is
	// DefaultNativeCoinGeckoID.
	NativeCoinGeckoID string
}

// Deps of the service. CoinGecko may be nil (no fallback); Log defaults to
// the standard logger, Clock to the wall clock.
type Deps struct {
	Books     Books
	Candles   Candles
	Trades    Trades
	Heights   Heights
	Pools     Pools
	Exponents dexunits.Exponents
	CoinGecko USDPrices
	Log       log.FieldLogger
	Clock     Clock
}

// Ticker is one market's last 24 hours in display units. Prices are quote
// per base; zero when there is none.
type Ticker struct {
	MarketID, Base, Quote string
	// Pool tells a liquidity pool from an order book market.
	Pool                    bool
	LastPrice, OpenPrice    *big.Rat
	BaseVolume, QuoteVolume *big.Rat
	High, Low               *big.Rat
	Bid, Ask                *big.Rat
	// Change is the aggregator's: (last − open) / open in percent, rounded
	// to two decimals, 0 without a last or an open price.
	Change float32
	// LiquidityUSD is a pool's liquidity in USD; nil when it has no price
	// (and always for an order book market).
	LiquidityUSD *big.Rat
}

// Service builds the tickers; safe for concurrent use.
type Service struct {
	cfg  Config
	deps Deps

	// mu serialises the builds: callers during a build wait for it and
	// take its result.
	mu      sync.Mutex
	built   []Ticker
	builtAt time.Time
	// lastMarkets is the last market list read, served while the node
	// fails.
	lastMarkets []orderbook.Market

	pricesMu sync.Mutex
	prices   map[string]cachedPrice
}

type cachedPrice struct {
	price   *big.Rat
	expires time.Time
}

// New returns a service.
func New(cfg Config, deps Deps) *Service {
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultTTL
	}
	if cfg.PriceTTL <= 0 {
		cfg.PriceTTL = DefaultPriceTTL
	}
	if cfg.Workers <= 0 {
		cfg.Workers = DefaultWorkers
	}
	if cfg.BuildTimeout <= 0 {
		cfg.BuildTimeout = DefaultBuildTimeout
	}
	if cfg.NativeCoinGeckoID == "" {
		cfg.NativeCoinGeckoID = DefaultNativeCoinGeckoID
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}
	return &Service{cfg: cfg, deps: deps, prices: map[string]cachedPrice{}}
}

// Tickers returns every market's and pool's ticker, by market id, built at
// most TTL ago. The caller must not modify them. It fails only when neither
// the market list nor the pools can be read and none was read before. The
// build is shared by every caller waiting for it and cached, so it does not
// end with the request that started it: it runs on its own context,
// bounded by BuildTimeout.
func (s *Service) Tickers(ctx context.Context) ([]Ticker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.deps.Clock.Now()
	if s.built != nil && now.Sub(s.builtAt) < s.cfg.TTL {
		return s.built, nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.BuildTimeout)
	defer cancel()
	list, err := s.list(ctx)
	if err != nil {
		return nil, err
	}
	since, heights := s.window(ctx, now)
	out := make([]Ticker, len(list))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.cfg.Workers)
	for i, m := range list {
		g.Go(func() error {
			// A failed part is logged and left zero: the others still answer.
			out[i], _ = s.build(gctx, m, since, heights, true)
			return nil
		})
	}
	_ = g.Wait()
	s.built, s.builtAt = out, now
	return out, nil
}

// Ticker returns marketID's ticker without its liquidity; nil when no
// market or pool has that id. Unlike Tickers, any part that fails to read
// is an error.
func (s *Service) Ticker(ctx context.Context, marketID string) (*Ticker, error) {
	// The USD prices and the market list it reads are shared and cached.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.BuildTimeout)
	defer cancel()
	s.mu.Lock()
	list, err := s.list(ctx)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(list, func(m listed) bool { return m.id == marketID })
	if i < 0 {
		return nil, nil
	}
	since, heights := s.window(ctx, s.deps.Clock.Now())
	t, err := s.build(ctx, list[i], since, heights, false)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// listed is a market or a pool to build a ticker of.
type listed struct {
	id, base, quote string
	pool            *Pool
}

// list returns the markets and pools by id: the markets the node answers
// (else the last ones read), the pools of Pools. Called under mu.
func (s *Service) list(ctx context.Context) ([]listed, error) {
	markets, mErr := s.deps.Books.Markets(ctx)
	if mErr != nil {
		s.deps.Log.WithError(mErr).Warn("tickers: market list")
		markets = s.lastMarkets
	} else {
		s.lastMarkets = markets
	}
	pools, pErr := s.deps.Pools.Pools(ctx)
	if pErr != nil {
		s.deps.Log.WithError(pErr).Warn("tickers: pools")
	}
	if markets == nil && pErr != nil {
		return nil, errors.Join(mErr, pErr)
	}
	out := make([]listed, 0, len(markets)+len(pools))
	for _, m := range markets {
		out = append(out, listed{id: m.ID, base: m.Base, quote: m.Quote})
	}
	for i := range pools {
		p := &pools[i]
		out = append(out, listed{id: p.ID, base: p.Base, quote: p.Quote, pool: p})
	}
	slices.SortFunc(out, func(a, b listed) int { return strings.Compare(a.id, b.id) })
	return out, nil
}

// heightRange is the window's blocks; ok false when it has none or the
// lookup failed.
type heightRange struct {
	lo, hi int64
	ok     bool
	err    error
}

// window returns the start of the window and its blocks for the last price.
func (s *Service) window(ctx context.Context, now time.Time) (time.Time, heightRange) {
	since := now.Add(-Window)
	// The aggregator's last price is of a trade after since.
	lo, hi, ok, err := s.deps.Heights.Heights(ctx, since.UnixMilli()+1, now.UnixMilli())
	if err != nil {
		s.deps.Log.WithError(err).Warn("tickers: heights of the last 24 hours")
	}
	return since, heightRange{lo: lo, hi: hi, ok: ok, err: err}
}

// build computes m's ticker; every part that fails is logged and left zero,
// and the failures are returned.
func (s *Service) build(ctx context.Context, m listed, since time.Time, h heightRange, liquidity bool) (Ticker, error) {
	t := Ticker{MarketID: m.id, Base: m.base, Quote: m.quote, Pool: m.pool != nil,
		LastPrice: new(big.Rat), OpenPrice: new(big.Rat), BaseVolume: new(big.Rat), QuoteVolume: new(big.Rat),
		High: new(big.Rat), Low: new(big.Rat), Bid: new(big.Rat), Ask: new(big.Rat)}
	lg := s.deps.Log.WithField("market", m.id)
	errs := []error{h.err}
	if m.pool == nil {
		if err := s.bidAsk(ctx, &t); err != nil {
			lg.WithError(err).Warn("tickers: order book")
			errs = append(errs, fmt.Errorf("order book of %s: %w", m.id, err))
		}
	}
	if err := s.candles(ctx, &t, since); err != nil {
		lg.WithError(err).Warn("tickers: candles")
		errs = append(errs, fmt.Errorf("candles of %s: %w", m.id, err))
	}
	last := false
	if h.ok {
		trades, err := s.deps.Trades.Trades(ctx, tradehistory.Filter{MarketID: m.id, FromHeight: h.lo, ToHeight: h.hi}, 1)
		if err != nil {
			lg.WithError(err).Warn("tickers: last trade")
			errs = append(errs, fmt.Errorf("last trade of %s: %w", m.id, err))
		} else if len(trades) > 0 {
			if p, ok := new(big.Rat).SetString(trades[0].Price); ok {
				t.LastPrice, last = p, true
			}
		}
	}
	if last {
		t.Change = change(t.OpenPrice, t.LastPrice)
	}
	if liquidity && m.pool != nil {
		t.LiquidityUSD = s.liquidity(ctx, m.pool)
	}
	return t, errors.Join(errs...)
}

// bidAsk sets the book's best levels.
func (s *Service) bidAsk(ctx context.Context, t *Ticker) error {
	b, err := s.deps.Books.Book(ctx, t.MarketID)
	if err != nil {
		return err
	}
	for _, side := range []struct {
		levels []orderbook.Level
		into   **big.Rat
	}{{b.Bids, &t.Bid}, {b.Asks, &t.Ask}} {
		if len(side.levels) == 0 {
			continue
		}
		p, ok := new(big.Rat).SetString(side.levels[0].Price)
		if !ok {
			return fmt.Errorf("level price %q", side.levels[0].Price)
		}
		*side.into = p
	}
	return nil
}

// candles sums the 5-minute candles starting at since or later the way the
// aggregator does: volumes added, the highest high, the lowest low (a zero
// low is replaced by the next one), the first candle's open.
func (s *Service) candles(ctx context.Context, t *Ticker, since time.Time) error {
	cs, err := s.deps.Candles.Candles(ctx, t.MarketID, CandleMinutes, since, candleLimit)
	if err != nil || len(cs) == 0 {
		return err
	}
	exps, err := s.deps.Exponents.Exponents(ctx, []string{t.Base, t.Quote})
	if err != nil {
		return err
	}
	baseExp, okB := exps[t.Base]
	quoteExp, okQ := exps[t.Quote]
	if !okB || !okQ {
		return fmt.Errorf("exponents of %s and %s not both known", t.Base, t.Quote)
	}
	// Oldest first: the reader answers newest first.
	ivs := make([]candles.Interval, len(cs))
	for i, c := range cs {
		iv, err := candles.Display(c, baseExp, quoteExp)
		if err != nil {
			return err
		}
		ivs[len(cs)-1-i] = iv
	}
	var open, high, low, base, quote big.Rat
	for i, iv := range ivs {
		var vals [5]big.Rat
		for j, str := range []string{iv.Open, iv.High, iv.Low, iv.BaseVolume, iv.QuoteVolume} {
			if _, ok := vals[j].SetString(str); !ok {
				return fmt.Errorf("candle %s: %q is not a number", iv.Start.Format(time.RFC3339), str)
			}
		}
		if i == 0 {
			open.Set(&vals[0])
		}
		if vals[1].Cmp(&high) > 0 {
			high.Set(&vals[1])
		}
		if vals[2].Cmp(&low) < 0 || low.Sign() == 0 {
			low.Set(&vals[2])
		}
		base.Add(&base, &vals[3])
		quote.Add(&quote, &vals[4])
	}
	t.OpenPrice, t.High, t.Low, t.BaseVolume, t.QuoteVolume = &open, &high, &low, &base, &quote
	return nil
}

// change is the aggregator's price change: (last − open) / open × 100,
// rounded to two decimals as a float32; 0 without a positive open.
func change(open, last *big.Rat) float32 {
	if open.Sign() <= 0 {
		return 0
	}
	c := new(big.Rat).Sub(last, open)
	c.Quo(c, open)
	c.Mul(c, big.NewRat(100, 1))
	f, _ := c.Float64()
	return float32(math.Round(f*100) / 100)
}

// liquidity is pool's LP token USD price times its LP supply in whole
// tokens; nil when either is not positive or cannot be read.
func (s *Service) liquidity(ctx context.Context, p *Pool) *big.Rat {
	price := s.USD(ctx, p.LPDenom)
	if price.Sign() <= 0 {
		return nil
	}
	supply, err := s.deps.Pools.Supply(ctx, p.LPDenom)
	if err != nil {
		s.deps.Log.WithError(err).WithField("denom", p.LPDenom).Warn("tickers: LP supply")
		return nil
	}
	whole := new(big.Rat).SetFrac(supply.BigInt(), pow10(LPExponent))
	return whole.Mul(whole, price)
}

// pow10 is 10^n.
func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}
