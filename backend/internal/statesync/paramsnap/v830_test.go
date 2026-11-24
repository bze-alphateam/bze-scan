package paramsnap_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
	"github.com/bze-alphateam/bze-scan/backend/internal/params"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/paramsnap"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// storedBeforeV830 is what the params set stored for tradebin and burner
// with a build on the v8.2.x types: the recorded answers of a v8.2.x node
// under their old names.
func storedBeforeV830(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	var tradebin struct {
		Params json.RawMessage `json:"params"`
	}
	require.NoError(t, json.Unmarshal(fakenode.ReadGRPCFixture(t, "tradebin", "Params", ""), &tradebin))
	var burner struct {
		Params json.RawMessage `json:"params"`
	}
	require.NoError(t, json.Unmarshal(fakenode.ReadGRPCFixture(t, "burner", "Params", ""), &burner))
	return map[string]json.RawMessage{"tradebin": tradebin.Params, "burner": burner.Params}
}

// The explorer's v8.3.0 types render a v8.2.x node's answer in the v8.3.0
// shape. The stored snapshot is rewritten to exactly that, so the first
// snapshot run after the deploy records nothing; without the rewrite it
// would record a change that is the explorer's own.
func TestTheV830TypesDoNotLookLikeAParameterChange(t *testing.T) {
	codec, err := chain.NewCodec()
	require.NoError(t, err)
	g := fakenode.NewGRPC(t)
	conn, err := grpcclient.Dial(grpcclient.Config{Addr: g.Addr}, codec.GRPC())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	live, err := params.NewGRPC(conn, codec).Params(context.Background())
	require.NoError(t, err)
	require.Len(t, live, len(params.Modules))

	var tradebin map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(live["tradebin"], &tradebin))
	for _, k := range []string{"priceLevelOwnersWindow", "priceLevelExtraGas", "maxFillsPerOrder",
		"poolPriceHalfLifeBlocks", "feeMaxPriceDrift", "moduleMaxPriceDrift"} {
		assert.Contains(t, tradebin, k)
	}
	for _, k := range []string{"orderBookExtraGasWindow", "orderBookQueueExtraGas", "orderBookPerBlockMessages", "orderBookQueueMessageScanExtraGas"} {
		assert.NotContains(t, tradebin, k)
	}
	assert.JSONEq(t, `"100"`, string(tradebin["priceLevelOwnersWindow"]), "field 7 keeps its value")
	assert.JSONEq(t, `"25000"`, string(tradebin["priceLevelExtraGas"]), "field 8 keeps its value")
	assert.JSONEq(t, `"500"`, string(tradebin["maxFillsPerOrder"]), "field 11 keeps its value")

	// What the explorer stored before this deploy, every other module as the
	// types render it now.
	stored := map[string]json.RawMessage{}
	for m, raw := range live {
		stored[m] = raw
	}
	for m, raw := range storedBeforeV830(t) {
		stored[m] = raw
	}

	run := func(stored map[string]json.RawMessage) *fakeStore {
		store := &fakeStore{}
		for m, raw := range stored {
			store.snaps = append(store.snaps, paramsnapSnapshot(m, raw))
		}
		before := len(store.snaps)
		set := paramsnap.New(paramsnap.Deps{Params: &fakeReader{params: live}, Node: &fakeNode{height: 200}, Store: store}, 0)
		require.NoError(t, set.FullResync(context.Background()))
		store.snaps = store.snaps[before:] // what the run wrote
		return store
	}

	unrewritten := run(stored)
	assert.ElementsMatch(t, []string{"tradebin", "burner"}, snapshotModules(unrewritten), "the old shape reads as a change")

	rewritten := map[string]json.RawMessage{}
	for m, raw := range stored {
		out, _, err := params.UpgradeStoredToV830(m, raw)
		require.NoError(t, err)
		rewritten[m] = out
	}
	assert.Empty(t, run(rewritten).snaps, "the rewritten snapshots match what the set reads: no new row")
}

func paramsnapSnapshot(module string, raw json.RawMessage) paramsnap.Snapshot {
	return paramsnap.Snapshot{Module: module, Height: 100, Params: raw, ChangedKeys: []string{}}
}

func snapshotModules(s *fakeStore) []string {
	var out []string
	for _, sn := range s.snaps {
		out = append(out, sn.Module)
	}
	return out
}
