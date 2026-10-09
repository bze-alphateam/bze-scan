package registry_test

import (
	"context"
	"errors"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chainregistry"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/registry"
)

// fakeRegistry serves chains by directory and counts the reads.
type fakeRegistry struct {
	mu     sync.Mutex
	dirs   []string
	chains map[string]string // dir → chain id
	fail   map[string]bool   // dir → its reads fail
	listed int
	reads  []string
}

func (r *fakeRegistry) Dirs(context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listed++
	return slices.Clone(r.dirs), nil
}

func (r *fakeRegistry) Chain(_ context.Context, dir string) (*chainregistry.Chain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads = append(r.reads, dir+"/chain.json")
	if r.fail[dir] {
		return nil, errors.New("HTTP 503")
	}
	id, ok := r.chains[dir]
	if !ok {
		return nil, chainregistry.ErrNotFound
	}
	return &chainregistry.Chain{ChainID: id, Name: path.Base(dir), PrettyName: "Pretty " + path.Base(dir)}, nil
}

func (r *fakeRegistry) Assets(_ context.Context, dir string) ([]chainregistry.Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads = append(r.reads, dir+"/assetlist.json")
	return []chainregistry.Asset{{Base: "u" + path.Base(dir)}}, nil
}

func (r *fakeRegistry) chainReads() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, x := range r.reads {
		if d, ok := strings.CutSuffix(x, "/chain.json"); ok {
			out = append(out, d)
		}
	}
	return out
}

func (r *fakeRegistry) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads = nil
}

// fakeStore keeps the saved chains.
type fakeStore struct {
	wanted  []string
	saved   map[string][]string // chain id → asset bases
	unknown []string
}

func (s *fakeStore) Wanted(context.Context) ([]string, error) { return s.wanted, nil }

func (s *fakeStore) Save(_ context.Context, c chainregistry.Chain, assets []chainregistry.Asset, _ time.Time) error {
	if s.saved == nil {
		s.saved = map[string][]string{}
	}
	var bases []string
	for _, a := range assets {
		bases = append(bases, a.Base)
	}
	s.saved[c.ChainID] = bases
	return nil
}

func (s *fakeStore) SaveUnknown(_ context.Context, id string) error {
	s.unknown = append(s.unknown, id)
	return nil
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newRegistry() *fakeRegistry {
	return &fakeRegistry{
		dirs: []string{"akash", "beezee", "cosmoshub", "noble", "zenrock", "testnets/beezeetestnet"},
		chains: map[string]string{"akash": "akashnet-2", "beezee": "beezee-1", "cosmoshub": "cosmoshub-4",
			"noble": "noble-1", "zenrock": "diamond-1", "testnets/beezeetestnet": "beezee-testnet-2"},
	}
}

func newSet(reg *fakeRegistry, store *fakeStore, c *clock) *registry.Set {
	return registry.New(registry.Deps{Registry: reg, Store: store, ChainID: "beezee-1", Clock: c}, 0)
}

func TestFullResyncFetchesBZEAndEveryChainMet(t *testing.T) {
	reg, store := newRegistry(), &fakeStore{wanted: []string{"noble-1", "cosmoshub-4", "beezee-1"}}
	s := newSet(reg, store, &clock{now: time.Unix(1000, 0)})
	assert.Equal(t, statesync.ChainRegistry, s.Name())
	assert.Equal(t, registry.DefaultInterval, s.Interval())

	require.NoError(t, s.FullResync(context.Background()))
	assert.Equal(t, map[string][]string{"beezee-1": {"ubeezee"}, "cosmoshub-4": {"ucosmoshub"}, "noble-1": {"unoble"}}, store.saved)
	assert.Equal(t, []string{"beezee", "cosmoshub", "noble"}, reg.chainReads(),
		"each chain id found in the directory named like it, nothing else read")
	assert.Empty(t, store.unknown)
}

func TestAChainIDNamedUnlikeItsDirectoryIsFoundByReadingTheRest(t *testing.T) {
	reg, store := newRegistry(), &fakeStore{wanted: []string{"diamond-1"}}
	c := &clock{now: time.Unix(1000, 0)}
	s := newSet(reg, store, c)
	require.NoError(t, s.FullResync(context.Background()))
	assert.Contains(t, store.saved, "diamond-1")

	// What the scan read is remembered: another chain needs no read but its own.
	reg.reset()
	require.NoError(t, s.ResyncOne(context.Background(), "akashnet-2"))
	assert.Equal(t, []string{"akash"}, reg.chainReads())
	assert.Contains(t, store.saved, "akashnet-2")
}

func TestAnUnknownChainIsRecordedWithoutLoopingTheRegistry(t *testing.T) {
	reg, store := newRegistry(), &fakeStore{}
	c := &clock{now: time.Unix(1000, 0)}
	s := newSet(reg, store, c)
	ctx := context.Background()

	require.NoError(t, s.ResyncOne(ctx, "unknown-1"))
	assert.Equal(t, []string{"unknown-1"}, store.unknown)
	assert.Len(t, reg.chainReads(), len(reg.dirs), "every directory read once")

	// Within the cool-down a miss fetches nothing.
	reg.reset()
	c.now = c.now.Add(registry.CoolDown - time.Second)
	require.NoError(t, s.ResyncOne(ctx, "unknown-1"))
	assert.Empty(t, reg.chainReads())
	assert.Len(t, store.unknown, 1)

	// After it, the chain is looked up again, without rereading the registry.
	c.now = c.now.Add(2 * time.Second)
	require.NoError(t, s.ResyncOne(ctx, "unknown-1"))
	assert.Empty(t, reg.chainReads(), "every directory was read since the listing")
	assert.Equal(t, []string{"unknown-1", "unknown-1"}, store.unknown)
	assert.Equal(t, 1, reg.listed)

	// A new listing forgets what the old one taught.
	c.now = c.now.Add(registry.ListingTTL)
	reg.dirs = append(reg.dirs, "newchain")
	reg.chains["newchain"] = "unknown-1"
	require.NoError(t, s.ResyncOne(ctx, "unknown-1"))
	assert.Equal(t, 2, reg.listed)
	assert.Contains(t, store.saved, "unknown-1", "the registry caught up")
}

func TestARegistryNameIsFetchedByItsDirectory(t *testing.T) {
	reg, store := newRegistry(), &fakeStore{}
	s := newSet(reg, store, &clock{now: time.Unix(1000, 0)})
	ctx := context.Background()

	require.NoError(t, s.ResyncOne(ctx, statesync.RegistryNameKey("noble")))
	assert.Equal(t, []string{"noble"}, reg.chainReads())
	assert.Contains(t, store.saved, "noble-1")

	require.NoError(t, s.ResyncOne(ctx, statesync.RegistryNameKey("beezeetestnet")))
	assert.Contains(t, store.saved, "beezee-testnet-2", "a testnet's directory")

	reg.reset()
	require.NoError(t, s.ResyncOne(ctx, statesync.RegistryNameKey("ethereum")))
	assert.Empty(t, reg.chainReads(), "a name the registry does not list")
	assert.Empty(t, store.unknown, "no chain id to record it under")
}

func TestAFailedFetchKeepsTheRowsAndDoesNotStopTheOthers(t *testing.T) {
	reg, store := newRegistry(), &fakeStore{wanted: []string{"cosmoshub-4", "noble-1"}}
	reg.fail = map[string]bool{"cosmoshub": true}
	c := &clock{now: time.Unix(1000, 0)}
	s := newSet(reg, store, c)

	err := s.FullResync(context.Background())
	require.Error(t, err)
	assert.Contains(t, store.saved, "noble-1")
	assert.Contains(t, store.saved, "beezee-1")
	assert.NotContains(t, store.saved, "cosmoshub-4", "nothing written for the failed chain")
	assert.Empty(t, store.unknown, "a failure is not an unknown chain")

	// The daily run ignores the cool-down.
	reg.fail = nil
	require.NoError(t, s.FullResync(context.Background()))
	assert.Contains(t, store.saved, "cosmoshub-4")
}

func TestAFailedListingFailsTheFetch(t *testing.T) {
	store := &fakeStore{}
	s := registry.New(registry.Deps{Registry: failingList{}, Store: store, ChainID: "beezee-1"}, time.Hour)
	assert.Equal(t, time.Hour, s.Interval())
	require.Error(t, s.FullResync(context.Background()))
	require.Error(t, s.ResyncOne(context.Background(), statesync.RegistryNameKey("noble")))
	assert.Empty(t, store.unknown)
}

type failingList struct{ registry.Registry }

func (failingList) Dirs(context.Context) ([]string, error) { return nil, errors.New("HTTP 403") }
