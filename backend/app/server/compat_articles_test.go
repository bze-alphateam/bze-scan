package server_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/server"
	"github.com/bze-alphateam/bze-scan/backend/internal/medium"
)

const mediumURL = "https://medium.test/feed/bzedge-community"

// feedDoer answers the feed with body (status 200 unless set) until it is
// told to fail, and counts the calls.
type feedDoer struct {
	mu     sync.Mutex
	body   string
	status int
	fail   bool
	calls  int
	agents []string
	urls   []string
}

func (d *feedDoer) Do(req *http.Request) (*http.Response, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	d.urls = append(d.urls, req.URL.String())
	d.agents = append(d.agents, req.Header.Get("User-Agent"))
	if d.fail {
		return nil, errors.New("connection refused")
	}
	status := d.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(d.body))}, nil
}

func articlesServer(t *testing.T, doer *feedDoer, clock *compatClock) *echo.Echo {
	t.Helper()
	lg, _ := logtest.NewNullLogger()
	feed := medium.NewFeed(medium.FeedDeps{Source: medium.New(doer, mediumURL), Log: lg, Clock: clock})
	return server.New(server.Deps{CompatArticles: feed})
}

func testdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/medium/" + name)
	require.NoError(t, err)
	return string(b)
}

func articles(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code)
	var out []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	return out
}

// rss wraps items in the feed's envelope.
func rss(items ...string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><rss xmlns:dc="http://purl.org/dc/elements/1.1/" ` +
		`xmlns:content="http://purl.org/rss/1.0/modules/content/" version="2.0"><channel><title>beezee Community</title>` +
		strings.Join(items, "") + `</channel></rss>`
}

// item is one feed item; an empty author leaves dc:creator out.
func item(title, author, date, content string) string {
	s := `<item><title><![CDATA[` + title + `]]></title><link>https://medium.com/bzedge-community/` + title + `</link>`
	if author != "" {
		s += `<dc:creator><![CDATA[` + author + `]]></dc:creator>`
	}
	return s + `<pubDate>` + date + `</pubDate><content:encoded><![CDATA[` + content + `]]></content:encoded></item>`
}

func TestCompatArticlesMatchTheAggregatorsAnswerToTheSameFeed(t *testing.T) {
	// feed.xml was read from Medium (ten items) at the moment getbze.com's
	// aggregator answered aggregator.json.
	doer := &feedDoer{body: testdata(t, "feed.xml")}
	rec := serve(t, articlesServer(t, doer, &compatClock{now: time.Unix(1000, 0)}), http.MethodGet, "/api/articles/medium")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, testdata(t, "aggregator.json"), rec.Body.String())
	assert.Equal(t, []string{mediumURL}, doer.urls)
	assert.Equal(t, []string{"Gofeed/1.0"}, doer.agents, "the aggregator's fetch")
	assert.Empty(t, rec.Header().Get(echo.HeaderCacheControl), "the aggregator sends none")
}

func TestCompatArticlesKeepTheFeedsFirstSix(t *testing.T) {
	var items []string
	for _, title := range []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8"} {
		items = append(items, item(title, "beezee", "Fri, 23 Oct 2026 12:07:15 GMT", "<p>"+title+"</p>"))
	}
	rec := serve(t, articlesServer(t, &feedDoer{body: rss(items...)}, &compatClock{now: time.Unix(1000, 0)}),
		http.MethodGet, "/api/articles/medium")
	var titles []string
	for _, a := range articles(t, rec) {
		titles = append(titles, a["title"].(string))
	}
	assert.Equal(t, []string{"a1", "a2", "a3", "a4", "a5", "a6"}, titles)
}

func TestCompatArticlesPictureDescriptionDateAndAuthor(t *testing.T) {
	long := strings.Repeat("a", medium.DescriptionLimit-1) + "é and more"
	body := rss(
		item("cdn", "beezee", "Fri, 23 Oct 2026 12:07:15 GMT",
			`<figure><img alt="" src="https://cdn-images-1.medium.com/max/1024/1*x.png" /></figure>`+
				`<p>Fish &amp; <b>chips</b> “quoted”</p><img src="https://cdn-images-1.medium.com/max/1024/2.png">`),
		item("not-cdn", "", "Fri, 23 Oct 2026 15:07:15 +0300",
			`<img src="https://medium.com/_/stat?event=post.clientViewed" width="1" height="1"><img src="https://cdn-images-1.medium.com/y.png">`+long),
		item("none", "beezee", "Thu, 22 Oct 2026 08:00:00 GMT", `<p>No picture at all.</p>`),
	)
	rec := serve(t, articlesServer(t, &feedDoer{body: body}, &compatClock{now: time.Unix(1000, 0)}),
		http.MethodGet, "/api/articles/medium")
	require.Equal(t, http.StatusOK, rec.Code)

	// The first <img> counts, and only when its src holds "https://cdn";
	// picture_url is always present. HTML is stripped (entities escaped
	// again, as bluemonday does); the cut is at 150 bytes, as the aggregator
	// cuts, so a two-byte character across it leaves a broken byte that the
	// JSON encoder turns into U+FFFD. The date is in UTC, as gofeed parses it.
	assert.Equal(t, `[`+
		`{"title":"cdn","url":"https://medium.com/bzedge-community/cdn",`+
		`"picture_url":"https://cdn-images-1.medium.com/max/1024/1*x.png",`+
		`"description":"Fish \u0026amp; chips “quoted”","publish_date":"2026-10-23T12:07:15Z","author_name":"beezee"},`+
		`{"title":"not-cdn","url":"https://medium.com/bzedge-community/not-cdn","picture_url":"",`+
		`"description":"`+strings.Repeat("a", medium.DescriptionLimit-1)+`\ufffd...",`+
		`"publish_date":"2026-10-23T12:07:15Z","author_name":""},`+
		`{"title":"none","url":"https://medium.com/bzedge-community/none","picture_url":"",`+
		`"description":"No picture at all.","publish_date":"2026-10-22T08:00:00Z","author_name":"beezee"}`+
		`]`+"\n", rec.Body.String())
}

func TestCompatArticlesCutAtExactlyTheLimit(t *testing.T) {
	exact := strings.Repeat("é", medium.DescriptionLimit/2)
	over := exact + "x"
	body := rss(item("exact", "beezee", "Fri, 23 Oct 2026 12:07:15 GMT", exact),
		item("over", "beezee", "Fri, 23 Oct 2026 12:07:15 GMT", over))
	got := articles(t, serve(t, articlesServer(t, &feedDoer{body: body}, &compatClock{now: time.Unix(1000, 0)}),
		http.MethodGet, "/api/articles/medium"))
	require.Len(t, got, 2)
	assert.Equal(t, exact, got[0]["description"], "150 bytes are kept whole")
	assert.Equal(t, exact+"...", got[1]["description"], "151 bytes are cut at 150")
}

func TestCompatArticlesFallBackToTheItemDescription(t *testing.T) {
	body := rss(`<item><title>t</title><link>https://medium.com/x</link><pubDate>Fri, 23 Oct 2026 12:07:15 GMT</pubDate>` +
		`<description><![CDATA[<img src="https://cdn-images-1.medium.com/z.png"><p>From the description.</p>]]></description></item>`)
	got := articles(t, serve(t, articlesServer(t, &feedDoer{body: body}, &compatClock{now: time.Unix(1000, 0)}),
		http.MethodGet, "/api/articles/medium"))
	require.Len(t, got, 1)
	assert.Equal(t, "From the description.", got[0]["description"])
	assert.Equal(t, "https://cdn-images-1.medium.com/z.png", got[0]["picture_url"])
}

func TestCompatArticlesOfAnEmptyFeedAreAnEmptyList(t *testing.T) {
	rec := serve(t, articlesServer(t, &feedDoer{body: rss()}, &compatClock{now: time.Unix(1000, 0)}),
		http.MethodGet, "/api/articles/medium")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]\n", rec.Body.String(), "[] where the aggregator answers null")
}

func TestCompatArticlesAreCachedThenBackedUp(t *testing.T) {
	doer := &feedDoer{body: rss(item("first", "beezee", "Fri, 23 Oct 2026 12:07:15 GMT", "one"))}
	clock := &compatClock{now: time.Unix(1000, 0)}
	e := articlesServer(t, doer, clock)
	get := func() string { return serve(t, e, http.MethodGet, "/api/articles/medium").Body.String() }
	first := get()
	require.Contains(t, first, `"title":"first"`)

	// Within 600 s the feed is not asked again.
	doer.body = rss(item("second", "beezee", "Fri, 23 Oct 2026 12:07:15 GMT", "two"))
	clock.now = clock.now.Add(medium.FreshFor - time.Second)
	assert.Equal(t, first, get())
	assert.Equal(t, 1, doer.calls)

	// After 600 s the feed fails: the last read is served (the aggregator
	// answers [] for 600 s), and the feed is asked again at most every
	// minute meanwhile.
	doer.fail = true
	clock.now = clock.now.Add(time.Second)
	assert.Equal(t, first, get())
	assert.Equal(t, 2, doer.calls)
	clock.now = clock.now.Add(medium.RetryAfter - time.Second)
	assert.Equal(t, first, get())
	assert.Equal(t, 2, doer.calls)
	clock.now = clock.now.Add(time.Second)
	assert.Equal(t, first, get())
	assert.Equal(t, 3, doer.calls)

	// Still failing up to 24 hours after the last read.
	clock.now = time.Unix(1000, 0).Add(medium.BackupFor - time.Second)
	assert.Equal(t, first, get())

	// 24 hours after the last read: nothing to serve, an empty list.
	clock.now = clock.now.Add(time.Second)
	rec := serve(t, e, http.MethodGet, "/api/articles/medium")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]\n", rec.Body.String())

	// The feed answers again: the new list.
	doer.fail = false
	clock.now = clock.now.Add(medium.RetryAfter)
	assert.Contains(t, get(), `"title":"second"`)
}

func TestCompatArticlesTreatErrorAnswersAsFailures(t *testing.T) {
	for name, doer := range map[string]*feedDoer{
		"unreachable": {fail: true},
		"HTTP 503":    {status: http.StatusServiceUnavailable, body: rss(item("x", "", "Fri, 23 Oct 2026 12:07:15 GMT", "x"))},
		"not a feed":  {body: "<html>Too many requests</html>"},
	} {
		rec := serve(t, articlesServer(t, doer, &compatClock{now: time.Unix(1000, 0)}), http.MethodGet, "/api/articles/medium")
		assert.Equal(t, http.StatusOK, rec.Code, name)
		assert.Equal(t, "[]\n", rec.Body.String(), name)
	}
}

func TestCompatArticlesNeedAFeed(t *testing.T) {
	assert.Equal(t, http.StatusNotFound, serve(t, server.New(server.Deps{}), http.MethodGet, "/api/articles/medium").Code)
}

func TestCompatArticlesAllowAnyOrigin(t *testing.T) {
	doer := &feedDoer{body: rss()}
	lg, _ := logtest.NewNullLogger()
	feed := medium.NewFeed(medium.FeedDeps{Source: medium.New(doer, mediumURL), Log: lg, Clock: &compatClock{now: time.Unix(1000, 0)}})
	e := server.New(server.Deps{CompatArticles: feed, CORSAllowedOrigins: []string{"https://explorer.getbze.com"}})
	rec := corsRequest(e, http.MethodGet, "/api/articles/medium", "https://getbze.com")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "*", rec.Header().Get(echo.HeaderAccessControlAllowOrigin))
}
