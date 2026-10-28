// Package medium reads the latest articles of a Medium publication's RSS
// feed the way the BZE aggregator does: Client fetches and parses the feed,
// Feed keeps the answer for FreshFor and serves it for BackupFor more when
// the feed fails. Feed backs GET /api/articles/medium of the
// aggregator-compatible routes.
package medium

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/mmcdole/gofeed"
	log "github.com/sirupsen/logrus"
)

// DefaultFeedURL is the BZE community publication's feed.
const DefaultFeedURL = "https://medium.com/feed/bzedge-community"

// Count is how many articles are served: the feed's first ones.
const Count = 6

// DescriptionLimit is the length, in bytes as the aggregator cuts it, past
// which a description is cut and "..." appended.
const DescriptionLimit = 150

// userAgent is the one gofeed sends, which the aggregator's fetch uses.
const userAgent = "Gofeed/1.0"

// maxBody bounds a feed: Medium's is about 80 KB for ten articles.
const maxBody = 16 << 20

// imgSrc matches an <img> tag and captures its src, as the aggregator does.
var imgSrc = regexp.MustCompile(`<img[^>]+src="([^">]+)"`)

// Doer sends HTTP requests; *http.Client satisfies it.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Article is one feed item.
type Article struct {
	Title string
	URL   string
	// PictureURL is the first <img> of the content when its src holds
	// "https://cdn" (Medium's image CDN), else empty.
	PictureURL string
	// Description is the content without HTML, cut at DescriptionLimit.
	Description string
	PublishDate time.Time
	AuthorName  string
}

// Client reads a feed at a URL.
type Client struct {
	http   Doer
	url    string
	policy *bluemonday.Policy
}

// New returns a client over http for the feed at url.
func New(http Doer, url string) *Client {
	return &Client{http: http, url: url, policy: bluemonday.StrictPolicy()}
}

// Articles returns the feed's first Count items, in its order.
func (c *Client) Articles(ctx context.Context) ([]Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("medium feed: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("medium feed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("medium feed: HTTP %d", resp.StatusCode)
	}
	feed, err := gofeed.NewParser().Parse(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("medium feed: %w", err)
	}
	items := feed.Items
	if len(items) > Count {
		items = items[:Count]
	}
	out := make([]Article, 0, len(items))
	for _, item := range items {
		out = append(out, c.article(item))
	}
	return out, nil
}

func (c *Client) article(item *gofeed.Item) Article {
	content := item.Content
	if content == "" {
		content = item.Description
	}
	a := Article{
		Title:       item.Title,
		URL:         item.Link,
		PictureURL:  firstPicture(content),
		Description: c.describe(content),
	}
	// The aggregator panics on an item without a date; it gets the zero
	// time here.
	if item.PublishedParsed != nil {
		a.PublishDate = *item.PublishedParsed
	}
	if len(item.Authors) > 0 && item.Authors[0] != nil {
		a.AuthorName = item.Authors[0].Name
	}
	return a
}

// describe strips content's HTML and cuts it at DescriptionLimit bytes, as
// the aggregator does: a multi-byte character across the cut is split, and
// its bytes left become U+FFFD when the answer is encoded.
func (c *Client) describe(content string) string {
	stripped := c.policy.Sanitize(content)
	if len(stripped) > DescriptionLimit {
		return stripped[:DescriptionLimit] + "..."
	}
	return stripped
}

func firstPicture(content string) string {
	m := imgSrc.FindStringSubmatch(content)
	if len(m) > 1 && strings.Contains(m[1], "https://cdn") {
		return m[1]
	}
	return ""
}

// Source reads articles; *Client satisfies it.
type Source interface {
	Articles(ctx context.Context) ([]Article, error)
}

// Clock is the feed's time source; tests swap it.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// FreshFor is how long a read is served before the feed is asked again.
const FreshFor = 600 * time.Second

// BackupFor is how long after a read it is still served when the feed
// fails.
const BackupFor = 24 * time.Hour

// RetryAfter is how long after a failed read the feed is not asked again,
// so a feed that hangs does not hold every request for its timeout.
const RetryAfter = time.Minute

// FeedDeps are the feed's collaborators; Log defaults to the standard
// logger, Clock to the wall clock.
type FeedDeps struct {
	Source Source
	Log    log.FieldLogger
	Clock  Clock
}

// Feed serves the latest articles from memory.
type Feed struct {
	deps FeedDeps

	mu       sync.Mutex
	held     []Article
	readAt   time.Time
	failedAt time.Time
	failing  bool
}

// NewFeed returns a feed over deps.Source.
func NewFeed(deps FeedDeps) *Feed {
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}
	return &Feed{deps: deps}
}

// Articles returns the held read while younger than FreshFor, else a new
// one. While reads fail (asked again at most every RetryAfter) the held
// read is served while younger than BackupFor, and an empty list after
// that. The slice is never nil and shared: callers must not change it.
func (f *Feed) Articles(ctx context.Context) []Article {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.deps.Clock.Now()
	if f.held != nil && now.Sub(f.readAt) < FreshFor {
		return f.held
	}
	if !f.failing || now.Sub(f.failedAt) >= RetryAfter {
		got, err := f.deps.Source.Articles(ctx)
		f.note(err, now)
		if err == nil {
			if got == nil {
				got = []Article{}
			}
			f.held, f.readAt = got, now
			return got
		}
	}
	if f.held != nil && now.Sub(f.readAt) < BackupFor {
		return f.held
	}
	return []Article{}
}

// note logs a failure when one starts and the recovery when it ends, not
// every read.
func (f *Feed) note(err error, now time.Time) {
	switch {
	case err != nil:
		f.failedAt = now
		if !f.failing {
			f.failing = true
			f.deps.Log.WithError(err).Warn("medium: the feed fails, the last read is served for up to 24 hours")
		}
	case f.failing:
		f.failing = false
		f.deps.Log.Info("medium: the feed answers again")
	}
}
