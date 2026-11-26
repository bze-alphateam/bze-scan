//go:build e2e

package e2e_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// daoIDs are the ids of a DAOs response, in order.
func daoIDs(l dto.DaoList) []int64 {
	out := []int64{}
	for _, d := range l.Items {
		out = append(out, d.ID)
	}
	return out
}

// editDaos rewrites the recorded DAOs answers: edit gets each DAO object (as
// the node's JSON) and may change it. Both the list and the per-DAO answers
// are rewritten, as the node answers both from the same state.
func editDaos(t *testing.T, e *syncEnv, edit func(dao map[string]any)) {
	t.Helper()
	var list struct {
		Daos       []map[string]any `json:"daos"`
		Pagination map[string]any   `json:"pagination"`
	}
	require.NoError(t, json.Unmarshal(fakenode.ReadGRPCFixture(t, "dao", "Daos", ""), &list))
	for _, d := range list.Daos {
		edit(d)
		one, err := json.Marshal(map[string]any{"dao": d})
		require.NoError(t, err)
		e.grpc.SetResponse("dao", "Dao", d["id"].(string), one)
	}
	body, err := json.Marshal(list)
	require.NoError(t, err)
	e.grpc.SetResponse("dao", "Daos", "", body)
}

// The members, electorate sources and pending nominations of the chain's DAOs
// are indexed by the state sync, served as filters of the DAOs list, and
// replaced (never accumulated) when the chain's record changes.
func TestDaoFiltersFollowTheChainsRecord(t *testing.T) {
	e := newSyncEnv(t)
	indexTheDaos(t, e)
	// The staking rewards of the electorate: numeric ids, as the filter requires.
	editDaos(t, e, func(d map[string]any) {
		if d["id"] == "2" {
			d["electorate"] = map[string]any{"sr_ids": []string{"1", "2"}, "dr_denom": "ubze"}
		}
	})
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	base := e.serveAPI(t)
	list := func(query string) dto.DaoList {
		t.Helper()
		r := fetch(t, base+"/daos?"+query)
		require.Equal(t, http.StatusOK, r.status, query+": "+string(r.body))
		assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
		return into[dto.DaoList](t, r)
	}

	assert.Equal(t, []string{"1 " + daoMember, "1 " + daoAdmin}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', dao_id, address) FROM explorer.dao_members ORDER BY address`))
	assert.Equal(t, []string{"2 dr/ubze", "2 sr/1", "2 sr/2"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', dao_id, source) FROM explorer.dao_sources ORDER BY source`))

	// member: the static-list DAO that lists the address; the spelling is normalised.
	assert.Equal(t, []int64{1}, daoIDs(list("member="+daoAdmin)))
	assert.Equal(t, []int64{1}, daoIDs(list("member="+strings.ToUpper(daoMember))))
	assert.Empty(t, list("member="+daoOne).Items, "the address of a DAO is not a member")
	assert.Empty(t, list("member="+daoAdmin+"&type=stake").Items, "filters combine")

	// source: OR-ed, repeatable; sr/ and dr/ spellings are the chain's.
	assert.Equal(t, []int64{2}, daoIDs(list("source=sr/1")))
	assert.Equal(t, []int64{2}, daoIDs(list("source=dr/ubze")))
	assert.Equal(t, []int64{2}, daoIDs(list("source=sr/3&source=sr/2&source=dr/other")))
	assert.Equal(t, []int64{2}, daoIDs(list("source=sr/1&source=dr/ubze")), "a DAO matching two is listed once")
	assert.Empty(t, list("source=sr/9&source=dr/uatom").Items)
	assert.Empty(t, list("source=dr/sr-1").Items, "the kind is part of the source")

	// q: a case-insensitive substring of the name, literal wildcards, with type.
	assert.Equal(t, []int64{1}, daoIDs(list("q=FOUND")))
	assert.Equal(t, []int64{2}, daoIDs(list("q=ers%20cl")))
	assert.Equal(t, []int64{2}, daoIDs(list("q=stakers&type=stake")))
	assert.Empty(t, list("q=stakers&type=static_list").Items)
	assert.Empty(t, list("q="+url.QueryEscape("%%")).Items, "% is not a wildcard")
	assert.Empty(t, list("q="+url.QueryEscape("Stakers_club")).Items, "_ is not a wildcard")
	assert.Empty(t, list("q="+url.QueryEscape(`\\`)).Items)
	assert.Empty(t, list("pending_admin="+daoAdmin).Items, "nothing pending yet")
	assert.Nil(t, list("").Items[0].PendingAdmin)

	// A nomination is made and a member replaced: the next sync follows the chain.
	editDaos(t, e, func(d map[string]any) {
		if d["id"] == "1" {
			d["pending_admin"] = daoMember
			d["members"] = []string{daoAdmin, daoTwo}
		}
	})
	code, _ = e.syncState(t)
	require.Equal(t, 0, code)
	got := list("pending_admin=" + daoMember)
	require.Equal(t, []int64{1}, daoIDs(got))
	require.NotNil(t, got.Items[0].PendingAdmin)
	assert.Equal(t, daoMember, *got.Items[0].PendingAdmin)
	assert.Empty(t, list("member="+daoMember).Items, "the removed member is gone")
	assert.Equal(t, []int64{1}, daoIDs(list("member="+daoTwo)), "the added member is listed")
	assert.Equal(t, []int64{1}, daoIDs(list("member="+daoAdmin)), "a kept member stays, once")

	// The nomination is accepted (or withdrawn) and a source is dropped.
	editDaos(t, e, func(d map[string]any) {
		if d["id"] == "1" {
			d["members"] = []string{daoAdmin, daoTwo}
		}
		if d["id"] == "2" {
			d["electorate"] = map[string]any{"sr_ids": []string{"1"}, "dr_denom": ""}
		}
	})
	code, _ = e.syncState(t)
	require.Equal(t, 0, code)
	assert.Empty(t, list("pending_admin="+daoMember).Items, "a cleared nomination is gone")
	assert.Equal(t, []int64{2}, daoIDs(list("source=sr/1")))
	assert.Empty(t, list("source=sr/2&source=dr/ubze").Items, "a dropped source is gone")
	assert.Equal(t, []string{"2 sr/1"}, queryStrings(t, e.db, `SELECT concat_ws(' ', dao_id, source) FROM explorer.dao_sources`))

	// Bad input.
	tooMany := strings.Repeat("&source=sr/1", 21)[1:]
	for _, q := range []string{"member=nope", "pending_admin=nope", "source=ubze", "source=sr/", "source=sr/abc", "source=sr/-1", "source=x/1", tooMany, "q=a", "q=", "type=nope"} {
		assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/daos?"+q).status, q)
	}
	assert.Equal(t, http.StatusOK, fetch(t, base+"/daos?"+strings.Repeat("&source=sr/1", 20)[1:]).status)
}

// seedDaoProposalRows writes proposals and ballots straight into the tables:
// the orderings and the cursors are what is under test, not the indexing.
func seedDaoProposalRows(t *testing.T, e *syncEnv) {
	t.Helper()
	_, err := e.db.Exec(`INSERT INTO explorer.dao_proposals (id, dao_id, title, status, voting_end, cleaned_height) VALUES
		(10, 1, 'Late', 'voting', '2027-01-01 12:00:00+00', NULL),
		(11, 2, 'Soon A', 'voting', '2027-01-01 11:00:00.000001+00', NULL),
		(12, 2, 'Soon B', 'voting', '2027-01-01 11:00:00.000001+00', NULL),
		(13, 1, 'Cleaned', 'voting', '2027-01-01 10:00:00+00', 25100500),
		(14, 1, 'Done', 'executed', '2026-12-01 10:00:00+00', NULL),
		(15, 2, 'No', 'rejected', '2026-12-01 10:00:00+00', NULL),
		(16, 2, 'Old', 'expired', '2026-12-01 10:00:00+00', NULL),
		(17, NULL, NULL, NULL, NULL, NULL)`)
	require.NoError(t, err)
	_, err = e.db.Exec(`INSERT INTO explorer.dao_votes (proposal_id, voter, option, weight, height, tx_index, msg_index, tx_hash, time) VALUES
		(10, $1, 'yes', 100, 25100100, 0, 0, 'AA', '2026-12-01 10:00:00+00'),
		(11, $1, 'no',  100, 25100200, 0, 0, 'BB', '2026-12-01 11:00:00+00'),
		(12, $1, 'abstain', 7, 25100200, 1, 0, 'CC', '2026-12-01 11:00:00+00'),
		(99, $1, 'yes', 5, 25100050, 0, 0, 'DD', '2026-12-01 09:00:00+00'),
		(11, $2, 'yes', 3, 25100300, 0, 0, 'EE', '2026-12-01 12:00:00+00')`, daoAdmin, daoMember)
	require.NoError(t, err)
	_, err = e.db.Exec(`INSERT INTO explorer.dao_ballot_reductions (height, tx_index, msg_index, seq, proposal_id, voter, source, amount)
		VALUES (25100300, 0, 0, 0, 11, $1, 'sr:1', 40), (25100150, 0, 0, 0, 11, $1, 'sr:1', 1000)`, daoAdmin)
	require.NoError(t, err)
}

func proposalIDs(l dto.DaoProposalFeed) []int64 {
	out := []int64{}
	for _, p := range l.Items {
		out = append(out, p.ID)
	}
	return out
}

// walk follows next_cursor of GET path with the given page size and returns
// every page's ids.
func walkProposals(t *testing.T, base, path string, limit string) [][]int64 {
	t.Helper()
	var pages [][]int64
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	cursor := ""
	for range 20 {
		u := base + path + sep + "limit=" + limit
		if cursor != "" {
			u += "&cursor=" + cursor
		}
		r := fetch(t, u)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		page := into[dto.DaoProposalFeed](t, r)
		pages = append(pages, proposalIDs(page))
		if page.NextCursor == nil {
			return pages
		}
		cursor = *page.NextCursor
	}
	t.Fatal("the cursor never ends")
	return nil
}

func TestDaoProposalsAcrossDaosAreOrderedPerStatus(t *testing.T) {
	e := newSyncEnv(t)
	indexTheDaos(t, e)
	seedDaoProposalRows(t, e)
	base := e.serveAPI(t)

	// Open proposals: soonest voting_end first; a tie breaks by id; the
	// cursor survives it (the two tied rows are on different pages); the
	// cleaned one is not open.
	assert.Equal(t, [][]int64{{11}, {12}, {10}}, walkProposals(t, base, "/dao-proposals?status=voting", "1"))
	assert.Equal(t, [][]int64{{11, 12}, {10}}, walkProposals(t, base, "/dao-proposals?status=voting", "2"))
	r := fetch(t, base+"/dao-proposals?status=voting")
	assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
	open := into[dto.DaoProposalFeed](t, r)
	assert.True(t, open.Supported)
	require.Len(t, open.Items, 3)
	assert.Equal(t, &dto.DaoRef{ID: 2, Name: "Stakers club", Type: "stake"}, open.Items[0].Dao)
	assert.Equal(t, &dto.DaoRef{ID: 1, Name: "Founders", Type: "static_list"}, open.Items[2].Dao)
	assert.Equal(t, "Soon A", *open.Items[0].Title)

	// Any other status: newest (highest id) first.
	assert.Equal(t, [][]int64{{15}, {14}}, walkProposals(t, base, "/dao-proposals?status=executed&status=rejected", "1"))
	assert.Equal(t, [][]int64{{16, 15}}, walkProposals(t, base, "/dao-proposals?status=expired&status=rejected", "5"))
	assert.Equal(t, [][]int64{{16, 15, 14}, {12, 11, 10}}, walkProposals(t, base, "/dao-proposals", "3"),
		"no filter: every real proposal, by id; a placeholder row and a cleaned open one are left out")
	assert.Equal(t, [][]int64{{14}, {12}, {11}, {10}}, walkProposals(t, base, "/dao-proposals?status=voting&status=executed", "1"),
		"mixed statuses are not the open feed: by id")

	for _, q := range []string{"status=passed", "status=voting&status=nope", "cursor=@@", "status=voting&cursor=1", "limit=0"} {
		assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/dao-proposals?"+q).status, q)
	}
}

func TestDaoVotesOfAVoterAreNewestFirstWithEffectiveWeight(t *testing.T) {
	e := newSyncEnv(t)
	indexTheDaos(t, e)
	seedDaoProposalRows(t, e)
	base := e.serveAPI(t)

	type ballot = dto.VoterBallot
	var all []ballot
	cursor := ""
	var sizes []int
	for range 10 {
		u := base + "/dao-votes?limit=3&voter=" + strings.ToUpper(daoAdmin)
		if cursor != "" {
			u += "&cursor=" + cursor
		}
		r := fetch(t, u)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
		page := into[dto.VoterBallotList](t, r)
		assert.True(t, page.Supported)
		all = append(all, page.Items...)
		sizes = append(sizes, len(page.Items))
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	assert.Equal(t, []int{3, 1}, sizes)
	ids := []int64{}
	for _, b := range all {
		ids = append(ids, b.Proposal.ID)
	}
	assert.Equal(t, []int64{12, 11, 10, 99}, ids, "newest block first; the same block by proposal id, highest first")

	assert.Equal(t, "7", all[0].EffectiveWeight)
	assert.Equal(t, "abstain", all[0].Option)
	assert.Equal(t, "100", all[1].Weight)
	assert.Equal(t, "60", all[1].EffectiveWeight, "100 less the 40 reduced after the ballot; the earlier 1000 does not count")
	assert.Equal(t, "no", all[1].Option)
	assert.Equal(t, "Soon A", *all[1].Proposal.Title)
	assert.Equal(t, "voting", *all[1].Proposal.Status)
	assert.NotNil(t, all[1].Proposal.VotingEnd)
	assert.Equal(t, &dto.VoterDao{ID: 2, Name: "Stakers club"}, all[1].Dao)
	assert.Equal(t, &dto.VoterDao{ID: 1, Name: "Founders"}, all[2].Dao)
	assert.Equal(t, "100", all[2].EffectiveWeight)
	assert.Nil(t, all[3].Dao, "a ballot whose proposal the index does not hold")
	assert.Nil(t, all[3].Proposal.Title)
	assert.Equal(t, int64(25100050), all[3].Height)

	other := into[dto.VoterBallotList](t, fetch(t, base+"/dao-votes?voter="+daoMember))
	require.Len(t, other.Items, 1)
	assert.Equal(t, "3", other.Items[0].EffectiveWeight)
	assert.Empty(t, into[dto.VoterBallotList](t, fetch(t, base+"/dao-votes?voter="+daoOne)).Items)

	for _, q := range []string{"", "voter=nope", "voter=cosmos1" + daoAdmin[4:], "voter=" + daoAdmin + "&cursor=@@", "voter=" + daoAdmin + "&limit=0"} {
		assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/dao-votes?"+q).status, q)
	}
}

// Against a node before chain v8.3.0 the new lists are empty and say so.
func TestDaoAppListsAgainstANodeWithoutDaos(t *testing.T) {
	e := newSyncEnv(t)
	for _, m := range []string{"Params", "Daos", "Dao", "DaoController"} {
		e.grpc.SetResponse("dao", m, "", nil)
	}
	for _, id := range []string{"1", "2"} {
		e.grpc.SetResponse("dao", "Dao", id, nil)
		e.grpc.SetResponse("dao", "DaoController", id, nil)
	}
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	base := e.serveAPI(t)

	for _, q := range []string{"member=" + daoAdmin, "source=sr/1", "pending_admin=" + daoAdmin, "q=ab"} {
		l := into[dto.DaoList](t, fetch(t, base+"/daos?"+q))
		assert.False(t, l.Supported, q)
		assert.Empty(t, l.Items, q)
	}
	feed := into[dto.DaoProposalFeed](t, fetch(t, base+"/dao-proposals?status=voting"))
	assert.False(t, feed.Supported)
	assert.Empty(t, feed.Items)
	ballots := into[dto.VoterBallotList](t, fetch(t, base+"/dao-votes?voter="+daoAdmin))
	assert.False(t, ballots.Supported)
	assert.Empty(t, ballots.Items)
}
