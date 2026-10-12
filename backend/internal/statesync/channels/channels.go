// Package channels is the channels set of the state sync: it rewrites
// explorer.ibc_channels from the local node's IBC channel queries (the
// transfer channels, the chain id of each one's light client and whether
// that client is still active or expired), names
// each channel's ICS-20 escrow account in explorer.labels, and fills the
// counterparty chain of the cross-chain transfers stored before their
// channel was known. A counterparty chain absent from the registry cache is
// published to the chain_registry set.
package channels

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cosmos/gogoproto/proto"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	ibctm "github.com/cosmos/ibc-go/v8/modules/light-clients/07-tendermint"
	"google.golang.org/grpc"

	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultInterval is the hourly safety net; channel events mark the set
// dirty in between.
const DefaultInterval = time.Hour

// PageLimit is the page size of the channels query.
const PageLimit = 200

// tendermintClientState is the type URL of the light client whose state
// names the counterparty chain id.
var tendermintClientState = "/" + proto.MessageName(&ibctm.ClientState{})

// Channel is the part of the IBC channel query client the set uses;
// channeltypes.QueryClient satisfies it.
type Channel interface {
	Channels(ctx context.Context, in *channeltypes.QueryChannelsRequest, opts ...grpc.CallOption) (*channeltypes.QueryChannelsResponse, error)
	ChannelClientState(ctx context.Context, in *channeltypes.QueryChannelClientStateRequest, opts ...grpc.CallOption) (*channeltypes.QueryChannelClientStateResponse, error)
}

// Client is the part of the IBC client query client the set uses;
// clienttypes.QueryClient satisfies it.
type Client interface {
	ClientStatus(ctx context.Context, in *clienttypes.QueryClientStatusRequest, opts ...grpc.CallOption) (*clienttypes.QueryClientStatusResponse, error)
}

// Row is one explorer.ibc_channels row; empty strings are stored as NULL.
type Row struct {
	ChannelID             string
	PortID                string
	ClientID              string
	ConnectionID          string
	CounterpartyChainID   string
	CounterpartyChannelID string
	// State is the channel state without its STATE_ prefix, lower case:
	// open, closed, init, tryopen…
	State string
	// ClientStatus is the light client's status, lower case: active,
	// expired, frozen…; empty when unknown (the stored one is kept).
	ClientStatus string
	// Escrow is the channel's ICS-20 escrow account.
	Escrow string
}

// Store persists the set.
type Store interface {
	// Save upserts the channels (keeping a stored counterparty chain the
	// node did not answer), names their escrow accounts after the
	// counterparty chain, and fills the counterparty chain of the stored
	// transfers of each channel where it is missing, in one transaction.
	Save(ctx context.Context, rows []Row) error
	// UnknownChains returns the chain ids of ids absent from the registry
	// cache (explorer.chains).
	UnknownChains(ctx context.Context, ids []string) ([]string, error)
}

// Deps of the set.
type Deps struct {
	Channel Channel
	Client  Client
	Store   Store
	// Misses receives the counterparty chains the registry cache lacks.
	Misses statesync.Publisher
}

// Set is the channels set.
type Set struct {
	deps     Deps
	interval time.Duration
}

// New returns the set; interval zero is DefaultInterval.
func New(deps Deps, interval time.Duration) *Set {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Set{deps: deps, interval: interval}
}

var _ statesync.Set = (*Set)(nil)

// Name is statesync.Channels.
func (s *Set) Name() string { return statesync.Channels }

// Interval is the hourly safety net.
func (s *Set) Interval() time.Duration { return s.interval }

// ResyncOne resyncs every channel: a block marks the set as a whole.
func (s *Set) ResyncOne(ctx context.Context, _ string) error { return s.FullResync(ctx) }

// FullResync rewrites every transfer channel. A channel whose client state
// or client status fails is saved without its counterparty chain or client
// status (the stored ones are kept); the failures are returned joined. The
// hourly run is what notices a client expiring: no event marks it.
func (s *Set) FullResync(ctx context.Context) error {
	var channels []*channeltypes.IdentifiedChannel
	req := &channeltypes.QueryChannelsRequest{Pagination: &query.PageRequest{Limit: PageLimit}}
	for {
		res, err := s.deps.Channel.Channels(ctx, req)
		if err != nil {
			return fmt.Errorf("channels: %w", err)
		}
		channels = append(channels, res.Channels...)
		if res.Pagination == nil || len(res.Pagination.NextKey) == 0 {
			break
		}
		req.Pagination = &query.PageRequest{Key: res.Pagination.NextKey, Limit: PageLimit}
	}

	var errs []error
	rows := make([]Row, 0, len(channels))
	chains := map[string]bool{}
	for _, c := range channels {
		if c.PortId != chain.TransferPort {
			continue
		}
		row := Row{
			ChannelID: c.ChannelId, PortID: c.PortId, CounterpartyChannelID: c.Counterparty.ChannelId,
			State:  strings.ToLower(strings.TrimPrefix(c.State.String(), "STATE_")),
			Escrow: chain.EscrowAddress(c.PortId, c.ChannelId),
		}
		if len(c.ConnectionHops) > 0 {
			row.ConnectionID = c.ConnectionHops[0]
		}
		clientID, chainID, err := s.client(ctx, c.PortId, c.ChannelId)
		if err != nil {
			errs = append(errs, err)
		}
		row.ClientID, row.CounterpartyChainID = clientID, chainID
		if clientID != "" {
			status, err := s.status(ctx, clientID)
			if err != nil {
				errs = append(errs, err)
			}
			row.ClientStatus = status
		}
		if chainID != "" {
			chains[chainID] = true
		}
		rows = append(rows, row)
	}
	if err := s.deps.Store.Save(ctx, rows); err != nil {
		return errors.Join(append(errs, err)...)
	}

	ids := make([]string, 0, len(chains))
	for id := range chains {
		ids = append(ids, id)
	}
	unknown, err := s.deps.Store.UnknownChains(ctx, ids)
	if err != nil {
		errs = append(errs, err)
	}
	var misses statesync.Dirty
	for _, id := range unknown {
		misses.Mark(statesync.ChainRegistry, id)
	}
	if !misses.Empty() && s.deps.Misses != nil {
		s.deps.Misses.Publish(misses)
	}
	return errors.Join(errs...)
}

// client returns the channel's light client and, for a Tendermint client,
// the counterparty chain id.
func (s *Set) client(ctx context.Context, port, channel string) (clientID, chainID string, err error) {
	res, err := s.deps.Channel.ChannelClientState(ctx, &channeltypes.QueryChannelClientStateRequest{PortId: port, ChannelId: channel})
	if err != nil {
		return "", "", fmt.Errorf("channel %s client state: %w", channel, err)
	}
	cs := res.IdentifiedClientState
	if cs == nil {
		return "", "", nil
	}
	if cs.ClientState == nil || cs.ClientState.TypeUrl != tendermintClientState {
		return cs.ClientId, "", nil
	}
	var tm ibctm.ClientState
	if err := proto.Unmarshal(cs.ClientState.Value, &tm); err != nil {
		return cs.ClientId, "", fmt.Errorf("channel %s client state: %w", channel, err)
	}
	return cs.ClientId, tm.ChainId, nil
}

// status returns the light client's status, lower case.
func (s *Set) status(ctx context.Context, clientID string) (string, error) {
	res, err := s.deps.Client.ClientStatus(ctx, &clienttypes.QueryClientStatusRequest{ClientId: clientID})
	if err != nil {
		return "", fmt.Errorf("client %s status: %w", clientID, err)
	}
	return strings.ToLower(res.Status), nil
}
