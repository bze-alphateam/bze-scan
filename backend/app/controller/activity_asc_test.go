package controller_test

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// ascRows are activity rows oldest first, two transactions of one block
// among them.
func ascRows() []repository.Activity {
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	row := func(h, i int64) repository.Activity {
		return repository.Activity{Height: h, TxIndex: i, Time: at, Kind: "send", Category: "received", Success: true,
			Deltas: json.RawMessage(`[]`), MsgTypes: []string{}}
	}
	return []repository.Activity{row(101, 0), row(101, 1), row(102, 0)}
}

func TestActivityAscFromAHeight(t *testing.T) {
	repo := &fakeAccounts{activity: ascRows()}
	rec := get(t, newActivityAPI(repo), "/accounts/"+account+"/activity?incoming=true&order=asc&after_height=100&limit=2&denom=ubze")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get("Cache-Control"))
	assert.Equal(t, 1, repo.ascCalls)
	assert.Nil(t, repo.gotBefore, "the newest-first list is not read")
	require.NotNil(t, repo.gotStart)
	assert.Equal(t, repository.ActivityStart{After: &repository.ActivityKey{Height: 100, TxIndex: math.MaxInt32}},
		*repo.gotStart, "every row of the height itself is behind the start")
	assert.Equal(t, 3, repo.gotLimit, "one more than the page")
	assert.True(t, repo.gotIncoming)
	require.NotNil(t, repo.gotDenom)
	assert.Equal(t, "ubze", *repo.gotDenom)

	page := decode[dto.PagedList[dto.Activity]](t, rec)
	require.Len(t, page.Items, 2)
	assert.Equal(t, int64(101), page.Items[1].Height)
	assert.Nil(t, page.Total)
	require.NotNil(t, page.NextCursor)
	k, err := dto.DecodeActivityCursor(*page.NextCursor)
	require.NoError(t, err)
	assert.Equal(t, &repository.ActivityKey{Height: 101, TxIndex: 1}, k, "the cursor is the page's last row")

	// The next page continues after it.
	rec = get(t, newActivityAPI(repo), "/accounts/"+account+"/activity?incoming=true&order=asc&limit=2&cursor="+*page.NextCursor)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, repository.ActivityStart{After: &repository.ActivityKey{Height: 101, TxIndex: 1}}, *repo.gotStart)
}

func TestActivityAscFromATime(t *testing.T) {
	repo := &fakeAccounts{activity: ascRows()}
	rec := get(t, newActivityAPI(repo), "/accounts/"+account+"/activity?order=asc&after_time=2026-11-01T02:00:00%2B02:00&category=received")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotNil(t, repo.gotStart)
	assert.Nil(t, repo.gotStart.After)
	require.NotNil(t, repo.gotStart.Since)
	assert.True(t, repo.gotStart.Since.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)))
	require.NotNil(t, repo.gotCategory)
	assert.Equal(t, "received", *repo.gotCategory)
	assert.Contains(t, rec.Body.String(), `"next_cursor":null`, "three rows fit the default page")

	repo.activityErr = errors.New("db down")
	rec = get(t, newActivityAPI(repo), "/accounts/"+account+"/activity?order=asc&after_height=0")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// order=desc, or none, is the newest-first list it always was.
func TestActivityDescIsTheDefault(t *testing.T) {
	cursor := dto.EncodeActivityCursor(repository.Activity{Height: 30, TxIndex: 2})
	for _, q := range []string{"", "?order=", "?order=desc", "?order=desc&cursor=" + cursor, "?order=desc&page=2&limit=1"} {
		repo := &fakeAccounts{activity: activityRows(), total: 3}
		rec := get(t, newActivityAPI(repo), "/accounts/"+account+"/activity"+q)
		require.Equal(t, http.StatusOK, rec.Code, q+" "+rec.Body.String())
		assert.Zero(t, repo.ascCalls, q)
	}
}

func TestActivityAscValidation(t *testing.T) {
	cursor := dto.EncodeActivityCursor(repository.Activity{Height: 30, TxIndex: -1})
	const start = "order=asc needs exactly one of after_height, after_time or cursor"
	for q, msg := range map[string]string{
		"?order=up":                               "order must be asc or desc",
		"?order=ASC&after_height=1":               "order must be asc or desc",
		"?order=asc&after_height=1&page=1":        "page is not available with order=asc",
		"?order=asc&cursor=" + cursor + "&page=2": "page is not available with order=asc",
		"?order=asc":                              start,
		"?order=asc&after_height=1&after_time=2026-11-01T00:00:00Z":   start,
		"?order=asc&after_height=1&cursor=" + cursor:                  start,
		"?order=asc&after_time=2026-11-01T00:00:00Z&cursor=" + cursor: start,
		"?order=asc&after_height=-1":                                  "after_height must be a non-negative integer",
		"?order=asc&after_height=1.5":                                 "after_height must be a non-negative integer",
		"?order=asc&after_height=%2B5":                                "after_height must be a non-negative integer",
		"?order=asc&after_height=99999999999999999999":                "after_height must be a non-negative integer",
		"?order=asc&after_time=2026-11-01":                            "after_time must be an RFC 3339 time",
		"?order=asc&after_time=yesterday":                             "after_time must be an RFC 3339 time",
		"?order=asc&cursor=@@":                                        "cursor is not valid",
		"?after_height=1":                                             "after_height and after_time need order=asc",
		"?order=desc&after_time=2026-11-01T00:00:00Z":                 "after_height and after_time need order=asc",
	} {
		repo := &fakeAccounts{activity: ascRows()}
		rec := get(t, newActivityAPI(repo), "/accounts/"+account+"/activity"+q)
		assert.Equal(t, http.StatusBadRequest, rec.Code, q)
		body := decode[middleware.ErrorBody](t, rec)
		assert.Equal(t, msg, body.Error.Message, q)
		assert.Zero(t, repo.ascCalls, q)
	}
}
