// Package paramsnap is the params set of the state sync: it snapshots the
// parameters of every module at start, daily, and when a proposal passes,
// and writes an explorer.param_snapshots row only for a module whose
// parameters differ from its latest snapshot, with the top-level keys that
// changed. The snapshots only explain changes ("changed by proposal N");
// the parameters page always shows the live values.
//
// Upgrade handlers change parameters without any message, so diffing is the
// only general method. A change is attributed to the passed parameter-change
// proposal whose resolution triggered the run, else to a passed software
// upgrade whose plan height falls between the module's previous snapshot
// and this one.
package paramsnap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"time"

	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/params"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/proposals"
)

// DefaultInterval is the daily snapshot.
const DefaultInterval = 24 * time.Hour

// Node is the local node's RPC; *node.Client satisfies it.
type Node interface {
	Status(ctx context.Context) (*node.Status, []byte, error)
}

// Snapshot is one param_snapshots row.
type Snapshot struct {
	Module string
	Height int64
	Time   time.Time
	Params json.RawMessage
	// ChangedKeys are the top-level keys that differ from the previous
	// snapshot, sorted; empty for the module's first.
	ChangedKeys []string
	ProposalID  *uint64
}

// Latest is a module's latest snapshot.
type Latest struct {
	Height int64
	Params json.RawMessage
}

// Store persists the set.
type Store interface {
	// Latest returns every module's latest snapshot.
	Latest(ctx context.Context) (map[string]Latest, error)
	// ProposalKind returns a proposal's kind and status; found is false for
	// an id the explorer does not hold.
	ProposalKind(ctx context.Context, id uint64) (kind, status string, found bool, err error)
	// UpgradeBetween returns the passed software-upgrade proposal whose
	// plan height is in (after, upTo], the latest one when several; nil
	// when none.
	UpgradeBetween(ctx context.Context, after, upTo int64) (*uint64, error)
	// Insert writes the snapshots in one transaction.
	Insert(ctx context.Context, snaps []Snapshot) error
}

// Deps of the set.
type Deps struct {
	Params params.Reader
	Node   Node
	Store  Store
}

// Set is the params set.
type Set struct {
	deps     Deps
	interval time.Duration
}

// New returns the set; interval zero is DefaultInterval.
func New(deps Deps, interval time.Duration) *Set {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Set{deps: deps, interval: interval}
}

var _ statesync.Set = (*Set)(nil)

// Name is statesync.Params.
func (s *Set) Name() string { return statesync.Params }

// Interval is the daily snapshot.
func (s *Set) Interval() time.Duration { return s.interval }

// FullResync snapshots every module.
func (s *Set) FullResync(ctx context.Context) error {
	return s.snapshot(ctx, nil)
}

// ResyncOne snapshots every module after proposal key (proposals.Key)
// passed: a change is attributed to it when it is a parameter change or a
// software upgrade. Any other key is a plain snapshot.
func (s *Set) ResyncOne(ctx context.Context, key string) error {
	id, err := strconv.ParseUint(key, 10, 64)
	if err != nil {
		return s.snapshot(ctx, nil)
	}
	kind, status, found, err := s.deps.Store.ProposalKind(ctx, id)
	if err != nil {
		return err
	}
	if !found || status != proposals.StatusPassed ||
		(kind != proposals.KindParameterChange && kind != proposals.KindSoftwareUpgrade) {
		return s.snapshot(ctx, nil)
	}
	return s.snapshot(ctx, &id)
}

// snapshot reads every module at the node's latest height and writes the
// ones that changed; trigger is the passed proposal the run is for.
func (s *Set) snapshot(ctx context.Context, trigger *uint64) error {
	st, _, err := s.deps.Node.Status(ctx)
	if err != nil {
		return fmt.Errorf("node status: %w", err)
	}
	at := metadata.AppendToOutgoingContext(ctx, grpctypes.GRPCBlockHeightHeader,
		strconv.FormatInt(st.LatestBlockHeight, 10))
	live, readErr := s.deps.Params.Params(at)
	latest, err := s.deps.Store.Latest(ctx)
	if err != nil {
		return errors.Join(readErr, err)
	}
	var snaps []Snapshot
	var errs []error
	for _, module := range params.Modules {
		cur, ok := live[module]
		if !ok {
			continue
		}
		prev, had := latest[module]
		snap := Snapshot{Module: module, Height: st.LatestBlockHeight, Time: st.LatestBlockTime.UTC(), Params: cur, ChangedKeys: []string{}}
		if had {
			keys, err := ChangedKeys(prev.Params, cur)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s params: %w", module, err))
				continue
			}
			if len(keys) == 0 {
				continue
			}
			snap.ChangedKeys = keys
			if snap.ProposalID, err = s.cause(ctx, trigger, prev.Height, st.LatestBlockHeight); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		snaps = append(snaps, snap)
	}
	if len(snaps) > 0 {
		if err := s.deps.Store.Insert(ctx, snaps); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(append([]error{readErr}, errs...)...)
}

// cause is the proposal a change seen between after and upTo is attributed
// to: the trigger, else an upgrade planned in between.
func (s *Set) cause(ctx context.Context, trigger *uint64, after, upTo int64) (*uint64, error) {
	if trigger != nil {
		return trigger, nil
	}
	return s.deps.Store.UpgradeBetween(ctx, after, upTo)
}

// ChangedKeys returns the top-level keys of two JSON objects whose values
// differ (present in one only, or different), sorted.
func ChangedKeys(prev, cur json.RawMessage) ([]string, error) {
	var a, b map[string]any
	if err := json.Unmarshal(prev, &a); err != nil {
		return nil, fmt.Errorf("previous snapshot: %w", err)
	}
	if err := json.Unmarshal(cur, &b); err != nil {
		return nil, fmt.Errorf("live params: %w", err)
	}
	var keys []string
	for k, v := range a {
		if w, ok := b[k]; !ok || !reflect.DeepEqual(v, w) {
			keys = append(keys, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys, nil
}
