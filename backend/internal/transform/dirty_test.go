package transform_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/denoms"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// A recorded restake: an authz MsgExec of delegations to one validator,
// whose power change is in the block's validator updates. The delegation
// names the operator, the update the consensus address of the same
// validator; the syncer folds the two (statesync.Canonicaliser).
func TestRecordedDelegationsMarkTheirValidator(t *testing.T) {
	n := fakenode.New(t)
	in := fetchInput(t, n, 25000440)
	require.NotEmpty(t, in.Results.ValidatorUpdates)
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"bzevaloper1prm55vzlp5u6excqdunwlm4tw254cq943m6e6m",
		statesync.ConsKey("090703A2C594C5BA93C0D0E263A9F79AEEE17D10"),
	}, ents.Dirty.Keys(statesync.Validators))
}

// A recorded downtime slash: the liveness event, the slash event and the
// validator update removing it from the active set all name one validator,
// which is marked once.
func TestRecordedSlashMarksTheSlashedValidatorOnce(t *testing.T) {
	n := fakenode.New(t)
	ents, err := realTransformer(t).Transform(fetchInput(t, n, 24160001))
	require.NoError(t, err)
	assert.Equal(t, []string{statesync.ConsKey(scafireCons)}, ents.Dirty.Keys(statesync.Validators))
}

// Blocks without staking or token changes mark no validator and no denom;
// the denoms their transfers moved are only "seen", which the denoms set
// resyncs when it does not hold them yet.
func TestBlocksWithoutStakingOrTokenChangesMarkOnlySeenDenoms(t *testing.T) {
	n := fakenode.New(t)
	for _, h := range []int64{24998316, 24999134, 25000894} {
		ents, err := realTransformer(t).Transform(fetchInput(t, n, h))
		require.NoError(t, err)
		assert.Empty(t, ents.Dirty.Keys(statesync.Validators), "height %d", h)
		keys := ents.Dirty.Keys(statesync.Denoms)
		require.NotEmpty(t, keys, "height %d: every transaction moves its fee", h)
		for _, k := range keys {
			assert.True(t, strings.HasPrefix(k, "seen:"), "height %d: %s", h, k)
		}
		assert.Contains(t, keys, denoms.SeenKey("ubze"))
	}
	ents, err := realTransformer(t).Transform(fetchInput(t, n, 24998317))
	require.NoError(t, err)
	assert.True(t, ents.Dirty.Empty(), "an empty block moves nothing but routine mints")
}

// Scafire, slashed and jailed at 24160001, and ChainTools.
const (
	scafireBech32       = "bzevalcons1al6clt29tyceyv7zstu59su2p0502qfq8pu2d3"
	scafireCons         = "EFF58FAD4559319233C282F942C38A0BE8F50120"
	scafireUpdateKey    = `{"Sum":{"type":"tendermint.crypto.PublicKey_Ed25519","value":{"ed25519":"oR/+r77iVloVCK5qHDck8K/fOjwjQbUHnLJw4cuHOqg="}}}`
	chainToolsCons      = "090703A2C594C5BA93C0D0E263A9F79AEEE17D10"
	chainToolsUpdateKey = `{"Sum":{"type":"tendermint.crypto.PublicKey_Ed25519","value":{"ed25519":"ikmP1GM73Y1vVKsLZYjFENPGve7uLWV3Q+8YF60LHMA="}}}`
)

func TestValidatorDirtyRules(t *testing.T) {
	ev := func(typ string, kv ...string) node.Event { return node.Event{Type: typ, Attributes: attrs(kv...)} }
	update := func(pubKey string, power int64) node.ValidatorUpdate {
		return node.ValidatorUpdate{PubKey: json.RawMessage(pubKey), Power: power}
	}
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
		"slash and liveness name a consensus address": {
			finalize: []node.Event{
				ev("liveness", "address", scafireBech32, "missed_blocks", "8001"),
				ev("slash", "address", scafireBech32, "reason", "missing_signature"),
			},
			want: []string{statesync.ConsKey(scafireCons)},
		},
		"an unreadable consensus address: every validator": {
			finalize: []node.Event{ev("slash", "address", "bzevalcons1x", "reason", "missing_signature")},
			want:     []string{statesync.All},
		},
		"each validator update by its consensus address": {
			updates: []node.ValidatorUpdate{update(scafireUpdateKey, 0), update(chainToolsUpdateKey, 10)},
			want:    []string{statesync.ConsKey(chainToolsCons), statesync.ConsKey(scafireCons)},
		},
		"an unreadable validator update: every validator": {
			updates: []node.ValidatorUpdate{{Power: 10}},
			want:    []string{statesync.All},
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
