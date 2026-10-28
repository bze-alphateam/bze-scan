// Package coingecko reads USD prices from CoinGecko's simple price API, the
// way the BZE aggregator does: Client asks for a list of CoinGecko ids, Feed
// keeps the answer for FreshFor and serves it for BackupFor more when
// CoinGecko fails. Feed backs GET /api/prices of the aggregator-compatible
// routes and the prices set of the state sync.
package coingecko

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// DefaultHost is CoinGecko's public API.
const DefaultHost = "https://api.coingecko.com"

// DefaultID is the CoinGecko id read when none is configured: BZE's.
const DefaultID = "bzedge"

// USD is the currency every price is read in.
const USD = "usd"

// maxBody bounds an answer: a price per id is a few bytes.
const maxBody = 1 << 20

// Doer sends HTTP requests; *http.Client satisfies it.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client reads CoinGecko at a host (https://api.coingecko.com).
type Client struct {
	http Doer
	host string
}

// New returns a client over http for CoinGecko at host.
func New(http Doer, host string) *Client {
	return &Client{http: http, host: strings.TrimRight(host, "/")}
}

// Prices returns the USD price of each of ids CoinGecko knows, as the
// shortest decimal string of the float64 it decodes to (the number the
// aggregator serves). Ids CoinGecko leaves out, or answers without a USD
// price, are missing from the map.
func (c *Client) Prices(ctx context.Context, ids []string) (map[string]string, error) {
	escaped := make([]string, len(ids))
	for i, id := range ids {
		escaped[i] = url.QueryEscape(id)
	}
	u := c.host + "/api/v3/simple/price?ids=" + strings.Join(escaped, ",") + "&vs_currencies=" + USD
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("coingecko prices: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("coingecko prices: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coingecko prices: HTTP %d", resp.StatusCode)
	}
	// Decoded as the aggregator decodes it, so a price is the same float64.
	var data map[string]map[string]float64
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&data); err != nil {
		return nil, fmt.Errorf("coingecko prices: %w", err)
	}
	out := map[string]string{}
	for id, byCurrency := range data {
		if p, ok := byCurrency[USD]; ok {
			out[id] = strconv.FormatFloat(p, 'f', -1, 64)
		}
	}
	return out, nil
}

// Source reads prices; *Client satisfies it.
type Source interface {
	Prices(ctx context.Context, ids []string) (map[string]string, error)
}

// Clock is the feed's time source; tests swap it.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// FreshFor is how long a read is served before CoinGecko is asked again.
const FreshFor = 180 * time.Second

// BackupFor is how long after a read it is still served when CoinGecko
// fails.
const BackupFor = 24 * time.Hour

// Price is one USD price, by CoinGecko id.
type Price struct {
	ID    string
	Price float64
}

// FeedDeps are the feed's collaborators; Log defaults to the standard
// logger, Clock to the wall clock.
type FeedDeps struct {
	Source Source
	Log    log.FieldLogger
	Clock  Clock
}

// Feed serves the prices of a fixed list of ids from memory.
type Feed struct {
	deps FeedDeps
	ids  []string

	mu      sync.Mutex
	held    []Price
	readAt  time.Time
	failing bool
}

// NewFeed returns a feed of ids, in that order and each once; none reads
// DefaultID.
func NewFeed(deps FeedDeps, ids []string) *Feed {
	var unique []string
	for _, id := range ids {
		if id != "" && !slices.Contains(unique, id) {
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		unique = []string{DefaultID}
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}
	return &Feed{deps: deps, ids: unique}
}

// Prices returns the prices of the feed's ids CoinGecko knows, in the
// configured order: the held read while younger than FreshFor, else a new
// one. When the new read fails or prices nothing, the held read is served
// while younger than BackupFor, and nil after that. The slice is shared:
// callers must not change it.
func (f *Feed) Prices(ctx context.Context) []Price {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.deps.Clock.Now()
	if f.held != nil && now.Sub(f.readAt) < FreshFor {
		return f.held
	}
	got, err := f.read(ctx)
	f.note(err)
	if err == nil {
		f.held, f.readAt = got, now
		return got
	}
	if f.held != nil && now.Sub(f.readAt) < BackupFor {
		return f.held
	}
	return nil
}

// USD returns Prices by id as decimal strings, without negative ones; an
// error when there is none to serve.
func (f *Feed) USD(ctx context.Context) (map[string]string, error) {
	list := f.Prices(ctx)
	if list == nil {
		return nil, errNoPrices
	}
	out := make(map[string]string, len(list))
	for _, p := range list {
		if p.Price >= 0 {
			out[p.ID] = strconv.FormatFloat(p.Price, 'f', -1, 64)
		}
	}
	return out, nil
}

var errNoPrices = errors.New("coingecko prices: none read in the last 24 hours")

// read asks the source for the feed's ids; an answer pricing none of them
// is an error, as the aggregator treats it.
func (f *Feed) read(ctx context.Context) ([]Price, error) {
	byID, err := f.deps.Source.Prices(ctx, f.ids)
	if err != nil {
		return nil, err
	}
	var out []Price
	for _, id := range f.ids {
		s, ok := byID[id]
		if !ok {
			continue
		}
		p, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("coingecko prices: %s: %w", id, err)
		}
		out = append(out, Price{ID: id, Price: p})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("coingecko prices: none of %s priced", strings.Join(f.ids, ","))
	}
	return out, nil
}

// note logs a failure when one starts and the recovery when it ends, not
// every read.
func (f *Feed) note(err error) {
	switch {
	case err != nil && !f.failing:
		f.failing = true
		f.deps.Log.WithError(err).Warn("coingecko: prices fail, the last read is served for up to 24 hours")
	case err == nil && f.failing:
		f.failing = false
		f.deps.Log.Info("coingecko: prices answer again")
	}
}
