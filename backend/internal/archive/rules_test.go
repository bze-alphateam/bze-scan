package archive_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/archive"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// legacyHeight is any height of the legacy generation.
const legacyHeight = 15_000_000

// mockDecoder answers every Decode with tx, or err.
type mockDecoder struct {
	tx  *chain.Tx
	err error
}

func (d mockDecoder) Decode([]byte) (*chain.Tx, error) {
	return d.tx, d.err
}

func ev(typ string, kv ...string) node.Event {
	e := node.Event{Type: typ, Attributes: []node.Attribute{}}
	for i := 0; i+1 < len(kv); i += 2 {
		e.Attributes = append(e.Attributes, node.Attribute{Key: kv[i], Value: kv[i+1]})
	}
	return e
}

// ante is the ante-handler prefix of a legacy transaction.
func ante() []node.Event {
	return []node.Event{
		ev("transfer", "recipient", "bze1fee", "sender", "bze1alice", "amount", "10ubze"),
		ev("message", "sender", "bze1alice"),
		ev("tx", "fee", "10ubze", "fee_payer", "bze1alice"),
	}
}

func input(code uint32, events ...node.Event) *transform.Input {
	raw := base64.StdEncoding.EncodeToString([]byte("tx"))
	return &transform.Input{
		Block: &node.Block{Height: legacyHeight, Txs: []string{raw}},
		Results: &node.BlockResults{
			Height:     legacyHeight,
			TxsResults: []node.TxResult{{Code: code, Events: events}},
		},
	}
}

func adapt(tx *chain.Tx, in *transform.Input) error {
	return archive.New(mockDecoder{tx: tx}).Adapt(legacyHeight, in)
}

func body(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func events(in *transform.Input) []node.Event {
	return in.Results.TxsResults[0].Events
}

func TestMsgIndexIsReconstructedFromMessageOrder(t *testing.T) {
	tx := &chain.Tx{Msgs: []chain.Msg{
		{TypeURL: "/bze.tradebin.MsgCreateOrder", Signer: "bze1alice"},
		{TypeURL: "/cosmos.bank.v1beta1.MsgSend", Signer: "bze1alice"},
	}}
	in := input(0, append(ante(),
		ev("message", "action", "create_order"),
		ev("transfer", "sender", "bze1alice", "amount", "1ubze"),
		ev("message", "action", "/cosmos.bank.v1beta1.MsgSend"),
		ev("transfer", "sender", "bze1alice", "amount", "2ubze"),
		ev("message", "module", "bank"),
	)...)

	require.NoError(t, adapt(tx, in))
	got := events(in)
	for _, e := range got[:3] {
		_, ok := e.Get("msg_index")
		assert.False(t, ok, "the ante handler's events belong to no message")
	}
	assert.Equal(t, ev("message", "action", "/bze.tradebin.MsgCreateOrder", "sender", "bze1alice", "module", "tradebin", "msg_index", "0"), got[3])
	assert.Equal(t, ev("transfer", "sender", "bze1alice", "amount", "1ubze", "msg_index", "0"), got[4])
	assert.Equal(t, ev("message", "action", "/cosmos.bank.v1beta1.MsgSend", "sender", "bze1alice", "msg_index", "1"), got[5],
		"no module: the message's own events carry one, as SDK 0.50 decides")
	assert.Equal(t, ev("message", "module", "bank", "msg_index", "1"), got[7])
}

func TestActionOfAnOldProtoPackage(t *testing.T) {
	tx := &chain.Tx{Msgs: []chain.Msg{{TypeURL: "/bze.rewards.MsgJoinStaking", Signer: "bze1alice"}}}
	in := input(0, append(ante(), ev("message", "action", "/bze.v1.rewards.MsgJoinStaking"))...)

	require.NoError(t, adapt(tx, in))
	action, _ := events(in)[3].Get("action")
	assert.Equal(t, "/bze.rewards.MsgJoinStaking", action)
}

func TestScavengeKeepsItsTypeURL(t *testing.T) {
	tx := &chain.Tx{Msgs: []chain.Msg{{TypeURL: "/bzedgev5.scavenge.MsgSubmitScavenge", Err: errors.New("unknown type")}}}
	in := input(0, append(ante(), ev("message", "action", "SubmitScavenge"))...)

	require.NoError(t, adapt(tx, in))
	assert.Equal(t, ev("message", "action", "/bzedgev5.scavenge.MsgSubmitScavenge", "module", "scavenge", "msg_index", "0"), events(in)[3])
}

func TestTypedEventsAreRenamed(t *testing.T) {
	tx := &chain.Tx{Msgs: []chain.Msg{{TypeURL: "/bze.tradebin.MsgCreateOrder", Signer: "bze1alice"}}}
	in := input(0, append(ante(),
		ev("message", "action", "create_order"),
		ev("bze.tradebin.v1.OrderCreateMessageEvent", "amount", `"10"`, "creator", `"bze1alice"`),
	)...)
	in.Results.FinalizeBlockEvents = []node.Event{ev("bze.tradebin.v1.OrderExecutedEvent", "price", `"1"`, "mode", "EndBlock")}

	require.NoError(t, adapt(tx, in))
	assert.Equal(t, "bze.tradebin.OrderCreateMessageEvent", events(in)[4].Type)
	assert.Equal(t, ev("bze.tradebin.OrderExecutedEvent", "price", `"1"`, "mode", "EndBlock"), in.Results.FinalizeBlockEvents[0])
}

func TestVoteGetsItsVoterAndTheOptionArray(t *testing.T) {
	tx := &chain.Tx{Msgs: []chain.Msg{{
		TypeURL: "/cosmos.gov.v1beta1.MsgVoteWeighted", Signer: "bze1alice",
		Body: body(t, map[string]any{"proposal_id": "44", "voter": "bze1alice"}),
	}}}
	in := input(0, append(ante(),
		ev("message", "action", "/cosmos.gov.v1beta1.MsgVoteWeighted"),
		ev("proposal_vote", "option", "{\"option\":1,\"weight\":\"0.7\"}\n{\"option\":3,\"weight\":\"0.3\"}", "proposal_id", "44"),
		ev("message", "module", "governance", "sender", "bze1alice"),
	)...)

	require.NoError(t, adapt(tx, in))
	assert.Equal(t, ev("proposal_vote", "voter", "bze1alice",
		"option", `[{"option":1,"weight":"0.7"},{"option":3,"weight":"0.3"}]`, "proposal_id", "44", "msg_index", "0"), events(in)[4])
}

func TestVotesInsideAnAuthzExecArePairedInOrder(t *testing.T) {
	exec := map[string]any{"grantee": "bze1bot", "msgs": []any{
		map[string]any{"@type": "/cosmos.gov.v1beta1.MsgVote", "voter": "bze1alice"},
		map[string]any{"@type": "/cosmos.bank.v1beta1.MsgSend"},
		map[string]any{"@type": "/cosmos.gov.v1beta1.MsgVote", "voter": "bze1bob"},
	}}
	tx := &chain.Tx{Msgs: []chain.Msg{{TypeURL: "/cosmos.authz.v1beta1.MsgExec", Signer: "bze1bot", Body: body(t, exec)}}}
	in := input(0, append(ante(),
		ev("message", "action", "/cosmos.authz.v1beta1.MsgExec"),
		ev("proposal_vote", "option", `{"option":1,"weight":"1"}`, "proposal_id", "1"),
		ev("proposal_vote", "option", `{"option":2,"weight":"1"}`, "proposal_id", "1"),
	)...)

	require.NoError(t, adapt(tx, in))
	alice, _ := events(in)[4].Get("voter")
	bob, _ := events(in)[5].Get("voter")
	assert.Equal(t, []string{"bze1alice", "bze1bob"}, []string{alice, bob})
}

func TestDepositorAndProposerOfAProposal(t *testing.T) {
	tx := &chain.Tx{Msgs: []chain.Msg{
		{TypeURL: "/cosmos.gov.v1beta1.MsgSubmitProposal", Signer: "bze1alice", Body: body(t, map[string]any{"proposer": "bze1alice"})},
		{TypeURL: "/cosmos.gov.v1beta1.MsgDeposit", Signer: "bze1bob", Body: body(t, map[string]any{"depositor": "bze1bob"})},
	}}
	in := input(0, append(ante(),
		ev("message", "action", "/cosmos.gov.v1beta1.MsgSubmitProposal"),
		ev("submit_proposal", "proposal_id", "44"),
		ev("proposal_deposit", "amount", "", "proposal_id", "44"),
		ev("submit_proposal", "proposal_type", "SoftwareUpgrade"),
		ev("message", "action", "/cosmos.gov.v1beta1.MsgDeposit"),
		ev("proposal_deposit", "amount", "5ubze", "proposal_id", "44"),
		ev("proposal_deposit", "voting_period_start", "44"),
	)...)

	require.NoError(t, adapt(tx, in))
	got := events(in)
	assert.Equal(t, ev("submit_proposal", "proposal_id", "44", "proposer", "bze1alice", "msg_index", "0"), got[4])
	assert.Equal(t, ev("proposal_deposit", "depositor", "bze1alice", "amount", "", "proposal_id", "44", "msg_index", "0"), got[5])
	assert.Equal(t, ev("submit_proposal", "proposal_type", "SoftwareUpgrade", "msg_index", "0"), got[6])
	assert.Equal(t, ev("proposal_deposit", "depositor", "bze1bob", "amount", "5ubze", "proposal_id", "44", "msg_index", "1"), got[8])
	assert.Equal(t, ev("proposal_deposit", "voting_period_start", "44", "msg_index", "1"), got[9])
}

func TestIBCTransferGetsAmountDenomAndMemo(t *testing.T) {
	transfer := map[string]any{"token": map[string]any{"denom": "ubze", "amount": "7"}, "receiver": "osmo1x"}
	tx := &chain.Tx{Msgs: []chain.Msg{{TypeURL: "/ibc.applications.transfer.v1.MsgTransfer", Signer: "bze1alice", Body: body(t, transfer)}}}
	in := input(0, append(ante(),
		ev("message", "action", "/ibc.applications.transfer.v1.MsgTransfer"),
		ev("ibc_transfer", "sender", "bze1alice", "receiver", "osmo1x"),
		ev("message", "module", "transfer"),
	)...)

	require.NoError(t, adapt(tx, in))
	assert.Equal(t, ev("ibc_transfer", "sender", "bze1alice", "receiver", "osmo1x",
		"amount", "7", "denom", "ubze", "memo", "", "msg_index", "0"), events(in)[4], "no memo before ibc-go 4")
}

func TestAFailedTransactionKeepsItsAnteEvents(t *testing.T) {
	in := input(5, ante()...)
	want := ante()
	require.NoError(t, archive.New(mockDecoder{err: errors.New("never called")}).Adapt(legacyHeight, in))
	assert.Equal(t, want, events(in))
}

func TestMismatchesAreUnknownGenerations(t *testing.T) {
	createOrder := &chain.Tx{Msgs: []chain.Msg{{TypeURL: "/bze.tradebin.MsgCreateOrder", Signer: "bze1alice"}}}
	vote := &chain.Tx{Msgs: []chain.Msg{{TypeURL: "/cosmos.gov.v1beta1.MsgVote", Body: body(t, map[string]any{"voter": "bze1alice"})}}}
	cases := []struct {
		name string
		tx   *chain.Tx
		in   *transform.Input
	}{
		{"more messages than message events", createOrder, input(0, ante()...)},
		{"an action naming another message", createOrder, input(0, append(ante(), ev("message", "action", "cancel_order"))...)},
		{"an unknown legacy action", createOrder, input(0, append(ante(), ev("message", "action", "make_order"))...)},
		{"msg_index already there", createOrder, input(0, append(ante(), ev("message", "action", "create_order", "msg_index", "0"))...)},
		{"an unknown typed event", createOrder, input(0, append(ante(), ev("message", "action", "create_order"), ev("bze.tradebin.v1.Unknown", "a", `"1"`))...)},
		{"a typed value that is not JSON", createOrder, input(0, append(ante(), ev("message", "action", "create_order"), ev("bze.tradebin.v1.OrderSavedEvent", "price", "1.5x"))...)},
		{"message events in a failed transaction", createOrder, input(5, append(ante(), ev("message", "action", "create_order"))...)},
		{"two votes for one MsgVote", vote, input(0, append(ante(), ev("message", "action", "/cosmos.gov.v1beta1.MsgVote"),
			ev("proposal_vote", "option", `{"option":1,"weight":"1"}`), ev("proposal_vote", "option", `{"option":1,"weight":"1"}`))...)},
		{"a vote option that is not JSON", vote, input(0, append(ante(), ev("message", "action", "/cosmos.gov.v1beta1.MsgVote"),
			ev("proposal_vote", "option", "option:VOTE_OPTION_YES"))...)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.ErrorIs(t, adapt(c.tx, c.in), archive.ErrUnknownGeneration)
		})
	}
}

func TestPreCometBFT038LayoutIsRejected(t *testing.T) {
	for _, h := range []int64{legacyHeight, 25_000_000} {
		in := input(0)
		in.Results.BeginBlockEvents = []node.Event{ev("mint", "amount", "1")}
		assert.ErrorIs(t, archive.New(mockDecoder{}).Adapt(h, in), archive.ErrUnknownGeneration, "height %d", h)
	}
}

func TestAnUndecodableTransactionFailsTheHeight(t *testing.T) {
	in := input(0, append(ante(), ev("message", "action", "create_order"))...)
	err := archive.New(mockDecoder{err: errors.New("not a transaction")}).Adapt(legacyHeight, in)
	assert.ErrorContains(t, err, "not a transaction")
}

func TestTransactionsWithoutResults(t *testing.T) {
	in := input(0)
	in.Results.TxsResults = nil
	assert.Error(t, adapt(&chain.Tx{}, in))
}
