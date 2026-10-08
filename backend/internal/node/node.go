// Package node is the CometBFT RPC client of the backend. It calls the
// by-height routes only (/status, /block, /block_results, /commit), in the
// GET URI form, and never /tx, /tx_search or /block_search.
//
// Every call returns the parsed response and the raw response body: the raw
// bytes are what the raw-JSON cache serves later.
package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Timeout bounds every request to the node.
const Timeout = 10 * time.Second

// maxBody bounds a response body. Mainnet blocks are tens of kilobytes; a
// full block at the consensus limit stays well below this.
const maxBody = 64 << 20

// BlockIDFlagCommit is the block_id_flag of a signature that committed the
// block (CometBFT's BlockIDFlagCommit).
const BlockIDFlagCommit = 2

// Client calls one CometBFT RPC endpoint.
type Client struct {
	base string
	http *http.Client
}

// New returns a client for the RPC endpoint at baseURL (scheme, host and
// port, e.g. http://127.0.0.1:26657).
func New(baseURL string) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"),
		http: &http.Client{Timeout: Timeout},
	}
}

// Status is the part of /status the backend uses.
type Status struct {
	Network           string
	LatestBlockHeight int64
	LatestBlockTime   time.Time
}

// Block is the part of /block the backend uses.
type Block struct {
	Height          int64
	Time            time.Time
	Hash            string
	ProposerAddress string
	// Txs are the raw transactions, base64 as the node encodes them.
	Txs []string
	// Raw is the "block" object of the response as the node sent it.
	Raw json.RawMessage
}

// BlockResults is the part of /block_results the backend uses.
type BlockResults struct {
	Height              int64
	TxsResults          []TxResult
	FinalizeBlockEvents []Event
}

// TxResult is one entry of txs_results.
type TxResult struct {
	Code      uint32  `json:"code"`
	Codespace string  `json:"codespace"`
	Log       string  `json:"log"`
	GasWanted int64   `json:"gas_wanted,string"`
	GasUsed   int64   `json:"gas_used,string"`
	Events    []Event `json:"events"`
}

// Event is an ABCI event with string attributes (CometBFT 0.38).
type Event struct {
	Type       string      `json:"type"`
	Attributes []Attribute `json:"attributes"`
}

// Attribute is one key/value pair of an event.
type Attribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Get returns the value of the first attribute named key, and whether it
// exists.
func (e Event) Get(key string) (string, bool) {
	for _, a := range e.Attributes {
		if a.Key == key {
			return a.Value, true
		}
	}
	return "", false
}

// Commit is the part of /commit the backend uses.
type Commit struct {
	Height     int64
	Signatures []CommitSig
}

// CommitSig is one signature slot of a commit.
type CommitSig struct {
	BlockIDFlag      int    `json:"block_id_flag"`
	ValidatorAddress string `json:"validator_address"`
}

// Status calls /status.
func (c *Client) Status(ctx context.Context) (*Status, []byte, error) {
	var res struct {
		NodeInfo struct {
			Network string `json:"network"`
		} `json:"node_info"`
		SyncInfo struct {
			LatestBlockHeight int64     `json:"latest_block_height,string"`
			LatestBlockTime   time.Time `json:"latest_block_time"`
		} `json:"sync_info"`
	}
	raw, err := c.call(ctx, "status", 0, &res)
	if err != nil {
		return nil, nil, err
	}
	return &Status{
		Network:           res.NodeInfo.Network,
		LatestBlockHeight: res.SyncInfo.LatestBlockHeight,
		LatestBlockTime:   res.SyncInfo.LatestBlockTime,
	}, raw, nil
}

// Block calls /block?height=height.
func (c *Client) Block(ctx context.Context, height int64) (*Block, []byte, error) {
	var res struct {
		BlockID struct {
			Hash string `json:"hash"`
		} `json:"block_id"`
		Block json.RawMessage `json:"block"`
	}
	raw, err := c.call(ctx, "block", height, &res)
	if err != nil {
		return nil, nil, err
	}
	var b struct {
		Header struct {
			Height          int64     `json:"height,string"`
			Time            time.Time `json:"time"`
			ProposerAddress string    `json:"proposer_address"`
		} `json:"header"`
		Data struct {
			Txs []string `json:"txs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Block, &b); err != nil {
		return nil, nil, fmt.Errorf("block %d: decode block: %w", height, err)
	}
	if b.Header.Height != height {
		return nil, nil, fmt.Errorf("block %d: node answered height %d", height, b.Header.Height)
	}
	return &Block{
		Height:          b.Header.Height,
		Time:            b.Header.Time,
		Hash:            res.BlockID.Hash,
		ProposerAddress: b.Header.ProposerAddress,
		Txs:             b.Data.Txs,
		Raw:             res.Block,
	}, raw, nil
}

// BlockResults calls /block_results?height=height.
func (c *Client) BlockResults(ctx context.Context, height int64) (*BlockResults, []byte, error) {
	var res struct {
		Height              int64      `json:"height,string"`
		TxsResults          []TxResult `json:"txs_results"`
		FinalizeBlockEvents []Event    `json:"finalize_block_events"`
	}
	raw, err := c.call(ctx, "block_results", height, &res)
	if err != nil {
		return nil, nil, err
	}
	if res.Height != height {
		return nil, nil, fmt.Errorf("block_results %d: node answered height %d", height, res.Height)
	}
	return &BlockResults{
		Height:              res.Height,
		TxsResults:          res.TxsResults,
		FinalizeBlockEvents: res.FinalizeBlockEvents,
	}, raw, nil
}

// Commit calls /commit?height=height.
func (c *Client) Commit(ctx context.Context, height int64) (*Commit, []byte, error) {
	var res struct {
		SignedHeader struct {
			Commit struct {
				Height     int64       `json:"height,string"`
				Signatures []CommitSig `json:"signatures"`
			} `json:"commit"`
		} `json:"signed_header"`
	}
	raw, err := c.call(ctx, "commit", height, &res)
	if err != nil {
		return nil, nil, err
	}
	cm := res.SignedHeader.Commit
	if cm.Height != height {
		return nil, nil, fmt.Errorf("commit %d: node answered height %d", height, cm.Height)
	}
	return &Commit{Height: cm.Height, Signatures: cm.Signatures}, raw, nil
}

// call GETs route (with ?height= when height > 0), decodes the JSON-RPC
// envelope's result into out and returns the raw body.
func (c *Client) call(ctx context.Context, route string, height int64, out any) ([]byte, error) {
	u := c.base + "/" + route
	if height > 0 {
		u += "?" + url.Values{"height": {strconv.FormatInt(height, 10)}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", route, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("%s: read body: %w", route, err)
	}

	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    string `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("%s: HTTP %d, not a JSON-RPC response: %w", route, resp.StatusCode, err)
	}
	if env.Error != nil {
		return nil, newRPCError(route, height, env.Error.Code, env.Error.Message, env.Error.Data)
	}
	if resp.StatusCode != http.StatusOK || len(env.Result) == 0 {
		return nil, fmt.Errorf("%s: HTTP %d without a result", route, resp.StatusCode)
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return nil, fmt.Errorf("%s: decode result: %w", route, err)
	}
	return body, nil
}

// Kinds of JSON-RPC errors the backend tells apart.
var (
	// ErrAboveTip: the height is above the node's latest height.
	ErrAboveTip = errors.New("height above the node's tip")
	// ErrPruned: the node no longer has the height.
	ErrPruned = errors.New("height pruned by the node")
)

// RPCError is a JSON-RPC error envelope answered by the node. errors.Is
// matches ErrAboveTip or ErrPruned when the node's message says so.
type RPCError struct {
	Route   string
	Height  int64
	Code    int
	Message string
	Data    string
	kind    error
}

func newRPCError(route string, height int64, code int, message, data string) *RPCError {
	e := &RPCError{Route: route, Height: height, Code: code, Message: message, Data: data}
	switch {
	case strings.Contains(data, "must be less than or equal to the current blockchain height"):
		e.kind = ErrAboveTip
	case strings.Contains(data, "is not available, lowest height is"):
		e.kind = ErrPruned
	}
	return e
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("%s: rpc error %d: %s: %s", e.Route, e.Code, e.Message, e.Data)
}

// Is reports whether target is the kind of this error.
func (e *RPCError) Is(target error) bool {
	return e.kind != nil && target == e.kind
}
