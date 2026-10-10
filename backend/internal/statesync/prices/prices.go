// Package prices is the prices set of the state sync: every minute it reads
// the aggregator's USD prices, published by CoinGecko id, and writes them to
// the denoms whose chain-registry asset carries that id (BZE's own asset
// list first, then the origin chain's for an IBC denom). The native denom's
// 24-hour change comes from the aggregator's ticker of one BZE/USDC market
// (the liquidity pool), read at most every ChangeTTL. The explorer computes
// no DEX price; a failed fetch keeps the previous values and is logged once
// per change of state, not every minute.
package prices

import (
	"context"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/aggregator"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultInterval is the minute refresh.
const DefaultInterval = time.Minute

// ChangeTTL is how long a read 24-hour change is used before the ticker is
// read again.
const ChangeTTL = 5 * time.Minute

// Source is the aggregator; *aggregator.Client satisfies it.
type Source interface {
	// Prices returns USD prices by CoinGecko id.
	Prices(ctx context.Context) (map[string]string, error)
	// Ticker returns a market's ticker, nil when the market is not listed.
	Ticker(ctx context.Context, marketID string) (*aggregator.Ticker, error)
}

// Snapshot is one write of the set.
type Snapshot struct {
	// ChainID is BZE's, whose asset list maps its own denoms.
	ChainID string
	// Prices are USD prices by CoinGecko id: every denom whose registry
	// asset's id is listed takes its price, the others are cleared.
	Prices map[string]string
	// ChangeDenom's price_change_24h_pct becomes Change (nil clears it),
	// unless KeepChange.
	ChangeDenom string
	Change      *string
	KeepChange  bool
}

// Store persists the set.
type Store interface {
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
	Source Source
	Store  Store
	// ChainID is BZE's chain id.
	ChainID string
	// Denom is the native denom, whose 24-hour change is read.
	Denom string
	// ChangeMarket is the aggregator market id of the ticker the change is
	// read from (the BZE/USDC liquidity pool); empty reads none and leaves
	// the change alone.
	ChangeMarket string
	// Log defaults to the standard logger, Clock to the wall clock.
	Log   log.FieldLogger
	Clock Clock
}

// Set is the prices set.
type Set struct {
	deps     Deps
	interval time.Duration
	mu       sync.Mutex

	pricesFailing, changeFailing bool
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

// FullResync reads the prices and, when the one held is older than
// ChangeTTL, the change, and writes them. A failed read keeps what it would
// have written; the prices are still written when only the change failed.
func (s *Set) FullResync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prices, err := s.deps.Source.Prices(ctx)
	if s.note(&s.pricesFailing, err, "prices") != nil {
		return statesync.Logged(err)
	}
	snap := Snapshot{ChainID: s.deps.ChainID, Prices: prices, ChangeDenom: s.deps.Denom, KeepChange: true}
	changeErr := s.readChange(ctx)
	if s.changed {
		snap.Change, snap.KeepChange = s.change, false
	}
	if err := s.deps.Store.Save(ctx, snap); err != nil {
		return err
	}
	return statesync.Logged(changeErr)
}

// ResyncOne refreshes every price: there is one source call for all.
func (s *Set) ResyncOne(ctx context.Context, _ string) error {
	return s.FullResync(ctx)
}

// readChange reads the change from the ticker when the one held is older
// than ChangeTTL. A market the aggregator does not list, or a ticker without
// prices, is no change (null); a failed read keeps the one held.
func (s *Set) readChange(ctx context.Context) error {
	if s.deps.ChangeMarket == "" || (s.changed && s.deps.Clock.Now().Sub(s.changeAt) < ChangeTTL) {
		return nil
	}
	t, err := s.deps.Source.Ticker(ctx, s.deps.ChangeMarket)
	if err == nil && t != nil && t.Base != s.deps.Denom && t.Quote != s.deps.Denom {
		err = fmt.Errorf("market %s does not trade %s", s.deps.ChangeMarket, s.deps.Denom)
	}
	if s.note(&s.changeFailing, err, "24-hour change") != nil {
		return err
	}
	s.change, s.changeAt, s.changed = nil, s.deps.Clock.Now(), true
	if pct, ok := aggregator.ChangePct(t, s.deps.Denom); ok {
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
		s.deps.Log.WithError(err).Warnf("prices: the aggregator's %s fails, kept until it answers again", what)
	case err == nil && *failing:
		*failing = false
		s.deps.Log.Infof("prices: the aggregator's %s answers again", what)
	}
	return err
}
