package coingecko_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/coingecko"
)

// doer answers every request with status and body, or err, and keeps the
// last request.
type doer struct {
	status int
	body   string
	err    error
	last   *http.Request
}

func (d *doer) Do(req *http.Request) (*http.Response, error) {
	d.last = req
	if d.err != nil {
		return nil, d.err
	}
	return &http.Response{StatusCode: d.status, Body: io.NopCloser(strings.NewReader(d.body))}, nil
}

func TestClientPricesByID(t *testing.T) {
	d := &doer{status: http.StatusOK, body: `{"bzedge":{"usd":0.00016806,"eur":0.00015},"cosmos":{"usd":1.96},
		"tiny":{"usd":1e-9},"euro":{"eur":1}}`}
	got, err := coingecko.New(d, "https://api.coingecko.com/").Prices(context.Background(), []string{"bzedge", "cosmos", "tiny", "euro", "a b"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"bzedge": "0.00016806", "cosmos": "1.96", "tiny": "0.000000001"}, got,
		"USD only, as plain decimals")
	assert.Equal(t, "https://api.coingecko.com/api/v3/simple/price?ids=bzedge,cosmos,tiny,euro,a+b&vs_currencies=usd", d.last.URL.String())
	assert.Equal(t, "application/json", d.last.Header.Get("Accept"))
}

func TestClientFailures(t *testing.T) {
	for name, d := range map[string]*doer{
		"an error status": {status: http.StatusTooManyRequests, body: `{}`},
		"not json":        {status: http.StatusOK, body: `<html>`},
		"not a number":    {status: http.StatusOK, body: `{"bzedge":{"usd":"1"}}`},
		"unreachable":     {err: errors.New("connection refused")},
	} {
		_, err := coingecko.New(d, "https://api.coingecko.com").Prices(context.Background(), []string{"bzedge"})
		assert.Error(t, err, name)
	}
}

// source answers Prices with prices or err and records the ids asked.
type source struct {
	prices map[string]string
	err    error
	calls  int
	ids    []string
}

func (s *source) Prices(_ context.Context, ids []string) (map[string]string, error) {
	s.calls++
	s.ids = ids
	return s.prices, s.err
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

// inline runs a background refresh before the call that started it
// returns, so its outcome is visible to the next call.
func inline(run func()) { run() }

func newFeed(src *source, c *clock, lg logrus.FieldLogger, ids ...string) *coingecko.Feed {
	return coingecko.NewFeed(coingecko.FeedDeps{Source: src, Log: lg, Clock: c, Spawn: inline}, ids)
}

func TestFeedListsTheConfiguredIDsOnceInOrder(t *testing.T) {
	lg, _ := logtest.NewNullLogger()
	src := &source{prices: map[string]string{"cosmos": "1.96", "bzedge": "0.00016806", "unasked": "1"}}
	f := newFeed(src, &clock{now: time.Unix(1000, 0)}, lg, "bzedge", "", "osmosis", "cosmos", "bzedge")
	assert.Equal(t, []coingecko.Price{{ID: "bzedge", Price: 0.00016806}, {ID: "cosmos", Price: 1.96}}, f.Prices(context.Background()),
		"osmosis unpriced, unasked ids left out")
	assert.Equal(t, []string{"bzedge", "osmosis", "cosmos"}, src.ids)

	newFeed(src, &clock{}, lg).Prices(context.Background())
	assert.Equal(t, []string{coingecko.DefaultID}, src.ids, "no id reads BZE's")
}

func TestFeedCachesThenServesTheBackup(t *testing.T) {
	lg, hook := logtest.NewNullLogger()
	src := &source{prices: map[string]string{"bzedge": "0.0002"}}
	c := &clock{now: time.Unix(1000, 0)}
	f := newFeed(src, c, lg, "bzedge")
	ctx := context.Background()
	want := []coingecko.Price{{ID: "bzedge", Price: 0.0002}}

	assert.Equal(t, want, f.Prices(ctx))
	c.now = c.now.Add(coingecko.FreshFor - time.Nanosecond)
	src.prices = map[string]string{"bzedge": "0.0003"}
	assert.Equal(t, want, f.Prices(ctx))
	assert.Equal(t, 1, src.calls)

	// An answer pricing none of the ids is a failure, like an error.
	c.now = time.Unix(1000, 0).Add(coingecko.FreshFor)
	src.prices = map[string]string{}
	assert.Equal(t, want, f.Prices(ctx), "the backup")
	assert.Equal(t, 2, src.calls)

	// While failing, CoinGecko is asked at most every RetryAfter.
	src.err = errors.New("HTTP 429")
	c.now = c.now.Add(coingecko.RetryAfter - time.Nanosecond)
	assert.Equal(t, want, f.Prices(ctx))
	assert.Equal(t, 2, src.calls)
	c.now = c.now.Add(time.Nanosecond)
	assert.Equal(t, want, f.Prices(ctx))
	assert.Equal(t, 3, src.calls)

	c.now = time.Unix(1000, 0).Add(coingecko.BackupFor - time.Nanosecond)
	assert.Equal(t, want, f.Prices(ctx))
	usd, err := f.USD(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"bzedge": "0.0002"}, usd)

	c.now = time.Unix(1000, 0).Add(coingecko.BackupFor)
	assert.Nil(t, f.Prices(ctx))
	_, err = f.USD(ctx)
	require.Error(t, err)
	assert.Equal(t, 1, countLevel(hook, logrus.WarnLevel), "one warning per failure, not per read")

	// Nothing held: the read happens in the call, once RetryAfter has passed.
	src.err, src.prices = nil, map[string]string{"bzedge": "0.0003"}
	c.now = c.now.Add(coingecko.RetryAfter)
	assert.Equal(t, []coingecko.Price{{ID: "bzedge", Price: 0.0003}}, f.Prices(ctx))
	assert.Equal(t, logrus.InfoLevel, hook.LastEntry().Level)
}

func TestFeedRefreshesAStaleReadInTheBackground(t *testing.T) {
	lg, _ := logtest.NewNullLogger()
	src := &source{prices: map[string]string{"bzedge": "0.0002"}}
	c := &clock{now: time.Unix(1000, 0)}
	var spawned []func()
	f := coingecko.NewFeed(coingecko.FeedDeps{Source: src, Log: lg, Clock: c, Spawn: func(run func()) {
		spawned = append(spawned, run)
	}}, []string{"bzedge"})
	ctx := context.Background()
	old := []coingecko.Price{{ID: "bzedge", Price: 0.0002}}

	assert.Equal(t, old, f.Prices(ctx), "nothing held: read in the call")
	assert.Empty(t, spawned)

	// Stale: the held read is served at once, one refresh is started and
	// no second one while it runs.
	src.prices = map[string]string{"bzedge": "0.0003"}
	c.now = c.now.Add(coingecko.FreshFor)
	assert.Equal(t, old, f.Prices(ctx))
	assert.Equal(t, old, f.Prices(ctx))
	require.Len(t, spawned, 1)
	assert.Equal(t, 1, src.calls, "the request did not read CoinGecko")

	spawned[0]()
	assert.Equal(t, []coingecko.Price{{ID: "bzedge", Price: 0.0003}}, f.Prices(ctx))
	assert.Equal(t, 2, src.calls)
	assert.Len(t, spawned, 1, "the new read is fresh")
}

func TestFeedUSDLeavesNegativePricesOut(t *testing.T) {
	lg, _ := logtest.NewNullLogger()
	src := &source{prices: map[string]string{"bzedge": "0.00016806", "broken": "-1", "big": "123456789.5"}}
	usd, err := newFeed(src, &clock{}, lg, "bzedge", "broken", "big").USD(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"bzedge": "0.00016806", "big": "123456789.5"}, usd)
}

func TestFeedRejectsAPriceThatIsNotANumber(t *testing.T) {
	lg, _ := logtest.NewNullLogger()
	src := &source{prices: map[string]string{"bzedge": "x"}}
	assert.Nil(t, newFeed(src, &clock{}, lg, "bzedge").Prices(context.Background()))
}

func countLevel(hook *logtest.Hook, level logrus.Level) int {
	n := 0
	for _, e := range hook.AllEntries() {
		if e.Level == level {
			n++
		}
	}
	return n
}
