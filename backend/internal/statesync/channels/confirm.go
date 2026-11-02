package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	connectiontypes "github.com/cosmos/ibc-go/v8/modules/core/03-connection/types"
	"google.golang.org/grpc"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainregistry"
)

// Ways a channel is confirmed, as explorer.ibc_channels.confirmed_via
// stores them.
const (
	ViaRegistry = "registry"
	ViaEndpoint = "endpoint"
)

// RetryInterval is the shortest time between two checks of an unconfirmed
// channel.
const RetryInterval = time.Hour

// EndpointTimeout bounds the two requests to one counterparty endpoint.
const EndpointTimeout = 10 * time.Second

// MaxEndpoints is how many of the counterparty's REST endpoints a check
// tries, in the registry's order.
const MaxEndpoints = 3

// maxBody bounds one counterparty response.
const maxBody = 1 << 20

// Connection is the part of the IBC connection query client the set uses;
// connectiontypes.QueryClient satisfies it.
type Connection interface {
	Connection(ctx context.Context, in *connectiontypes.QueryConnectionRequest, opts ...grpc.CallOption) (*connectiontypes.QueryConnectionResponse, error)
}

// Registry reads the chain registry's IBC connection files;
// *chainregistry.Client satisfies it.
type Registry interface {
	IBC(ctx context.Context, a, b string, testnet bool) (*chainregistry.IBCFile, error)
}

// Doer sends HTTP requests to the counterparties' REST endpoints;
// *http.Client satisfies it.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Clock is the set's time source; tests swap it.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Confirmation is whether a channel's counterparty was confirmed to be the
// chain its light client claims.
type Confirmation struct {
	// Via is ViaRegistry or ViaEndpoint; empty while unconfirmed.
	Via string
	// At is when it was confirmed; zero while unconfirmed.
	At time.Time
	// CheckedAt is the last check; zero when never checked.
	CheckedAt time.Time
	// Note is the last failed check's reason.
	Note string
}

// Confirmed is whether the channel is confirmed.
func (c Confirmation) Confirmed() bool { return c.Via != "" }

// Stored is what the store holds of a channel: the identity a confirmation
// is bound to, and the confirmation.
type Stored struct {
	ClientID              string
	ConnectionID          string
	CounterpartyChainID   string
	CounterpartyChannelID string
	Confirmation          Confirmation
}

// ChainRef is a chain of the registry cache, as a check needs it.
type ChainRef struct {
	ChainID string
	// Name is the registry directory name (chain_name).
	Name        string
	NetworkType string
	REST        []string
}

// confirmer checks the channels of one run; it reads each IBC connection
// file once per run.
type confirmer struct {
	s     *Set
	bze   ChainRef
	files map[string]*chainregistry.IBCFile
	errs  map[string]error
}

// check runs the confirmation rules for row, whose counterparty is cp: the
// registry's IBC connection file first, then the counterparty's endpoints.
// It returns how the channel was confirmed, or empty and why not.
func (c *confirmer) check(ctx context.Context, row Row, cp ChainRef) (via, note string) {
	var notes []string
	if ok, n := c.registry(ctx, row, cp); ok {
		return ViaRegistry, ""
	} else if n != "" {
		notes = append(notes, n)
	}
	ok, n := c.endpoints(ctx, row, cp)
	if ok {
		return ViaEndpoint, ""
	}
	notes = append(notes, n)
	return "", strings.Join(notes, "; ")
}

// registry looks the channel up in the IBC connection file of BZE and cp:
// BZE's end must be row's channel on the transfer port and the far end's
// chain cp. note is set only for a failed read.
func (c *confirmer) registry(ctx context.Context, row Row, cp ChainRef) (ok bool, note string) {
	if c.s.deps.Registry == nil || c.bze.Name == "" || cp.Name == "" {
		return false, ""
	}
	testnet := c.bze.NetworkType == "testnet"
	path := chainregistry.IBCPath(c.bze.Name, cp.Name, testnet)
	f, read := c.files[path]
	err := c.errs[path]
	if !read && err == nil {
		f, err = c.s.deps.Registry.IBC(ctx, c.bze.Name, cp.Name, testnet)
		c.files[path], c.errs[path] = f, err
	}
	if err != nil {
		return false, fmt.Sprintf("registry: %v", err)
	}
	if f == nil {
		return false, ""
	}
	_, other, found := f.Channel(c.bze.Name, chain.TransferPort, row.ChannelID)
	return found && other == cp.Name, ""
}

// endpoints asks cp's REST endpoints about the channel's far end. The
// first endpoint that answers decides: it confirms when its channel's
// counterparty is row's channel on the transfer port, its connection is
// the counterparty of row's connection on BZE, and that connection's
// client tracks BZE's chain id.
func (c *confirmer) endpoints(ctx context.Context, row Row, cp ChainRef) (ok bool, note string) {
	switch {
	case c.s.deps.HTTP == nil || c.s.deps.Connection == nil:
		return false, "endpoints: not configured"
	case row.CounterpartyChannelID == "":
		return false, "endpoints: no counterparty channel"
	case row.ConnectionID == "":
		return false, "endpoints: no connection"
	case len(cp.REST) == 0:
		return false, "endpoints: the registry lists no REST endpoint"
	}
	res, err := c.s.deps.Connection.Connection(ctx, &connectiontypes.QueryConnectionRequest{ConnectionId: row.ConnectionID})
	if err != nil {
		return false, fmt.Sprintf("endpoints: connection %s: %v", row.ConnectionID, err)
	}
	if res.Connection == nil || res.Connection.Counterparty.ConnectionId == "" {
		return false, fmt.Sprintf("endpoints: connection %s has no counterparty connection", row.ConnectionID)
	}
	ourCounterparty := res.Connection.Counterparty.ConnectionId

	var lastErr error
	for i, rest := range cp.REST {
		if i == MaxEndpoints {
			break
		}
		disagree, err := c.endpoint(ctx, strings.TrimRight(rest, "/"), row, ourCounterparty)
		if err != nil {
			lastErr = err
			continue
		}
		if disagree != "" {
			return false, "endpoint " + rest + ": " + disagree
		}
		return true, ""
	}
	return false, fmt.Sprintf("endpoints: none answered: %v", lastErr)
}

// endpoint asks one REST endpoint; err means it did not answer, disagree
// what it answered that does not match.
func (c *confirmer) endpoint(ctx context.Context, rest string, row Row, ourCounterparty string) (disagree string, err error) {
	ctx, cancel := context.WithTimeout(ctx, EndpointTimeout)
	defer cancel()

	var ch struct {
		Channel struct {
			Counterparty struct {
				PortID    string `json:"port_id"`
				ChannelID string `json:"channel_id"`
			} `json:"counterparty"`
			ConnectionHops []string `json:"connection_hops"`
		} `json:"channel"`
	}
	if err := c.get(ctx, rest+"/ibc/core/channel/v1/channels/"+row.CounterpartyChannelID+"/ports/"+chain.TransferPort, &ch); err != nil {
		return "", err
	}
	if got := ch.Channel.Counterparty; got.ChannelID != row.ChannelID || got.PortID != chain.TransferPort {
		return fmt.Sprintf("channel %s leads to %s/%s, not %s/%s", row.CounterpartyChannelID, got.PortID, got.ChannelID,
			chain.TransferPort, row.ChannelID), nil
	}
	if len(ch.Channel.ConnectionHops) == 0 {
		return fmt.Sprintf("channel %s has no connection", row.CounterpartyChannelID), nil
	}
	theirs := ch.Channel.ConnectionHops[0]
	if theirs != ourCounterparty {
		return fmt.Sprintf("channel %s is on connection %s, not %s", row.CounterpartyChannelID, theirs, ourCounterparty), nil
	}

	var cs struct {
		IdentifiedClientState struct {
			ClientState struct {
				Type    string `json:"@type"`
				ChainID string `json:"chain_id"`
			} `json:"client_state"`
		} `json:"identified_client_state"`
	}
	if err := c.get(ctx, rest+"/ibc/core/connection/v1/connections/"+theirs+"/client_state", &cs); err != nil {
		return "", err
	}
	state := cs.IdentifiedClientState.ClientState
	if state.Type != tendermintClientState || state.ChainID != c.s.deps.ChainID {
		return fmt.Sprintf("connection %s's client tracks %q (%s), not %q", theirs, state.ChainID, state.Type, c.s.deps.ChainID), nil
	}
	return "", nil
}

// get reads url's JSON into v; any answer but a 200 with JSON is an error.
func (c *confirmer) get(ctx context.Context, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.s.deps.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	return nil
}
