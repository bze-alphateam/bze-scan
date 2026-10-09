package validators_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/validators"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// Two recorded validators: ChainTools (bonded, rank 10 of 22) and PaceVali
// (jailed, unbonded, its owner's delegation is down to 0).
const (
	chainTools      = "bzevaloper1prm55vzlp5u6excqdunwlm4tw254cq943m6e6m"
	chainToolsOwner = "bze1prm55vzlp5u6excqdunwlm4tw254cq94fl9jwy"
	chainToolsCons  = "bzevalcons1pyrs8gk9jnzm4y7q6r3x820hnthwzlgsky4x3e"
	paceVali        = "bzevaloper1qawmt58g2kf3pesnwkvss7gxse6gp340ul4l4w"
	recordedCount   = 57
	recordedBonded  = 22
)

// fakeNode answers the staking and slashing queries from the recorded
// fixtures (no network), counting the calls.
type fakeNode struct {
	t *testing.T

	mu    sync.Mutex
	calls map[string]int
	err   error // answered by every call when set
	// pages splits the Validators answer in two pages when true.
	pages bool
}

func newFakeNode(t *testing.T) *fakeNode {
	return &fakeNode{t: t, calls: map[string]int{}}
}

func (n *fakeNode) count(method string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls[method]++
}

func (n *fakeNode) callsOf(method string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls[method]
}

func (n *fakeNode) Validators(_ context.Context, in *stakingtypes.QueryValidatorsRequest, _ ...grpc.CallOption) (*stakingtypes.QueryValidatorsResponse, error) {
	n.count("Validators")
	if n.err != nil {
		return nil, n.err
	}
	var resp stakingtypes.QueryValidatorsResponse
	fakenode.LoadGRPCFixture(n.t, "staking", "Validators", "", &resp)
	resp.Pagination.NextKey = nil
	if n.pages {
		half := len(resp.Validators) / 2
		if string(in.Pagination.Key) == "page2" {
			resp.Validators = resp.Validators[half:]
		} else {
			resp.Validators = resp.Validators[:half]
			resp.Pagination.NextKey = []byte("page2")
		}
	}
	return &resp, nil
}

func (n *fakeNode) Validator(_ context.Context, in *stakingtypes.QueryValidatorRequest, _ ...grpc.CallOption) (*stakingtypes.QueryValidatorResponse, error) {
	n.count("Validator")
	if n.err != nil {
		return nil, n.err
	}
	if in.ValidatorAddr != chainTools {
		return nil, status.Error(codes.NotFound, "validator does not exist")
	}
	var resp stakingtypes.QueryValidatorResponse
	fakenode.LoadGRPCFixture(n.t, "staking", "Validator", in.ValidatorAddr, &resp)
	return &resp, nil
}

func (n *fakeNode) Delegation(_ context.Context, in *stakingtypes.QueryDelegationRequest, _ ...grpc.CallOption) (*stakingtypes.QueryDelegationResponse, error) {
	n.count("Delegation")
	if n.err != nil {
		return nil, n.err
	}
	var resp stakingtypes.QueryDelegationResponse
	key := in.DelegatorAddr + "." + in.ValidatorAddr
	if body := string(fakenode.ReadGRPCFixture(n.t, "staking", "Delegation", key)); strings.Contains(body, `"code": 5`) {
		return nil, status.Error(codes.NotFound, "delegation not found")
	}
	fakenode.LoadGRPCFixture(n.t, "staking", "Delegation", key, &resp)
	return &resp, nil
}

func (n *fakeNode) ValidatorDelegations(_ context.Context, in *stakingtypes.QueryValidatorDelegationsRequest, _ ...grpc.CallOption) (*stakingtypes.QueryValidatorDelegationsResponse, error) {
	n.count("ValidatorDelegations")
	if n.err != nil {
		return nil, n.err
	}
	assert.True(n.t, in.Pagination.CountTotal)
	assert.Equal(n.t, uint64(1), in.Pagination.Limit, "the count needs no rows")
	var resp stakingtypes.QueryValidatorDelegationsResponse
	fakenode.LoadGRPCFixture(n.t, "staking", "ValidatorDelegations", in.ValidatorAddr, &resp)
	return &resp, nil
}

func (n *fakeNode) Params(context.Context, *slashingtypes.QueryParamsRequest, ...grpc.CallOption) (*slashingtypes.QueryParamsResponse, error) {
	n.count("Params")
	if n.err != nil {
		return nil, n.err
	}
	var resp slashingtypes.QueryParamsResponse
	fakenode.LoadGRPCFixture(n.t, "slashing", "Params", "", &resp)
	return &resp, nil
}

func (n *fakeNode) SigningInfo(_ context.Context, in *slashingtypes.QuerySigningInfoRequest, _ ...grpc.CallOption) (*slashingtypes.QuerySigningInfoResponse, error) {
	n.count("SigningInfo")
	if n.err != nil {
		return nil, n.err
	}
	var resp slashingtypes.QuerySigningInfoResponse
	fakenode.LoadGRPCFixture(n.t, "slashing", "SigningInfo", in.ConsAddress, &resp)
	return &resp, nil
}

func (n *fakeNode) SigningInfos(context.Context, *slashingtypes.QuerySigningInfosRequest, ...grpc.CallOption) (*slashingtypes.QuerySigningInfosResponse, error) {
	n.count("SigningInfos")
	if n.err != nil {
		return nil, n.err
	}
	var resp slashingtypes.QuerySigningInfosResponse
	fakenode.LoadGRPCFixture(n.t, "slashing", "SigningInfos", "", &resp)
	resp.Pagination.NextKey = nil
	return &resp, nil
}

// fakeStore keeps the standings and records the snapshots.
type fakeStore struct {
	standings []validators.Standing
	saved     []validators.Snapshot
	err       error
}

func (s *fakeStore) Standings(context.Context) ([]validators.Standing, error) {
	return s.standings, s.err
}

func (s *fakeStore) Save(_ context.Context, snap validators.Snapshot) error {
	s.saved = append(s.saved, snap)
	return s.err
}

var (
	codecOnce sync.Once
	codec     *chain.Codec
)

func newSet(n *fakeNode, st *fakeStore) *validators.Set {
	codecOnce.Do(func() {
		var err error
		codec, err = chain.NewCodec()
		require.NoError(n.t, err)
	})
	return validators.New(validators.Deps{Staking: n, Slashing: n, Store: st, Keys: codec.InterfaceRegistry()}, 0)
}

func find(t *testing.T, vals []validators.Validator, op string) validators.Validator {
	t.Helper()
	i := slices.IndexFunc(vals, func(v validators.Validator) bool { return v.OperatorAddress == op })
	require.GreaterOrEqual(t, i, 0, op)
	return vals[i]
}

func ptr[T any](v T) *T { return &v }

func date(s string) *time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func TestSetIdentity(t *testing.T) {
	set := validators.New(validators.Deps{}, 0)
	assert.Equal(t, statesync.Validators, set.Name())
	assert.Equal(t, time.Minute, set.Interval())
	assert.Equal(t, time.Second, validators.New(validators.Deps{}, time.Second).Interval())
}

func TestFullResyncMapsEveryColumn(t *testing.T) {
	n, st := newFakeNode(t), &fakeStore{}
	require.NoError(t, newSet(n, st).FullResync(context.Background()))
	require.Len(t, st.saved, 1)
	snap := st.saved[0]
	assert.True(t, snap.Full, "absent validators become unbonded")
	require.Len(t, snap.Validators, recordedCount)

	window := ptr(int64(10000))
	assert.Equal(t, validators.Validator{
		OperatorAddress:         chainTools,
		AccountAddress:          chainToolsOwner,
		ConsensusAddress:        "090703A2C594C5BA93C0D0E263A9F79AEEE17D10",
		ConsensusPubkey:         "ikmP1GM73Y1vVKsLZYjFENPGve7uLWV3Q+8YF60LHMA=",
		Moniker:                 "ChainTools",
		Identity:                "1857FF1BC2EA6A69",
		Website:                 "https://chaintools.tech",
		SecurityContact:         "contact@chaintools.tech",
		Details:                 "STAKE | LEARN | EARN | Delegate to our validators and dive into blockchain technology with us.",
		Status:                  validators.StatusBonded,
		Tokens:                  "7895930982372",
		DelegatorShares:         "7895930982372.000000000000000000",
		CommissionRate:          "0.050000000000000000",
		CommissionMaxRate:       "0.500000000000000000",
		CommissionMaxChangeRate: "0.100000000000000000",
		CommissionUpdateTime:    date("2022-04-29T05:31:29.027929378Z"),
		MinSelfDelegation:       "1",
		SelfDelegation:          "30081000000",
		DelegatorCount:          57,
		MissedBlocks:            ptr(int64(0)),
		SignedBlocksWindow:      window,
	}, find(t, snap.Validators, chainTools))

	pace := find(t, snap.Validators, paceVali)
	assert.Equal(t, validators.StatusUnbonded, pace.Status)
	assert.True(t, pace.Jailed)
	assert.Equal(t, date("2024-07-09T04:05:25.548177853Z"), pace.JailedUntil)
	assert.Empty(t, pace.SecurityContact, "stored as NULL")
	assert.Equal(t, "0", pace.SelfDelegation)
	assert.Equal(t, int64(61), pace.DelegatorCount)

	// An owner without any delegation (the node answers NotFound).
	noSelf := find(t, snap.Validators, "bzevaloper1jv9wveqgmuwje2qlaxd6jv4n488uyzfwr2gsyt")
	assert.Equal(t, "0", noSelf.SelfDelegation)

	require.Len(t, snap.Ranks, recordedBonded)
	i := slices.IndexFunc(snap.Ranks, func(r validators.Ranking) bool { return r.Operator == chainTools })
	assert.Equal(t, validators.Ranking{Operator: chainTools, Rank: 10, VotingPowerPct: "5.37048"}, snap.Ranks[i])

	assert.Equal(t, 1, n.callsOf("Validators"))
	assert.Equal(t, 1, n.callsOf("SigningInfos"))
	assert.Zero(t, n.callsOf("SigningInfo"), "the list answers every signing info")
	assert.Equal(t, recordedCount, n.callsOf("Delegation"))
	assert.Equal(t, recordedCount, n.callsOf("ValidatorDelegations"))
}

func TestFullResyncFollowsPages(t *testing.T) {
	n, st := newFakeNode(t), &fakeStore{}
	n.pages = true
	require.NoError(t, newSet(n, st).FullResync(context.Background()))
	assert.Equal(t, 2, n.callsOf("Validators"))
	assert.Len(t, st.saved[0].Validators, recordedCount)
}

func TestNodeDownKeepsTheRows(t *testing.T) {
	n, st := newFakeNode(t), &fakeStore{}
	n.err = status.Error(codes.Unavailable, "connection refused")
	set := newSet(n, st)

	err := set.FullResync(context.Background())
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(errors.Unwrap(err)))
	err = set.ResyncOne(context.Background(), chainTools)
	require.Error(t, err)
	assert.Empty(t, st.saved, "nothing written: the stored rows stay as they were")
}

func TestResyncOneReranksOverTheStoredStandings(t *testing.T) {
	n := newFakeNode(t)
	st := &fakeStore{standings: []validators.Standing{
		{Operator: "bzevaloper1big", Status: validators.StatusBonded, Tokens: sdkmath.NewInt(9_000_000_000_000)},
		{Operator: chainTools, Status: validators.StatusUnbonded, Tokens: sdkmath.NewInt(1)}, // stale row
		{Operator: "bzevaloper1small", Status: validators.StatusBonded, Tokens: sdkmath.NewInt(1_000_000_000_000)},
		{Operator: "bzevaloper1gone", Status: validators.StatusUnbonded, Tokens: sdkmath.NewInt(5)},
	}}
	require.NoError(t, newSet(n, st).ResyncOne(context.Background(), chainTools))

	require.Len(t, st.saved, 1)
	snap := st.saved[0]
	assert.False(t, snap.Full)
	require.Len(t, snap.Validators, 1)
	v := snap.Validators[0]
	assert.Equal(t, chainTools, v.OperatorAddress)
	assert.Equal(t, "ChainTools", v.Moniker)
	assert.Equal(t, ptr(int64(0)), v.MissedBlocks, "from its own signing info")
	assert.Equal(t, int64(57), v.DelegatorCount)
	assert.Equal(t, "30081000000", v.SelfDelegation)
	assert.Equal(t, []validators.Ranking{
		{Operator: "bzevaloper1big", Rank: 1, VotingPowerPct: "50.29076"},
		{Operator: chainTools, Rank: 2, VotingPowerPct: "44.12138"},
		{Operator: "bzevaloper1small", Rank: 3, VotingPowerPct: "5.58786"},
	}, snap.Ranks, "the fresh tokens replace the stored ones")

	assert.Equal(t, 1, n.callsOf("Validator"))
	assert.Equal(t, 1, n.callsOf("SigningInfo"))
	assert.Zero(t, n.callsOf("Validators"))
}

func TestResyncOneOfAValidatorTheNodeNoLongerKnows(t *testing.T) {
	n := newFakeNode(t)
	st := &fakeStore{standings: []validators.Standing{
		{Operator: "bzevaloper1gone", Status: validators.StatusBonded, Tokens: sdkmath.NewInt(5)},
		{Operator: "bzevaloper1other", Status: validators.StatusBonded, Tokens: sdkmath.NewInt(15)},
	}}
	require.NoError(t, newSet(n, st).ResyncOne(context.Background(), "bzevaloper1gone"))
	require.Len(t, st.saved, 1)
	assert.Equal(t, validators.Snapshot{
		Unbonded: []string{"bzevaloper1gone"},
		Ranks:    []validators.Ranking{{Operator: "bzevaloper1other", Rank: 1, VotingPowerPct: "100.00000"}},
	}, st.saved[0])
}

func TestResyncOneOfAllIsAFullResync(t *testing.T) {
	n, st := newFakeNode(t), &fakeStore{}
	require.NoError(t, newSet(n, st).ResyncOne(context.Background(), statesync.All))
	require.Len(t, st.saved, 1)
	assert.True(t, st.saved[0].Full)
}

func TestRank(t *testing.T) {
	st := func(op, status string, tokens int64) validators.Standing {
		return validators.Standing{Operator: op, Status: status, Tokens: sdkmath.NewInt(tokens)}
	}
	cases := map[string]struct {
		in   []validators.Standing
		want []validators.Ranking
	}{
		"empty": {in: nil, want: []validators.Ranking{}},
		"only bonded validators are ranked, ties by operator": {
			in: []validators.Standing{
				st("c", validators.StatusBonded, 1), st("b", validators.StatusBonded, 1),
				st("a", validators.StatusUnbonding, 100), st("d", validators.StatusUnbonded, 100),
				st("e", validators.StatusBonded, 2),
			},
			want: []validators.Ranking{{"e", 1, "50.00000"}, {"b", 2, "25.00000"}, {"c", 3, "25.00000"}},
		},
		"rounded half up to five decimals": {
			in:   []validators.Standing{st("a", validators.StatusBonded, 2), st("b", validators.StatusBonded, 1)},
			want: []validators.Ranking{{"a", 1, "66.66667"}, {"b", 2, "33.33333"}},
		},
		"tiny shares": {
			in:   []validators.Standing{st("a", validators.StatusBonded, 999_999_999), st("b", validators.StatusBonded, 1)},
			want: []validators.Ranking{{"a", 1, "100.00000"}, {"b", 2, "0.00000"}},
		},
		"a half step rounds up": {
			in:   []validators.Standing{st("a", validators.StatusBonded, 15), st("b", validators.StatusBonded, 9_999_985)},
			want: []validators.Ranking{{"b", 1, "99.99985"}, {"a", 2, "0.00015"}},
		},
		"zero tokens": {
			in:   []validators.Standing{st("a", validators.StatusBonded, 0)},
			want: []validators.Ranking{{"a", 1, "0.00000"}},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, c.want, validators.Rank(c.in))
		})
	}
}
