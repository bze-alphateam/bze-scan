package channels_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	connectiontypes "github.com/cosmos/ibc-go/v8/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/bze-alphateam/bze-scan/backend/internal/chainregistry"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/channels"
)

type fakeRegistry struct {
	files map[string]*chainregistry.IBCFile // by IBCPath
	err   error
	asked []string
}

func (r *fakeRegistry) IBC(_ context.Context, a, b string, testnet bool) (*chainregistry.IBCFile, error) {
	p := chainregistry.IBCPath(a, b, testnet)
	r.asked = append(r.asked, p)
	return r.files[p], r.err
}

type fakeConnection struct {
	counterparty map[string]string // our connection → theirs
	err          error
}

func (c *fakeConnection) Connection(_ context.Context, in *connectiontypes.QueryConnectionRequest, _ ...grpc.CallOption) (*connectiontypes.QueryConnectionResponse, error) {
	if c.err != nil {
		return nil, c.err
	}
	return &connectiontypes.QueryConnectionResponse{Connection: &connectiontypes.ConnectionEnd{
		Counterparty: connectiontypes.Counterparty{ConnectionId: c.counterparty[in.ConnectionId]},
	}}, nil
}

// fakeHTTP answers by URL: a body (200), a status, or a transport error
// for a URL it does not know.
type fakeHTTP struct {
	bodies map[string]string
	status map[string]int
	asked  []string
}

func (h *fakeHTTP) Do(req *http.Request) (*http.Response, error) {
	u := req.URL.String()
	h.asked = append(h.asked, u)
	if code, ok := h.status[u]; ok {
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(`{"code":5}`))}, nil
	}
	body, ok := h.bodies[u]
	if !ok {
		return nil, errors.New("connection refused")
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

const (
	hubREST       = "https://rest.hub"
	tmClientState = "/ibc.lightclients.tendermint.v1.ClientState"
)

// hubAnswers are the Cosmos Hub endpoint's answers for BZE's channel-11
// (the hub's channel-1898 on connection-1294).
func hubAnswers(rest, cpChannel, cpPort, hop, chainID, typ string) map[string]string {
	return map[string]string{
		rest + "/ibc/core/channel/v1/channels/channel-1898/ports/transfer": `{"channel":{"state":"STATE_OPEN",
			"counterparty":{"port_id":"` + cpPort + `","channel_id":"` + cpChannel + `"},"connection_hops":["` + hop + `"]}}`,
		rest + "/ibc/core/connection/v1/connections/" + hop + "/client_state": `{"identified_client_state":{
			"client_id":"07-tendermint-1488","client_state":{"@type":"` + typ + `","chain_id":"` + chainID + `"}}}`,
	}
}

func goodHub() map[string]string {
	return hubAnswers(hubREST, "channel-11", "transfer", "connection-1294", "beezee-1", tmClientState)
}

// confirmEnv is BZE with channel-3 to Noble and channel-11 to the Cosmos
// Hub, both chains in the registry cache.
type confirmEnv struct {
	ch    *fakeChannel
	store *fakeStore
	reg   *fakeRegistry
	conn  *fakeConnection
	http  *fakeHTTP
	clock *clock
	set   *channels.Set
}

func newConfirmEnv() *confirmEnv {
	e := &confirmEnv{
		ch: &fakeChannel{
			channels: []*channeltypes.IdentifiedChannel{
				channel("transfer", "channel-3", "channel-95", "connection-3", channeltypes.OPEN),
				channel("transfer", "channel-11", "channel-1898", "connection-11", channeltypes.OPEN),
			},
			chainOf: map[string]string{"channel-3": "noble-1", "channel-11": "cosmoshub-4"},
		},
		store: &fakeStore{known: map[string]bool{"noble-1": true, "cosmoshub-4": true}, chains: map[string]channels.ChainRef{
			"beezee-1":    {ChainID: "beezee-1", Name: "beezee", NetworkType: "mainnet"},
			"noble-1":     {ChainID: "noble-1", Name: "noble", NetworkType: "mainnet", REST: []string{"https://rest.noble"}},
			"cosmoshub-4": {ChainID: "cosmoshub-4", Name: "cosmoshub", NetworkType: "mainnet", REST: []string{hubREST + "/"}},
		}},
		reg:   &fakeRegistry{files: map[string]*chainregistry.IBCFile{}},
		conn:  &fakeConnection{counterparty: map[string]string{"connection-11": "connection-1294", "connection-3": "connection-107"}},
		http:  &fakeHTTP{bodies: goodHub(), status: map[string]int{}},
		clock: &clock{now: t0},
	}
	e.set = channels.New(channels.Deps{Channel: e.ch, Client: e.ch, Store: e.store, ChainID: "beezee-1",
		Registry: e.reg, Connection: e.conn, HTTP: e.http, Clock: e.clock}, 0)
	return e
}

func (e *confirmEnv) run(t *testing.T) {
	t.Helper()
	require.NoError(t, e.set.FullResync(context.Background()))
}

func (e *confirmEnv) confirmation(t *testing.T, id string) channels.Confirmation {
	t.Helper()
	for _, r := range e.store.saved {
		if r.ChannelID == id {
			require.NotNil(t, r.Confirmation)
			return *r.Confirmation
		}
	}
	t.Fatalf("channel %s not saved", id)
	return channels.Confirmation{}
}

func nobleFile(bzeFirst bool) *chainregistry.IBCFile {
	bze := chainregistry.IBCChain{ChainName: "beezee", ClientID: "07-tendermint-9", ConnectionID: "connection-3"}
	noble := chainregistry.IBCChain{ChainName: "noble", ClientID: "07-tendermint-112", ConnectionID: "connection-107"}
	bzeEnd := chainregistry.IBCEnd{ChannelID: "channel-3", PortID: "transfer"}
	nobleEnd := chainregistry.IBCEnd{ChannelID: "channel-95", PortID: "transfer"}
	if bzeFirst {
		return &chainregistry.IBCFile{Chain1: bze, Chain2: noble, Channels: []chainregistry.IBCChannel{{Chain1: bzeEnd, Chain2: nobleEnd}}}
	}
	return &chainregistry.IBCFile{Chain1: noble, Chain2: bze, Channels: []chainregistry.IBCChannel{{Chain1: nobleEnd, Chain2: bzeEnd}}}
}

func TestTheRegistrysIBCFileConfirmsAChannel(t *testing.T) {
	for _, bzeFirst := range []bool{true, false} {
		e := newConfirmEnv()
		e.reg.files["_IBC/beezee-noble.json"] = nobleFile(bzeFirst)
		e.run(t)
		assert.Equal(t, channels.Confirmation{Via: channels.ViaRegistry, At: t0, CheckedAt: t0}, e.confirmation(t, "channel-3"),
			"BZE as chain_1 %v", bzeFirst)
		for _, u := range e.http.asked {
			assert.NotContains(t, u, "rest.noble", "no endpoint asked")
		}
	}
}

func TestTheRegistrysIBCFileMustNameBothEnds(t *testing.T) {
	e := newConfirmEnv()
	f := nobleFile(true)
	f.Channels[0].Chain1.ChannelID = "channel-7"
	e.reg.files["_IBC/beezee-noble.json"] = f
	e.conn.err = errors.New("unavailable")
	e.run(t)
	c := e.confirmation(t, "channel-3")
	assert.False(t, c.Confirmed(), "the file lists another BZE channel")
	assert.Contains(t, c.Note, "connection-3")

	e = newConfirmEnv()
	f = nobleFile(true)
	f.Channels[0].Chain1.PortID = "icahost"
	e.reg.files["_IBC/beezee-noble.json"] = f
	e.conn.err = errors.New("unavailable")
	e.run(t)
	assert.False(t, e.confirmation(t, "channel-3").Confirmed(), "not the transfer port")

	// The channel's light client claims Noble, the file connects BZE with
	// Noble, but lists the channel for another chain: not this file then.
	e = newConfirmEnv()
	e.store.chains["noble-1"] = channels.ChainRef{ChainID: "noble-1", Name: "noble"}
	e.reg.files["_IBC/beezee-noble.json"] = &chainregistry.IBCFile{
		Chain1: chainregistry.IBCChain{ChainName: "beezee"}, Chain2: chainregistry.IBCChain{ChainName: "osmosis"},
		Channels: []chainregistry.IBCChannel{{Chain1: chainregistry.IBCEnd{ChannelID: "channel-3", PortID: "transfer"}}},
	}
	e.conn.err = errors.New("unavailable")
	e.run(t)
	assert.False(t, e.confirmation(t, "channel-3").Confirmed())
}

func TestTestnetsReadTheTestnetsIBCFiles(t *testing.T) {
	e := newConfirmEnv()
	e.store.chains["beezee-1"] = channels.ChainRef{ChainID: "beezee-1", Name: "beezee", NetworkType: "testnet"}
	e.run(t)
	assert.Contains(t, e.reg.asked, "testnets/_IBC/beezee-noble.json")
}

func TestTheCounterpartysEndpointsConfirmAChannel(t *testing.T) {
	e := newConfirmEnv()
	e.run(t)
	assert.Equal(t, channels.Confirmation{Via: channels.ViaEndpoint, At: t0, CheckedAt: t0}, e.confirmation(t, "channel-11"))
	assert.Contains(t, e.reg.asked, "_IBC/beezee-cosmoshub.json", "the registry first")
	assert.Equal(t, []string{
		hubREST + "/ibc/core/channel/v1/channels/channel-1898/ports/transfer",
		hubREST + "/ibc/core/connection/v1/connections/connection-1294/client_state",
	}, e.http.asked[len(e.http.asked)-2:], "the trailing slash of the registry's address dropped")
}

// An endpoint that answers but disagrees decides: unconfirmed, the reason
// noted, no other endpoint asked.
func TestAnEndpointThatDisagreesLeavesTheChannelUnconfirmed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers map[string]string
		note    string
	}{
		{"its channel leads to another channel", hubAnswers(hubREST, "channel-8", "transfer", "connection-1294", "beezee-1", tmClientState),
			"leads to transfer/channel-8, not transfer/channel-11"},
		{"its channel leads to another port", hubAnswers(hubREST, "channel-11", "icahost", "connection-1294", "beezee-1", tmClientState),
			"leads to icahost/channel-11"},
		{"its channel is on another connection", hubAnswers(hubREST, "channel-11", "transfer", "connection-9", "beezee-1", tmClientState),
			"is on connection connection-9, not connection-1294"},
		{"its client tracks another chain", hubAnswers(hubREST, "channel-11", "transfer", "connection-1294", "beezee-2", tmClientState),
			`tracks "beezee-2"`},
		{"its client is not a Tendermint client", hubAnswers(hubREST, "channel-11", "transfer", "connection-1294", "beezee-1", "/ibc.lightclients.solomachine.v3.ClientState"),
			"solomachine"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newConfirmEnv()
			e.store.chains["cosmoshub-4"] = channels.ChainRef{ChainID: "cosmoshub-4", Name: "cosmoshub",
				REST: []string{hubREST, "https://rest2.hub"}}
			e.http.bodies = tc.answers
			for k, v := range hubAnswers("https://rest2.hub", "channel-11", "transfer", "connection-1294", "beezee-1", tmClientState) {
				e.http.bodies[k] = v
			}
			e.run(t)
			c := e.confirmation(t, "channel-11")
			assert.False(t, c.Confirmed())
			assert.Equal(t, t0, c.CheckedAt)
			assert.Contains(t, c.Note, tc.note)
			for _, u := range e.http.asked {
				assert.NotContains(t, u, "rest2.hub", "the first endpoint that answers decides")
			}
		})
	}
}

// Endpoints that do not answer are skipped, three at most; with none
// answering the channel is checked again after an hour, not before.
func TestEndpointsDownAreRetriedHourly(t *testing.T) {
	e := newConfirmEnv()
	down := []string{"https://a.hub", "https://b.hub", "https://c.hub", hubREST}
	delete(e.store.chains, "noble-1")
	e.store.chains["cosmoshub-4"] = channels.ChainRef{ChainID: "cosmoshub-4", Name: "cosmoshub", REST: down}
	e.http.status["https://b.hub/ibc/core/channel/v1/channels/channel-1898/ports/transfer"] = http.StatusBadGateway
	e.run(t)
	c := e.confirmation(t, "channel-11")
	assert.False(t, c.Confirmed())
	assert.Contains(t, c.Note, "none answered")
	assert.Len(t, e.http.asked, 3, "the fourth endpoint is not tried")

	e.http.asked = nil
	e.clock.now = t0.Add(59 * time.Minute)
	e.run(t)
	assert.Empty(t, e.http.asked, "not before an hour")
	assert.Equal(t, t0, e.confirmation(t, "channel-11").CheckedAt)

	// An hour later the hub's endpoints come first and answer.
	e.store.chains["cosmoshub-4"] = channels.ChainRef{ChainID: "cosmoshub-4", Name: "cosmoshub", REST: []string{"https://a.hub", hubREST}}
	e.clock.now = t0.Add(time.Hour)
	e.run(t)
	assert.Equal(t, channels.Confirmation{Via: channels.ViaEndpoint, At: e.clock.now, CheckedAt: e.clock.now}, e.confirmation(t, "channel-11"),
		"a down endpoint is skipped, the next one answers")
}

func TestAConfirmedChannelIsNeverCheckedAgain(t *testing.T) {
	e := newConfirmEnv()
	e.reg.files["_IBC/beezee-noble.json"] = nobleFile(true)
	e.run(t)
	require.True(t, e.confirmation(t, "channel-3").Confirmed())
	require.True(t, e.confirmation(t, "channel-11").Confirmed())

	e.reg.asked, e.http.asked = nil, nil
	e.reg.files = map[string]*chainregistry.IBCFile{}
	e.http.bodies = map[string]string{}
	e.clock.now = t0.Add(48 * time.Hour)
	e.run(t)
	assert.Empty(t, e.reg.asked)
	assert.Empty(t, e.http.asked)
	assert.Equal(t, channels.Confirmation{Via: channels.ViaRegistry, At: t0, CheckedAt: t0}, e.confirmation(t, "channel-3"))
	assert.Equal(t, channels.Confirmation{Via: channels.ViaEndpoint, At: t0, CheckedAt: t0}, e.confirmation(t, "channel-11"))
}

// A client, connection, counterparty chain or counterparty channel change
// resets the confirmation; the channel is checked again at once.
func TestAConfirmationResetsWhenTheChannelChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(e *confirmEnv)
	}{
		{"client", func(e *confirmEnv) { e.ch.clientOf = map[string]string{"channel-11": "07-tendermint-99"} }},
		{"connection", func(e *confirmEnv) { e.ch.channels[1].ConnectionHops = []string{"connection-20"} }},
		{"counterparty chain", func(e *confirmEnv) {
			e.ch.chainOf["channel-11"] = "cosmoshub-5"
			e.store.chains["cosmoshub-5"] = channels.ChainRef{ChainID: "cosmoshub-5", Name: "cosmoshub", REST: []string{hubREST}}
		}},
		{"counterparty channel", func(e *confirmEnv) { e.ch.channels[1].Counterparty.ChannelId = "channel-1899" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newConfirmEnv()
			e.run(t)
			require.True(t, e.confirmation(t, "channel-11").Confirmed())

			// The hub no longer agrees (nothing answers for the new
			// identity), ten minutes later.
			e.http.bodies = map[string]string{}
			e.clock.now = t0.Add(10 * time.Minute)
			tc.change(e)
			e.run(t)
			c := e.confirmation(t, "channel-11")
			assert.False(t, c.Confirmed(), "reset")
			assert.Equal(t, e.clock.now, c.CheckedAt, "and checked again at once")
		})
	}

	// A client state the node did not answer is no change.
	e := newConfirmEnv()
	e.run(t)
	e.ch.fail = map[string]bool{"channel-11": true}
	e.http.bodies = map[string]string{}
	require.Error(t, e.set.FullResync(context.Background()))
	assert.True(t, e.confirmation(t, "channel-11").Confirmed())
}

func TestOnlyChainsInTheRegistryCacheAreChecked(t *testing.T) {
	e := newConfirmEnv()
	delete(e.store.chains, "cosmoshub-4")
	e.run(t)
	assert.Equal(t, channels.Confirmation{}, e.confirmation(t, "channel-11"), "not checked")
	assert.NotContains(t, e.reg.asked, "_IBC/beezee-cosmoshub.json")
	for _, u := range e.http.asked {
		assert.NotContains(t, u, "hub")
	}
}

// A failed registry read is noted; the endpoints still confirm.
func TestARegistryFailureFallsBackToTheEndpoints(t *testing.T) {
	e := newConfirmEnv()
	e.reg.err = errors.New("HTTP 503")
	e.run(t)
	assert.Equal(t, channels.ViaEndpoint, e.confirmation(t, "channel-11").Via)
	c := e.confirmation(t, "channel-3")
	assert.False(t, c.Confirmed())
	assert.Contains(t, c.Note, "registry: HTTP 503")
	assert.Equal(t, []string{"_IBC/beezee-cosmoshub.json", "_IBC/beezee-noble.json"}, slices.Sorted(slices.Values(e.reg.asked)),
		"each file read once per run")
}

// Stored channels that cannot be read: the confirmations are left as
// stored (the rows carry none) and the failure returned.
func TestAFailedStoredReadKeepsTheConfirmations(t *testing.T) {
	e := newConfirmEnv()
	e.store.storedErr = errors.New("db down")
	require.ErrorContains(t, e.set.FullResync(context.Background()), "db down")
	require.Len(t, e.store.saved, 2, "the channels are still written")
	for _, r := range e.store.saved {
		assert.Nil(t, r.Confirmation)
	}
}
