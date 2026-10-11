package transform_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// Heights without cross-chain transfers have no golden file.
func TestIBCGolden(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	var withRows []int64
	for _, h := range n.FixtureHeights() {
		t.Run(fmt.Sprint(h), func(t *testing.T) {
			ents, err := tr.Transform(fetchInput(t, n, h))
			require.NoError(t, err)
			file := filepath.Join("testdata", fmt.Sprintf("ibc_%d.golden.json", h))
			if len(ents.IBCTransfers) == 0 {
				_, err := os.Stat(file)
				assert.True(t, os.IsNotExist(err), "no rows, no golden file")
				return
			}
			withRows = append(withRows, h)
			got, err := json.MarshalIndent(ents.IBCTransfers, "", "  ")
			require.NoError(t, err)
			got = append(got, '\n')
			if *update {
				require.NoError(t, os.WriteFile(file, got, 0o644))
			}
			want, err := os.ReadFile(file)
			require.NoError(t, err)
			assert.JSONEq(t, string(want), string(got))
		})
	}
	assert.Equal(t, []int64{4002550, 4002556, 10002143, 20235036, 20237220, 24999205, 24999209}, withRows,
		"the recorded heights with cross-chain transfers")
}

const (
	usdcVoucher = "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4"
	ibcUser     = "bze1mjpllwcpca0t2dmnr3wez2ugkr5l07lf62a32y"
)

// byKey finds the row of a packet.
func byKey(t *testing.T, rows []transform.IBCTransfer, channel string, seq uint64, dir string) transform.IBCTransfer {
	t.Helper()
	i := slices.IndexFunc(rows, func(r transform.IBCTransfer) bool {
		return r.ChannelID == channel && r.Sequence == seq && r.Direction == dir
	})
	require.GreaterOrEqual(t, i, 0, "%s/%d/%s", channel, seq, dir)
	return rows[i]
}

// 24999205 sends USDC to Noble over channel-3 (sequence 5437) and 24999209
// carries its acknowledgement, with an inbound ubze transfer in the same
// block; 20237220 (chain v7, through the archive adapter) times out two
// transfers.
func TestRecordedTransferLifecycle(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)

	sent, err := tr.Transform(fetchInput(t, n, 24999205))
	require.NoError(t, err)
	require.Len(t, sent.IBCTransfers, 1)
	out := sent.IBCTransfers[0]
	assert.Equal(t, "channel-3", out.ChannelID)
	assert.Equal(t, uint64(5437), out.Sequence)
	assert.Equal(t, transform.IBCOut, out.Direction)
	assert.Equal(t, transform.IBCPending, out.Status)
	assert.Equal(t, ibcUser, out.Sender)
	assert.Equal(t, "noble1mjpllwcpca0t2dmnr3wez2ugkr5l07lf0dyjqa", out.Receiver)
	assert.Equal(t, usdcVoucher, out.Denom, "the BZE denom of the packet's trace, as ibc_transfer names it")
	assert.Equal(t, "957617", out.Amount)
	assert.Contains(t, out.Memo, `"forward"`)
	assert.Empty(t, out.TimeoutHeight, `"0-0" is no timeout height`)
	require.NotNil(t, out.TimeoutTime)
	assert.Equal(t, time.Unix(0, 1791479095283079700).UTC(), *out.TimeoutTime)
	assert.Nil(t, out.ResultHeight)
	assert.False(t, out.Resolves)
	assert.Equal(t, sent.Transactions[out.CreatedTxIndex].Hash, out.CreatedTxHash)

	settled, err := tr.Transform(fetchInput(t, n, 24999209))
	require.NoError(t, err)
	ack := byKey(t, settled.IBCTransfers, "channel-3", 5437, transform.IBCOut)
	assert.Equal(t, transform.IBCDelivered, ack.Status, "success carries the ack result, here a wasm hook's JSON")
	assert.True(t, ack.Resolves)
	assert.Empty(t, ack.AckError)
	require.NotNil(t, ack.ResultHeight)
	assert.Equal(t, int64(24999209), *ack.ResultHeight)
	assert.Equal(t, ack.CreatedTxHash, ack.ResultTxHash)
	// The acknowledgement carries the whole packet: the row it creates when
	// the send is not stored yet holds the same packet columns.
	for _, pair := range [][2]any{
		{out.Sender, ack.Sender}, {out.Receiver, ack.Receiver}, {out.Denom, ack.Denom}, {out.Amount, ack.Amount},
		{out.Memo, ack.Memo}, {out.TimeoutHeight, ack.TimeoutHeight}, {*out.TimeoutTime, *ack.TimeoutTime},
	} {
		assert.Equal(t, pair[0], pair[1])
	}

	in := byKey(t, settled.IBCTransfers, "channel-0", 10973, transform.IBCIn)
	assert.Equal(t, transform.IBCReceived, in.Status)
	assert.Equal(t, "osmo1m8wg4vxkefhs374qxmmqpyusgz289wmulex5qdwpfx7jnrxzer5s9cv83q", in.Sender)
	assert.Equal(t, ibcUser, in.Receiver)
	assert.Equal(t, "transfer/channel-340/ubze", in.Denom, "the packet denom before BZE's ibc/ hash")
	assert.Equal(t, "5051266516", in.Amount)
	assert.False(t, in.Resolves)
	require.NotNil(t, in.ResultHeight)
	assert.Equal(t, in.CreatedTxHash, in.ResultTxHash)

	timedOut, err := tr.Transform(fetchInput(t, n, 20237220))
	require.NoError(t, err)
	require.Len(t, timedOut.IBCTransfers, 2)
	for i, seq := range []uint64{10650, 10651} {
		r := timedOut.IBCTransfers[i]
		assert.Equal(t, "channel-0", r.ChannelID)
		assert.Equal(t, seq, r.Sequence)
		assert.Equal(t, transform.IBCRefundedTimeout, r.Status)
		assert.True(t, r.Resolves)
		assert.Equal(t, "ubze", r.Denom)
		assert.Equal(t, ibcUser, r.Sender)
	}
	assert.Equal(t, "21730924671", timedOut.IBCTransfers[0].Amount)
}

// replaceEvents swaps the events of one message of a transaction.
func replaceEvents(res *node.TxResult, msgIndex string, keep func(node.Event) bool, add ...node.Event) {
	var out []node.Event
	for _, ev := range res.Events {
		if v, _ := ev.Get("msg_index"); v == msgIndex && !keep(ev) {
			continue
		}
		out = append(out, ev)
	}
	res.Events = append(out, add...)
}

// No recorded height has a rejected acknowledgement: this one is the
// recorded acknowledgement of 24999209 with the transfer module's outcome
// turned into the error the other chain would have answered.
func TestRejectedAcknowledgementRefunds(t *testing.T) {
	n := fakenode.New(t)
	in := fetchInput(t, n, 24999209)
	ackTx := &in.Results.TxsResults[1]
	replaceEvents(ackTx, "1", func(ev node.Event) bool {
		_, success := ev.Get("success")
		return ev.Type != "fungible_token_packet" || !success
	}, withIndex(node.Event{Type: "fungible_token_packet", Attributes: attrs("error", "ABCI code: 1: error handling packet")}, "1"))

	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	r := byKey(t, ents.IBCTransfers, "channel-3", 5437, transform.IBCOut)
	assert.Equal(t, transform.IBCRefundedRejected, r.Status)
	assert.Equal(t, "ABCI code: 1: error handling packet", r.AckError)
	assert.True(t, r.Resolves)
}

// No recorded height has a blocked inbound transfer (mainnet runs v8.1.1,
// which has no inbound block): this one is the recorded MsgRecvPacket of
// 24999209 answered as chain v8.2.0's txfeecollector middleware answers a
// blocked denom, with its typed event and an error acknowledgement instead
// of the transfer module's events.
func TestBlockedInboundIsRejected(t *testing.T) {
	n := fakenode.New(t)
	in := fetchInput(t, n, 24999209)
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
			a.Value = `{"error":"ABCI code: 1102: error handling packet: see events for details"}`
		case "packet_ack_hex":
			continue
		}
		errorAck.Attributes = append(errorAck.Attributes, a)
	}
	replaceEvents(recvTx, "1", func(ev node.Event) bool { return ev.Type == "recv_packet" },
		typed("bze.txfeecollector.BlockedIbcInboundEvent", "1", "channel", "channel-0",
			"denom", "transfer/channel-340/ubze", "amount", "5051266516",
			"sender", "osmo1m8wg4vxkefhs374qxmmqpyusgz289wmulex5qdwpfx7jnrxzer5s9cv83q", "receiver", ibcUser),
		errorAck)

	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	r := byKey(t, ents.IBCTransfers, "channel-0", 10973, transform.IBCIn)
	assert.Equal(t, transform.IBCRejected, r.Status)
	assert.Equal(t, transform.IBCBlockedInbound, r.AckError)
	require.NotNil(t, r.ResultHeight)
}

// An inbound packet the transfer module answered with an error ack (a
// receiver it cannot pay, say) is rejected with that error.
func TestInboundErrorAckIsRejected(t *testing.T) {
	n := fakenode.New(t)
	in := fetchInput(t, n, 24999209)
	replaceEvents(&in.Results.TxsResults[0], "1", func(ev node.Event) bool {
		return ev.Type != "fungible_token_packet" && ev.Type != "transfer"
	}, withIndex(node.Event{Type: "fungible_token_packet", Attributes: attrs("module", "transfer", "success", "false",
		"error", "invalid receiver")}, "1"))

	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	r := byKey(t, ents.IBCTransfers, "channel-0", 10973, transform.IBCIn)
	assert.Equal(t, transform.IBCRejected, r.Status)
	assert.Equal(t, "invalid receiver", r.AckError)
}

// A failed transaction sent and settled nothing.
func TestFailedRelayHasNoTransfers(t *testing.T) {
	n := fakenode.New(t)
	in := fetchInput(t, n, 24999209)
	in.Results.TxsResults[0].Code = 11
	in.Results.TxsResults[1].Code = 11
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	assert.Empty(t, ents.IBCTransfers)
}

// Packets of other ports (interchain accounts, say) are not ICS-20
// transfers.
func TestOtherPortsAreNotTransfers(t *testing.T) {
	n := fakenode.New(t)
	in := fetchInput(t, n, 24999205)
	for i, ev := range in.Results.TxsResults[0].Events {
		if ev.Type != "send_packet" {
			continue
		}
		for j, a := range ev.Attributes {
			if a.Key == "packet_src_port" {
				in.Results.TxsResults[0].Events[i].Attributes[j].Value = "icacontroller-bze1x"
			}
		}
	}
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	assert.Empty(t, ents.IBCTransfers)
}

func TestChannelEventsMarkTheChannels(t *testing.T) {
	n := fakenode.New(t)
	in := fetchInput(t, n, 24999205)
	ents, err := realTransformer(t).Transform(in)
	require.NoError(t, err)
	assert.Empty(t, ents.Dirty.Keys(statesync.Channels))

	in.Results.TxsResults[0].Events = append(in.Results.TxsResults[0].Events,
		withIndex(node.Event{Type: "channel_open_confirm", Attributes: attrs("port_id", "transfer", "channel_id", "channel-14")}, "0"))
	ents, err = realTransformer(t).Transform(in)
	require.NoError(t, err)
	assert.Equal(t, []string{statesync.All}, ents.Dirty.Keys(statesync.Channels))
}
