//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
)

// seedPagedDaoRows writes, straight into the tables, the proposals of DAO 1
// (every status, a cleaned-up one and a second DAO's proposal that must not
// count), the ballots of proposal 101 and more history rows of DAO 1: the
// numbered pages, their totals and counts are what is under test.
func seedPagedDaoRows(t *testing.T, e *syncEnv) {
	t.Helper()
	_, err := e.db.Exec(`INSERT INTO explorer.dao_proposals (id, dao_id, title, status, voting_end, cleaned_height) VALUES
		(101, 1, 'Voting late', 'voting', '2027-01-03 10:00:00+00', NULL),
		(102, 1, 'Executed old', 'executed', '2026-11-01 10:00:00+00', NULL),
		(103, 1, 'Failed old', 'execution_failed', '2026-11-02 10:00:00+00', NULL),
		(104, 1, 'Voting soon B', 'voting', '2027-01-01 10:00:00+00', NULL),
		(105, 1, 'Rejected', 'rejected', '2026-11-03 10:00:00+00', NULL),
		(106, 1, 'Voting soon A', 'voting', '2027-01-01 10:00:00+00', NULL),
		(107, 1, 'Failed new', 'execution_failed', '2026-11-04 10:00:00+00', NULL),
		(108, 1, 'Expired', 'expired', '2026-11-05 10:00:00+00', NULL),
		(109, 1, 'Cancelled', 'cancelled', '2026-11-06 10:00:00+00', NULL),
		(110, 1, 'Executed new', 'executed', '2026-11-07 10:00:00+00', 25200100),
		(111, 2, 'Other DAO', 'voting', '2027-01-01 09:00:00+00', NULL),
		(112, NULL, NULL, NULL, NULL, NULL)`)
	require.NoError(t, err)
	_, err = e.db.Exec(`INSERT INTO explorer.dao_votes (proposal_id, voter, option, weight, height, tx_index, msg_index, tx_hash, time) VALUES
		(101, 'bze1voter1', 'yes', 50, 25200010, 0, 0, 'V1', '2026-12-01 10:00:00+00'),
		(101, 'bze1voter2', 'no', 40, 25200011, 0, 0, 'V2', '2026-12-01 10:00:00+00'),
		(101, 'bze1voter3', 'yes', 40, 25200012, 0, 0, 'V3', '2026-12-01 10:00:00+00'),
		(101, 'bze1voter4', 'abstain', 30, 25200013, 0, 0, 'V4', '2026-12-01 10:00:00+00'),
		(101, 'bze1voter5', 'yes', 20, 25200014, 0, 0, 'V5', '2026-12-01 10:00:00+00'),
		(101, 'bze1voter6', 'no', 10, 25200015, 0, 0, 'V6', '2026-12-01 10:00:00+00'),
		(104, 'bze1voter1', 'yes', 99, 25200016, 0, 0, 'V7', '2026-12-01 10:00:00+00')`)
	require.NoError(t, err)
	_, err = e.db.Exec(`INSERT INTO explorer.dao_events (height, tx_index, msg_index, seq, dao_id, type, attrs, tx_hash, time) VALUES
		(25200001, 0, 0, 0, 1, 'EventDaoMetadataUpdated', '{"id":"1"}', 'H1', '2026-12-01 10:00:00+00'),
		(25200002, 0, 0, 0, 1, 'EventDaoConfigUpdated', '{"id":"1"}', 'H2', '2026-12-01 10:01:00+00'),
		(25200003, 0, 0, 0, 1, 'EventDaoMembersUpdated', '{"id":"1"}', 'H3', '2026-12-01 10:02:00+00'),
		(25200003, 0, 0, 1, 2, 'EventDaoMetadataUpdated', '{"id":"2"}', 'H3', '2026-12-01 10:02:00+00'),
		(25200004, -1, -1, 0, 1, 'EventAdminChanged', '{"id":"1"}', NULL, '2026-12-01 10:03:00+00')`)
	require.NoError(t, err)
}

func pagedProposalIDs(l dto.DaoProposalList) []int64 {
	out := []int64{}
	for _, p := range l.Items {
		out = append(out, p.ID)
	}
	return out
}

func pagedVoters(l dto.DaoVoteList) []string {
	out := []string{}
	for _, v := range l.Items {
		out = append(out, v.Voter)
	}
	return out
}

// getOK fetches url, requires a 200 and decodes the body.
func getOK[T any](t *testing.T, url string) T {
	t.Helper()
	r := fetch(t, url)
	require.Equal(t, http.StatusOK, r.status, url+": "+string(r.body))
	return into[T](t, r)
}

func TestDaoPageCountsItsProposals(t *testing.T) {
	e := newSyncEnv(t)
	indexTheDaos(t, e)
	seedPagedDaoRows(t, e)
	base := e.serveAPI(t)

	one := getOK[dto.Dao](t, base+"/daos/1")
	// Open = the DAOs list's definition: voting and not cleaned up.
	assert.Equal(t, int64(3), one.ProposalsOpen)
	assert.Equal(t, int64(10), one.ProposalsTotal, "every indexed proposal of the DAO, the cleaned-up one included")
	two := getOK[dto.Dao](t, base+"/daos/2")
	assert.Equal(t, int64(1), two.ProposalsOpen)
	assert.Equal(t, int64(1), two.ProposalsTotal)

	list := getOK[dto.DaoList](t, base+"/daos?limit=100")
	for _, d := range list.Items {
		if d.ID == 1 {
			assert.Equal(t, one.ProposalsOpen, d.ProposalsOpen, "the page and the list agree")
		}
	}
}

// The numbered pages of a DAO's proposals: active first (voting by soonest
// end then id, then execution_failed newest first), then the finished ones
// newest first; the order holds across page boundaries.
func TestDaoProposalsInNumberedPagesActiveFirst(t *testing.T) {
	e := newSyncEnv(t)
	indexTheDaos(t, e)
	seedPagedDaoRows(t, e)
	base := e.serveAPI(t)
	page := func(query string) dto.DaoProposalList {
		t.Helper()
		return getOK[dto.DaoProposalList](t, base+"/daos/1/proposals?"+query)
	}
	allCounts := map[string]int64{"voting": 3, "execution_failed": 2, "executed": 2, "rejected": 1, "expired": 1, "cancelled": 1}

	// Pages of 4: the boundary between the active and the finished rows falls
	// inside page 2.
	p1 := page("page=1&limit=4")
	assert.Equal(t, []int64{104, 106, 101, 107}, pagedProposalIDs(p1))
	require.NotNil(t, p1.Total)
	assert.Equal(t, int64(10), *p1.Total)
	assert.Nil(t, p1.NextCursor)
	assert.Equal(t, allCounts, p1.StatusCounts)
	assert.Equal(t, []int64{103, 110, 109, 108}, pagedProposalIDs(page("page=2&limit=4")))
	assert.Equal(t, []int64{105, 102}, pagedProposalIDs(page("page=3&limit=4")))
	p4 := page("page=4&limit=4")
	assert.Empty(t, p4.Items)
	require.NotNil(t, p4.Total)
	assert.Equal(t, int64(10), *p4.Total, "a page past the end keeps the real total")

	// The active alias, its total and the counts, which ignore the filter.
	active := page("status=active&page=1&limit=3")
	assert.Equal(t, []int64{104, 106, 101}, pagedProposalIDs(active))
	require.NotNil(t, active.Total)
	assert.Equal(t, int64(5), *active.Total)
	assert.Equal(t, allCounts, active.StatusCounts, "the counts are the whole DAO's")
	assert.Equal(t, []int64{107, 103}, pagedProposalIDs(page("status=active&page=2&limit=3")))
	assert.Equal(t, page("status=voting&status=execution_failed&page=1&limit=10").Items, page("status=active&page=1&limit=10").Items)

	// The total of every filter is what the cursor list holds, and equals the
	// status count.
	for st, n := range allCounts {
		cursorList := getOK[dto.DaoProposalList](t, base+"/daos/1/proposals?limit=100&status="+st)
		paged := page("page=1&limit=100&status=" + st)
		require.NotNil(t, paged.Total, st)
		assert.Equal(t, int64(len(cursorList.Items)), *paged.Total, st)
		assert.Equal(t, n, *paged.Total, st)
		assert.ElementsMatch(t, pagedProposalIDs(cursorList), pagedProposalIDs(paged), st)
	}

	// Cursor mode is unchanged: newest first, no total, no counts.
	r := fetch(t, base+"/daos/1/proposals?limit=4")
	require.Equal(t, http.StatusOK, r.status)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(r.body, &raw))
	assert.NotContains(t, raw, "total")
	assert.NotContains(t, raw, "status_counts")
	c1 := into[dto.DaoProposalList](t, r)
	assert.Equal(t, []int64{110, 109, 108, 107}, pagedProposalIDs(c1))
	require.NotNil(t, c1.NextCursor)
	c2 := getOK[dto.DaoProposalList](t, base+"/daos/1/proposals?limit=4&cursor="+*c1.NextCursor)
	assert.Equal(t, []int64{106, 105, 104, 103}, pagedProposalIDs(c2))
	assert.Equal(t, []int64{107, 106, 104, 103, 101}, pagedProposalIDs(getOK[dto.DaoProposalList](t,
		base+"/daos/1/proposals?status=active")), "the alias in cursor mode: newest first")

	for _, q := range []string{"status=open", "page=0", "page=1&cursor=" + *c1.NextCursor} {
		assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/daos/1/proposals?"+q).status, q)
	}
	assert.Equal(t, http.StatusNotFound, fetch(t, base+"/daos/9/proposals?page=1").status)
}

// The numbered pages of a proposal's votes: heaviest first as in cursor mode,
// total under the option filter, option_counts over every ballot.
func TestDaoVotesInNumberedPagesWithCounts(t *testing.T) {
	e := newSyncEnv(t)
	indexTheDaos(t, e)
	seedPagedDaoRows(t, e)
	base := e.serveAPI(t)
	page := func(query string) dto.DaoVoteList {
		t.Helper()
		return getOK[dto.DaoVoteList](t, base+"/dao-proposals/101/votes?"+query)
	}
	counts := map[string]int64{"yes": 3, "no": 2, "abstain": 1}

	// Walk the cursor list, then the numbered pages: same rows, same order.
	var walked []string
	cursor := ""
	for range 10 {
		u := base + "/dao-proposals/101/votes?limit=4"
		if cursor != "" {
			u += "&cursor=" + cursor
		}
		r := fetch(t, u)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(r.body, &raw))
		assert.NotContains(t, raw, "total")
		assert.NotContains(t, raw, "option_counts")
		l := into[dto.DaoVoteList](t, r)
		walked = append(walked, pagedVoters(l)...)
		if l.NextCursor == nil {
			break
		}
		cursor = *l.NextCursor
	}
	assert.Equal(t, []string{"bze1voter1", "bze1voter2", "bze1voter3", "bze1voter4", "bze1voter5", "bze1voter6"}, walked,
		"heaviest first, then by voter")

	p1 := page("page=1&limit=4")
	p2 := page("page=2&limit=4")
	assert.Equal(t, walked, append(pagedVoters(p1), pagedVoters(p2)...))
	require.NotNil(t, p1.Total)
	assert.Equal(t, int64(6), *p1.Total)
	assert.Nil(t, p1.NextCursor)
	assert.Equal(t, counts, p1.OptionCounts)
	p3 := page("page=3&limit=4")
	assert.Empty(t, p3.Items)
	require.NotNil(t, p3.Total)
	assert.Equal(t, int64(6), *p3.Total)

	for option, n := range counts {
		paged := page("page=1&limit=1&option=" + option)
		require.NotNil(t, paged.Total, option)
		assert.Equal(t, n, *paged.Total, "the total follows the option filter")
		assert.Equal(t, counts, paged.OptionCounts, "the counts ignore the option filter")
		require.Len(t, paged.Items, 1)
		assert.Equal(t, option, paged.Items[0].Option)
	}

	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/dao-proposals/101/votes?page=-1").status)
	assert.Equal(t, http.StatusNotFound, fetch(t, base+"/dao-proposals/999/votes?page=1").status)
}

// The numbered pages of a DAO's history hold the cursor walk's rows, in its
// order, with the total of the DAO's events.
func TestDaoHistoryInNumberedPages(t *testing.T) {
	e := newSyncEnv(t)
	indexTheDaos(t, e)
	seedPagedDaoRows(t, e)
	base := e.serveAPI(t)
	key := func(ev dto.DaoEvent) string { return fmt.Sprintf("%d/%d/%d/%d", ev.Height, ev.TxIndex, ev.MsgIndex, ev.Seq) }

	var walked []string
	cursor := ""
	for range 10 {
		u := base + "/daos/1/history?limit=2"
		if cursor != "" {
			u += "&cursor=" + cursor
		}
		r := fetch(t, u)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		assert.NotContains(t, string(r.body), `"total"`)
		l := into[dto.List[dto.DaoEvent]](t, r)
		for _, ev := range l.Items {
			walked = append(walked, key(ev))
		}
		if l.NextCursor == nil {
			break
		}
		cursor = *l.NextCursor
	}
	require.Len(t, walked, 5, "the indexed creation and the four seeded events of DAO 1")

	var paged []string
	for n := 1; n <= 3; n++ {
		l := getOK[dto.PagedList[dto.DaoEvent]](t, fmt.Sprintf("%s/daos/1/history?page=%d&limit=2", base, n))
		require.NotNil(t, l.Total)
		assert.Equal(t, int64(5), *l.Total)
		assert.Nil(t, l.NextCursor)
		for _, ev := range l.Items {
			paged = append(paged, key(ev))
		}
	}
	assert.Equal(t, walked, paged)
	past := getOK[dto.PagedList[dto.DaoEvent]](t, base+"/daos/1/history?page=4&limit=2")
	assert.Empty(t, past.Items)
	require.NotNil(t, past.Total)
	assert.Equal(t, int64(5), *past.Total)

	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/daos/1/history?page=abc").status)
	assert.Equal(t, http.StatusNotFound, fetch(t, base+"/daos/9/history?page=1").status)
}
