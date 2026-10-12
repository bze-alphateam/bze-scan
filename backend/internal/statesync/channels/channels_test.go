package channels_test

import (
	"context"
	"errors"
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	ibctm "github.com/cosmos/ibc-go/v8/modules/light-clients/07-tendermint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/channels"
)

// fakeChannel serves channels in pages of two and a Tendermint client per
// channel naming chainOf[channel], Active unless its status says otherwise.
type fakeChannel struct {
	channels   []*channeltypes.IdentifiedChannel
	chainOf    map[string]string
	solo       map[string]bool   // channel → a non-Tendermint client
	fail       map[string]bool   // channel → its client state fails
	status     map[string]string // client → its status
	statusFail map[string]bool   // client → its status query fails
	pages      int
}

func (f *fakeChannel) ClientStatus(_ context.Context, in *clienttypes.QueryClientStatusRequest, _ ...grpc.CallOption) (*clienttypes.QueryClientStatusResponse, error) {
	if f.statusFail[in.ClientId] {
		return nil, errors.New("unavailable")
	}
	if s, ok := f.status[in.ClientId]; ok {
		return &clienttypes.QueryClientStatusResponse{Status: s}, nil
	}
	return &clienttypes.QueryClientStatusResponse{Status: "Active"}, nil
}

func (f *fakeChannel) Channels(_ context.Context, in *channeltypes.QueryChannelsRequest, _ ...grpc.CallOption) (*channeltypes.QueryChannelsResponse, error) {
	f.pages++
	start := 0
	if len(in.Pagination.Key) > 0 {
		start = int(in.Pagination.Key[0])
	}
	end := min(start+2, len(f.channels))
	res := &channeltypes.QueryChannelsResponse{Channels: f.channels[start:end], Pagination: &query.PageResponse{}}
	if end < len(f.channels) {
		res.Pagination.NextKey = []byte{byte(end)}
	}
	return res, nil
}

func (f *fakeChannel) ChannelClientState(_ context.Context, in *channeltypes.QueryChannelClientStateRequest, _ ...grpc.CallOption) (*channeltypes.QueryChannelClientStateResponse, error) {
	if f.fail[in.ChannelId] {
		return nil, errors.New("unavailable")
	}
	var cs *codectypes.Any
	var err error
	if f.solo[in.ChannelId] {
		cs = &codectypes.Any{TypeUrl: "/ibc.lightclients.solomachine.v3.ClientState"}
	} else if cs, err = codectypes.NewAnyWithValue(&ibctm.ClientState{ChainId: f.chainOf[in.ChannelId]}); err != nil {
		return nil, err
	}
	return &channeltypes.QueryChannelClientStateResponse{
		IdentifiedClientState: &clienttypes.IdentifiedClientState{ClientId: "07-tendermint-" + in.ChannelId, ClientState: cs},
	}, nil
}

type fakeStore struct {
	saved []channels.Row
	known map[string]bool
	asked []string
	err   error
}

func (s *fakeStore) Save(_ context.Context, rows []channels.Row) error {
	s.saved = rows
	return s.err
}

func (s *fakeStore) UnknownChains(_ context.Context, ids []string) ([]string, error) {
	s.asked = ids
	var out []string
	for _, id := range ids {
		if !s.known[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

type publisher struct{ got []statesync.Dirty }

func (p *publisher) Publish(d statesync.Dirty) { p.got = append(p.got, d) }

func channel(port, id, cp, conn string, state channeltypes.State) *channeltypes.IdentifiedChannel {
	return &channeltypes.IdentifiedChannel{PortId: port, ChannelId: id, State: state,
		Counterparty: channeltypes.Counterparty{PortId: port, ChannelId: cp}, ConnectionHops: []string{conn}}
}

func TestFullResyncWritesTheTransferChannels(t *testing.T) {
	ch := &fakeChannel{
		channels: []*channeltypes.IdentifiedChannel{
			channel("transfer", "channel-0", "channel-340", "connection-0", channeltypes.OPEN),
			channel("icacontroller-bze1x", "channel-1", "channel-9", "connection-1", channeltypes.OPEN),
			channel("transfer", "channel-2", "channel-147", "connection-2", channeltypes.CLOSED),
			channel("transfer", "channel-3", "channel-95", "connection-3", channeltypes.OPEN),
			channel("transfer", "channel-4", "channel-1", "connection-4", channeltypes.TRYOPEN),
		},
		chainOf: map[string]string{"channel-0": "osmosis-1", "channel-2": "cosmoshub-4", "channel-3": "noble-1"},
		solo:    map[string]bool{"channel-4": true},
		status:  map[string]string{"07-tendermint-channel-2": "Expired"},
	}
	store := &fakeStore{known: map[string]bool{"osmosis-1": true}}
	misses := &publisher{}
	set := channels.New(channels.Deps{Channel: ch, Client: ch, Store: store, Misses: misses}, 0)

	require.NoError(t, set.FullResync(context.Background()))
	assert.Equal(t, 3, ch.pages, "every page, two channels each")
	assert.Equal(t, []channels.Row{
		{ChannelID: "channel-0", PortID: "transfer", ClientID: "07-tendermint-channel-0", ConnectionID: "connection-0",
			CounterpartyChainID: "osmosis-1", CounterpartyChannelID: "channel-340", State: "open",
			ClientStatus: "active", Escrow: "bze1a53udazy8ayufvy0s434pfwjcedzqv34tgwwee"},
		{ChannelID: "channel-2", PortID: "transfer", ClientID: "07-tendermint-channel-2", ConnectionID: "connection-2",
			CounterpartyChainID: "cosmoshub-4", CounterpartyChannelID: "channel-147", State: "closed",
			ClientStatus: "expired", Escrow: store.saved[1].Escrow},
		{ChannelID: "channel-3", PortID: "transfer", ClientID: "07-tendermint-channel-3", ConnectionID: "connection-3",
			CounterpartyChainID: "noble-1", CounterpartyChannelID: "channel-95", State: "open",
			ClientStatus: "active", Escrow: store.saved[2].Escrow},
		{ChannelID: "channel-4", PortID: "transfer", ClientID: "07-tendermint-channel-4", ConnectionID: "connection-4",
			CounterpartyChannelID: "channel-1", State: "tryopen", ClientStatus: "active", Escrow: store.saved[3].Escrow},
	}, store.saved, "transfer port only; a non-Tendermint client names no chain")
	assert.NotEqual(t, store.saved[1].Escrow, store.saved[2].Escrow)

	assert.ElementsMatch(t, []string{"osmosis-1", "cosmoshub-4", "noble-1"}, store.asked)
	require.Len(t, misses.got, 1)
	assert.Equal(t, []string{"cosmoshub-4", "noble-1"}, misses.got[0].Keys(statesync.ChainRegistry),
		"the chains the registry cache lacks go to the registry set")
}

// A channel whose client state fails is saved without its chain (the store
// keeps the stored one); the others are written and the failure returned.
func TestAFailingClientStateKeepsTheOthers(t *testing.T) {
	ch := &fakeChannel{
		channels: []*channeltypes.IdentifiedChannel{
			channel("transfer", "channel-0", "channel-340", "connection-0", channeltypes.OPEN),
			channel("transfer", "channel-3", "channel-95", "connection-3", channeltypes.OPEN),
		},
		chainOf: map[string]string{"channel-0": "osmosis-1", "channel-3": "noble-1"},
		fail:    map[string]bool{"channel-0": true},
	}
	store := &fakeStore{known: map[string]bool{"noble-1": true}}
	misses := &publisher{}
	err := channels.New(channels.Deps{Channel: ch, Client: ch, Store: store, Misses: misses}, 0).FullResync(context.Background())
	require.ErrorContains(t, err, "channel-0 client state")
	require.Len(t, store.saved, 2)
	assert.Empty(t, store.saved[0].CounterpartyChainID)
	assert.Equal(t, "noble-1", store.saved[1].CounterpartyChainID)
	assert.Empty(t, misses.got, "every chain is known")
}

// A client whose status query fails is saved without a status (the store
// keeps the stored one); the failure is returned.
func TestAFailingClientStatusKeepsTheChannel(t *testing.T) {
	ch := &fakeChannel{
		channels: []*channeltypes.IdentifiedChannel{
			channel("transfer", "channel-1", "channel-0", "connection-1", channeltypes.OPEN),
			channel("transfer", "channel-3", "channel-95", "connection-3", channeltypes.OPEN),
		},
		chainOf:    map[string]string{"channel-1": "vidulum-1", "channel-3": "noble-1"},
		statusFail: map[string]bool{"07-tendermint-channel-1": true},
	}
	store := &fakeStore{known: map[string]bool{"vidulum-1": true, "noble-1": true}}
	err := channels.New(channels.Deps{Channel: ch, Client: ch, Store: store}, 0).FullResync(context.Background())
	require.ErrorContains(t, err, "client 07-tendermint-channel-1 status")
	require.Len(t, store.saved, 2)
	assert.Equal(t, "vidulum-1", store.saved[0].CounterpartyChainID)
	assert.Empty(t, store.saved[0].ClientStatus)
	assert.Equal(t, "active", store.saved[1].ClientStatus)
}

func TestAFailingSaveFailsTheRun(t *testing.T) {
	ch := &fakeChannel{channels: []*channeltypes.IdentifiedChannel{
		channel("transfer", "channel-0", "channel-340", "connection-0", channeltypes.OPEN),
	}, chainOf: map[string]string{"channel-0": "osmosis-1"}}
	store := &fakeStore{err: errors.New("db down")}
	misses := &publisher{}
	set := channels.New(channels.Deps{Channel: ch, Client: ch, Store: store, Misses: misses}, 0)
	require.ErrorContains(t, set.ResyncOne(context.Background(), statesync.All), "db down")
	assert.Empty(t, misses.got)
	assert.Equal(t, statesync.Channels, set.Name())
	assert.Equal(t, channels.DefaultInterval, set.Interval())
}
