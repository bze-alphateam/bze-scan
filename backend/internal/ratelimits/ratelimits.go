// Package ratelimits reads the IBC rate limits of chain v8.3.0 (the
// cosmos/ibc-apps rate-limiting module) from the local node's gRPC and works
// out, for each limit and direction, the threshold, the net flow of the
// window, what is used, what room is left and when the window resets. The
// API serves them through a short in-process cache (Cached); the explorer
// stores nothing of them.
package ratelimits

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cosmossdk.io/math"
	ratelimittypes "github.com/cosmos/ibc-apps/modules/rate-limiting/v10/types"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DefaultTTL is how long Cached serves a read of the node.
const DefaultTTL = 30 * time.Second

// Query is the part of the module's query client the reader uses;
// ratelimittypes.QueryClient satisfies it.
type Query interface {
	AllRateLimits(ctx context.Context, in *ratelimittypes.QueryAllRateLimitsRequest, opts ...grpc.CallOption) (*ratelimittypes.QueryAllRateLimitsResponse, error)
	AllBlacklistedDenoms(ctx context.Context, in *ratelimittypes.QueryAllBlacklistedDenomsRequest, opts ...grpc.CallOption) (*ratelimittypes.QueryAllBlacklistedDenomsResponse, error)
}

var _ Query = ratelimittypes.QueryClient(nil)

// Direction is one direction (send or receive) of a limit in its current
// window. Amounts are base units.
type Direction struct {
	// MaxPercent is the quota, a percent of the channel value.
	MaxPercent math.Int
	// Threshold is channel_value × max_percent / 100, truncated as the
	// module does.
	Threshold math.Int
	// NetFlow is the flow of this direction minus the opposite one; negative
	// when more went the other way.
	NetFlow math.Int
	// Used is NetFlow floored at zero.
	Used math.Int
	// Remaining is Threshold − NetFlow floored at zero: the largest transfer
	// the module still lets through (it refuses a net flow strictly greater
	// than the threshold).
	Remaining math.Int
	// UsedPct is Used / Threshold in percent, one decimal; nil when the
	// threshold is 0.
	UsedPct *float64
	// Enforced is false when the module never refuses: a channel value of 0.
	Enforced bool
}

// Limit is one (denom, channel) rate limit.
type Limit struct {
	Denom     string
	ChannelID string
	// WindowHours is the quota's duration.
	WindowHours uint64
	// NextReset is the next window reset (the first block after that UTC
	// hour); nil when the duration does not divide 24, so the phase of its
	// windows is not derivable from the queries.
	NextReset *time.Time
	// ChannelValue is the denom's supply when the window started.
	ChannelValue math.Int
	Inflow       math.Int
	Outflow      math.Int
	Send         Direction
	Recv         Direction
}

// Snapshot is every limit of the node and its blacklisted denoms.
type Snapshot struct {
	// Supported is false when the node does not have the module (it answers
	// Unimplemented): before chain v8.3.0.
	Supported   bool
	Limits      []Limit
	Blacklisted []string
}

// Reader reads the limits.
type Reader interface {
	// Snapshot returns the limits, reading the node when the cache is stale.
	Snapshot(ctx context.Context) (*Snapshot, error)
	// Peek returns the last successful read, whatever its age, and false
	// when there is none yet. It never calls the node.
	Peek() (*Snapshot, bool)
}

// Service reads the limits through Query, caching the read of the node for a
// TTL (concurrent misses share one read; only a successful read is cached).
// next_reset is worked out from the clock at every call. It implements
// Reader.
type Service struct {
	query Query
	ttl   time.Duration
	now   func() time.Time

	group singleflight.Group
	mu    sync.Mutex
	raw   *read
	at    time.Time
}

var _ Reader = (*Service)(nil)

// New returns a service over query; ttl zero is DefaultTTL, now nil the wall
// clock.
func New(query Query, ttl time.Duration, now func() time.Time) *Service {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if now == nil {
		now = time.Now
	}
	return &Service{query: query, ttl: ttl, now: now}
}

// read is what the node answered.
type read struct {
	supported   bool
	limits      []ratelimittypes.RateLimit
	blacklisted []string
}

// Snapshot returns the limits with their computed fields.
func (s *Service) Snapshot(ctx context.Context) (*Snapshot, error) {
	r, err := s.cached(ctx)
	if err != nil {
		return nil, err
	}
	return compute(r, s.now()), nil
}

// Peek returns the last good read without calling the node.
func (s *Service) Peek() (*Snapshot, bool) {
	s.mu.Lock()
	r := s.raw
	s.mu.Unlock()
	if r == nil {
		return nil, false
	}
	return compute(r, s.now()), true
}

// Run keeps the cache warm: it reads the node every TTL until ctx is done,
// so the pages that only Peek have something to show. A failed read is
// logged and tried again at the next tick.
func (s *Service) Run(ctx context.Context) error {
	tick := time.NewTicker(s.ttl)
	defer tick.Stop()
	for {
		if _, err := s.Snapshot(ctx); err != nil {
			log.WithError(err).Warn("rate limits: reading the node failed")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

func (s *Service) cached(ctx context.Context) (*read, error) {
	s.mu.Lock()
	if s.raw != nil && s.now().Sub(s.at) < s.ttl {
		r := s.raw
		s.mu.Unlock()
		return r, nil
	}
	s.mu.Unlock()
	type answer struct {
		r   *read
		err error
	}
	res, _, _ := s.group.Do("read", func() (any, error) {
		r, err := s.fetch(context.WithoutCancel(ctx))
		if err == nil {
			s.mu.Lock()
			s.raw, s.at = r, s.now()
			s.mu.Unlock()
		}
		return answer{r: r, err: err}, nil
	})
	a, _ := res.(answer) // the closure returns only answers
	return a.r, a.err
}

func (s *Service) fetch(ctx context.Context) (*read, error) {
	all, err := s.query.AllRateLimits(ctx, &ratelimittypes.QueryAllRateLimitsRequest{})
	if status.Code(err) == codes.Unimplemented {
		return &read{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("rate limits: %w", err)
	}
	out := &read{supported: true, limits: all.RateLimits}
	black, err := s.query.AllBlacklistedDenoms(ctx, &ratelimittypes.QueryAllBlacklistedDenomsRequest{})
	switch {
	case status.Code(err) == codes.Unimplemented:
	case err != nil:
		return nil, fmt.Errorf("blacklisted denoms: %w", err)
	default:
		out.blacklisted = black.Denoms
	}
	return out, nil
}

// compute works the computed fields out of what the node answered at now.
func compute(r *read, now time.Time) *Snapshot {
	snap := &Snapshot{Supported: r.supported, Limits: make([]Limit, 0, len(r.limits)), Blacklisted: []string{}}
	if !r.supported {
		return snap
	}
	snap.Blacklisted = append(snap.Blacklisted, r.blacklisted...)
	for _, rl := range r.limits {
		snap.Limits = append(snap.Limits, computeLimit(rl, now))
	}
	sort.SliceStable(snap.Limits, func(i, j int) bool {
		a, b := snap.Limits[i], snap.Limits[j]
		if a.Denom != b.Denom {
			return a.Denom < b.Denom
		}
		an, bn := channelNumber(a.ChannelID), channelNumber(b.ChannelID)
		if an != bn {
			return an < bn
		}
		return a.ChannelID < b.ChannelID
	})
	return snap
}

func computeLimit(rl ratelimittypes.RateLimit, now time.Time) Limit {
	l := Limit{}
	if rl.Path != nil {
		l.Denom, l.ChannelID = rl.Path.Denom, rl.Path.ChannelOrClientId
	}
	send, recv := math.ZeroInt(), math.ZeroInt()
	if rl.Quota != nil {
		l.WindowHours = rl.Quota.DurationHours
		send, recv = orZero(rl.Quota.MaxPercentSend), orZero(rl.Quota.MaxPercentRecv)
	}
	l.ChannelValue, l.Inflow, l.Outflow = math.ZeroInt(), math.ZeroInt(), math.ZeroInt()
	if rl.Flow != nil {
		l.ChannelValue, l.Inflow, l.Outflow = orZero(rl.Flow.ChannelValue), orZero(rl.Flow.Inflow), orZero(rl.Flow.Outflow)
	}
	l.NextReset = nextReset(now, l.WindowHours)
	l.Send = direction(l.ChannelValue, send, l.Outflow.Sub(l.Inflow))
	l.Recv = direction(l.ChannelValue, recv, l.Inflow.Sub(l.Outflow))
	return l
}

// direction mirrors the module's quota check: the threshold is
// channel_value × percent / 100 truncated, a transfer is refused when the net
// flow after it is strictly greater than the threshold, and a channel value
// of 0 never refuses.
func direction(channelValue, percent, net math.Int) Direction {
	threshold := channelValue.Mul(percent).Quo(math.NewInt(100))
	used := net
	if used.IsNegative() {
		used = math.ZeroInt()
	}
	remaining := threshold.Sub(net)
	if remaining.IsNegative() {
		remaining = math.ZeroInt()
	}
	return Direction{
		MaxPercent: percent, Threshold: threshold, NetFlow: net, Used: used, Remaining: remaining,
		UsedPct: usedPct(used, threshold), Enforced: channelValue.IsPositive(),
	}
}

// usedPct is used / threshold in percent rounded to one decimal; nil for a
// threshold of 0.
func usedPct(used, threshold math.Int) *float64 {
	if !threshold.IsPositive() {
		return nil
	}
	// tenths of a percent = round(used × 1000 / threshold), half up
	num := new(big.Int).Mul(used.BigInt(), big.NewInt(2000))
	tenths := num.Quo(num, threshold.BigInt())
	tenths.Add(tenths, big.NewInt(1)).Quo(tenths, big.NewInt(2))
	f, _ := new(big.Float).SetInt(tenths).Float64()
	v := f / 10
	return &v
}

// nextReset is the next UTC hour after now at which the module resets a
// limit of hours hours: it counts hour epochs from the hour of day of its
// genesis and resets when the epoch number is a multiple of the duration, so
// a duration that divides 24 resets at the UTC hours that are multiples of
// it. Any other duration has no phase the queries show.
func nextReset(now time.Time, hours uint64) *time.Time {
	if hours == 0 || hours > 24 || 24%hours != 0 {
		return nil
	}
	t := now.UTC().Truncate(time.Hour).Add(time.Hour)
	for uint64(t.Hour())%hours != 0 {
		t = t.Add(time.Hour)
	}
	return &t
}

func orZero(i math.Int) math.Int {
	if i.IsNil() {
		return math.ZeroInt()
	}
	return i
}

// channelNumber is N of "channel-N", -1 for anything else (a client id).
func channelNumber(id string) int64 {
	n, err := strconv.ParseInt(strings.TrimPrefix(id, "channel-"), 10, 64)
	if err != nil || !strings.HasPrefix(id, "channel-") {
		return -1
	}
	return n
}
