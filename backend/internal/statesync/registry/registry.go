// Package registry is the chain_registry set of the state sync: it caches
// the Cosmos chain registry's chain.json and assetlist.json of BZE itself
// and of every chain the explorer meets (an IBC channel's counterparty, an
// IBC denom's origin) in explorer.chains and explorer.registry_assets. It
// runs daily, and on every cache miss another set publishes (a chain id, or
// a registry name from an asset's traces) with a cool-down per key so a
// chain the registry does not know cannot cause a fetch per minute. A
// failed fetch keeps the previous rows.
//
// The registry is organised by directory, not by chain id. A chain id is
// found by reading chain.json files, the likeliest directory first
// (cosmoshub for cosmoshub-4), and every chain.json read is remembered, so
// the whole registry is read at most once per listing refresh.
package registry

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/chainregistry"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultInterval is the daily refresh.
const DefaultInterval = 24 * time.Hour

// CoolDown is the shortest time between two fetches of one chain on a
// cache miss.
const CoolDown = 10 * time.Minute

// ListingTTL is how long a directory listing (and what the set learnt from
// reading chain.json files) is used before the registry is listed again.
const ListingTTL = 24 * time.Hour

// Registry reads the chain registry; *chainregistry.Client satisfies it.
type Registry interface {
	Dirs(ctx context.Context) ([]string, error)
	Chain(ctx context.Context, dir string) (*chainregistry.Chain, error)
	Assets(ctx context.Context, dir string) ([]chainregistry.Asset, error)
}

// Store persists the set.
type Store interface {
	// Wanted returns the chain ids the explorer has met: the IBC channels'
	// counterparties, the denoms' origins and the chains already cached.
	Wanted(ctx context.Context) ([]string, error)
	// Save writes a chain and replaces its assets, in one transaction.
	Save(ctx context.Context, chain chainregistry.Chain, assets []chainregistry.Asset, fetched time.Time) error
	// SaveUnknown records a chain id the registry does not know (a chains
	// row with no registry_fetched_at), leaving a row already there as it
	// is.
	SaveUnknown(ctx context.Context, chainID string) error
}

// Clock is the set's time source; tests swap it.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Deps of the set.
type Deps struct {
	Registry Registry
	Store    Store
	// ChainID is BZE's own chain id, always fetched.
	ChainID string
	// Clock defaults to the wall clock.
	Clock Clock
}

// Set is the chain_registry set.
type Set struct {
	deps     Deps
	interval time.Duration
	mu       sync.Mutex

	// attempts is when each key was last fetched, for the cool-down.
	attempts map[string]time.Time
	// dirs is the listing, read at listed.
	dirs   []string
	listed time.Time
	// dirOf maps the chain ids read so far to their directory; read holds
	// the directories whose chain.json was read since the listing.
	dirOf map[string]string
	read  map[string]bool
}

// New returns the set; interval zero is DefaultInterval.
func New(deps Deps, interval time.Duration) *Set {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}
	return &Set{deps: deps, interval: interval, attempts: map[string]time.Time{}}
}

var _ statesync.Set = (*Set)(nil)

// Name is statesync.ChainRegistry.
func (s *Set) Name() string { return statesync.ChainRegistry }

// Interval is the daily refresh.
func (s *Set) Interval() time.Duration { return s.interval }

// FullResync refetches BZE and every chain the explorer has met. A chain
// that fails does not stop the others; the failures are returned joined.
func (s *Set) FullResync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	wanted, err := s.deps.Store.Wanted(ctx)
	if err != nil {
		return err
	}
	ids := append([]string{s.deps.ChainID}, wanted...)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	var errs []error
	for _, id := range ids {
		if id == "" {
			continue
		}
		if err := s.fetch(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ResyncOne fetches the chain key names (a chain id or a RegistryNameKey),
// unless it was fetched less than CoolDown ago.
func (s *Set) ResyncOne(ctx context.Context, key string) error {
	if key == statesync.All {
		return s.FullResync(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if last, ok := s.attempts[key]; ok && s.deps.Clock.Now().Sub(last) < CoolDown {
		return nil
	}
	return s.fetch(ctx, key)
}

// fetch fetches the chain key names and records the attempt.
func (s *Set) fetch(ctx context.Context, key string) error {
	s.attempts[key] = s.deps.Clock.Now()
	if name, ok := statesync.RegistryName(key); ok {
		return s.fetchByName(ctx, name)
	}
	return s.fetchByID(ctx, key)
}

func (s *Set) fetchByID(ctx context.Context, chainID string) error {
	dir, chain, err := s.find(ctx, chainID)
	if err != nil {
		return err
	}
	if chain == nil {
		return s.deps.Store.SaveUnknown(ctx, chainID)
	}
	return s.save(ctx, dir, chain)
}

// fetchByName fetches the chain in directory name (or testnets/name). A name
// the registry does not list is left alone: there is no chain id to record
// it under.
func (s *Set) fetchByName(ctx context.Context, name string) error {
	if err := s.list(ctx); err != nil {
		return err
	}
	for _, dir := range []string{name, chainregistry.TestnetsDir + "/" + name} {
		if !slices.Contains(s.dirs, dir) {
			continue
		}
		chain, err := s.readChain(ctx, dir)
		if err != nil {
			return err
		}
		if chain != nil {
			return s.save(ctx, dir, chain)
		}
	}
	return nil
}

func (s *Set) save(ctx context.Context, dir string, chain *chainregistry.Chain) error {
	assets, err := s.deps.Registry.Assets(ctx, dir)
	if err != nil {
		return err
	}
	return s.deps.Store.Save(ctx, *chain, assets, s.deps.Clock.Now())
}

// find returns the directory and chain.json of chainID; a nil chain when no
// directory of the registry holds it.
func (s *Set) find(ctx context.Context, chainID string) (string, *chainregistry.Chain, error) {
	if err := s.list(ctx); err != nil {
		return "", nil, err
	}
	if dir, ok := s.dirOf[chainID]; ok {
		chain, err := s.readChain(ctx, dir)
		if err != nil || (chain != nil && chain.ChainID == chainID) {
			return dir, chain, err
		}
	}
	for _, dir := range candidates(chainID, s.dirs) {
		if s.read[dir] {
			continue
		}
		chain, err := s.readChain(ctx, dir)
		if err != nil {
			return "", nil, err
		}
		if chain != nil && chain.ChainID == chainID {
			return dir, chain, nil
		}
	}
	return "", nil, nil
}

// readChain reads dir's chain.json and remembers its chain id; nil when the
// directory has none.
func (s *Set) readChain(ctx context.Context, dir string) (*chainregistry.Chain, error) {
	chain, err := s.deps.Registry.Chain(ctx, dir)
	if errors.Is(err, chainregistry.ErrNotFound) {
		s.read[dir] = true
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.read[dir] = true
	s.dirOf[chain.ChainID] = dir
	return chain, nil
}

// list reads the directory listing when the one held is older than
// ListingTTL, and forgets what the old one taught.
func (s *Set) list(ctx context.Context) error {
	now := s.deps.Clock.Now()
	if s.dirs != nil && now.Sub(s.listed) < ListingTTL {
		return nil
	}
	dirs, err := s.deps.Registry.Dirs(ctx)
	if err != nil {
		return fmt.Errorf("chain registry: %w", err)
	}
	s.dirs, s.listed = dirs, now
	s.dirOf, s.read = map[string]string{}, map[string]bool{}
	return nil
}

// candidates orders dirs for a chain id search: the directory named like
// the chain id without its revision (cosmoshub for cosmoshub-4, evmos for
// evmos_9001-2) first, the mainnets before the testnets after it.
func candidates(chainID string, dirs []string) []string {
	stem := chainID
	if i := strings.IndexAny(stem, "-_"); i > 0 {
		stem = stem[:i]
	}
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if d == stem || d == chainID {
			out = append(out, d)
		}
	}
	for _, d := range dirs {
		if d == chainregistry.TestnetsDir+"/"+stem {
			out = append(out, d)
		}
	}
	for _, d := range dirs {
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}
