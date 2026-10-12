package node

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"

	gogoproto "github.com/cosmos/gogoproto/proto"
)

// ABCIError is a query the application answered with a non-zero code.
type ABCIError struct {
	Path      string
	Height    int64
	Code      uint32
	Codespace string
	Log       string
}

func (e *ABCIError) Error() string {
	return fmt.Sprintf("abci_query %s at %d: code %d (%s): %s", e.Path, e.Height, e.Code, e.Codespace, e.Log)
}

// ABCIQuery calls /abci_query with path (/<service>/<Method>), the encoded
// request data and height (the state after that block), and returns the
// answer's value. A non-zero code is an *ABCIError.
func (c *Client) ABCIQuery(ctx context.Context, path string, data []byte, height int64) ([]byte, error) {
	if height <= 0 {
		return nil, fmt.Errorf("abci_query %s: height must be positive (got %d)", path, height)
	}
	var res struct {
		Response struct {
			Code      uint32 `json:"code"`
			Log       string `json:"log"`
			Codespace string `json:"codespace"`
			Value     []byte `json:"value"`
			Height    int64  `json:"height,string"`
		} `json:"response"`
	}
	q := url.Values{
		"path":   {strconv.Quote(path)},
		"data":   {"0x" + hex.EncodeToString(data)},
		"height": {strconv.FormatInt(height, 10)},
	}
	if _, err := c.callQuery(ctx, "abci_query", height, q, &res); err != nil {
		return nil, err
	}
	r := res.Response
	if r.Code != 0 {
		return nil, &ABCIError{Path: path, Height: height, Code: r.Code, Codespace: r.Codespace, Log: r.Log}
	}
	if r.Height != height {
		return nil, fmt.Errorf("abci_query %s at %d: node answered height %d", path, height, r.Height)
	}
	return r.Value, nil
}

// Query calls the gRPC method path (/<service>/<Method>) through
// /abci_query at height: req is marshalled as the data, the value is
// unmarshalled into resp.
func (c *Client) Query(ctx context.Context, path string, req, resp gogoproto.Message, height int64) error {
	data, err := gogoproto.Marshal(req)
	if err != nil {
		return fmt.Errorf("abci_query %s: encode request: %w", path, err)
	}
	value, err := c.ABCIQuery(ctx, path, data, height)
	if err != nil {
		return err
	}
	if err := gogoproto.Unmarshal(value, resp); err != nil {
		return fmt.Errorf("abci_query %s at %d: decode response: %w", path, height, err)
	}
	return nil
}
