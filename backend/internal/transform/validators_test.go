package transform_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// Every validator_events row of every fixture height, in one golden file.
// The recorded heights cover each kind the transformer writes: a creation
// (24113494), a commission change (22933748), a description change
// (24151894), an unjail (24129272) and a downtime slash (24160001).
func TestValidatorEventsGolden(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	byHeight := map[string][]transform.ValidatorEvent{}
	kinds := map[string]bool{}
	for _, h := range n.FixtureHeights() {
		ents, err := tr.Transform(fetchInput(t, n, h))
		require.NoError(t, err)
		if len(ents.ValidatorEvents) > 0 {
			byHeight[fmt.Sprint(h)] = ents.ValidatorEvents
		}
		for _, e := range ents.ValidatorEvents {
			kinds[e.Kind] = true
		}
	}
	assert.Equal(t, map[string]bool{
		transform.ValidatorCreated: true, transform.ValidatorCommissionChanged: true,
		transform.ValidatorDescriptionChanged: true, transform.ValidatorUnjailed: true, transform.ValidatorSlashed: true,
	}, kinds)

	got, err := json.MarshalIndent(byHeight, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	file := filepath.Join("testdata", "validator_events.golden.json")
	if *update {
		require.NoError(t, os.WriteFile(file, got, 0o644))
	}
	want, err := os.ReadFile(file)
	require.NoError(t, err, "run go test ./internal/transform -golden to create it")
	assert.Equal(t, string(want), string(got))
}

func TestRecordedValidatorEvents(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	events := func(h int64) []transform.ValidatorEvent {
		ents, err := tr.Transform(fetchInput(t, n, h))
		require.NoError(t, err)
		return ents.ValidatorEvents
	}

	slash := events(24160001)
	require.Len(t, slash, 1)
	assert.Equal(t, -1, slash[0].TxIndex, "block-level")
	assert.Empty(t, slash[0].Operator, "the writer resolves it by consensus address")
	assert.Equal(t, scafireCons, slash[0].ConsensusAddress)
	assert.Equal(t, map[string]any{
		"consensus_address": scafireCons, "reason": "missing_signature", "power": "6138062", "burned": "613806200",
	}, slash[0].Details)

	commission := events(22933748)
	require.Len(t, commission, 1, "the description is all [do-not-modify]")
	assert.Equal(t, transform.ValidatorCommissionChanged, commission[0].Kind)
	assert.True(t, commission[0].CommissionFrom)
	assert.Equal(t, map[string]any{"to": "0.200000000000000000"}, commission[0].Details)

	description := events(24151894)
	require.Len(t, description, 1, "no commission_rate in the message")
	assert.Equal(t, transform.ValidatorDescriptionChanged, description[0].Kind)
	assert.False(t, description[0].CommissionFrom)
}

func TestValidatorEventRules(t *testing.T) {
	msg := func(typeURL, body string) chain.Msg { return chain.Msg{TypeURL: typeURL, Body: json.RawMessage(body)} }
	cases := map[string]struct {
		res  node.TxResult
		msgs []chain.Msg
		want []transform.ValidatorEvent
	}{
		"a failed transaction writes none": {
			res:  node.TxResult{Code: 5},
			msgs: []chain.Msg{msg("/cosmos.slashing.v1beta1.MsgUnjail", `{"validator_addr":"v1"}`)},
		},
		"edit of both: description first, seq counts per transaction": {
			msgs: []chain.Msg{
				msg("/cosmos.slashing.v1beta1.MsgUnjail", `{"validator_addr":"v1"}`),
				msg("/cosmos.staking.v1beta1.MsgEditValidator",
					`{"validator_address":"v2","commission_rate":"0.07","description":{"moniker":"New","website":"[do-not-modify]"}}`),
			},
			want: []transform.ValidatorEvent{
				{TxIndex: 0, Seq: 0, Operator: "v1", Kind: transform.ValidatorUnjailed},
				{TxIndex: 0, Seq: 1, Operator: "v2", Kind: transform.ValidatorDescriptionChanged,
					Details: map[string]any{"to": map[string]any{"moniker": "New"}}},
				{TxIndex: 0, Seq: 2, Operator: "v2", Kind: transform.ValidatorCommissionChanged,
					Details: map[string]any{"to": "0.07"}, CommissionFrom: true},
			},
		},
		"an undecodable creation falls back to its event": {
			res: node.TxResult{Events: []node.Event{{Type: "create_validator", Attributes: attrs(
				"validator", "v3", "amount", "5ubze", "msg_index", "0")}}},
			msgs: []chain.Msg{{TypeURL: "/cosmos.staking.v1beta1.MsgCreateValidator"}},
			want: []transform.ValidatorEvent{
				{TxIndex: 0, Operator: "v3", Kind: transform.ValidatorCreated, Details: map[string]any{"self_bond": "5ubze"}},
			},
		},
		"an undecodable unjail names no validator": {
			msgs: []chain.Msg{{TypeURL: "/cosmos.slashing.v1beta1.MsgUnjail"}},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			tr, _ := mockTransformer(mockDecoder{txs: map[string]*chain.Tx{"tx": {Msgs: c.msgs}}})
			in := oneTx(c.res)
			ents, err := tr.Transform(in)
			require.NoError(t, err)
			for i := range c.want {
				c.want[i].Height, c.want[i].Time = in.Block.Height, in.Block.Time.UTC()
			}
			assert.Equal(t, c.want, ents.ValidatorEvents)
		})
	}
}

func TestSlashWithAnUnreadableAddressKeepsIt(t *testing.T) {
	tr, _ := mockTransformer(mockDecoder{})
	in := baseInput()
	in.Results.FinalizeBlockEvents = []node.Event{{Type: "slash", Attributes: attrs("address", "nope", "reason", "double_sign")}}
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	require.Len(t, ents.ValidatorEvents, 1)
	assert.Equal(t, map[string]any{"consensus_address": "", "address": "nope", "reason": "double_sign"},
		ents.ValidatorEvents[0].Details)
}
