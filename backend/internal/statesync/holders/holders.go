// Package holders is the holders set of the state sync: a snapshot of every
// non-zero balance of every denom from the bank's DenomOwners, never a
// balance derived from events, with denoms.holders_count recomputed from it
// as the owners of at least one display unit. It runs at start and hourly,
// one denom after the other with a pause between pages, so the local node
// is never hammered; the position (the denom and the page key) is the
// job's cursor in sync_jobs, so a restart resumes where the last run
// stopped.
package holders

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"google.golang.org/grpc"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultInterval is the hourly snapshot.
const DefaultInterval = time.Hour

// PageLimit is the page size of DenomOwners.
const PageLimit = 1000

// DefaultPause is the wait between two pages of one denom.
const DefaultPause = 200 * time.Millisecond

// Bank is the part of the bank query client the set uses;
// banktypes.QueryClient satisfies it.
type Bank interface {
	DenomOwners(ctx context.Context, in *banktypes.QueryDenomOwnersRequest, opts ...grpc.CallOption) (*banktypes.QueryDenomOwnersResponse, error)
}

// Owner is one balance of a denom, in base units.
type Owner struct {
	Address string
	Balance string
}

// Store persists the set.
type Store interface {
	// Denoms returns every stored denom, sorted.
	Denoms(ctx context.Context) ([]string, error)
	// Cursor returns the cursor the last run recorded in sync_jobs; nil
	// without one.
	Cursor(ctx context.Context) (json.RawMessage, error)
	// SavePage upserts one page of a denom's owners, stamped with the run's
	// start.
	SavePage(ctx context.Context, denom string, stamp time.Time, owners []Owner) error
	// Finish ends a denom's snapshot in one transaction: deletes its rows the
	// run did not stamp and recomputes its holders_count.
	Finish(ctx context.Context, denom string, stamp time.Time) error
}

// Clock is the set's time source and pause; tests swap it.
type Clock interface {
	Now() time.Time
	// Sleep waits d, or returns ctx's error when it ends first.
	Sleep(ctx context.Context, d time.Duration) error
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Deps of the set.
type Deps struct {
	Bank  Bank
	Store Store
	// Pause between two pages of a denom; zero is DefaultPause.
	Pause time.Duration
	// Clock defaults to the wall clock.
	Clock Clock
}

// cursor is the position of an unfinished run: the denom being paged, the
// key of its next page and the run's stamp. The zero value is "no run
// unfinished".
type cursor struct {
	Denom string    `json:"denom,omitempty"`
	Key   []byte    `json:"key,omitempty"`
	Stamp time.Time `json:"stamp,omitzero"`
}

// Set is the holders set.
type Set struct {
	deps     Deps
	interval time.Duration
	mu       sync.Mutex

	// cur is the position, loaded from the store before the first run.
	curMu  sync.Mutex
	cur    cursor
	loaded bool
}

// New returns the set; interval zero is DefaultInterval.
func New(deps Deps, interval time.Duration) *Set {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if deps.Pause <= 0 {
		deps.Pause = DefaultPause
	}
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}
	return &Set{deps: deps, interval: interval}
}

var (
	_ statesync.Set      = (*Set)(nil)
	_ statesync.Cursorer = (*Set)(nil)
)

// Name is statesync.Holders.
func (s *Set) Name() string { return statesync.Holders }

// Interval is the hourly snapshot.
func (s *Set) Interval() time.Duration { return s.interval }

// Cursor is the position of the run, {} once it finished.
func (s *Set) Cursor() json.RawMessage {
	s.curMu.Lock()
	defer s.curMu.Unlock()
	raw, _ := json.Marshal(s.cur) // strings, bytes and a time always marshal
	return raw
}

func (s *Set) setCursor(c cursor) {
	s.curMu.Lock()
	defer s.curMu.Unlock()
	s.cur = c
}

// FullResync snapshots every denom, resuming an unfinished run at its
// denom and page.
func (s *Set) FullResync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(ctx); err != nil {
		return err
	}
	denoms, err := s.deps.Store.Denoms(ctx)
	if err != nil {
		return err
	}
	s.curMu.Lock()
	resume := s.cur
	s.curMu.Unlock()
	for _, d := range denoms {
		if d < resume.Denom {
			continue
		}
		pos := cursor{Denom: d, Stamp: stamp(s.deps.Clock.Now())}
		if d == resume.Denom && !resume.Stamp.IsZero() {
			pos = resume
		}
		if err := s.snapshot(ctx, pos); err != nil {
			return err
		}
	}
	s.setCursor(cursor{})
	return nil
}

// ResyncOne snapshots the denom key alone; an unfinished full run keeps its
// position.
func (s *Set) ResyncOne(ctx context.Context, key string) error {
	if key == statesync.All {
		return s.FullResync(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(ctx); err != nil {
		return err
	}
	s.curMu.Lock()
	keep := s.cur
	s.curMu.Unlock()
	err := s.snapshot(ctx, cursor{Denom: key, Stamp: stamp(s.deps.Clock.Now())})
	s.setCursor(keep)
	return err
}

// snapshot pages through one denom's owners from pos, then finishes it.
// The cursor follows every page, so a failure leaves it at the page to
// retry.
func (s *Set) snapshot(ctx context.Context, pos cursor) error {
	for {
		s.setCursor(pos)
		resp, err := s.deps.Bank.DenomOwners(ctx, &banktypes.QueryDenomOwnersRequest{
			Denom: pos.Denom, Pagination: &query.PageRequest{Key: pos.Key, Limit: PageLimit},
		})
		if err != nil {
			return fmt.Errorf("bank DenomOwners %s: %w", pos.Denom, err)
		}
		owners := make([]Owner, 0, len(resp.DenomOwners))
		for _, o := range resp.DenomOwners {
			if o == nil || !o.Balance.Amount.IsPositive() {
				continue
			}
			owners = append(owners, Owner{Address: o.Address, Balance: o.Balance.Amount.String()})
		}
		if len(owners) > 0 {
			if err := s.deps.Store.SavePage(ctx, pos.Denom, pos.Stamp, owners); err != nil {
				return err
			}
		}
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			break
		}
		pos.Key = resp.Pagination.NextKey
		s.setCursor(pos)
		if err := s.deps.Clock.Sleep(ctx, s.deps.Pause); err != nil {
			return err
		}
	}
	return s.deps.Store.Finish(ctx, pos.Denom, pos.Stamp)
}

// load reads the stored cursor once. A cursor that does not parse is
// dropped: the next run starts over.
func (s *Set) load(ctx context.Context) error {
	if s.loaded {
		return nil
	}
	raw, err := s.deps.Store.Cursor(ctx)
	if err != nil {
		return err
	}
	var c cursor
	if len(raw) > 0 && json.Unmarshal(raw, &c) != nil {
		c = cursor{}
	}
	s.setCursor(c)
	s.loaded = true
	return nil
}

// stamp is t as the database stores it (microseconds), so the rows of a run
// compare equal to its stamp.
func stamp(t time.Time) time.Time {
	return t.UTC().Truncate(time.Microsecond)
}
