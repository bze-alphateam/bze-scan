// Package prices is the prices set of the state sync: every minute it prices
// every denom of explorer.denoms by the DEX price chain first (USDPricer:
// the USDC pool's spot price, else the last trade, over the USDC and BZE
// routes, as the dapps price) and by CoinGecko last, through the in-process
// feed that also serves /aggregator/api/prices, for the denoms whose
// chain-registry asset carries a priced id (BZE's own asset list first,
// then the origin chain's for an IBC denom). price_source records which
// step priced a denom. The native denom's 24-hour change comes from the DEX
// ticker of one BZE/USDC market (the liquidity pool), computed in process
// (package tickers) at most every ChangeTTL. A failed read keeps the
// previous values and is logged once per change of state, not every minute.
package prices

import (
	"context"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/tickers"
)

// DefaultInterval is the minute refresh.
const DefaultInterval = time.Minute

// ChangeTTL is how long a read 24-hour change is used before the ticker is
// read again.
const ChangeTTL = 5 * time.Minute

// DefaultWorkers bounds the denoms priced by the DEX price chain at once.
const DefaultWorkers = 8

// PriceDecimals is the decimals a DEX price is written with, the column's.
const PriceDecimals = 12

// Sources of a price, as price_source records them.
const (
	SourceDEX       = "dex"
	SourceCoinGecko = "coingecko"
)

// Prices reads USD prices; *coingecko.Feed satisfies it.
type Prices interface {
	// USD returns USD prices by CoinGecko id, as decimal strings.
	USD(ctx context.Context) (map[string]string, error)
}

// USDPricer prices a denom by the DEX price chain; *tickers.Service
// satisfies it.
type USDPricer interface {
	// USD is denom's USD price, zero when the chain has none (the set then
	// falls through to CoinGecko); nil when the pricer could not answer at
	// all, which keeps the denom's stored price.
	USD(ctx context.Context, denom string) *big.Rat
}

// Tickers computes the DEX tickers; *tickers.Service satisfies it.
type Tickers interface {
	// Ticker returns a market's ticker, nil when the market is not listed.
	Ticker(ctx context.Context, marketID string) (*tickers.Ticker, error)
}

// Snapshot is one write of the set.
type Snapshot struct {
	// ChainID is BZE's, whose asset list maps its own denoms.
	ChainID string
	// DEX are the DEX price chain's USD prices by denom, as decimal
	// strings: a denom listed takes its price with source dex.
	DEX map[string]string
	// Prices are USD prices by CoinGecko id: every denom not priced by
	// DEX whose registry asset's id is listed takes its price with source
	// coingecko, the others are cleared.
	Prices map[string]string
	// Kept are the denoms the DEX pricer could not answer: their price
	// columns are left as they are.
	Kept []string
	// ChangeDenom's price_change_24h_pct becomes Change (nil clears it),
	// unless KeepChange.
	ChangeDenom string
	Change      *string
	KeepChange  bool
}

// Store persists the set.
type Store interface {
	// Denoms lists every denom to price.
	Denoms(ctx context.Context) ([]string, error)
	Save(ctx context.Context, s Snapshot) error
}

// Clock is the set's time source; tests swap it.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Deps of the set.
type Deps struct {
	Prices Prices
	// USDPricer prices the denoms by the DEX price chain before CoinGecko;
	// nil prices by CoinGecko only.
	USDPricer USDPricer
	// Tickers gives the 24-hour change; nil reads none and leaves the
	// change alone.
	Tickers Tickers
	Store   Store
	// ChainID is BZE's chain id.
	ChainID string
	// Denom is the native denom, whose 24-hour change is read and whose
	// DEX price is read first (the other denoms' BZE route needs it).
	Denom string
	// ChangeMarket is the market id (an order book's base/quote, a pool's
	// base_quote) of the ticker the change is read from (the BZE/USDC
	// liquidity pool); empty (or no Tickers) reads none and leaves the
	// change alone.
	ChangeMarket string
	// Workers bounds the denoms priced at once; zero is DefaultWorkers.
	Workers int
	// Log defaults to the standard logger, Clock to the wall clock.
	Log   log.FieldLogger
	Clock Clock
}

// Set is the prices set.
type Set struct {
	deps     Deps
	interval time.Duration
	mu       sync.Mutex

	pricesFailing, denomsFailing, pricerFailing, changeFailing bool
	// change is the last change read, at changeAt; changed reports a read.
	change   *string
	changeAt time.Time
	changed  bool
}

// New returns the set; interval zero is DefaultInterval.
func New(deps Deps, interval time.Duration) *Set {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if deps.Workers <= 0 {
		deps.Workers = DefaultWorkers
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}
	return &Set{deps: deps, interval: interval}
}

var _ statesync.Set = (*Set)(nil)

// Name is statesync.Prices.
func (s *Set) Name() string { return statesync.Prices }

// Interval is the minute refresh.
func (s *Set) Interval() time.Duration { return s.interval }

// FullResync reads CoinGecko's prices, the DEX prices of every denom and,
// when the one held is older than ChangeTTL, the change, and writes them. A
// failed CoinGecko or denoms read writes nothing; a denom the DEX pricer
// could not answer keeps its price; the prices are still written when only
// the change failed.
func (s *Set) FullResync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prices, err := s.deps.Prices.USD(ctx)
	if s.note(&s.pricesFailing, err, "CoinGecko prices") != nil {
		return statesync.Logged(err)
	}
	snap := Snapshot{ChainID: s.deps.ChainID, Prices: prices, ChangeDenom: s.deps.Denom, KeepChange: true}
	var pricerErr error
	if s.deps.USDPricer != nil {
		denoms, err := s.deps.Store.Denoms(ctx)
		if s.note(&s.denomsFailing, err, "denoms list") != nil {
			return statesync.Logged(err)
		}
		snap.DEX, snap.Kept = s.dexPrices(ctx, denoms)
		if len(snap.Kept) > 0 {
			pricerErr = fmt.Errorf("DEX price chain: %d of %d denoms unanswered (%s)", len(snap.Kept), len(denoms),
				strings.Join(snap.Kept, ", "))
		}
		s.note(&s.pricerFailing, pricerErr, "DEX price chain")
	}
	changeErr := s.readChange(ctx)
	if s.changed {
		snap.Change, snap.KeepChange = s.change, false
	}
	if err := s.deps.Store.Save(ctx, snap); err != nil {
		return err
	}
	if pricerErr != nil {
		return statesync.Logged(pricerErr)
	}
	return statesync.Logged(changeErr)
}

// ResyncOne refreshes every price: there is one source call for all.
func (s *Set) ResyncOne(ctx context.Context, _ string) error {
	return s.FullResync(ctx)
}

// dexPrices prices denoms by the DEX price chain: the native denom first
// (the others' BZE route reads its cached price), then the rest Workers at
// a time. It returns the prices found and the denoms left unanswered.
func (s *Set) dexPrices(ctx context.Context, denoms []string) (prices map[string]string, kept []string) {
	started := s.deps.Clock.Now()
	results := make([]*big.Rat, len(denoms))
	rest := make([]int, 0, len(denoms))
	for i, d := range denoms {
		if d == s.deps.Denom {
			results[i] = s.deps.USDPricer.USD(ctx, d)
		} else {
			rest = append(rest, i)
		}
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.deps.Workers)
	for _, i := range rest {
		g.Go(func() error {
			results[i] = s.deps.USDPricer.USD(gctx, denoms[i])
			return nil
		})
	}
	_ = g.Wait()
	prices = map[string]string{}
	for i, p := range results {
		switch {
		case p == nil:
			kept = append(kept, denoms[i])
		case p.Sign() > 0:
			if v := p.FloatString(PriceDecimals); strings.ContainsFunc(v, func(r rune) bool { return r >= '1' && r <= '9' }) {
				prices[denoms[i]] = v
			}
		}
	}
	slices.Sort(kept)
	s.deps.Log.WithFields(log.Fields{"denoms": len(denoms), "priced": len(prices), "unanswered": len(kept),
		"took": s.deps.Clock.Now().Sub(started).String()}).Debug("prices: DEX price chain run")
	return prices, kept
}

// readChange reads the change from the ticker when the one held is older
// than ChangeTTL. A market that is not listed, or a ticker without
// prices, is no change (null); a failed read keeps the one held.
func (s *Set) readChange(ctx context.Context) error {
	if s.deps.Tickers == nil || s.deps.ChangeMarket == "" || (s.changed && s.deps.Clock.Now().Sub(s.changeAt) < ChangeTTL) {
		return nil
	}
	t, err := s.deps.Tickers.Ticker(ctx, s.deps.ChangeMarket)
	if err == nil && t != nil && t.Base != s.deps.Denom && t.Quote != s.deps.Denom {
		err = fmt.Errorf("market %s does not trade %s", s.deps.ChangeMarket, s.deps.Denom)
	}
	if s.note(&s.changeFailing, err, "DEX ticker's 24-hour change") != nil {
		return err
	}
	s.change, s.changeAt, s.changed = nil, s.deps.Clock.Now(), true
	if pct, ok := ChangePct(t, s.deps.Denom); ok {
		s.change = &pct
	}
	return nil
}

// note logs what as failing when err starts a failure and as answering again
// when a success ends one; it returns err.
func (s *Set) note(failing *bool, err error, what string) error {
	switch {
	case err != nil && !*failing:
		*failing = true
		s.deps.Log.WithError(err).Warnf("prices: the %s fails, kept until it answers again", what)
	case err == nil && *failing:
		*failing = false
		s.deps.Log.Infof("prices: the %s answers again", what)
	}
	return err
}

// ChangePct is the 24-hour change of denom's price, in percent rounded half
// away from zero to four decimals, from the ticker of a market it trades in:
// last over open when denom is the base, open over last when it is the
// quote (the market prices the other side). ok is false when denom is not
// in the market or a price is not positive.
func ChangePct(t *tickers.Ticker, denom string) (pct string, ok bool) {
	if t == nil || t.OpenPrice == nil || t.LastPrice == nil || t.OpenPrice.Sign() <= 0 || t.LastPrice.Sign() <= 0 {
		return "", false
	}
	var ratio *big.Rat
	switch denom {
	case t.Base:
		ratio = new(big.Rat).Quo(t.LastPrice, t.OpenPrice)
	case t.Quote:
		ratio = new(big.Rat).Quo(t.OpenPrice, t.LastPrice)
	default:
		return "", false
	}
	change := ratio.Sub(ratio, big.NewRat(1, 1))
	change.Mul(change, big.NewRat(100, 1))
	return change.FloatString(4), true
}
