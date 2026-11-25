package daostate_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"
	rewardstypes "github.com/bze-alphateam/bze/x/rewards/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/daostate"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

type fakeDAO struct {
	daos       map[uint64]daotypes.Dao
	controller *daotypes.QueryDaoControllerResponse
	err        error
	calls      atomic.Int32
}

func (f *fakeDAO) Params(context.Context, *daotypes.QueryParamsRequest, ...grpc.CallOption) (*daotypes.QueryParamsResponse, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return &daotypes.QueryParamsResponse{Params: daotypes.Params{MaxSrPerDao: 5, MaxStaticMembers: 100}}, nil
}

func (f *fakeDAO) Dao(_ context.Context, in *daotypes.QueryDaoRequest, _ ...grpc.CallOption) (*daotypes.QueryDaoResponse, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	d, ok := f.daos[in.Id]
	if !ok {
		return nil, status.Error(codes.NotFound, "dao not found")
	}
	return &daotypes.QueryDaoResponse{Dao: d}, nil
}

func (f *fakeDAO) DaoController(_ context.Context, _ *daotypes.QueryDaoControllerRequest, _ ...grpc.CallOption) (*daotypes.QueryDaoControllerResponse, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return f.controller, nil
}

type fakeRewards struct {
	staking map[string]rewardstypes.StakingReward
	denom   map[string]rewardstypes.DenomReward
	err     error
}

func (f *fakeRewards) StakingReward(_ context.Context, in *rewardstypes.QueryGetStakingRewardRequest, _ ...grpc.CallOption) (*rewardstypes.QueryGetStakingRewardResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	sr, ok := f.staking[in.RewardId]
	if !ok {
		return nil, status.Error(codes.NotFound, "no such reward")
	}
	return &rewardstypes.QueryGetStakingRewardResponse{StakingReward: sr}, nil
}

func (f *fakeRewards) DenomReward(_ context.Context, in *rewardstypes.QueryDenomRewardRequest, _ ...grpc.CallOption) (*rewardstypes.QueryDenomRewardResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	dr, ok := f.denom[in.Denom]
	if !ok {
		return nil, status.Error(codes.NotFound, "no such reward")
	}
	return &rewardstypes.QueryDenomRewardResponse{DenomReward: dr}, nil
}

var (
	staticDao = daotypes.Dao{
		Id: 1, Address: "bze1dao1", Admin: "bze1dao1", Type: daotypes.DaoType_DAO_TYPE_STATIC_LIST, Name: "Founders",
		Description: "the founders", Members: []string{"bze1a", "bze1b"}, CreatedAt: 1700000000,
		Config: daotypes.DaoConfig{
			MinQuorum: sdkmath.LegacyMustNewDecFromStr("0.4"), Threshold: sdkmath.LegacyMustNewDecFromStr("0.5"),
			VotingPeriod: 72 * time.Hour, AllowExpedite: true, ExpeditedPeriod: 24 * time.Hour,
			ExpediteQuorum: sdkmath.LegacyMustNewDecFromStr("0.6"), ExpediteThreshold: sdkmath.LegacyMustNewDecFromStr("0.7"),
			ProposalCost: &sdk.Coin{Denom: "ubze", Amount: sdkmath.NewInt(5)}, ProposalDeposit: &sdk.Coin{Denom: "ubze", Amount: sdkmath.NewInt(100)},
			DepositRefundPolicy: daotypes.DepositRefundPolicy_ONLY_PASSED, CostDestination: daotypes.FundsDestination_TREASURY,
			KeptDepositDestination: daotypes.FundsDestination_BURN,
		},
	}
	stakeDao = daotypes.Dao{
		Id: 2, Address: "bze1dao2", Admin: "bze1admin", PendingAdmin: "bze1next", Type: daotypes.DaoType_DAO_TYPE_STAKE,
		Name: "Stakers", AdminSeq: 2, PendingAdminSeq: 4, Electorate: &daotypes.StakeElectorate{SrIds: []string{"sr1", "gone", "sr2"}, DrDenom: "factory/x/y"},
	}
)

func newReader(dao *fakeDAO, rw *fakeRewards) *daostate.Reader {
	return daostate.New(daostate.Config{}, daostate.Deps{DAO: dao, Rewards: rw})
}

func fixtureDAO() *fakeDAO {
	return &fakeDAO{
		daos:       map[uint64]daotypes.Dao{1: staticDao, 2: stakeDao},
		controller: &daotypes.QueryDaoControllerResponse{Controller: "bze1admin", EndReason: daotypes.ControllerEndReason_ACCOUNT, Path: []string{"bze1dao2"}},
	}
}

func fixtureRewards() *fakeRewards {
	return &fakeRewards{
		staking: map[string]rewardstypes.StakingReward{
			"sr1": {RewardId: "sr1", StakingDenom: "ubze", StakedAmount: "1000"},
			"sr2": {RewardId: "sr2", StakingDenom: "ubze", StakedAmount: "250"},
		},
		denom: map[string]rewardstypes.DenomReward{"factory/x/y": {StakingDenom: "ubze", StakedAmount: sdkmath.NewInt(70)}},
	}
}

func TestLiveStaticListDao(t *testing.T) {
	live, err := newReader(fixtureDAO(), fixtureRewards()).Live(context.Background(), 1)
	require.NoError(t, err)
	d := live.Dao
	assert.Equal(t, "static_list", d.Type)
	assert.Equal(t, []string{"bze1a", "bze1b"}, d.Members)
	assert.Nil(t, d.Electorate)
	assert.Nil(t, live.Stake, "a static-list DAO has no electorate stake")
	assert.Equal(t, daostate.VotingConfig{
		MinQuorum: "0.400000000000000000", Threshold: "0.500000000000000000", VotingPeriodS: 259200, AllowExpedite: true,
		ExpeditedPeriodS: 86400, ExpediteQuorum: "0.600000000000000000", ExpediteThreshold: "0.700000000000000000",
		ProposalCost: &daostate.Coin{Denom: "ubze", Amount: "5"}, ProposalDeposit: &daostate.Coin{Denom: "ubze", Amount: "100"},
		DepositRefundPolicy: "only_passed", CostDestination: "treasury", KeptDepositDestination: "burn",
	}, d.Config)
	assert.Equal(t, daostate.Controller{Address: "bze1admin", EndReason: "account", Path: []string{"bze1dao2"}}, live.Controller)
}

// A stake DAO's electorate stake is the sum of its sources' staked amounts:
// two staking rewards and a denom reward; a reward the chain no longer has
// holds nothing.
func TestLiveStakeDaoSumsItsSources(t *testing.T) {
	live, err := newReader(fixtureDAO(), fixtureRewards()).Live(context.Background(), 2)
	require.NoError(t, err)
	require.NotNil(t, live.Stake)
	assert.Equal(t, []daostate.Source{
		{SrID: "sr1", StakingDenom: "ubze", StakedAmount: daostate.Coin{Denom: "ubze", Amount: "1000"}},
		{SrID: "gone", StakedAmount: daostate.Coin{Denom: "ubze", Amount: "0"}},
		{SrID: "sr2", StakingDenom: "ubze", StakedAmount: daostate.Coin{Denom: "ubze", Amount: "250"}},
		{DrDenom: "factory/x/y", StakingDenom: "ubze", StakedAmount: daostate.Coin{Denom: "ubze", Amount: "70"}},
	}, live.Stake.Sources)
	assert.Equal(t, daostate.Coin{Denom: "ubze", Amount: "1320"}, live.Stake.VotingStake)
	assert.Equal(t, "bze1next", live.Dao.PendingAdmin)
	assert.Equal(t, uint64(2), live.Dao.AdminSeq)
	assert.Equal(t, uint64(4), live.Dao.PendingAdminSeq)
}

func TestUnknownDaoIsNotFound(t *testing.T) {
	_, err := newReader(fixtureDAO(), fixtureRewards()).Live(context.Background(), 99)
	assert.ErrorIs(t, err, daostate.ErrNotFound)
}

// A node before v8.3.0 answers Unimplemented; a node that is down fails the
// read; a failing rewards source fails a stake DAO's read.
func TestNodeFailures(t *testing.T) {
	old := fixtureDAO()
	old.err = status.Error(codes.Unimplemented, "unknown service")
	r := newReader(old, fixtureRewards())
	_, err := r.Live(context.Background(), 1)
	assert.ErrorIs(t, err, daostate.ErrUnsupported)
	assert.False(t, r.Supported(context.Background()), "never answered")

	down := fixtureDAO()
	down.err = status.Error(codes.Unavailable, "down")
	_, err = newReader(down, fixtureRewards()).Live(context.Background(), 1)
	require.Error(t, err)
	assert.NotErrorIs(t, err, daostate.ErrNotFound)
	assert.NotErrorIs(t, err, daostate.ErrUnsupported)

	rw := fixtureRewards()
	rw.err = status.Error(codes.Unavailable, "down")
	_, err = newReader(fixtureDAO(), rw).Live(context.Background(), 2)
	assert.ErrorContains(t, err, "reward")
	_, err = newReader(fixtureDAO(), rw).Live(context.Background(), 1)
	assert.NoError(t, err, "a static-list DAO reads no rewards")
}

// Supported turns true once a query answered, and stays true when the node
// goes down afterwards.
func TestSupportedIsStickyOnceTheNodeAnswered(t *testing.T) {
	dao := fixtureDAO()
	now := time.Unix(1000, 0)
	r := daostate.New(daostate.Config{Now: func() time.Time { return now }}, daostate.Deps{DAO: dao, Rewards: fixtureRewards()})
	assert.True(t, r.Supported(context.Background()))
	dao.err = status.Error(codes.Unavailable, "down")
	assert.True(t, r.Supported(context.Background()))

	// A NotFound is an answer too.
	other := fixtureDAO()
	r2 := newReader(other, fixtureRewards())
	_, err := r2.Dao(context.Background(), 99)
	require.ErrorIs(t, err, daostate.ErrNotFound)
	other.err = status.Error(codes.Unavailable, "down")
	assert.True(t, r2.Supported(context.Background()))
}

func TestParams(t *testing.T) {
	p, err := newReader(fixtureDAO(), fixtureRewards()).Params(context.Background())
	require.NoError(t, err)
	assert.Equal(t, daostate.Params{MaxSrPerDao: 5, MaxStaticMembers: 100}, p)
}

// Answers are cached for the TTL, so open tabs of one DAO cost one query.
func TestAnswersAreCachedUntilTheTTLPasses(t *testing.T) {
	dao := fixtureDAO()
	now := time.Unix(1000, 0)
	r := daostate.New(daostate.Config{Now: func() time.Time { return now }}, daostate.Deps{DAO: dao, Rewards: fixtureRewards()})
	for range 3 {
		_, err := r.Dao(context.Background(), 1)
		require.NoError(t, err)
	}
	assert.EqualValues(t, 1, dao.calls.Load())

	now = now.Add(daostate.DefaultTTL + time.Second)
	_, err := r.Dao(context.Background(), 1)
	require.NoError(t, err)
	assert.EqualValues(t, 2, dao.calls.Load())
}

// A node without x/dao is asked once per TTL, not once per request.
func TestSupportedRemembersAFailureForTheTTL(t *testing.T) {
	dao := fixtureDAO()
	dao.err = status.Error(codes.Unimplemented, "unknown service")
	now := time.Unix(1000, 0)
	r := daostate.New(daostate.Config{Now: func() time.Time { return now }}, daostate.Deps{DAO: dao, Rewards: fixtureRewards()})
	for range 5 {
		assert.False(t, r.Supported(context.Background()))
	}
	assert.EqualValues(t, 1, dao.calls.Load())

	// The node upgrades: the next look after the TTL finds it.
	dao.err = nil
	now = now.Add(daostate.DefaultTTL + time.Second)
	assert.True(t, r.Supported(context.Background()))
}

// The hand-written gRPC fixtures of the DAO e2e tests parse as the chain's
// proto JSON.
func TestTheFakeNodeFixturesDecode(t *testing.T) {
	var daos daotypes.QueryDaosResponse
	fakenode.LoadGRPCFixture(t, "dao", "Daos", "", &daos)
	require.Len(t, daos.Daos, 2)
	assert.Equal(t, daotypes.DaoType_DAO_TYPE_STATIC_LIST, daos.Daos[0].Type)
	assert.Equal(t, daos.Daos[0].Address, daos.Daos[0].Admin, "DAO 1 owns itself")
	assert.Equal(t, daotypes.DaoType_DAO_TYPE_STAKE, daos.Daos[1].Type)
	require.NotNil(t, daos.Daos[1].Electorate)
	assert.Equal(t, []string{"sr-1", "sr-gone"}, daos.Daos[1].Electorate.SrIds)
	assert.Equal(t, 72*time.Hour, daos.Daos[1].Config.VotingPeriod)
	assert.Equal(t, daotypes.DepositRefundPolicy_ONLY_PASSED, daos.Daos[1].Config.DepositRefundPolicy)

	for _, id := range []string{"1", "2"} {
		var dao daotypes.QueryDaoResponse
		fakenode.LoadGRPCFixture(t, "dao", "Dao", id, &dao)
		assert.Equal(t, id, fmt.Sprint(dao.Dao.Id))
		var ctl daotypes.QueryDaoControllerResponse
		fakenode.LoadGRPCFixture(t, "dao", "DaoController", id, &ctl)
		assert.NotEmpty(t, ctl.Controller)
	}
	var sr rewardstypes.QueryGetStakingRewardResponse
	fakenode.LoadGRPCFixture(t, "rewards", "StakingReward", "sr-1", &sr)
	assert.Equal(t, "1000000", sr.StakingReward.StakedAmount)
	var dr rewardstypes.QueryDenomRewardResponse
	fakenode.LoadGRPCFixture(t, "rewards", "DenomReward", "ubze", &dr)
	assert.Equal(t, "250000", dr.DenomReward.StakedAmount.String())
}

// The reader over a real gRPC connection to the fake node: the fixtures'
// file names match the requests the reader sends.
func TestLiveOverTheFakeGRPC(t *testing.T) {
	g := fakenode.NewGRPC(t)
	codec, err := chain.NewCodec()
	require.NoError(t, err)
	conn, err := grpcclient.Dial(grpcclient.Config{Addr: g.Addr}, codec.GRPC())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	r := daostate.New(daostate.Config{}, daostate.Deps{DAO: daotypes.NewQueryClient(conn), Rewards: rewardstypes.NewQueryClient(conn)})

	live, err := r.Live(context.Background(), 2)
	require.NoError(t, err)
	assert.Equal(t, "stake", live.Dao.Type)
	require.NotNil(t, live.Stake)
	assert.Equal(t, daostate.Coin{Denom: "ubze", Amount: "1250000"}, live.Stake.VotingStake, "sr-1 + the vanished sr-gone (0) + the ubze denom reward")
	assert.Equal(t, "account", live.Controller.EndReason)

	_, err = r.Live(context.Background(), 99)
	assert.ErrorIs(t, err, daostate.ErrUnsupported, "the fake node answers Unimplemented for a DAO it has no fixture of")
}
