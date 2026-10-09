// Package prices is the prices set of the state sync: every minute it reads
// the aggregator's USD prices, published by CoinGecko id, and writes them to
// the denoms whose chain-registry asset carries that id (BZE's own asset
// list first, then the origin chain's for an IBC denom). The explorer
// computes no DEX price; a failed fetch keeps the previous values and is
// logged once per change of state, not every minute.
package prices

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultInterval is the minute refresh.
const DefaultInterval = time.Minute

// Source publishes USD prices by CoinGecko id; *aggregator.Client
// satisfies it.
type Source interface {
	Prices(ctx context.Context) (map[string]string, error)
}

// Store persists the set.
type Store interface {
	// Save sets the price of every denom whose registry asset's CoinGecko id
	// is in prices, and clears the price of the others; chainID is BZE's,
	// whose asset list maps its own denoms.
	Save(ctx context.Context, chainID string, prices map[string]string) error
}

// Deps of the set.
type Deps struct {
	Source Source
	Store  Store
	// ChainID is BZE's chain id.
	ChainID string
	// Log defaults to the standard logger.
	Log log.FieldLogger
}

// Set is the prices set.
type Set struct {
	deps     Deps
	interval time.Duration
	mu       sync.Mutex
	failing  bool
}

// New returns the set; interval zero is DefaultInterval.
func New(deps Deps, interval time.Duration) *Set {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	return &Set{deps: deps, interval: interval}
}

var _ statesync.Set = (*Set)(nil)

// Name is statesync.Prices.
func (s *Set) Name() string { return statesync.Prices }

// Interval is the minute refresh.
func (s *Set) Interval() time.Duration { return s.interval }

// FullResync reads the prices and writes them. A failed read keeps the
// stored prices; it is logged when the source starts failing and when it
// answers again.
func (s *Set) FullResync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prices, err := s.deps.Source.Prices(ctx)
	if err != nil {
		if !s.failing {
			s.failing = true
			s.deps.Log.WithError(err).Warn("prices: the aggregator fails, prices kept until it answers again")
		}
		return statesync.Logged(err)
	}
	if s.failing {
		s.failing = false
		s.deps.Log.Info("prices: the aggregator answers again")
	}
	return s.deps.Store.Save(ctx, s.deps.ChainID, prices)
}

// ResyncOne refreshes every price: there is one source call for all.
func (s *Set) ResyncOne(ctx context.Context, _ string) error {
	return s.FullResync(ctx)
}
