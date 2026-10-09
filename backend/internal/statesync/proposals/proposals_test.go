package proposals_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/types/query"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/proposals"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// Recorded mainnet governance (chain v8.1.1): 47 proposals, all passed.
const recorded = 47

func TestKind(t *testing.T) {
	for _, tc := range []struct {
		name     string
		messages string
		want     string
	}{
		{"upgrade", `[{"@type":"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade"}]`, "software_upgrade"},
		{"pool spend", `[{"@type":"/cosmos.distribution.v1beta1.MsgCommunityPoolSpend"}]`, "community_pool_spend"},
		{"any module's params", `[{"@type":"/bze.tradebin.MsgUpdateParams"}]`, "parameter_change"},
		{"cointrunk publisher", `[{"@type":"/bze.cointrunk.MsgSavePublisher"}]`, "cointrunk_publisher"},
		{"ibc client", `[{"@type":"/ibc.core.client.v1.MsgRecoverClient"}]`, "ibc_client_update"},
		{"ibc upgrade", `[{"@type":"/ibc.core.client.v1.MsgIBCSoftwareUpgrade"}]`, "ibc_client_update"},
		{"no messages", `[]`, "text"},
		{"unknown", `[{"@type":"/bze.burner.MsgFundBurner"}]`, "other"},
		{"first known wins", `[{"@type":"/x.MsgA"},{"@type":"/cosmos.bank.v1beta1.MsgUpdateParams"}]`, "parameter_change"},
		{"legacy upgrade", `[{"@type":"/cosmos.gov.v1.MsgExecLegacyContent","content":{"@type":"/cosmos.upgrade.v1beta1.SoftwareUpgradeProposal"}}]`, "software_upgrade"},
		{"legacy params", `[{"@type":"/cosmos.gov.v1.MsgExecLegacyContent","content":{"@type":"/cosmos.params.v1beta1.ParameterChangeProposal"}}]`, "parameter_change"},
		{"legacy publisher", `[{"@type":"/cosmos.gov.v1.MsgExecLegacyContent","content":{"@type":"/bze.cointrunk.v1.PublisherProposal"}}]`, "cointrunk_publisher"},
		{"legacy domain", `[{"@type":"/cosmos.gov.v1.MsgExecLegacyContent","content":{"@type":"/bze.cointrunk.v1.AcceptedDomainProposal"}}]`, "cointrunk_publisher"},
		{"legacy ibc", `[{"@type":"/cosmos.gov.v1.MsgExecLegacyContent","content":{"@type":"/ibc.core.client.v1.ClientUpdateProposal"}}]`, "ibc_client_update"},
		{"legacy pool spend", `[{"@type":"/cosmos.gov.v1.MsgExecLegacyContent","content":{"@type":"/cosmos.distribution.v1beta1.CommunityPoolSpendProposal"}}]`, "community_pool_spend"},
		{"legacy text", `[{"@type":"/cosmos.gov.v1.MsgExecLegacyContent","content":{"@type":"/cosmos.gov.v1beta1.TextProposal"}}]`, "text"},
		{"legacy burner", `[{"@type":"/cosmos.gov.v1.MsgExecLegacyContent","content":{"@type":"/bze.burner.v1.BurnCoinsProposal"}}]`, "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			types, contents := proposals.Messages(json.RawMessage(tc.messages))
			assert.Equal(t, tc.want, proposals.Kind(types, contents))
		})
	}
	types, _ := proposals.Messages(json.RawMessage(`{"not":"a list"}`))
	assert.Nil(t, types)
}

// fakeGov answers gov and staking queries from the recorded fixtures,
// counting the calls. voting turns the listed proposals' status into the
// voting period, as for a proposal not tallied yet.
type fakeGov struct {
	t *testing.T

	mu     sync.Mutex
	calls  map[string]int
	err    error
	voting map[uint64]bool
	gone   map[uint64]bool
	// pages splits the full list in two pages.
	pages bool
}

func newFakeGov(t *testing.T) *fakeGov {
	return &fakeGov{t: t, calls: map[string]int{}, voting: map[uint64]bool{}, gone: map[uint64]bool{}}
}

func (g *fakeGov) count(m string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls[m]++
	return g.err
}

func (g *fakeGov) callsOf(m string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls[m]
}

func (g *fakeGov) adjust(p *govv1.Proposal) {
	if g.voting[p.Id] {
		p.Status, p.FinalTallyResult = govv1.StatusVotingPeriod, nil
	}
}

func (g *fakeGov) Proposals(_ context.Context, in *govv1.QueryProposalsRequest, _ ...grpc.CallOption) (*govv1.QueryProposalsResponse, error) {
	if err := g.count("Proposals." + in.ProposalStatus.String()); err != nil {
		return nil, err
	}
	var resp govv1.QueryProposalsResponse
	key := ""
	if in.ProposalStatus != govv1.StatusNil {
		key = in.ProposalStatus.String()
	}
	fakenode.LoadGRPCFixture(g.t, "gov", "Proposals", key, &resp)
	for _, p := range resp.Proposals {
		g.adjust(p)
	}
	if in.ProposalStatus == govv1.StatusVotingPeriod {
		var all govv1.QueryProposalsResponse
		fakenode.LoadGRPCFixture(g.t, "gov", "Proposals", "", &all)
		for _, p := range all.Proposals {
			if g.voting[p.Id] {
				g.adjust(p)
				resp.Proposals = append(resp.Proposals, p)
			}
		}
	}
	if resp.Pagination != nil {
		resp.Pagination.NextKey = nil
	}
	if g.pages && in.ProposalStatus == govv1.StatusNil {
		half := len(resp.Proposals) / 2
		if string(in.Pagination.Key) == "page2" {
			resp.Proposals = resp.Proposals[half:]
		} else {
			resp.Proposals = resp.Proposals[:half]
			resp.Pagination = &query.PageResponse{NextKey: []byte("page2")}
		}
	}
	return &resp, nil
}

func (g *fakeGov) Proposal(_ context.Context, in *govv1.QueryProposalRequest, _ ...grpc.CallOption) (*govv1.QueryProposalResponse, error) {
	if err := g.count("Proposal"); err != nil {
		return nil, err
	}
	if g.gone[in.ProposalId] {
		return nil, status.Errorf(codes.NotFound, "proposal %d doesn't exist", in.ProposalId)
	}
	var resp govv1.QueryProposalResponse
	fakenode.LoadGRPCFixture(g.t, "gov", "Proposal", strconv.FormatUint(in.ProposalId, 10), &resp)
	g.adjust(resp.Proposal)
	return &resp, nil
}

func (g *fakeGov) TallyResult(_ context.Context, in *govv1.QueryTallyResultRequest, _ ...grpc.CallOption) (*govv1.QueryTallyResultResponse, error) {
	if err := g.count("TallyResult"); err != nil {
		return nil, err
	}
	var resp govv1.QueryTallyResultResponse
	fakenode.LoadGRPCFixture(g.t, "gov", "TallyResult", strconv.FormatUint(in.ProposalId, 10), &resp)
	return &resp, nil
}

func (g *fakeGov) Pool(_ context.Context, _ *stakingtypes.QueryPoolRequest, _ ...grpc.CallOption) (*stakingtypes.QueryPoolResponse, error) {
	if err := g.count("Pool"); err != nil {
		return nil, err
	}
	var resp stakingtypes.QueryPoolResponse
	fakenode.LoadGRPCFixture(g.t, "staking", "Pool", "", &resp)
	return &resp, nil
}

// fakeStore records the saves and answers Voting.
type fakeStore struct {
	mu     sync.Mutex
	saves  [][]proposals.Proposal
	voting []uint64
	err    error
}

func (s *fakeStore) Save(_ context.Context, ps []proposals.Proposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves = append(s.saves, ps)
	return s.err
}

func (s *fakeStore) Voting(context.Context) ([]uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.voting, s.err
}

func (s *fakeStore) last() []proposals.Proposal {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.saves) == 0 {
		return nil
	}
	return s.saves[len(s.saves)-1]
}

var now = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func newSet(t *testing.T, g *fakeGov, store *fakeStore) *proposals.Set {
	t.Helper()
	codec, err := chain.NewCodec()
	require.NoError(t, err)
	return proposals.New(proposals.Deps{Gov: g, Staking: g, JSON: codec, Store: store, Now: func() time.Time { return now }}, 0)
}

func byID(ps []proposals.Proposal, id uint64) proposals.Proposal {
	for _, p := range ps {
		if p.ID == id {
			return p
		}
	}
	return proposals.Proposal{}
}

func TestTheFirstRunListsEveryProposal(t *testing.T) {
	g, store := newFakeGov(t), &fakeStore{}
	g.pages = true
	set := newSet(t, g, store)
	assert.Equal(t, statesync.Proposals, set.Name())
	assert.Equal(t, time.Minute, set.Interval())

	require.NoError(t, set.FullResync(context.Background()))
	rows := store.last()
	require.Len(t, rows, recorded, "both pages")
	assert.Equal(t, 2, g.callsOf("Proposals.PROPOSAL_STATUS_UNSPECIFIED"))
	assert.Zero(t, g.callsOf("TallyResult"), "every recorded proposal is resolved")
	assert.Zero(t, g.callsOf("Pool"))

	p := byID(rows, 47)
	assert.Equal(t, "Upgrade network to v8.1.1", p.Title)
	assert.Equal(t, "passed", p.Status)
	assert.Equal(t, "software_upgrade", p.Kind)
	assert.Equal(t, []string{"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade"}, p.MessageTypes)
	assert.Contains(t, string(p.Messages), `"name":"v8.1.1"`)
	assert.JSONEq(t, `[{"denom":"ubze","amount":"100000000000"}]`, string(p.TotalDeposit))
	assert.Equal(t, &proposals.Tally{Yes: "106584798486204", No: "0", Abstain: "0", Veto: "0"}, p.Tally, "the final tally")
	assert.Empty(t, p.BondedTokens, "the store keeps the last running one")
	assert.Equal(t, p.VotingEndTime, p.TallyAt, "a final tally is as of the voting end")
	assert.Equal(t, time.Date(2026, 7, 16, 10, 28, 3, 533454546, time.UTC), p.SubmitTime)

	legacy := byID(rows, 43)
	assert.Equal(t, "cointrunk_publisher", legacy.Kind)
	assert.Equal(t, []string{"/cosmos.gov.v1.MsgExecLegacyContent"}, legacy.MessageTypes)
}

func TestLaterRunsRefreshTheVotingProposals(t *testing.T) {
	g, store := newFakeGov(t), &fakeStore{}
	set := newSet(t, g, store)
	require.NoError(t, set.FullResync(context.Background()))

	// 46 is voting on the node; 47 is stored as voting but has ended.
	g.voting[46] = true
	store.voting = []uint64{46, 47}
	require.NoError(t, set.FullResync(context.Background()))
	assert.Equal(t, 1, g.callsOf("Proposals.PROPOSAL_STATUS_UNSPECIFIED"), "the full list only once")
	assert.Equal(t, 1, g.callsOf("Proposals.PROPOSAL_STATUS_VOTING_PERIOD"))
	assert.Equal(t, 1, g.callsOf("Proposal"), "47 only: 46 came with the list")

	rows := store.last()
	require.Len(t, rows, 2)
	voting := byID(rows, 46)
	assert.Equal(t, "voting_period", voting.Status)
	var tally govv1.QueryTallyResultResponse
	fakenode.LoadGRPCFixture(t, "gov", "TallyResult", "46", &tally)
	assert.Equal(t, tally.Tally.YesCount, voting.Tally.Yes, "the running tally")
	assert.Equal(t, "147027371221650", voting.BondedTokens, "the bonded tokens it is measured against")
	assert.Equal(t, &now, voting.TallyAt)
	assert.Equal(t, "passed", byID(rows, 47).Status)
}

func TestResyncOne(t *testing.T) {
	g, store := newFakeGov(t), &fakeStore{}
	set := newSet(t, g, store)
	g.voting[47] = true
	require.NoError(t, set.ResyncOne(context.Background(), proposals.Key(47)))
	require.Len(t, store.last(), 1)
	assert.Equal(t, "voting_period", store.last()[0].Status)
	assert.Equal(t, 1, g.callsOf("TallyResult"))
	assert.Equal(t, 1, g.callsOf("Pool"))

	// A dropped or canceled proposal is gone from the node: its row stays as
	// its events wrote it.
	g.gone[48] = true
	require.NoError(t, set.ResyncOne(context.Background(), "48"))
	assert.Len(t, store.saves, 1)

	require.ErrorContains(t, set.ResyncOne(context.Background(), "x"), "not a proposal id")
}

func TestNodeAndStoreErrorsFailTheRun(t *testing.T) {
	g, store := newFakeGov(t), &fakeStore{}
	set := newSet(t, g, store)
	g.err = errors.New("node down")
	require.ErrorContains(t, set.FullResync(context.Background()), "node down")
	require.ErrorContains(t, set.ResyncOne(context.Background(), "47"), "node down")
	assert.Empty(t, store.saves)

	g.err, store.err = nil, errors.New("db down")
	require.ErrorContains(t, set.FullResync(context.Background()), "db down")
	store.err = nil
	require.NoError(t, set.FullResync(context.Background()))
	assert.Equal(t, 3, g.callsOf("Proposals.PROPOSAL_STATUS_UNSPECIFIED"),
		"until a run is saved, every run lists every proposal")
	require.NoError(t, set.FullResync(context.Background()))
	assert.Equal(t, 3, g.callsOf("Proposals.PROPOSAL_STATUS_UNSPECIFIED"))
}
