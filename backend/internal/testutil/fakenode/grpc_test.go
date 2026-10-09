package fakenode_test

import (
	"context"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

const chainTools = "bzevaloper1prm55vzlp5u6excqdunwlm4tw254cq943m6e6m"

func dial(t *testing.T, g *fakenode.GRPC) *grpc.ClientConn {
	t.Helper()
	codec, err := chain.NewCodec()
	require.NoError(t, err)
	conn, err := grpcclient.Dial(grpcclient.Config{Addr: g.Addr}, codec.GRPC())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestGRPCAnswersFromTheRecordedJSON(t *testing.T) {
	g := fakenode.NewGRPC(t)
	staking := stakingtypes.NewQueryClient(dial(t, g))
	ctx := context.Background()

	vals, err := staking.Validators(ctx, &stakingtypes.QueryValidatorsRequest{Pagination: &query.PageRequest{Limit: 200}})
	require.NoError(t, err)
	assert.Len(t, vals.Validators, 57)
	assert.Equal(t, 1, g.Requests("staking", "Validators"))

	one, err := staking.Validator(ctx, &stakingtypes.QueryValidatorRequest{ValidatorAddr: chainTools})
	require.NoError(t, err)
	assert.Equal(t, "ChainTools", one.Validator.Description.Moniker)
	assert.Equal(t, "/cosmos.crypto.ed25519.PubKey", one.Validator.ConsensusPubkey.TypeUrl, "the Any survives the round trip")
	assert.Equal(t, 1, g.RequestsFor("staking", "Validator", chainTools))

	// The key is every string field in order: delegator, then validator.
	owner := "bze1prm55vzlp5u6excqdunwlm4tw254cq94fl9jwy"
	del, err := staking.Delegation(ctx, &stakingtypes.QueryDelegationRequest{DelegatorAddr: owner, ValidatorAddr: chainTools})
	require.NoError(t, err)
	assert.Equal(t, "30081000000", del.DelegationResponse.Balance.Amount.String())
	assert.Equal(t, 1, g.RequestsFor("staking", "Delegation", owner+"."+chainTools))

	params, err := slashingtypes.NewQueryClient(dial(t, g)).Params(ctx, &slashingtypes.QueryParamsRequest{})
	require.NoError(t, err)
	assert.Equal(t, int64(10000), params.Params.SignedBlocksWindow)
}

func TestGRPCErrors(t *testing.T) {
	g := fakenode.NewGRPC(t)
	conn := dial(t, g)
	staking := stakingtypes.NewQueryClient(conn)
	ctx := context.Background()

	// A recorded gateway error answers its status.
	_, err := staking.Delegation(ctx, &stakingtypes.QueryDelegationRequest{
		DelegatorAddr: "bze1jv9wveqgmuwje2qlaxd6jv4n488uyzfwmwhms5", ValidatorAddr: "bzevaloper1jv9wveqgmuwje2qlaxd6jv4n488uyzfwr2gsyt",
	})
	assert.Equal(t, codes.NotFound, status.Code(err))

	// No file: Unimplemented, counted all the same.
	_, err = staking.Params(ctx, &stakingtypes.QueryParamsRequest{})
	assert.Equal(t, codes.Unimplemented, status.Code(err))
	assert.Equal(t, 1, g.Requests("staking", "Params"))
	_, err = distrtypes.NewQueryClient(conn).Params(ctx, &distrtypes.QueryParamsRequest{})
	assert.Equal(t, codes.Unimplemented, status.Code(err), "a registered service without fixtures yet")
	err = conn.Invoke(ctx, "/cosmos.auth.v1beta1.Query/Params", &stakingtypes.QueryPoolRequest{}, &stakingtypes.QueryPoolResponse{})
	assert.Equal(t, codes.Unimplemented, status.Code(err), "a service the fake does not serve")
}

// Integer and enum fields key a request too: a proposal by its id, the
// proposals list by its status filter; zero values do not.
func TestGRPCKeysIntegersAndEnums(t *testing.T) {
	g := fakenode.NewGRPC(t)
	gov := govv1.NewQueryClient(dial(t, g))
	ctx := context.Background()

	p, err := gov.Proposal(ctx, &govv1.QueryProposalRequest{ProposalId: 47})
	require.NoError(t, err)
	assert.Equal(t, "Upgrade network to v8.1.1", p.Proposal.Title)
	assert.Equal(t, 1, g.RequestsFor("gov", "Proposal", "47"))

	all, err := gov.Proposals(ctx, &govv1.QueryProposalsRequest{})
	require.NoError(t, err)
	assert.Len(t, all.Proposals, 47)
	voting, err := gov.Proposals(ctx, &govv1.QueryProposalsRequest{ProposalStatus: govv1.StatusVotingPeriod})
	require.NoError(t, err)
	assert.Empty(t, voting.Proposals)
	assert.Equal(t, 1, g.RequestsFor("gov", "Proposals", "PROPOSAL_STATUS_VOTING_PERIOD"))
}

func TestGRPCOverridesAndStop(t *testing.T) {
	g := fakenode.NewGRPC(t)
	staking := stakingtypes.NewQueryClient(dial(t, g))
	ctx := context.Background()

	g.SetResponse("staking", "Validators", "", []byte(`{"validators":[],"pagination":{"next_key":null,"total":"0"}}`))
	vals, err := staking.Validators(ctx, &stakingtypes.QueryValidatorsRequest{})
	require.NoError(t, err)
	assert.Empty(t, vals.Validators)

	g.SetResponse("staking", "Validator", chainTools, nil)
	_, err = staking.Validator(ctx, &stakingtypes.QueryValidatorRequest{ValidatorAddr: chainTools})
	assert.Equal(t, codes.Unimplemented, status.Code(err), "a removed answer")

	g.ResetRequests()
	assert.Zero(t, g.Requests("staking", "Validators"))

	g.Stop()
	_, err = staking.Validators(ctx, &stakingtypes.QueryValidatorsRequest{})
	assert.Equal(t, codes.Unavailable, status.Code(err))
	g.Stop()
}
