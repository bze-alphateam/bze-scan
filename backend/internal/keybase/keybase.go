// Package keybase reads validator pictures from Keybase: a validator's
// description identity is the 16-hex-digit suffix of a Keybase key, and the
// user owning that key has a primary picture. This is how the BZE staking
// dapp shows validator logos.
package keybase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// DefaultURL is the Keybase user lookup endpoint.
const DefaultURL = "https://keybase.io/_/api/1.0/user/lookup.json"

// maxBody bounds a response: one user's pictures are a few hundred bytes.
const maxBody = 1 << 20

// Doer sends HTTP requests; *http.Client satisfies it.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client looks identities up at a lookup URL (DefaultURL).
type Client struct {
	http Doer
	url  string
}

// New returns a client over http for the lookup endpoint lookupURL.
func New(http Doer, lookupURL string) *Client {
	return &Client{http: http, url: lookupURL}
}

// PictureURL returns the primary picture of the Keybase user whose key ends
// in identity; "" when no user has that key or the user has no picture.
func (c *Client) PictureURL(ctx context.Context, identity string) (string, error) {
	q := url.Values{"key_suffix": {identity}, "fields": {"pictures"}}
	sep := "?"
	if strings.Contains(c.url, "?") {
		sep = "&"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+sep+q.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("keybase %s: %w", identity, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("keybase %s: %w", identity, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("keybase %s: HTTP %d", identity, resp.StatusCode)
	}
	var body struct {
		Status struct {
			Code int    `json:"code"`
			Name string `json:"name"`
		} `json:"status"`
		Them []struct {
			Pictures struct {
				Primary struct {
					URL string `json:"url"`
				} `json:"primary"`
			} `json:"pictures"`
		} `json:"them"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return "", fmt.Errorf("keybase %s: %w", identity, err)
	}
	if body.Status.Code != 0 {
		return "", fmt.Errorf("keybase %s: status %d %s", identity, body.Status.Code, body.Status.Name)
	}
	if len(body.Them) == 0 {
		return "", nil
	}
	return body.Them[0].Pictures.Primary.URL, nil
}
