package transform_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// A recorded restake: an authz MsgExec of delegations to one validator,
// whose power change is in the block's validator updates.
func TestRecordedDelegationsMarkTheirValidator(t *testing.T) {
	n := fakenode.New(t)
	in := fetchInput(t, n, 25000440)
	require.NotEmpty(t, in.Results.ValidatorUpdates)
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	assert.Equal(t, []string{"bzevaloper1prm55vzlp5u6excqdunwlm4tw254cq943m6e6m"}, ents.Dirty.Keys(statesync.Validators),
		"a power change the events explain asks for that validator only")
}

func TestBlocksWithoutStakingChangesAreClean(t *testing.T) {
	n := fakenode.New(t)
	for _, h := range []int64{24998316, 24999134, 25000894} {
		ents, err := realTransformer(t).Transform(fetchInput(t, n, h))
		require.NoError(t, err)
		assert.True(t, ents.Dirty.Empty(), "height %d", h)
	}
}

func TestValidatorDirtyRules(t *testing.T) {
	ev := func(typ string, kv ...string) node.Event { return node.Event{Type: typ, Attributes: attrs(kv...)} }
	cases := map[string]struct {
		res      node.TxResult
		msgs     []chain.Msg
		finalize []node.Event
		updates  []node.ValidatorUpdate
		want     []string
	}{
		"delegate, unbond, create, cancel unbonding": {
			res: node.TxResult{Events: []node.Event{
				ev("delegate", "validator", "v1"), ev("unbond", "validator", "v2"),
				ev("create_validator", "validator", "v3"), ev("cancel_unbonding_delegation", "validator", "v4"),
			}},
			want: []string{"v1", "v2", "v3", "v4"},
		},
		"redelegate names both": {
			res:  node.TxResult{Events: []node.Event{ev("redelegate", "source_validator", "v1", "destination_validator", "v2")}},
			want: []string{"v1", "v2"},
		},
		"edit and unjail by their message": {
			msgs: []chain.Msg{
				{TypeURL: "/cosmos.staking.v1beta1.MsgEditValidator", Body: json.RawMessage(`{"validator_address":"v1"}`)},
				{TypeURL: "/cosmos.slashing.v1beta1.MsgUnjail", Body: json.RawMessage(`{"validator_addr":"v2"}`)},
				{TypeURL: "/cosmos.slashing.v1beta1.MsgUnjail"}, // undecodable: nothing to mark
			},
			want: []string{"v1", "v2"},
		},
		"a failed transaction changes nothing": {
			res: node.TxResult{Code: 5, Events: []node.Event{ev("delegate", "validator", "v1")}},
			msgs: []chain.Msg{
				{TypeURL: "/cosmos.staking.v1beta1.MsgEditValidator", Body: json.RawMessage(`{"validator_address":"v1"}`)},
			},
		},
		"a slash names a consensus address: every validator": {
			finalize: []node.Event{ev("slash", "address", "bzevalcons1x", "reason", "missing_signature")},
			want:     []string{statesync.All},
		},
		"leaving the active set: every validator": {
			updates: []node.ValidatorUpdate{{Power: 10}, {Power: 0}},
			want:    []string{statesync.All},
		},
		"a power change alone is explained by its events": {
			updates: []node.ValidatorUpdate{{Power: 10}},
		},
		"other events": {
			res: node.TxResult{Events: []node.Event{ev("withdraw_rewards", "validator", "v1"), ev("transfer", "sender", "x")}},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			tr, _ := mockTransformer(mockDecoder{txs: map[string]*chain.Tx{"tx": {Msgs: c.msgs}}})
			in := oneTx(c.res)
			in.Results.FinalizeBlockEvents = c.finalize
			in.Results.ValidatorUpdates = c.updates
			ents, err := tr.Transform(in)
			require.NoError(t, err)
			if c.want == nil {
				c.want = []string{}
			}
			assert.Equal(t, c.want, ents.Dirty.Keys(statesync.Validators))
		})
	}
}
