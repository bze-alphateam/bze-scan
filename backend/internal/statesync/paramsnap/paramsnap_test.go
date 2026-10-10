package paramsnap_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/paramsnap"
)

type fakeReader struct {
	params map[string]json.RawMessage
	err    error
	height string // the block height the last read was pinned to
}

func (r *fakeReader) Params(ctx context.Context) (map[string]json.RawMessage, error) {
	md, _ := metadata.FromOutgoingContext(ctx)
	if h := md.Get("x-cosmos-block-height"); len(h) == 1 {
		r.height = h[0]
	}
	return r.params, r.err
}

type fakeNode struct {
	height int64
	err    error
}

func (n *fakeNode) Status(context.Context) (*node.Status, []byte, error) {
	if n.err != nil {
		return nil, nil, n.err
	}
	return &node.Status{LatestBlockHeight: n.height, LatestBlockTime: time.Unix(n.height, 0)}, nil, nil
}

type proposal struct{ kind, status string }

type fakeStore struct {
	snaps     []paramsnap.Snapshot
	proposals map[uint64]proposal
	// upgrades are passed upgrades by plan height.
	upgrades map[int64]uint64
}

func (s *fakeStore) Latest(context.Context) (map[string]paramsnap.Latest, error) {
	out := map[string]paramsnap.Latest{}
	for _, sn := range s.snaps {
		if l, ok := out[sn.Module]; !ok || sn.Height > l.Height {
			out[sn.Module] = paramsnap.Latest{Height: sn.Height, Params: sn.Params}
		}
	}
	return out, nil
}

func (s *fakeStore) ProposalKind(_ context.Context, id uint64) (string, string, bool, error) {
	p, ok := s.proposals[id]
	return p.kind, p.status, ok, nil
}

func (s *fakeStore) UpgradeBetween(_ context.Context, after, upTo int64) (*uint64, error) {
	var best *uint64
	var at int64
	for h, id := range s.upgrades {
		if h > after && h <= upTo && h > at {
			at, best = h, &id
		}
	}
	return best, nil
}

func (s *fakeStore) Insert(_ context.Context, snaps []paramsnap.Snapshot) error {
	s.snaps = append(s.snaps, snaps...)
	return nil
}

func (s *fakeStore) byModule(m string) []paramsnap.Snapshot {
	var out []paramsnap.Snapshot
	for _, sn := range s.snaps {
		if sn.Module == m {
			out = append(out, sn)
		}
	}
	return out
}

func id(n uint64) *uint64 { return &n }

func TestTheFirstRunSnapshotsEveryModuleAndAnUnchangedOneWritesNothing(t *testing.T) {
	r := &fakeReader{params: map[string]json.RawMessage{
		"staking": json.RawMessage(`{"max_validators":40,"unbonding_time":"1814400s"}`),
		"burner":  json.RawMessage(`{"periodic_burning_weeks":"4"}`),
	}}
	n := &fakeNode{height: 100}
	store := &fakeStore{}
	s := paramsnap.New(paramsnap.Deps{Params: r, Node: n, Store: store}, 0)
	assert.Equal(t, statesync.Params, s.Name())
	assert.Equal(t, paramsnap.DefaultInterval, s.Interval())

	require.NoError(t, s.FullResync(context.Background()))
	require.Len(t, store.snaps, 2)
	assert.Equal(t, "100", r.height, "the read is pinned to the height /status reported")
	st := store.byModule("staking")[0]
	assert.Equal(t, int64(100), st.Height)
	assert.Equal(t, time.Unix(100, 0).UTC(), st.Time)
	assert.Equal(t, []string{}, st.ChangedKeys, "the first snapshot of a module")
	assert.Nil(t, st.ProposalID)

	n.height = 200
	require.NoError(t, s.FullResync(context.Background()))
	assert.Len(t, store.snaps, 2, "nothing changed, nothing written")
}

func TestAChangedKeyIsAttributedOnlyToATriggeringProposal(t *testing.T) {
	r := &fakeReader{params: map[string]json.RawMessage{"staking": json.RawMessage(`{"max_validators":40,"max_entries":7}`)}}
	n := &fakeNode{height: 100}
	store := &fakeStore{proposals: map[uint64]proposal{
		50: {kind: "parameter_change", status: "passed"},
		51: {kind: "text", status: "passed"},
		52: {kind: "parameter_change", status: "rejected"},
	}}
	s := paramsnap.New(paramsnap.Deps{Params: r, Node: n, Store: store}, 0)
	require.NoError(t, s.FullResync(context.Background()))

	// The daily run sees a change: no proposal.
	r.params["staking"] = json.RawMessage(`{"max_validators":45,"max_entries":7}`)
	n.height = 200
	require.NoError(t, s.FullResync(context.Background()))
	// A passed text proposal is not a cause either, nor a rejected one.
	r.params["staking"] = json.RawMessage(`{"max_validators":45,"max_entries":8}`)
	n.height = 300
	require.NoError(t, s.ResyncOne(context.Background(), "51"))
	r.params["staking"] = json.RawMessage(`{"max_validators":45,"max_entries":9}`)
	n.height = 350
	require.NoError(t, s.ResyncOne(context.Background(), "52"))
	// A passed parameter change is.
	r.params["staking"] = json.RawMessage(`{"max_validators":50,"max_entries":9,"min_commission_rate":"0.05"}`)
	n.height = 400
	require.NoError(t, s.ResyncOne(context.Background(), "50"))
	// An unknown id or a key that is no id is a plain snapshot.
	r.params["staking"] = json.RawMessage(`{"max_validators":51,"max_entries":9,"min_commission_rate":"0.05"}`)
	n.height = 500
	require.NoError(t, s.ResyncOne(context.Background(), "999"))
	require.NoError(t, s.ResyncOne(context.Background(), "not-an-id"))

	got := store.byModule("staking")
	require.Len(t, got, 6)
	assert.Equal(t, []string{"max_validators"}, got[1].ChangedKeys)
	assert.Nil(t, got[1].ProposalID)
	assert.Equal(t, []string{"max_entries"}, got[2].ChangedKeys)
	assert.Nil(t, got[2].ProposalID, "a text proposal changes no parameter")
	assert.Nil(t, got[3].ProposalID, "a rejected proposal changes nothing")
	assert.Equal(t, []string{"max_validators", "min_commission_rate"}, got[4].ChangedKeys, "an added key counts")
	assert.Equal(t, id(50), got[4].ProposalID)
	assert.Nil(t, got[5].ProposalID)
}

func TestAChangeAfterAnUpgradeIsAttributedToIt(t *testing.T) {
	r := &fakeReader{params: map[string]json.RawMessage{"tradebin": json.RawMessage(`{"marketTakerFee":"100"}`)}}
	n := &fakeNode{height: 100}
	store := &fakeStore{upgrades: map[int64]uint64{90: 43, 150: 44, 300: 45}}
	s := paramsnap.New(paramsnap.Deps{Params: r, Node: n, Store: store}, 0)
	require.NoError(t, s.FullResync(context.Background()))

	// The upgrade handler changed the fee at 150; the daily run sees it at 200.
	r.params["tradebin"] = json.RawMessage(`{"marketTakerFee":"200"}`)
	n.height = 200
	require.NoError(t, s.FullResync(context.Background()))
	got := store.byModule("tradebin")
	require.Len(t, got, 2)
	assert.Equal(t, id(44), got[1].ProposalID, "planned after the previous snapshot, at or before this one")
}

func TestAModuleThatFailsKeepsTheOthers(t *testing.T) {
	r := &fakeReader{params: map[string]json.RawMessage{"burner": json.RawMessage(`{"periodic_burning_weeks":"4"}`)},
		err: errors.New("staking params: unavailable")}
	store := &fakeStore{}
	s := paramsnap.New(paramsnap.Deps{Params: r, Node: &fakeNode{height: 10}, Store: store}, time.Hour)
	assert.Equal(t, time.Hour, s.Interval())
	require.ErrorContains(t, s.FullResync(context.Background()), "staking params")
	assert.Len(t, store.byModule("burner"), 1)

	down := paramsnap.New(paramsnap.Deps{Params: r, Node: &fakeNode{err: errors.New("down")}, Store: store}, 0)
	require.ErrorContains(t, down.FullResync(context.Background()), "node status")
}

func TestChangedKeys(t *testing.T) {
	for _, tc := range []struct {
		name, prev, cur string
		want            []string
	}{
		{"unchanged, whatever the key order", `{"a":1,"b":{"x":[1,2]}}`, `{"b":{"x":[1,2]},"a":1}`, nil},
		{"one key changed", `{"a":1,"b":"2"}`, `{"a":1,"b":"3"}`, []string{"b"}},
		{"nested change is its top-level key", `{"a":{"x":1}}`, `{"a":{"x":2}}`, []string{"a"}},
		{"added and removed keys", `{"a":1,"gone":true}`, `{"a":1,"new":true}`, []string{"gone", "new"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := paramsnap.ChangedKeys(json.RawMessage(tc.prev), json.RawMessage(tc.cur))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	_, err := paramsnap.ChangedKeys(json.RawMessage(`[]`), json.RawMessage(`{}`))
	assert.Error(t, err)
	_, err = paramsnap.ChangedKeys(json.RawMessage(`{}`), json.RawMessage(`nope`))
	assert.Error(t, err)
}
