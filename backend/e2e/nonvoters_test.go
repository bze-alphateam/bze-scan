//go:build e2e

package e2e_test

import (
	"context"
	"encoding/base64"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/activeset"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// The tally sets of the recorded proposals: 47 tallied at 23821225 (v8.1.0,
// gov before staking: the set at H+1), 43 at 17131437 (v7.2.0, staking
// before gov: the set at H+2). Nobody was jailed in either block.
const (
	setHeight47 int64 = 23821226
	setHeight43 int64 = 17131439
	setSize47         = 23
	setSize43         = 31
	// kh8aq is a validator that voted on neither proposal.
	kh8aqCons     = "35AED0F494B5BB464648E335A4654D21B50EE43D"
	kh8aqOperator = "bzevaloper1kh8aqhy07e8c8fd7x8zcz79gy3mfh986m6x6eq"
)

// nonVoters47 and nonVoters43 are the validators of the tally sets whose
// account has no vote among the votes the archive kept until the tally
// (consensus address, operator, power, share), worked out from the
// fixtures independently of the code: the /validators set, the gov v1
// Votes at H−1 and the consensus keys of the recorded staking Validators.
var (
	nonVoters47 = []string{
		"2217EC92A4E73BA79E86E90FA6C4180429EB7C41 bzevaloper13snue6qplvfgk40gps54v84wgthj8ekfg5sl6q 9207117 6.99961",
		"EFF58FAD4559319233C282F942C38A0BE8F50120 bzevaloper1gk8k0u3vkt65xjc82l8qryf44pxp68dcsuth5c 6462578 4.91311",
		"33E576B6C93B5D2623CE34B225658756FF78063A bzevaloper1zww2j706zcanf79vphey96snpldxkkz8xq8pem 6438213 4.89458",
		"A1DB7908F8B108EC46E6C7B30C5AA2EE7E7F2B74 bzevaloper19qyr029qa9pmatm5w6p98g0v8suyutccr2ux75 3160019 2.40237",
		"A25583AAC5A99D5897C6DE4079E96A2F934E1993 bzevaloper1ash3048dh2s3xhav2xk53yj67x40r8fh8el5qx 2657156 2.02008",
		"35AED0F494B5BB464648E335A4654D21B50EE43D bzevaloper1kh8aqhy07e8c8fd7x8zcz79gy3mfh986m6x6eq 1537446 1.16883",
	}
	nonVoters43 = []string{
		"9982E3205F40C7047E0419640FD3377FEC6A28D1 bzevaloper1znh4223lfrj97q5kkfs2amgj402dku4uyx4gc3 8152138 5.51657",
		"A7300974711E8F6B9885A6F1CB50418274B2AE94 bzevaloper1u98eefp0del3jzgqxmuj0kqjal2fyxf5npftr4 6084862 4.11764",
		"101DF52F658F4EF69DA0333BC4FB519E36DC54CA bzevaloper1dnhhhhge8qphr5w7mz0cvp5ukxgr6drpw6ludu 5642444 3.81825",
		"A25583AAC5A99D5897C6DE4079E96A2F934E1993 bzevaloper1ash3048dh2s3xhav2xk53yj67x40r8fh8el5qx 5603825 3.79212",
		"2217EC92A4E73BA79E86E90FA6C4180429EB7C41 bzevaloper13snue6qplvfgk40gps54v84wgthj8ekfg5sl6q 5075738 3.43476",
		"60B11548387FD5259060DC546847208ADFE402A2 bzevaloper1xlrvy6y56xlls449874922yrwpyz5zphvxyps9 4851811 3.28323",
		"6567B8056E99FD960B61F58D19D3D8935A2DFA3D bzevaloper1y7l0svgngc9m933q627yd8z8cesnvhg7296w20 4800048 3.24820",
		"9EB75D1FF20FD14B46A58E220F18FC38EF492159 bzevaloper172fstuv62dq2c98fcyp9h32dgga5y6r5eaq7c9 4477453 3.02990",
		"0CEFEA74FB6BD6FB86C1BD57C8A1BEA5FAE412E1 bzevaloper1ngl7qkmpvh80t5ju3cq0ffyx8reph2qacjkpym 4335020 2.93352",
		"9761CDD5431F95F88BC9684A0794D970979028E7 bzevaloper1fqm96453f93jy97djtpq44ctyvxvwr2edufs9n 4156951 2.81302",
		"A1DB7908F8B108EC46E6C7B30C5AA2EE7E7F2B74 bzevaloper19qyr029qa9pmatm5w6p98g0v8suyutccr2ux75 3835024 2.59517",
		"A4C8C1086A5A8C8C16689B91DF11B34B904F7329 bzevaloper19fgph876c3rqxrn6xk5ch6wd73r3g05wzpsht0 3824823 2.58827",
		"213C087354635A43E10817601008CF683A82AA18 bzevaloper1hnvjttxzehwldcuwje9fkny3qz37ddn98apez3 3686652 2.49476",
		"35AED0F494B5BB464648E335A4654D21B50EE43D bzevaloper1kh8aqhy07e8c8fd7x8zcz79gy3mfh986m6x6eq 703227 0.47588",
	}
)

// nonVoterRows are proposal id's rows as "cons operator power pct", an
// unknown operator as "-", sorted.
func (e *syncEnv) nonVoterRows(t *testing.T, id int64) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', consensus_address, coalesce(operator_address, '-'), voting_power, power_pct)
		FROM explorer.proposal_non_voters WHERE proposal_id = $1`, id)
}

// activeSet is proposal id's "height size", "" while not computed.
func (e *syncEnv) activeSet(t *testing.T, id int64) string {
	t.Helper()
	rows := queryStrings(t, e.db, `SELECT concat_ws(' ', active_set_height, active_set_size, CASE WHEN non_voters_at IS NOT NULL THEN 'written' END)
		FROM explorer.proposals WHERE id = $1`, id)
	require.Len(t, rows, 1)
	return rows[0]
}

func sorted(rows []string) []string {
	out := append([]string(nil), rows...)
	sort.Strings(out)
	return out
}

func TestNonVotersOfClosedProposals(t *testing.T) {
	e := newSyncEnv(t)
	e.index(t, hSubmit, hVoteVal, hVote, hResolve)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)

	// 47 resolved, but the heights between its submission and its tally
	// are not indexed: its votes are not complete yet. 43 has no
	// resolution height.
	assert.Zero(t, e.count(t, `SELECT count(*) FROM explorer.proposal_non_voters`))
	assert.Equal(t, "", e.activeSet(t, 47))
	assert.Equal(t, "", e.activeSet(t, 43))
	code, _ = e.syncState(t, "--job", "proposal_non_voters", "--id", "47")
	assert.Equal(t, 1, code, "a forced run fails while the votes are not complete")
	assert.Zero(t, e.count(t, `SELECT count(*) FROM explorer.proposal_non_voters`))

	// Every height of 47's range indexed (moving its submission onto the
	// tally's block stands in for the whole range): its non-voters are
	// written from the indexed votes, the owner of BZE Alpha Team's
	// among them.
	_, err := e.db.Exec(`UPDATE explorer.proposals SET submit_height = $1 WHERE id = 47`, hResolve)
	require.NoError(t, err)
	code, out = e.syncState(t, "--job", "proposal_non_voters")
	require.Equal(t, 0, code, out)
	assert.Equal(t, "23821226 23 written", e.activeSet(t, 47))
	assert.Equal(t, setSize47-1, e.count(t, `SELECT count(*) FROM explorer.proposal_non_voters WHERE proposal_id = 47`))
	assert.Zero(t, e.count(t, `SELECT count(*) FROM explorer.proposal_non_voters WHERE operator_address = $1`, alphaOperator))

	// The snapshots read every vote from the archive: 43 is written on the
	// next run, 47 stays as written until a forced run computes it again.
	code, stderr := e.snapshots(t, "--id", "43", "--id", "47")
	require.Equal(t, 0, code, stderr)
	code, out = e.syncState(t, "--job", "proposal_non_voters")
	require.Equal(t, 0, code, out)
	assert.Equal(t, sorted(nonVoters43), e.nonVoterRows(t, 43))
	assert.Equal(t, "17131439 31 written", e.activeSet(t, 43))
	assert.Equal(t, setSize47-1, e.count(t, `SELECT count(*) FROM explorer.proposal_non_voters WHERE proposal_id = 47`))
	code, out = e.syncState(t, "--job", "proposal_non_voters", "--id", "47")
	require.Equal(t, 0, code, out)
	assert.Equal(t, sorted(nonVoters47), e.nonVoterRows(t, 47), "a recompute replaces the rows")
	assert.Equal(t, "23821226 23 written", e.activeSet(t, 47))

	// A validator the explorer does not know is stored without an operator.
	_, err = e.db.Exec(`DELETE FROM explorer.validators WHERE operator_address = $1`, kh8aqOperator)
	require.NoError(t, err)
	code, out = e.syncState(t, "--job", "proposal_non_voters", "--id", "43")
	require.Equal(t, 0, code, out)
	want := sorted(nonVoters43)
	for i, r := range want {
		want[i] = strings.Replace(r, kh8aqOperator, "-", 1)
	}
	assert.Equal(t, sorted(want), e.nonVoterRows(t, 43))

	// The API: the set on the proposal, the non-voters by power.
	base := e.serveAPI(t)
	p := into[dto.Proposal](t, fetch(t, base+"/proposals/47"))
	require.NotNil(t, p.ActiveSet)
	assert.Equal(t, dto.ActiveSet{Height: setHeight47, Size: setSize47, NonVotersCount: len(nonVoters47)}, *p.ActiveSet)
	page := into[dto.List[dto.ProposalNonVoter]](t, fetch(t, base+"/proposals/43/non-voters?limit=10"))
	require.Len(t, page.Items, 10)
	require.NotNil(t, page.NextCursor)
	first := page.Items[0]
	assert.Equal(t, "9982E3205F40C7047E0419640FD3377FEC6A28D1", first.ConsensusAddress)
	assert.Equal(t, int64(8152138), first.VotingPower)
	assert.Equal(t, "5.51657", first.PowerPct)
	require.NotNil(t, first.OperatorAddress)
	assert.Equal(t, "bzevaloper1znh4223lfrj97q5kkfs2amgj402dku4uyx4gc3", *first.OperatorAddress)
	assert.NotNil(t, first.Moniker, "named after the validators table")
	rest := into[dto.List[dto.ProposalNonVoter]](t, fetch(t, base+"/proposals/43/non-voters?limit=10&cursor="+*page.NextCursor))
	require.Len(t, rest.Items, len(nonVoters43)-10)
	assert.Nil(t, rest.NextCursor)
	last := rest.Items[len(rest.Items)-1]
	assert.Equal(t, kh8aqCons, last.ConsensusAddress)
	assert.Nil(t, last.OperatorAddress)
	assert.Nil(t, last.Moniker)
	for i := 1; i < len(page.Items); i++ {
		assert.GreaterOrEqual(t, page.Items[i-1].VotingPower, page.Items[i].VotingPower, "by power descending")
	}

	// A proposal without a recorded set: null, and an empty list.
	assert.Nil(t, into[dto.Proposal](t, fetch(t, base+"/proposals/46")).ActiveSet)
	assert.Empty(t, into[dto.List[dto.ProposalNonVoter]](t, fetch(t, base+"/proposals/46/non-voters")).Items)
	assert.Equal(t, 404, fetch(t, base+"/proposals/999/non-voters").status)
}

func TestNonVotersReadPrunedHeightsFromTheArchive(t *testing.T) {
	e := newSyncEnv(t)
	e.index(t, hResolve)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	code, stderr := e.snapshots(t, "--id", "47")
	require.Equal(t, 0, code, stderr)

	// The local node no longer has the tally's heights.
	e.archive = fakenode.New(t)
	e.node.SetEarliestHeight(setHeight47 + 1)
	code, out = e.syncState(t, "--job", "proposal_non_voters", "--id", "47")
	require.Equal(t, 0, code, out)
	assert.Equal(t, sorted(nonVoters47), e.nonVoterRows(t, 47))
	assert.Equal(t, 1, e.node.RequestsAt(fakenode.RouteValidators, setHeight47), "asked first, answered pruned")
	assert.Equal(t, 1, e.archive.RequestsAt(fakenode.RouteValidators, setHeight47))
	assert.Equal(t, 1, e.archive.RequestsAt(fakenode.RouteBlockResults, hResolve))
}

func TestActiveSetNamesValidatorsFromTheirCreation(t *testing.T) {
	e := newSyncEnv(t)
	e.index(t, hCreated)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	defer pool.Close()

	// BonyNode's MsgCreateValidator carries its consensus key.
	key, err := base64.StdEncoding.DecodeString("0+QWpq01Advi0YWhSCcJfL0NHPMgNxcSEmqYJZJbv7I=")
	require.NoError(t, err)
	bonyCons := activeset.ConsensusAddress(key)
	r := activeset.NewPGResolver(pool)
	ops, err := r.Operators(ctx, []string{bonyCons, "FFFF"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{bonyCons: bonyNode}, ops, "no validators row: the creation names it; an unknown address is left out")

	// The validators table first.
	_, err = e.db.Exec(`INSERT INTO explorer.validators (operator_address, consensus_address, account_address, moniker, status,
		tokens, delegator_shares, commission_rate, commission_max_rate, commission_max_change_rate, updated_at)
		VALUES ('bzevaloper1other', $1, 'bze1other', 'Other', 'bonded', 0, 0, 0, 0, 0, now())`, kh8aqCons)
	require.NoError(t, err)
	ops, err = r.Operators(ctx, []string{bonyCons, kh8aqCons})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{bonyCons: bonyNode, kh8aqCons: "bzevaloper1other"}, ops)
}
