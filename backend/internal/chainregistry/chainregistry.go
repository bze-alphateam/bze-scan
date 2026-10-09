// Package chainregistry reads the Cosmos chain registry
// (github.com/cosmos/chain-registry) at runtime: the GitHub contents API
// lists the chain directories, raw.githubusercontent.com (no API rate
// limit) serves each chain's chain.json and assetlist.json. The explorer
// keeps no configuration file of chain names: the registry is the source.
package chainregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Defaults of the two base URLs.
const (
	DefaultAPIURL = "https://api.github.com/repos/cosmos/chain-registry/contents"
	DefaultRawURL = "https://raw.githubusercontent.com/cosmos/chain-registry/master"
)

// TestnetsDir holds the testnets' directories, below the mainnets'.
const TestnetsDir = "testnets"

// maxBody bounds one response: the largest asset list is well under 1 MB.
const maxBody = 8 << 20

// ErrNotFound is returned for a directory or a file the registry does not
// have.
var ErrNotFound = errors.New("chain registry: not found")

// Doer sends HTTP requests; *http.Client satisfies it.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client reads the registry.
type Client struct {
	http Doer
	api  string
	raw  string
}

// New returns a client over http, with the contents API at apiURL and the
// raw files at rawURL (DefaultAPIURL and DefaultRawURL in production).
func New(http Doer, apiURL, rawURL string) *Client {
	return &Client{http: http, api: strings.TrimRight(apiURL, "/"), raw: strings.TrimRight(rawURL, "/")}
}

// Chain is the part of a chain.json the explorer keeps.
type Chain struct {
	ChainID      string
	Name         string
	PrettyName   string
	NetworkType  string
	Bech32Prefix string
	LogoURL      string
	// ExplorerTxURL and ExplorerAccountURL are the first explorer's
	// templates (${txHash}, ${accountAddress}); empty without explorers.
	ExplorerTxURL      string
	ExplorerAccountURL string
}

// Asset is the part of an assetlist.json asset the explorer keeps.
type Asset struct {
	Base    string
	Symbol  string
	Name    string
	Display string
	// Exponent is the display unit's; nil when the display unit is not
	// among the denom units.
	Exponent    *int
	LogoURL     string
	CoingeckoID string
	// Traces is the registry's trace list as it is; nil without.
	Traces json.RawMessage
}

// Dirs lists the chain directories: the mainnets' by name, the testnets'
// as testnets/<name>. Directories starting with "_" or "." (the IBC data,
// the scripts) are not chains.
func (c *Client) Dirs(ctx context.Context) ([]string, error) {
	root, err := c.list(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(root))
	hasTestnets := false
	for _, d := range root {
		if d == TestnetsDir {
			hasTestnets = true
			continue
		}
		out = append(out, d)
	}
	if !hasTestnets {
		return out, nil
	}
	testnets, err := c.list(ctx, TestnetsDir)
	if err != nil {
		return nil, err
	}
	for _, d := range testnets {
		out = append(out, TestnetsDir+"/"+d)
	}
	return out, nil
}

// list returns the chain directories directly under dir.
func (c *Client) list(ctx context.Context, dir string) ([]string, error) {
	url := c.api + "/"
	if dir != "" {
		url += dir
	}
	body, err := c.get(ctx, url, "application/vnd.github+json")
	if err != nil {
		return nil, fmt.Errorf("chain registry listing %q: %w", dir, err)
	}
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("chain registry listing %q: %w", dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Type == "dir" && e.Name != "" && !strings.HasPrefix(e.Name, "_") && !strings.HasPrefix(e.Name, ".") {
			out = append(out, e.Name)
		}
	}
	return out, nil
}

// Chain reads dir's chain.json; ErrNotFound when it has none.
func (c *Client) Chain(ctx context.Context, dir string) (*Chain, error) {
	body, err := c.get(ctx, c.raw+"/"+dir+"/chain.json", "")
	if err != nil {
		return nil, fmt.Errorf("chain registry %s/chain.json: %w", dir, err)
	}
	ch, err := ParseChain(body)
	if err != nil {
		return nil, fmt.Errorf("chain registry %s/chain.json: %w", dir, err)
	}
	return ch, nil
}

// Assets reads dir's assetlist.json; none (and no error) when the chain has
// no asset list.
func (c *Client) Assets(ctx context.Context, dir string) ([]Asset, error) {
	body, err := c.get(ctx, c.raw+"/"+dir+"/assetlist.json", "")
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("chain registry %s/assetlist.json: %w", dir, err)
	}
	assets, err := ParseAssets(body)
	if err != nil {
		return nil, fmt.Errorf("chain registry %s/assetlist.json: %w", dir, err)
	}
	return assets, nil
}

func (c *Client) get(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// GitHub refuses requests without a User-Agent.
	req.Header.Set("User-Agent", "bze-scan")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}

type logoURIs struct {
	PNG string `json:"png"`
	SVG string `json:"svg"`
}

// url is the PNG, else the SVG.
func (l *logoURIs) url() string {
	if l == nil {
		return ""
	}
	if l.PNG != "" {
		return l.PNG
	}
	return l.SVG
}

// ParseChain parses a chain.json.
func ParseChain(raw []byte) (*Chain, error) {
	var c struct {
		ChainID      string     `json:"chain_id"`
		ChainName    string     `json:"chain_name"`
		PrettyName   string     `json:"pretty_name"`
		NetworkType  string     `json:"network_type"`
		Bech32Prefix string     `json:"bech32_prefix"`
		LogoURIs     *logoURIs  `json:"logo_URIs"`
		Images       []logoURIs `json:"images"`
		Explorers    []struct {
			TxPage      string `json:"tx_page"`
			AccountPage string `json:"account_page"`
		} `json:"explorers"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	if c.ChainID == "" || c.ChainName == "" {
		return nil, errors.New("no chain_id or chain_name")
	}
	out := &Chain{
		ChainID: c.ChainID, Name: c.ChainName, PrettyName: c.PrettyName, NetworkType: c.NetworkType,
		Bech32Prefix: c.Bech32Prefix, LogoURL: c.LogoURIs.url(),
	}
	if out.LogoURL == "" && len(c.Images) > 0 {
		out.LogoURL = c.Images[0].url()
	}
	if len(c.Explorers) > 0 {
		out.ExplorerTxURL, out.ExplorerAccountURL = c.Explorers[0].TxPage, c.Explorers[0].AccountPage
	}
	return out, nil
}

// ParseAssets parses an assetlist.json. Assets without a base are skipped.
func ParseAssets(raw []byte) ([]Asset, error) {
	var list struct {
		Assets []struct {
			Base       string `json:"base"`
			Symbol     string `json:"symbol"`
			Name       string `json:"name"`
			Display    string `json:"display"`
			DenomUnits []struct {
				Denom    string   `json:"denom"`
				Exponent int      `json:"exponent"`
				Aliases  []string `json:"aliases"`
			} `json:"denom_units"`
			LogoURIs    *logoURIs       `json:"logo_URIs"`
			Images      []logoURIs      `json:"images"`
			CoingeckoID string          `json:"coingecko_id"`
			Traces      json.RawMessage `json:"traces"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]Asset, 0, len(list.Assets))
	for _, a := range list.Assets {
		if a.Base == "" {
			continue
		}
		asset := Asset{
			Base: a.Base, Symbol: a.Symbol, Name: a.Name, Display: a.Display,
			LogoURL: a.LogoURIs.url(), CoingeckoID: a.CoingeckoID,
		}
		if asset.LogoURL == "" && len(a.Images) > 0 {
			asset.LogoURL = a.Images[0].url()
		}
		for _, u := range a.DenomUnits {
			if u.Denom == a.Display {
				e := u.Exponent
				asset.Exponent = &e
				break
			}
		}
		if len(a.Traces) > 0 && string(a.Traces) != "null" {
			asset.Traces = a.Traces
		}
		out = append(out, asset)
	}
	return out, nil
}

// ibcTraces are the trace types that cross an IBC channel.
var ibcTraces = map[string]bool{"ibc": true, "ibc-cw20": true, "ibc-bridge": true}

// Home is where an asset known through IBC comes from: its earliest IBC
// trace's counterparty, the chain (by registry name) and the base denom
// there. ok is false when traces has no IBC trace.
func Home(traces json.RawMessage) (chainName, base string, ok bool) {
	if len(traces) == 0 {
		return "", "", false
	}
	var list []struct {
		Type         string `json:"type"`
		Counterparty struct {
			ChainName string `json:"chain_name"`
			BaseDenom string `json:"base_denom"`
		} `json:"counterparty"`
	}
	if json.Unmarshal(traces, &list) != nil {
		return "", "", false
	}
	for _, t := range list {
		if ibcTraces[t.Type] && t.Counterparty.ChainName != "" && t.Counterparty.BaseDenom != "" {
			return t.Counterparty.ChainName, t.Counterparty.BaseDenom, true
		}
	}
	return "", "", false
}
