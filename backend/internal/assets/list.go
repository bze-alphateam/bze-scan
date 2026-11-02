package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultInterval is the safety net of the asset list set: the sets it is
// built from ask for a rebuild after every write.
const DefaultInterval = time.Hour

// DefaultRetryDelay is the wait between two failed builds at start.
const DefaultRetryDelay = 5 * time.Second

// versionLen is the length of a version: hex of a truncated SHA-256.
const versionLen = 16

// Snapshot is one build, rendered: what the routes serve. It is never
// changed once built.
type Snapshot struct {
	// Version is the whole list's content hash.
	Version string
	// Body is {"version": …, "assets": […]}.
	Body   []byte
	assets map[string]rendered
}

type rendered struct {
	version string
	body    []byte
}

// Asset returns one asset's version and JSON; false for a denom the list
// does not hold.
func (s *Snapshot) Asset(denom string) (version string, body []byte, ok bool) {
	r, ok := s.assets[denom]
	return r.version, r.body, ok
}

// Clock is the list's time source; tests swap it.
type Clock interface {
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Deps of the list.
type Deps struct {
	Source   Source
	Curation Curation
	// ChainID is BZE's chain id: its registry asset list names the native
	// and factory denoms.
	ChainID string
	// Clock defaults to the wall clock.
	Clock Clock
	// Log defaults to the standard logger.
	Log log.FieldLogger
}

// Config of the list; zero values take the defaults.
type Config struct {
	Interval   time.Duration
	RetryDelay time.Duration
}

// List holds the current asset list. It is the asset_list set of the state
// sync (every run rebuilds it in full) and, through Run, builds it once at
// start from the tables.
type List struct {
	deps Deps
	cfg  Config
	mu   sync.Mutex
	cur  atomic.Pointer[Snapshot]
}

// New returns an empty list: Current is nil until the first build.
func New(cfg Config, deps Deps) *List {
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = DefaultRetryDelay
	}
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	return &List{deps: deps, cfg: cfg}
}

var _ statesync.Set = (*List)(nil)

// Current is the last build; nil before the first one.
func (l *List) Current() *Snapshot { return l.cur.Load() }

// Name is statesync.AssetList.
func (l *List) Name() string { return statesync.AssetList }

// Interval is the safety net.
func (l *List) Interval() time.Duration { return l.cfg.Interval }

// FullResync rebuilds the list.
func (l *List) FullResync(ctx context.Context) error { return l.Rebuild(ctx) }

// ResyncOne rebuilds the list: it has no entry of its own to resync.
func (l *List) ResyncOne(ctx context.Context, _ string) error { return l.Rebuild(ctx) }

// Rebuild reads the tables and replaces the list. A build that changes
// nothing keeps the current snapshot (and its version).
func (l *List) Rebuild(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	t, err := l.deps.Source.Load(ctx)
	if err != nil {
		return err
	}
	snap, err := render(build(t, l.deps.Curation, l.deps.ChainID))
	if err != nil {
		return err
	}
	if old := l.cur.Load(); old != nil && old.Version == snap.Version {
		return nil
	}
	l.cur.Store(snap)
	l.deps.Log.WithFields(log.Fields{"version": snap.Version, "assets": len(snap.assets)}).Debug("asset list rebuilt")
	return nil
}

// Run builds the list at start from the tables, trying again every
// RetryDelay until a build succeeds or ctx is cancelled. It returns nil
// either way: the state sync rebuilds it from then on.
func (l *List) Run(ctx context.Context) error {
	for {
		err := l.Rebuild(ctx)
		if err == nil || ctx.Err() != nil {
			return nil
		}
		l.deps.Log.WithError(err).Warn("asset list: first build failed, retrying")
		select {
		case <-ctx.Done():
			return nil
		case <-l.deps.Clock.After(l.cfg.RetryDelay):
		}
	}
}

// render versions and serialises a build.
func render(list []Asset) (*Snapshot, error) {
	snap := &Snapshot{assets: make(map[string]rendered, len(list))}
	whole := sha256.New()
	bodies := make([]json.RawMessage, 0, len(list))
	for _, a := range list {
		a.Version = ""
		raw, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("asset list %s: %w", a.Denom, err)
		}
		a.Version = hash(raw)
		body, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("asset list %s: %w", a.Denom, err)
		}
		snap.assets[a.Denom] = rendered{version: a.Version, body: body}
		bodies = append(bodies, body)
		_, _ = fmt.Fprintf(whole, "%s %s\n", a.Denom, a.Version)
	}
	snap.Version = hex.EncodeToString(whole.Sum(nil))[:versionLen]
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(struct {
		Version string            `json:"version"`
		Assets  []json.RawMessage `json:"assets"`
	}{snap.Version, bodies}); err != nil {
		return nil, fmt.Errorf("asset list: %w", err)
	}
	snap.Body = bytes.TrimRight(buf.Bytes(), "\n")
	return snap, nil
}

func hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:versionLen]
}
