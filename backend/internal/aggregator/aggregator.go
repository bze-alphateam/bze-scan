// Package aggregator reads the BZE aggregator API (the DEX aggregator behind
// the dapps, getbze.com and CoinGecko). The explorer computes no price of
// its own: GET /api/dex/tickers is the source of BZE's 24-hour change (the
// USD prices come from CoinGecko, see package coingecko).
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

// maxBody bounds a response: the tickers of every market are well under it.
const maxBody = 4 << 20

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

// Ticker is a DEX market's last 24 hours, prices in quote units per base
// unit as decimal strings.
type Ticker struct {
	MarketID  string
	Base      string
	Quote     string
	OpenPrice string
	LastPrice string
}

// Ticker returns the ticker of the market marketID (an order book's
// base/quote, a liquidity pool's base_quote); nil when the aggregator lists
// no such market.
func (c *Client) Ticker(ctx context.Context, marketID string) (*Ticker, error) {
	var list []struct {
		MarketID  string      `json:"market_id"`
		Base      string      `json:"base"`
		Quote     string      `json:"quote"`
		OpenPrice json.Number `json:"open_price"`
		LastPrice json.Number `json:"last_price"`
	}
	if err := c.get(ctx, "/api/dex/tickers", &list); err != nil {
		return nil, fmt.Errorf("aggregator tickers: %w", err)
	}
	for _, t := range list {
		if t.MarketID == marketID {
			return &Ticker{MarketID: t.MarketID, Base: t.Base, Quote: t.Quote,
				OpenPrice: t.OpenPrice.String(), LastPrice: t.LastPrice.String()}, nil
		}
	}
	return nil, nil
}

// get decodes the JSON answer of path into out, numbers as json.Number.
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxBody))
	dec.UseNumber()
	return dec.Decode(out)
}

// ChangePct is the 24-hour change of denom's price, in percent rounded half
// away from zero to four decimals, from the ticker of a market it trades in:
// last over open when denom is the base, open over last when it is the
// quote (the market prices the other side). ok is false when denom is not
// in the market or a price is not a positive number.
func ChangePct(t *Ticker, denom string) (pct string, ok bool) {
	if t == nil {
		return "", false
	}
	open, okOpen := new(big.Rat).SetString(t.OpenPrice)
	last, okLast := new(big.Rat).SetString(t.LastPrice)
	if !okOpen || !okLast || open.Sign() <= 0 || last.Sign() <= 0 {
		return "", false
	}
	var ratio *big.Rat
	switch denom {
	case t.Base:
		ratio = new(big.Rat).Quo(last, open)
	case t.Quote:
		ratio = new(big.Rat).Quo(open, last)
	default:
		return "", false
	}
	change := ratio.Sub(ratio, big.NewRat(1, 1))
	change.Mul(change, big.NewRat(100, 1))
	return change.FloatString(4), true
}
