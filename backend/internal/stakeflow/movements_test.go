package stakeflow_test

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/stakeflow"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

const (
	valA = "bzevaloper1prm55vzlp5u6excqdunwlm4tw254cq943m6e6m"
	valB = "bzevaloper10dcm7hqv9a5ajdjr9ctcu5pgg2969ag7qx0nyg"
	del1 = "bze1qly96sy3yklkwv6cf4he5ves4qu8yn588vltqp"
	del2 = "bze164h5f2p7vw3dxx64qlvxvpcu7us39kvqsdvsry"
)

var t0 = time.Date(2026, 10, 8, 19, 0, 5, 0, time.UTC)

func ev(typ string, attrs ...string) transform.Event {
	e := transform.Event{Type: typ, Attrs: map[string]any{}}
	for i := 0; i+1 < len(attrs); i += 2 {
		e.Attrs[attrs[i]] = attrs[i+1]
	}
	return e
}

func ref(h int64) stakeflow.Ref {
	return stakeflow.Ref{Height: h, TxIndex: 1, MsgIndex: 2, TxHash: "AB", Time: t0}
}

func TestFromEventsReadsEveryMovementKind(t *testing.T) {
	got := stakeflow.FromEvents(ref(10), []transform.Event{
		ev("delegate", "validator", valA, "delegator", del1, "amount", "772573ubze", "authz_msg_index", "0", "new_shares", "772573.0"),
		ev("unbond", "validator", valA, "delegator", del2, "amount", "500ubze", "completion_time", "x"),
		ev("cancel_unbonding_delegation", "validator", valB, "delegator", del2, "amount", "40ubze", "creation_height", "9"),
		ev("withdraw_rewards", "validator", valA, "delegator", del1, "amount", "3ubze"),
	})

	require.Len(t, got, 3)
	assert.Equal(t, stakeflow.Movement{Height: 10, TxIndex: 1, MsgIndex: 2, Seq: 0, TxHash: "AB", Time: t0,
		Kind: stakeflow.KindDelegate, Validator: valA, Delegator: del1, Amount: 772573}, got[0])
	assert.Equal(t, stakeflow.KindUndelegate, got[1].Kind)
	assert.Equal(t, 1, got[1].Seq)
	assert.Equal(t, int64(-500), got[1].Delta())
	assert.Equal(t, stakeflow.KindCancelUnbonding, got[2].Kind)
	assert.Equal(t, valB, got[2].Validator)
	assert.Equal(t, int64(40), got[2].Delta())
}

func TestFromEventsSplitsARedelegation(t *testing.T) {
	// The events of a MsgBeginRedelegate as SDK 0.50 emits them: the
	// redelegate event has no delegator, the source's withdraw_rewards has.
	got := stakeflow.FromEvents(ref(10), []transform.Event{
		ev("withdraw_rewards", "amount", "0ubze", "validator", valA, "delegator", del1),
		ev("withdraw_rewards", "amount", "9ubze", "validator", valB, "delegator", del1),
		ev("redelegate", "source_validator", valA, "destination_validator", valB, "amount", "640ubze", "completion_time", "x"),
	})

	require.Len(t, got, 2)
	assert.Equal(t, stakeflow.KindRedelegateOut, got[0].Kind)
	assert.Equal(t, valA, got[0].Validator)
	assert.Equal(t, valB, got[0].Counterparty)
	assert.Equal(t, int64(-640), got[0].Delta())
	assert.Equal(t, stakeflow.KindRedelegateIn, got[1].Kind)
	assert.Equal(t, valB, got[1].Validator)
	assert.Equal(t, valA, got[1].Counterparty)
	assert.Equal(t, int64(640), got[1].Delta())
	assert.Equal(t, got[0].Seq, got[1].Seq)
	assert.Equal(t, del1, got[0].Delegator, "the account comes from the source's withdraw_rewards")
	assert.Equal(t, del1, got[1].Delegator)
}

func TestFromEventsNamesTheAccountOfEachAuthzRedelegation(t *testing.T) {
	// Two nested redelegations from the same source by different granters:
	// authz_msg_index pairs each with its own withdraw_rewards.
	got := stakeflow.FromEvents(ref(10), []transform.Event{
		ev("withdraw_rewards", "amount", "1ubze", "validator", valA, "delegator", del1, "authz_msg_index", "0"),
		ev("redelegate", "source_validator", valA, "destination_validator", valB, "amount", "5ubze", "authz_msg_index", "0"),
		ev("withdraw_rewards", "amount", "1ubze", "validator", valA, "delegator", del2, "authz_msg_index", "1"),
		ev("redelegate", "source_validator", valA, "destination_validator", valB, "amount", "7ubze", "authz_msg_index", "1"),
	})

	require.Len(t, got, 4)
	assert.Equal(t, []string{del1, del1, del2, del2},
		[]string{got[0].Delegator, got[1].Delegator, got[2].Delegator, got[3].Delegator})

	// Without a withdraw_rewards the account stays unknown.
	got = stakeflow.FromEvents(ref(10), []transform.Event{
		ev("redelegate", "source_validator", valA, "destination_validator", valB, "amount", "5ubze"),
	})
	require.Len(t, got, 2)
	assert.Empty(t, got[0].Delegator)
}

func TestFromEventsCreateValidatorIsTheOperatorsSelfStake(t *testing.T) {
	got := stakeflow.FromEvents(ref(10), []transform.Event{ev("create_validator", "validator", valA, "amount", "1000000ubze")})

	require.Len(t, got, 1)
	_, bz, err := bech32.DecodeAndConvert(valA)
	require.NoError(t, err)
	want, err := bech32.ConvertAndEncode("bze", bz)
	require.NoError(t, err)
	assert.Equal(t, stakeflow.KindCreate, got[0].Kind)
	assert.Equal(t, want, got[0].Delegator)
	assert.Equal(t, int64(1000000), got[0].Amount)
}

func TestFromEventsSkipsWhatIsNotABondMovement(t *testing.T) {
	got := stakeflow.FromEvents(ref(10), []transform.Event{
		ev("delegate", "validator", valA, "delegator", del1, "amount", "5uvdl"),
		ev("delegate", "validator", valA, "delegator", del1, "amount", "5ubze,3uvdl"),
		ev("delegate", "validator", valA, "delegator", del1, "amount", "0ubze"),
		ev("delegate", "validator", valA, "delegator", del1),
		ev("delegate", "delegator", del1, "amount", "5ubze"),
		ev("redelegate", "source_validator", valA, "delegator", del1, "amount", "5ubze"),
		ev("delegate", "validator", valA, "delegator", del1, "amount", "99999999999999999999ubze"),
	})
	assert.Empty(t, got)

	// A bare integer is the bond denom.
	got = stakeflow.FromEvents(ref(10), []transform.Event{ev("delegate", "validator", valA, "delegator", del1, "amount", "7")})
	require.Len(t, got, 1)
	assert.Equal(t, int64(7), got[0].Amount)
}

func TestFromEntitiesReadsTheFlushedMessages(t *testing.T) {
	ents := &transform.Entities{
		Blocks: []transform.Block{{Height: 10, Time: t0}},
		Transactions: []transform.Transaction{
			{Height: 10, TxIndex: 0, Hash: "OK", Time: t0, Success: true},
			{Height: 10, TxIndex: 1, Hash: "FAILED", Time: t0},
		},
		Messages: []transform.Message{
			// An authz MsgExec carries the delegations of its nested messages.
			{Height: 10, TxIndex: 0, MsgIndex: 0, TypeURL: "/cosmos.authz.v1beta1.MsgExec", Events: []transform.Event{
				ev("delegate", "validator", valA, "delegator", del1, "amount", "10ubze", "authz_msg_index", "0"),
				ev("delegate", "validator", valA, "delegator", del2, "amount", "20ubze", "authz_msg_index", "1"),
			}},
			{Height: 10, TxIndex: 1, MsgIndex: 0, Events: []transform.Event{
				ev("delegate", "validator", valA, "delegator", del1, "amount", "30ubze"),
			}},
		},
	}

	blocks, moves := stakeflow.FromEntities(ents)

	assert.Equal(t, []stakeflow.Block{{Height: 10, Time: t0}}, blocks)
	require.Len(t, moves, 2)
	assert.Equal(t, "OK", moves[0].TxHash)
	assert.Equal(t, []int64{10, 20}, []int64{moves[0].Amount, moves[1].Amount})

	blocks, moves = stakeflow.FromEntities(nil)
	assert.Empty(t, blocks)
	assert.Empty(t, moves)
}
