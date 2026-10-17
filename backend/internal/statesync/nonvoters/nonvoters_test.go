package nonvoters_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/activeset"
	"github.com/bze-alphateam/bze-scan/backend/internal/archive"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/nonvoters"
)

// Heights of the v8 generation (gov first: the set at H+1) and of v7.2.0
// (staking first: the set at H+2).
const (
	hV8 int64 = 23821225
	hV7 int64 = 17131437
)

// validator n: its consensus address (hex and bech32), operator and account,
// all from the 20 bytes n repeated.
type validator struct{ cons, valcons, op, acc string }

func val(t *testing.T, n byte) validator {
	t.Helper()
	b := bytes.Repeat([]byte{n}, 20)
	enc := func(hrp string) string {
		s, err := bech32.ConvertAndEncode(hrp, b)
		require.NoError(t, err)
		return s
	}
	return validator{cons: strings.ToUpper(hex.EncodeToString(b)), valcons: enc("bzevalcons"), op: enc("bzevaloper"), acc: enc("bze")}
}

// fakeSet answers members at any height and records the heights asked.
type fakeSet struct {
	members []activeset.Member
	err     error
	heights []int64
}

func (s *fakeSet) At(_ context.Context, height int64) ([]activeset.Member, error) {
	s.heights = append(s.heights, height)
	return s.members, s.err
}

// fakeNode answers slashes as the slash events of every block.
type fakeNode struct {
	slashes []node.Event
	legacy  bool // BeginBlockEvents, as a node older than CometBFT 0.38
	err     error
	heights []int64
}

func (n *fakeNode) BlockResults(_ context.Context, height int64) (*node.BlockResults, []byte, error) {
	n.heights = append(n.heights, height)
	if n.err != nil {
		return nil, nil, n.err
	}
	events := append([]node.Event{{Type: "mint", Attributes: []node.Attribute{{Key: "amount", Value: "1"}}}}, n.slashes...)
	if n.legacy {
		return &node.BlockResults{Height: height, BeginBlockEvents: events}, nil, nil
	}
	return &node.BlockResults{Height: height, FinalizeBlockEvents: events}, nil, nil
}

func slash(valcons string, jailed bool) node.Event {
	ev := node.Event{Type: "slash", Attributes: []node.Attribute{
		{Key: "address", Value: valcons}, {Key: "power", Value: "5"}, {Key: "reason", Value: "missing_signature"},
	}}
	if jailed {
		ev.Attributes = append(ev.Attributes, node.Attribute{Key: "jailed", Value: valcons})
	}
	return ev
}

type fakeStore struct {
	proposals map[int64]nonvoters.Proposal
	written   map[int64]bool
	// indexed answers Indexed; the ranges asked are recorded.
	indexed bool
	ranges  [][2]int64
	voters  map[int64][]string
	saved   []nonvoters.Result
	saveErr error
}

func (s *fakeStore) Pending(context.Context) ([]nonvoters.Proposal, error) {
	var out []nonvoters.Proposal
	for id := int64(1); id <= 100; id++ {
		if p, ok := s.proposals[id]; ok && !s.written[id] {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *fakeStore) Proposal(_ context.Context, id int64) (nonvoters.Proposal, bool, error) {
	p, ok := s.proposals[id]
	return p, ok, nil
}

func (s *fakeStore) Indexed(_ context.Context, lo, hi int64) (bool, error) {
	s.ranges = append(s.ranges, [2]int64{lo, hi})
	return s.indexed, nil
}

func (s *fakeStore) Voters(_ context.Context, id int64) ([]string, error) { return s.voters[id], nil }

func (s *fakeStore) Save(_ context.Context, r nonvoters.Result) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = append(s.saved, r)
	if s.written == nil {
		s.written = map[int64]bool{}
	}
	s.written[r.ProposalID] = true
	return nil
}

func i64(v int64) *int64 { return &v }

type env struct {
	a, b, c, d validator
	set        *fakeSet
	node       *fakeNode
	store      *fakeStore
	logs       *logtest.Hook
	sync       *nonvoters.Set
}

// newEnv: a set of four validators (power 40, 30, 20, 10), d unknown to the
// explorer; proposal 47 resolved at hV8 with its votes indexed, a voted.
func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{a: val(t, 1), b: val(t, 2), c: val(t, 3), d: val(t, 4)}
	e.set = &fakeSet{members: []activeset.Member{
		{ConsensusAddress: e.a.cons, VotingPower: 40, Operator: e.a.op},
		{ConsensusAddress: e.b.cons, VotingPower: 30, Operator: e.b.op},
		{ConsensusAddress: e.c.cons, VotingPower: 20, Operator: e.c.op},
		{ConsensusAddress: e.d.cons, VotingPower: 10},
	}}
	e.node = &fakeNode{}
	e.store = &fakeStore{
		proposals: map[int64]nonvoters.Proposal{47: {ID: 47, SubmitHeight: i64(hV8 - 100), ResolvedHeight: hV8,
			TallySource: "sync", BondedTokens: "100000000"}},
		indexed: true,
		voters:  map[int64][]string{47: {e.a.acc, "bze1delegator"}},
	}
	logger, hook := logtest.NewNullLogger()
	logger.SetLevel(logrus.DebugLevel)
	e.logs = hook
	e.sync = nonvoters.New(nonvoters.Deps{Releases: archive.Mainnet, Set: e.set, Node: e.node, Store: e.store, Log: logger}, 0)
	return e
}

func (e *env) warnings() []string {
	var out []string
	for _, entry := range e.logs.AllEntries() {
		if entry.Level == logrus.WarnLevel {
			out = append(out, entry.Message)
		}
	}
	return out
}

func TestTheSetIsAStateSyncSet(t *testing.T) {
	e := newEnv(t)
	assert.Equal(t, statesync.ProposalNonVoters, e.sync.Name())
	assert.Equal(t, nonvoters.DefaultInterval, e.sync.Interval())
}

func TestWritesTheMembersWhoseAccountDidNotVote(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.sync.FullResync(context.Background()))

	assert.Equal(t, []int64{hV8 + 1}, e.set.heights, "v8: gov tallies before staking, the set at H+1")
	assert.Equal(t, []int64{hV8}, e.node.heights, "the slashes of the tally's block")
	assert.Equal(t, [][2]int64{{hV8 - 100, hV8}}, e.store.ranges)
	require.Len(t, e.store.saved, 1)
	assert.Equal(t, nonvoters.Result{ProposalID: 47, SetHeight: hV8 + 1, SetSize: 4, NonVoters: []nonvoters.NonVoter{
		{ConsensusAddress: e.b.cons, Operator: e.b.op, VotingPower: 30, PowerPct: "30.00000"},
		{ConsensusAddress: e.c.cons, Operator: e.c.op, VotingPower: 20, PowerPct: "20.00000"},
		{ConsensusAddress: e.d.cons, VotingPower: 10, PowerPct: "10.00000"},
	}}, e.store.saved[0])

	// An unknown consensus address is stored without an operator, and said.
	assert.Equal(t, []string{"proposal non-voters: no validator known with this consensus address, stored without an operator"}, e.warnings())

	// Once written, the proposal is not computed again.
	require.NoError(t, e.sync.FullResync(context.Background()))
	assert.Len(t, e.store.saved, 1)
}

func TestValidatorsJailedInTheTallysBlockAreNotInTheTallySet(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		e := newEnv(t)
		// b jailed for downtime at H (one event naming it twice), c slashed
		// for a double sign (the address only); another validator's slash
		// changes nothing.
		e.node.slashes = []node.Event{slash(e.b.valcons, true), slash(e.c.valcons, false), slash(val(t, 9).valcons, true),
			{Type: "slash", Attributes: []node.Attribute{{Key: "address", Value: "not bech32"}}}}
		e.node.legacy = legacy
		require.NoError(t, e.sync.FullResync(context.Background()))

		require.Len(t, e.store.saved, 1)
		assert.Equal(t, nonvoters.Result{ProposalID: 47, SetHeight: hV8 + 1, SetSize: 2, NonVoters: []nonvoters.NonVoter{
			{ConsensusAddress: e.d.cons, VotingPower: 10, PowerPct: "20.00000"},
		}}, e.store.saved[0], "a's 40 and d's 10 are the tally set; legacy=%v", legacy)
	}
}

func TestStakingFirstGenerationsReadTheSetAtHPlus2(t *testing.T) {
	e := newEnv(t)
	e.store.proposals = map[int64]nonvoters.Proposal{43: {ID: 43, ResolvedHeight: hV7, TallySource: nonvoters.TallySourceArchive}}
	require.NoError(t, e.sync.FullResync(context.Background()))
	assert.Equal(t, []int64{hV7 + 2}, e.set.heights)
	require.Len(t, e.store.saved, 1)
	assert.Equal(t, hV7+2, e.store.saved[0].SetHeight)
	assert.Empty(t, e.store.ranges, "archive votes: no need for the indexed range")
	assert.Len(t, e.store.saved[0].NonVoters, 4, "nobody voted on 43")
}

func TestWaitsForTheVotes(t *testing.T) {
	e := newEnv(t)
	e.store.indexed = false
	e.store.proposals[48] = nonvoters.Proposal{ID: 48, ResolvedHeight: hV8 + 50} // submitted below the indexed range
	require.NoError(t, e.sync.FullResync(context.Background()))
	assert.Empty(t, e.store.saved, "a gap in 47's range, 48's submission unknown")
	assert.Empty(t, e.set.heights)
	assert.Equal(t, [][2]int64{{hV8 - 100, hV8}}, e.store.ranges, "48 is not counted without a submission height")

	// The gap filled, 47 is written; 48 waits for an archive snapshot.
	e.store.indexed = true
	require.NoError(t, e.sync.FullResync(context.Background()))
	require.Len(t, e.store.saved, 1)
	assert.Equal(t, int64(47), e.store.saved[0].ProposalID)

	p := e.store.proposals[48]
	p.TallySource = nonvoters.TallySourceArchive
	e.store.proposals[48] = p
	require.NoError(t, e.sync.FullResync(context.Background()))
	require.Len(t, e.store.saved, 2)
	assert.Equal(t, int64(48), e.store.saved[1].ProposalID)
}

func TestResyncOneRecomputesAWrittenProposal(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.sync.FullResync(context.Background()))
	e.store.voters[47] = append(e.store.voters[47], e.b.acc) // b's vote, found later
	require.NoError(t, e.sync.ResyncOne(context.Background(), "47"))
	require.Len(t, e.store.saved, 2)
	assert.Equal(t, []string{e.c.cons, e.d.cons}, consOf(e.store.saved[1].NonVoters))

	e.store.indexed = false
	require.EqualError(t, e.sync.ResyncOne(context.Background(), "47"),
		"proposal 47: votes not complete yet (heights 23821125 to 23821225 not all indexed and no archive snapshot)")
	require.EqualError(t, e.sync.ResyncOne(context.Background(), "46"),
		"proposal 46: no tally to read (not passed, rejected or failed, or no resolution height yet)")
	require.EqualError(t, e.sync.ResyncOne(context.Background(), "x"), `proposal non-voters: "x" is not a proposal id`)
	assert.Len(t, e.store.saved, 2)
}

func consOf(nv []nonvoters.NonVoter) []string {
	out := make([]string, len(nv))
	for i, v := range nv {
		out[i] = v.ConsensusAddress
	}
	return out
}

func TestCrossChecksThePowerAgainstTheBondedTokens(t *testing.T) {
	cases := []struct {
		bonded string
		warn   bool
	}{
		{"100000000", false}, // 100 power, exactly
		{"103999999", false}, // within one unit per validator (4)
		{"104000001", true},  // more than that
		{"95999999", true},   // short of it
		{"", false},          // unknown: nothing to compare
		{"not a number", false},
	}
	for _, c := range cases {
		e := newEnv(t)
		e.set.members[3].Operator = e.d.op
		p := e.store.proposals[47]
		p.BondedTokens = c.bonded
		e.store.proposals[47] = p
		// The validators jailed at H are still bonded: counted in the check.
		e.node.slashes = []node.Event{slash(e.c.valcons, true)}
		require.NoError(t, e.sync.FullResync(context.Background()))
		if c.warn {
			assert.Equal(t, []string{"proposal non-voters: the set's power does not add up to the tally's bonded tokens"}, e.warnings(), c.bonded)
		} else {
			assert.Empty(t, e.warnings(), c.bonded)
		}
	}
}

func TestPowerSharesRoundHalfUp(t *testing.T) {
	e := newEnv(t)
	e.set.members = []activeset.Member{
		{ConsensusAddress: e.a.cons, VotingPower: 2, Operator: e.a.op},
		{ConsensusAddress: e.b.cons, VotingPower: 1, Operator: e.b.op},
		{ConsensusAddress: e.c.cons, VotingPower: 299_999_997, Operator: e.c.op},
	}
	e.store.voters[47] = []string{e.c.acc}
	require.NoError(t, e.sync.FullResync(context.Background()))
	require.Len(t, e.store.saved, 1)
	assert.Equal(t, []string{"0.00000", "0.00000"}, pctOf(e.store.saved[0].NonVoters))

	e = newEnv(t)
	e.set.members = []activeset.Member{
		{ConsensusAddress: e.a.cons, VotingPower: 1, Operator: e.a.op},
		{ConsensusAddress: e.b.cons, VotingPower: 2, Operator: e.b.op},
	}
	e.store.voters[47] = nil
	require.NoError(t, e.sync.FullResync(context.Background()))
	assert.Equal(t, []string{"33.33333", "66.66667"}, pctOf(e.store.saved[0].NonVoters))
}

func pctOf(nv []nonvoters.NonVoter) []string {
	out := make([]string, len(nv))
	for i, v := range nv {
		out[i] = v.PowerPct
	}
	return out
}

func TestAFailedProposalDoesNotStopTheOthers(t *testing.T) {
	e := newEnv(t)
	e.store.proposals[48] = nonvoters.Proposal{ID: 48, ResolvedHeight: hV8 + 50, TallySource: nonvoters.TallySourceArchive}
	e.node.err = errors.New("archive down")
	err := e.sync.FullResync(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "proposal 47: archive down")
	assert.Contains(t, err.Error(), "proposal 48: archive down")
	assert.Empty(t, e.store.saved)

	e.node.err = nil
	e.set.err = errors.New("validators: node unavailable")
	require.ErrorContains(t, e.sync.FullResync(context.Background()), "proposal 47: validators: node unavailable")

	e.set.err = nil
	e.store.saveErr = errors.New("db down")
	require.ErrorContains(t, e.sync.FullResync(context.Background()), "proposal 47: db down")
}
