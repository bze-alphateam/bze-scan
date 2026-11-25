package transform_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

const callbackErrorPrefix = "ibccallbackerror-"

// asCallbackError is the event as ibc-go emits it in an error
// acknowledgement: the type and every attribute key prefixed (the message's
// own msg_index is added by the SDK afterwards and stays plain).
func asCallbackError(ev node.Event) node.Event {
	out := node.Event{Type: callbackErrorPrefix + ev.Type}
	for _, a := range ev.Attributes {
		out.Attributes = append(out.Attributes, node.Attribute{Key: callbackErrorPrefix + a.Key, Value: a.Value})
	}
	return withIndex(out, "1")
}

// plainEvent is an event of the recorded relay's message, as an attribute list.
func plainEvent(typ string, kv ...string) node.Event {
	return withIndex(node.Event{Type: typ, Attributes: attrs(kv...)}, "1")
}

// refusedInbound is the recorded MsgRecvPacket of 24999209 answered with an
// error acknowledgement of the given code (the write_acknowledgement of a
// v8.3.0 chain: the code only) and the events the refusing module emitted.
func refusedInbound(t *testing.T, ackCode string, events ...node.Event) transform.Input {
	t.Helper()
	in := fetchInput(t, fakenode.New(t), 24999209)
	recvTx := &in.Results.TxsResults[0]
	var writeAck node.Event
	for _, ev := range recvTx.Events {
		if ev.Type == "write_acknowledgement" {
			writeAck = ev
		}
	}
	require.NotEmpty(t, writeAck.Type)
	errorAck := node.Event{Type: writeAck.Type}
	for _, a := range writeAck.Attributes {
		switch a.Key {
		case "packet_ack":
			a.Value = `{"error":"ABCI code: ` + ackCode + `: error handling packet: see events for details"}`
		case "packet_ack_hex":
			continue
		}
		errorAck.Attributes = append(errorAck.Attributes, a)
	}
	replaceEvents(recvTx, "1", func(ev node.Event) bool { return ev.Type == "recv_packet" }, append(events, errorAck)...)
	return in
}

func inboundRow(t *testing.T, in transform.Input) transform.IBCTransfer {
	t.Helper()
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	return byKey(t, ents.IBCTransfers, "channel-0", 10973, transform.IBCIn)
}

func rateLimitDenied(reason, ackAction string) node.Event {
	return plainEvent("transfer_denied", "module", "ratelimit", "reason", reason, "action", ackAction,
		"denom", "transfer/channel-340/ubze", "channel_or_client", "channel-0", "amount", "5051266516",
		"error", "exceeds quota")
}

func blockedInboundEvent() node.Event {
	return typed("bze.txfeecollector.BlockedIbcInboundEvent", "1", "channel", "channel-0",
		"denom", "transfer/channel-340/ubze", "amount", "5051266516",
		"sender", "osmo1m8wg4vxkefhs374qxmmqpyusgz289wmulex5qdwpfx7jnrxzer5s9cv83q", "receiver", ibcUser)
}

// The events of an error acknowledgement reach the block results prefixed:
// the chain's typed event must still be read.
func TestPrefixedBlockedInboundIsRejected(t *testing.T) {
	r := inboundRow(t, refusedInbound(t, "1101", asCallbackError(blockedInboundEvent())))
	assert.Equal(t, transform.IBCRejected, r.Status)
	assert.Equal(t, transform.IBCBlockedInbound, r.AckError)
	require.NotNil(t, r.ResultHeight)
}

func TestInboundRefusals(t *testing.T) {
	for _, c := range []struct {
		name string
		code string
		evs  []node.Event
		want string
	}{
		{"rate limit, prefixed", "4", []node.Event{asCallbackError(rateLimitDenied("rate_limit_exceeded", "packet_recv"))}, transform.IBCRateLimited},
		{"rate limit, plain", "4", []node.Event{rateLimitDenied("rate_limit_exceeded", "packet_recv")}, transform.IBCRateLimited},
		{"blacklisted denom, prefixed", "7", []node.Event{asCallbackError(rateLimitDenied("blacklisted_denom", "packet_recv"))}, transform.IBCBlockedDenom},
		{"blacklisted denom, plain", "7", []node.Event{rateLimitDenied("blacklisted_denom", "packet_recv")}, transform.IBCBlockedDenom},
		{"closed channel, no event", "1140", nil, transform.IBCChannelClosed},
		{"blocked inbound, plain", "1101", []node.Event{blockedInboundEvent()}, transform.IBCBlockedInbound},
		{"another module's transfer_denied", "4", []node.Event{
			plainEvent("transfer_denied", "module", "other", "reason", "rate_limit_exceeded"),
		}, "ABCI code: 4: error handling packet: see events for details"},
		{"unknown reason keeps the ack text", "4", []node.Event{
			asCallbackError(rateLimitDenied("something_new", "packet_recv")),
		}, "ABCI code: 4: error handling packet: see events for details"},
		{"another error ack keeps its text", "1102", nil, "ABCI code: 1102: error handling packet: see events for details"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := inboundRow(t, refusedInbound(t, c.code, c.evs...))
			assert.Equal(t, transform.IBCRejected, r.Status)
			assert.Equal(t, c.want, r.AckError)
			require.NotNil(t, r.ResultHeight)
		})
	}
}

// A refusal BZE names wins over the text the transfer module also reports.
func TestRefusalWinsOverTheTransferModuleError(t *testing.T) {
	r := inboundRow(t, refusedInbound(t, "4",
		asCallbackError(rateLimitDenied("rate_limit_exceeded", "packet_recv")),
		asCallbackError(plainEvent("fungible_token_packet", "module", "transfer", "success", "false", "error", "ABCI code: 4: exceeds quota")),
	))
	assert.Equal(t, transform.IBCRateLimited, r.AckError)
}

// ibc-go v10 shapes: the deprecated non-hex packet_data/packet_ack are gone,
// the transfer module's events carry what v10's events.go emits. Each case
// must give the row of its v8 counterpart.
func withoutDeprecatedPacketKeys(in transform.Input) {
	for i := range in.Results.TxsResults {
		for j, ev := range in.Results.TxsResults[i].Events {
			var kept []node.Attribute
			for _, a := range ev.Attributes {
				if a.Key == "packet_data" || a.Key == "packet_ack" {
					continue
				}
				kept = append(kept, a)
			}
			in.Results.TxsResults[i].Events[j].Attributes = kept
		}
	}
}

func TestIBCV10ShapesGiveTheRowsOfV8(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	for _, h := range []int64{24999205, 24999209} {
		want, err := tr.Transform(fetchInput(t, n, h))
		require.NoError(t, err)
		require.NotEmpty(t, want.IBCTransfers)
		in := fetchInput(t, n, h)
		withoutDeprecatedPacketKeys(in)
		got, err := tr.Transform(in)
		require.NoError(t, err)
		assert.Equal(t, want.IBCTransfers, got.IBCTransfers, "height %d", h)
	}

	t.Run("recv refused by the transfer app itself", func(t *testing.T) {
		in := refusedInbound(t, "1",
			asCallbackError(plainEvent("fungible_token_packet", "sender", "osmo1m8wg4vxkefhs374qxmmqpyusgz289wmulex5qdwpfx7jnrxzer5s9cv83q",
				"receiver", ibcUser, "denom", "transfer/channel-340/ubze", "amount", "5051266516", "memo", "",
				"success", "false", "error", "invalid receiver")),
			asCallbackError(plainEvent("message", "module", "transfer")),
		)
		withoutDeprecatedPacketKeys(in)
		r := inboundRow(t, in)
		assert.Equal(t, transform.IBCRejected, r.Status)
		assert.Equal(t, "invalid receiver", r.AckError)
	})

	t.Run("ack error", func(t *testing.T) {
		in := fetchInput(t, n, 24999209)
		replaceEvents(&in.Results.TxsResults[1], "1", func(ev node.Event) bool {
			return ev.Type == "acknowledge_packet"
		},
			plainEvent("fungible_token_packet", "sender", "bze1mjpllwcpca0t2dmnr3wez2ugkr5l07lf62a3",
				"receiver", "noble1mjpllwcpca0t2dmnr3wez2ugkr5l07lf0d", "denom", "transfer/channel-3/uusdc",
				"amount", "957617", "memo", "", "acknowledgement", `error:"invalid receiver"`),
			plainEvent("fungible_token_packet", "error", "invalid receiver"))
		withoutDeprecatedPacketKeys(in)
		ents, err := tr.Transform(in)
		require.NoError(t, err)
		r := byKey(t, ents.IBCTransfers, "channel-3", 5437, transform.IBCOut)
		assert.Equal(t, transform.IBCRefundedRejected, r.Status)
		assert.Equal(t, "invalid receiver", r.AckError)
		assert.True(t, r.Resolves)
	})

	t.Run("timeout with refund_tokens", func(t *testing.T) {
		want, err := tr.Transform(fetchInput(t, n, 20237220))
		require.NoError(t, err)
		in := fetchInput(t, n, 20237220)
		for i := range in.Results.TxsResults {
			for j, ev := range in.Results.TxsResults[i].Events {
				if ev.Type != "timeout" {
					continue
				}
				memo, _ := ev.Get("memo")
				recv, _ := ev.Get("refund_receiver")
				amount, _ := ev.Get("refund_amount")
				denom, _ := ev.Get("refund_denom")
				in.Results.TxsResults[i].Events[j].Attributes = attrs("module", "transfer", "receiver", recv,
					"refund_tokens", amount+denom, "memo", memo)
			}
		}
		withoutDeprecatedPacketKeys(in)
		got, err := tr.Transform(in)
		require.NoError(t, err)
		require.Len(t, got.IBCTransfers, 2)
		assert.Equal(t, want.IBCTransfers, got.IBCTransfers)
		assert.True(t, strings.HasPrefix(got.IBCTransfers[0].Status, "refunded_"))
	})
}
