package ratelimits_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"cosmossdk.io/math"
	ratelimittypes "github.com/cosmos/ibc-apps/modules/rate-limiting/v10/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/ratelimits"
)

type fakeQuery struct {
	limits    []ratelimittypes.RateLimit
	blacklist []string
	allErr    error
	blackErr  error
	calls     atomic.Int32
}

func (f *fakeQuery) AllRateLimits(context.Context, *ratelimittypes.QueryAllRateLimitsRequest, ...grpc.CallOption) (*ratelimittypes.QueryAllRateLimitsResponse, error) {
	f.calls.Add(1)
	if f.allErr != nil {
		return nil, f.allErr
	}
	return &ratelimittypes.QueryAllRateLimitsResponse{RateLimits: f.limits}, nil
}

func (f *fakeQuery) AllBlacklistedDenoms(context.Context, *ratelimittypes.QueryAllBlacklistedDenomsRequest, ...grpc.CallOption) (*ratelimittypes.QueryAllBlacklistedDenomsResponse, error) {
	if f.blackErr != nil {
		return nil, f.blackErr
	}
	return &ratelimittypes.QueryAllBlacklistedDenomsResponse{Denoms: f.blacklist}, nil
}

func limit(denom, channel string, send, recv int64, hours uint64, inflow, outflow, value int64) ratelimittypes.RateLimit {
	return ratelimittypes.RateLimit{
		Path: &ratelimittypes.Path{Denom: denom, ChannelOrClientId: channel},
		Quota: &ratelimittypes.Quota{MaxPercentSend: math.NewInt(send), MaxPercentRecv: math.NewInt(recv),
			DurationHours: hours},
		Flow: &ratelimittypes.Flow{Inflow: math.NewInt(inflow), Outflow: math.NewInt(outflow), ChannelValue: math.NewInt(value)},
	}
}

var noon = time.Date(2026, 11, 25, 12, 30, 0, 0, time.UTC)

func compute(l ratelimittypes.RateLimit) ratelimits.Limit {
	snap := ratelimits.Compute(true, []ratelimittypes.RateLimit{l}, nil, noon)
	return snap.Limits[0]
}

func TestThresholdTruncatesAndRemainingStopsAtTheBoundary(t *testing.T) {
	// 2% of 1,234,567 = 24,691.34: the module truncates to 24,691.
	l := compute(limit("ubze", "channel-0", 2, 2, 24, 0, 1000, 1234567))
	assert.Equal(t, "24691", l.Send.Threshold.String())
	assert.Equal(t, "1000", l.Send.NetFlow.String())
	assert.Equal(t, "1000", l.Send.Used.String())
	// A transfer of exactly remaining passes (net after = threshold, not
	// greater); one more unit fails.
	assert.Equal(t, "23691", l.Send.Remaining.String())
	require.NotNil(t, l.Send.UsedPct)
	assert.InDelta(t, 4.1, *l.Send.UsedPct, 1e-9) // 4.05%, half up
	assert.True(t, l.Send.Enforced)
}

func TestNegativeNetFlowGivesMoreRoomThanTheThreshold(t *testing.T) {
	l := compute(limit("ubze", "channel-0", 2, 2, 24, 5000, 1000, 1000000))
	assert.Equal(t, "20000", l.Send.Threshold.String())
	assert.Equal(t, "-4000", l.Send.NetFlow.String())
	assert.Equal(t, "0", l.Send.Used.String())
	assert.Equal(t, "24000", l.Send.Remaining.String())
	assert.Equal(t, "4000", l.Recv.NetFlow.String())
	assert.Equal(t, "16000", l.Recv.Remaining.String())
	require.NotNil(t, l.Send.UsedPct)
	assert.InDelta(t, 0.0, *l.Send.UsedPct, 1e-9)
}

func TestZeroPercentReceiveRefusesEverything(t *testing.T) {
	l := compute(limit("ibc/USDC", "channel-3", 100, 0, 24, 0, 0, 20000000))
	assert.Equal(t, "0", l.Recv.Threshold.String())
	assert.Equal(t, "0", l.Recv.Remaining.String())
	assert.Nil(t, l.Recv.UsedPct)
	assert.True(t, l.Recv.Enforced, "a threshold of 0 over a real supply refuses every receive")
	assert.Equal(t, "20000000", l.Send.Threshold.String())

	// Once something was sent the net receive is negative: that much may
	// come back (net after = 0 is not greater than the threshold).
	l = compute(limit("ibc/USDC", "channel-3", 100, 0, 24, 0, 5, 20000000))
	assert.Equal(t, "5", l.Recv.Remaining.String())
}

func TestZeroChannelValueNeverRefuses(t *testing.T) {
	l := compute(limit("ubze", "channel-0", 2, 2, 24, 0, 0, 0))
	assert.Equal(t, "0", l.Send.Threshold.String())
	assert.Nil(t, l.Send.UsedPct)
	assert.False(t, l.Send.Enforced)
	assert.False(t, l.Recv.Enforced)
}

func TestUsedPctRoundsToOneDecimal(t *testing.T) {
	l := compute(limit("ubze", "channel-0", 100, 100, 24, 0, 1, 3))
	require.NotNil(t, l.Send.UsedPct)
	assert.InDelta(t, 33.3, *l.Send.UsedPct, 1e-9)
	l = compute(limit("ubze", "channel-0", 100, 100, 24, 0, 2, 3))
	assert.InDelta(t, 66.7, *l.Send.UsedPct, 1e-9)
	l = compute(limit("ubze", "channel-0", 100, 100, 24, 0, 5, 3))
	assert.InDelta(t, 166.7, *l.Send.UsedPct, 1e-9, "over the threshold is shown over 100")
	assert.Equal(t, "0", l.Send.Remaining.String())
}

func TestNextReset(t *testing.T) {
	at := func(h int) *time.Time {
		v := time.Date(2026, 11, 25, h, 0, 0, 0, time.UTC)
		return &v
	}
	next := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		hours uint64
		want  *time.Time
	}{
		{24, &next},
		{6, at(18)},
		{12, &next},
		{1, at(13)},
		{5, nil},
		{0, nil},
		{48, nil},
	}
	for _, c := range cases {
		l := compute(limit("ubze", "channel-0", 2, 2, c.hours, 0, 0, 100))
		assert.Equal(t, c.want, l.NextReset, "%d h", c.hours)
		if c.want == nil {
			assert.Equal(t, c.hours, l.WindowHours)
		}
	}
}

func TestResetIsReckonedInUTC(t *testing.T) {
	zone := time.FixedZone("x", 7200)
	snap := ratelimits.Compute(true, []ratelimittypes.RateLimit{limit("ubze", "channel-0", 2, 2, 24, 0, 0, 100)},
		nil, time.Date(2026, 11, 25, 1, 30, 0, 0, zone)) // 23:30 UTC the day before
	assert.Equal(t, time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC), *snap.Limits[0].NextReset)
}

func TestLimitsAreOrderedByDenomThenChannelNumber(t *testing.T) {
	snap := ratelimits.Compute(true, []ratelimittypes.RateLimit{
		limit("ubze", "channel-10", 2, 2, 24, 0, 0, 1),
		limit("ubze", "channel-2", 2, 2, 24, 0, 0, 1),
		limit("ibc/A", "channel-3", 2, 2, 24, 0, 0, 1),
	}, []string{"ibc/X"}, noon)
	var got []string
	for _, l := range snap.Limits {
		got = append(got, l.Denom+"@"+l.ChannelID)
	}
	assert.Equal(t, []string{"ibc/A@channel-3", "ubze@channel-2", "ubze@channel-10"}, got)
	assert.Equal(t, []string{"ibc/X"}, snap.Blacklisted)
}

func TestUnimplementedIsNotSupported(t *testing.T) {
	q := &fakeQuery{allErr: status.Error(codes.Unimplemented, "unknown service")}
	snap, err := ratelimits.New(q, 0, nil).Snapshot(context.Background())
	require.NoError(t, err)
	assert.False(t, snap.Supported)
	assert.Empty(t, snap.Limits)
	assert.NotNil(t, snap.Blacklisted)
}

func TestBlacklistUnimplementedLeavesTheLimits(t *testing.T) {
	q := &fakeQuery{limits: []ratelimittypes.RateLimit{limit("ubze", "channel-0", 2, 2, 24, 0, 0, 100)},
		blackErr: status.Error(codes.Unimplemented, "no")}
	snap, err := ratelimits.New(q, 0, nil).Snapshot(context.Background())
	require.NoError(t, err)
	assert.True(t, snap.Supported)
	assert.Len(t, snap.Limits, 1)
}

func TestFailuresAreNotCachedAndReadsAre(t *testing.T) {
	now := noon
	clock := func() time.Time { return now }
	q := &fakeQuery{allErr: errors.New("node down")}
	s := ratelimits.New(q, time.Minute, clock)

	_, err := s.Snapshot(context.Background())
	require.Error(t, err)
	q.allErr = nil
	q.limits = []ratelimittypes.RateLimit{limit("ubze", "channel-0", 2, 2, 24, 0, 0, 100)}
	_, err = s.Snapshot(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 2, q.calls.Load())

	// Fresh: served from the cache; next_reset follows the clock.
	now = now.Add(30 * time.Second)
	snap, err := s.Snapshot(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 2, q.calls.Load())
	assert.Len(t, snap.Limits, 1)
	now = time.Date(2026, 11, 25, 23, 59, 30, 0, time.UTC)
	q.calls.Store(0)
	snap, err = s.Snapshot(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, q.calls.Load(), "stale: read again")
	assert.Equal(t, time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC), *snap.Limits[0].NextReset)
}
