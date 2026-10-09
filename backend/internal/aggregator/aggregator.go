// Package aggregator reads the BZE aggregator API (the DEX aggregator behind
// the dapps, getbze.com and CoinGecko). The explorer computes no price of
// its own: GET /api/prices is the source.
package aggregator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
)

// USD is the price_denom of a US dollar price.
const USD = "usd"

// maxBody bounds the prices response.
const maxBody = 1 << 20

// Doer sends HTTP requests; *http.Client satisfies it.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client reads the aggregator at a base URL (https://getbze.com).
type Client struct {
	http Doer
	base string
}

// New returns a client over http for the aggregator at baseURL.
func New(http Doer, baseURL string) *Client {
	return &Client{http: http, base: strings.TrimRight(baseURL, "/")}
}

// Prices returns the USD prices the aggregator publishes, by CoinGecko id
// (bzedge, cosmos, osmosis…), as decimal strings. Other currencies and
// entries that are not a non-negative number are left out.
func (c *Client) Prices(ctx context.Context) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/prices", nil)
	if err != nil {
		return nil, fmt.Errorf("aggregator prices: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aggregator prices: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aggregator prices: HTTP %d", resp.StatusCode)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxBody))
	dec.UseNumber()
	var list []struct {
		Denom      string      `json:"denom"`
		Price      json.Number `json:"price"`
		PriceDenom string      `json:"price_denom"`
	}
	if err := dec.Decode(&list); err != nil {
		return nil, fmt.Errorf("aggregator prices: %w", err)
	}
	out := map[string]string{}
	for _, p := range list {
		if p.Denom == "" || !strings.EqualFold(p.PriceDenom, USD) {
			continue
		}
		r, ok := new(big.Rat).SetString(p.Price.String())
		if !ok || r.Sign() < 0 {
			continue
		}
		out[p.Denom] = r.FloatString(12)
	}
	return out, nil
}
