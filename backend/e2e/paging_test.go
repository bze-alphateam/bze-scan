//go:build e2e

package e2e_test

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
)

// TestNumberedPages covers the parameters numbered pages use: /blocks?before
// (pages pinned to a top height) and an account's activity ?page with its
// total.
func TestNumberedPages(t *testing.T) {
	e := newSyncEnv(t)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	e.index(t, append([]int64{24998321, 24998322, 24998323, 24998324, 24998325, 24998326}, txHeights...)...)
	base := e.serveAPI(t)

	blocks := func(query string) []int64 {
		t.Helper()
		r := fetch(t, base+"/blocks?"+query)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
		var out []int64
		for _, b := range into[dto.List[dto.BlockSummary]](t, r).Items {
			out = append(out, b.Height)
		}
		return out
	}
	assert.Equal(t, []int64{24998325, 24998324, 24998323}, blocks("before=24998326&limit=3"), "before is exclusive")
	assert.Equal(t, blocks("limit=3"), blocks("before=999999999&limit=3"), "above the tip: the newest blocks")
	assert.Equal(t, slices.Max(txHeights), blocks("before=999999999&limit=1")[0])

	activity := func(address, query string) dto.PagedList[dto.Activity] {
		t.Helper()
		r := fetch(t, fmt.Sprintf("%s/accounts/%s/activity?%s", base, address, query))
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
		return into[dto.PagedList[dto.Activity]](t, r)
	}
	newest := activity(trader, "limit=2")
	require.Len(t, newest.Items, 2)
	assert.Nil(t, newest.Total, "no page, no total")

	rows := int64(len(positions(activityPages(t, base, trader, "", 100))))
	require.Greater(t, rows, int64(2))
	first, second := activity(trader, "page=1&limit=1"), activity(trader, "page=2&limit=1")
	require.Len(t, first.Items, 1)
	require.Len(t, second.Items, 1)
	assert.Equal(t, newest.Items, append(first.Items, second.Items...), "pages follow the keyset order")
	for _, p := range []dto.PagedList[dto.Activity]{first, second} {
		require.NotNil(t, p.Total)
		assert.Equal(t, rows, *p.Total, "the trader's row count")
	}
	require.NotNil(t, second.NextCursor)
	older := activity(trader, "limit=1&cursor="+*second.NextCursor)
	assert.Equal(t, activity(trader, "page=3&limit=1").Items, older.Items, "older from a numbered page")

	dex := activity(trader, "page=1&limit=25&category=dex")
	require.NotNil(t, dex.Total)
	assert.Equal(t, int64(len(dex.Items)), *dex.Total, "a category total counts its rows")
	assert.Less(t, *dex.Total, rows, "the trader also has a failed transaction outside dex")

	past := activity(trader, "page=99&limit=25")
	assert.Empty(t, past.Items)
	require.NotNil(t, past.Total)
	assert.Equal(t, rows, *past.Total)

	for address, query := range map[string]string{trader: "page=1&category=staking", chainToolsOwner: "page=1"} {
		p := activity(address, query)
		assert.Empty(t, p.Items, address)
		require.NotNil(t, p.Total, address)
		assert.Zero(t, *p.Total, "no rows, or no accounts row at all")
	}

	for query, status := range map[string]int{"page=0": http.StatusBadRequest, "page=1&cursor=" + *second.NextCursor: http.StatusBadRequest} {
		assert.Equal(t, status, fetch(t, base+"/accounts/"+trader+"/activity?"+query).status, query)
	}
	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/blocks?before=0").status)
}
