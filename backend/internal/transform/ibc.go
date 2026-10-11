package transform

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	"github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// A cross-chain transfer is followed from BZE's own events: the user's
// MsgTransfer sends the packet, and the relayer that carries it brings the
// other chain's acknowledgement, or the timeout, back to BZE as a
// transaction. An inbound transfer is one MsgRecvPacket. No remote chain is
// ever queried.

// Directions of an ibc_transfers row.
const (
	IBCOut = "out"
	IBCIn  = "in"
)

// Statuses of an ibc_transfers row: out is pending until its
// acknowledgement or timeout, in is final at once.
const (
	IBCPending          = "pending"
	IBCDelivered        = "delivered"
	IBCRefundedRejected = "refunded_rejected"
	IBCRefundedTimeout  = "refunded_timeout"
	IBCReceived         = "received"
	IBCRejected         = "rejected"
)

// IBCBlockedInbound is the ack_error of an inbound transfer BZE refused
// because its denom is blocked.
const IBCBlockedInbound = "blocked inbound denom"

// Events of the transfer lifecycle (ibc-go v8).
const (
	evSendPacket        = "send_packet"
	evRecvPacket        = "recv_packet"
	evAcknowledgePacket = "acknowledge_packet"
	evTimeoutPacket     = "timeout_packet"
	evFungibleToken     = "fungible_token_packet"
	evWriteAck          = "write_acknowledgement"
	evBlockedIBCInbound = "bze.txfeecollector.BlockedIbcInboundEvent"
)

// channelEvents change a channel's state: the channels set resyncs.
var channelEvents = map[string]bool{
	"channel_open_init": true, "channel_open_try": true, "channel_open_ack": true, "channel_open_confirm": true,
	"channel_close_init": true, "channel_close_confirm": true, "channel_closed": true,
}

// IBCTransfer is one explorer.ibc_transfers row, keyed by the BZE-side
// channel, the packet sequence and the direction.
type IBCTransfer struct {
	ChannelID string `json:"channel_id"`
	Sequence  uint64 `json:"sequence"`
	Direction string `json:"direction"`
	Status    string `json:"status"`
	Sender    string `json:"sender"`
	Receiver  string `json:"receiver"`
	// Denom is the BZE denom sent (out), or the packet denom before BZE's
	// ibc/ hash (in).
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
	Memo   string `json:"memo"`
	// Created* are the transaction that created the row: the MsgTransfer
	// (out) or the MsgRecvPacket (in), or the acknowledgement or timeout
	// when the row comes from the result side.
	CreatedHeight  int64     `json:"created_height"`
	CreatedTxIndex int       `json:"created_tx_index"`
	CreatedTxHash  string    `json:"created_tx_hash"`
	CreatedTime    time.Time `json:"created_time"`
	// TimeoutHeight is "revision-height", empty for none; TimeoutTime nil
	// for none.
	TimeoutHeight string     `json:"timeout_height"`
	TimeoutTime   *time.Time `json:"timeout_time"`
	// Result* are the transaction that settled the row, on every final
	// status.
	ResultHeight *int64     `json:"result_height"`
	ResultTxHash string     `json:"result_tx_hash"`
	ResultTime   *time.Time `json:"result_time"`
	AckError     string     `json:"ack_error"`
	// Resolves marks a row from the result side (an acknowledgement or a
	// timeout): it settles a pending row and creates the row only when the
	// send is not stored yet (the backfill walks down).
	Resolves bool `json:"resolves"`
}

// packetBody is the part of a MsgAcknowledgement, MsgTimeout or
// MsgTimeoutOnClose body the rows need: the packet it settles.
type packetBody struct {
	Packet struct {
		Data []byte `json:"data"`
	} `json:"packet"`
}

// markChannels marks the channels set when an event changes a channel.
func markChannels(d *statesync.Dirty, ev node.Event) {
	if channelEvents[ev.Type] {
		d.Mark(statesync.Channels, statesync.All)
	}
}

// txIBCTransfers appends the ibc_transfers rows of a successful
// transaction's messages.
func txIBCTransfers(ents *Entities, b Block, txIndex int, hash string, msgs []chain.Msg,
	byIndex map[int][]node.Event, log logrus.FieldLogger) {
	// A message the codec could not decode still has its events.
	n := len(msgs)
	for j := range byIndex {
		n = max(n, j+1)
	}
	for j := range n {
		events := byIndex[j]
		var body json.RawMessage
		if j < len(msgs) {
			body = msgs[j].Body
		}
		for _, ev := range events {
			markChannels(&ents.Dirty, ev)
			var row *IBCTransfer
			var err error
			switch ev.Type {
			case evSendPacket:
				row, err = sentTransfer(ev)
			case evRecvPacket:
				row, err = receivedTransfer(ev, events)
			case evAcknowledgePacket:
				status, ackErr := acknowledged(events)
				row, err = settledTransfer(ev, body, status, ackErr)
			case evTimeoutPacket:
				row, err = settledTransfer(ev, body, IBCRefundedTimeout, "")
			default:
				continue
			}
			if err != nil {
				log.WithError(err).WithFields(logrus.Fields{"height": b.Height, "tx_index": txIndex, "msg_index": j,
					"event": ev.Type}).Warn("cross-chain transfer not readable, no ibc_transfers row")
				continue
			}
			if row == nil {
				continue
			}
			row.CreatedHeight, row.CreatedTxIndex, row.CreatedTxHash, row.CreatedTime = b.Height, txIndex, hash, b.Time
			if row.Status != IBCPending {
				h, at := b.Height, b.Time
				row.ResultHeight, row.ResultTxHash, row.ResultTime = &h, hash, &at
			}
			ents.IBCTransfers = append(ents.IBCTransfers, *row)
		}
	}
}

// sentTransfer is the out row of a send_packet on the transfer port.
func sentTransfer(ev node.Event) (*IBCTransfer, error) {
	if port, _ := ev.Get("packet_src_port"); port != chain.TransferPort {
		return nil, nil
	}
	data, err := eventPacketData(ev)
	if err != nil {
		return nil, err
	}
	row, err := packetRow(ev, IBCOut, "packet_src_channel", data)
	if err != nil {
		return nil, err
	}
	row.Status = IBCPending
	return row, nil
}

// receivedTransfer is the in row of a recv_packet on the transfer port,
// received or rejected after what the message's other events say.
func receivedTransfer(ev node.Event, events []node.Event) (*IBCTransfer, error) {
	if port, _ := ev.Get("packet_dst_port"); port != chain.TransferPort {
		return nil, nil
	}
	data, err := eventPacketData(ev)
	if err != nil {
		return nil, err
	}
	row, err := packetRow(ev, IBCIn, "packet_dst_channel", data)
	if err != nil {
		return nil, err
	}
	row.Status = IBCRejected
	for _, e := range events {
		switch e.Type {
		case evBlockedIBCInbound:
			row.AckError = IBCBlockedInbound
			return row, nil
		case evFungibleToken:
			if s, ok := e.Get("success"); ok && s == "true" {
				row.Status = IBCReceived
				return row, nil
			}
			if msg, ok := e.Get("error"); ok && row.AckError == "" {
				row.AckError = msg
			}
		case evWriteAck:
			if msg := ackError(e); msg != "" && row.AckError == "" {
				row.AckError = msg
			}
		}
	}
	return row, nil
}

// acknowledged is the outcome of an acknowledgement: the transfer module's
// fungible_token_packet carries "success" (the ack result, any value) or
// "error".
func acknowledged(events []node.Event) (status, ackError string) {
	for _, e := range events {
		if e.Type != evFungibleToken {
			continue
		}
		if msg, ok := e.Get("error"); ok {
			return IBCRefundedRejected, msg
		}
	}
	return IBCDelivered, ""
}

// settledTransfer is the complete out row an acknowledgement or a timeout
// carries: the packet comes from the message body, the BZE-side channel
// from the event.
func settledTransfer(ev node.Event, body json.RawMessage, status, ackError string) (*IBCTransfer, error) {
	if port, _ := ev.Get("packet_src_port"); port != chain.TransferPort {
		return nil, nil
	}
	if body == nil {
		return nil, fmt.Errorf("no message body to read the packet from")
	}
	var b packetBody
	if err := json.Unmarshal(body, &b); err != nil {
		return nil, fmt.Errorf("packet of the message body: %w", err)
	}
	data, err := packetData(b.Packet.Data)
	if err != nil {
		return nil, err
	}
	row, err := packetRow(ev, IBCOut, "packet_src_channel", data)
	if err != nil {
		return nil, err
	}
	row.Status, row.AckError, row.Resolves = status, ackError, true
	return row, nil
}

// packetRow fills the row's key, packet data and timeouts from a packet
// event. The out denom is the BZE denom the packet's trace names.
func packetRow(ev node.Event, direction, channelKey string, data ibctransfertypes.FungibleTokenPacketData) (*IBCTransfer, error) {
	channel, _ := ev.Get(channelKey)
	rawSeq, _ := ev.Get("packet_sequence")
	seq, err := strconv.ParseUint(rawSeq, 10, 64)
	if channel == "" || err != nil {
		return nil, fmt.Errorf("packet key: channel %q, sequence %q", channel, rawSeq)
	}
	denom := data.Denom
	if direction == IBCOut {
		denom = ibctransfertypes.ParseDenomTrace(data.Denom).IBCDenom()
	}
	row := &IBCTransfer{
		ChannelID: channel, Sequence: seq, Direction: direction,
		Sender: data.Sender, Receiver: data.Receiver, Denom: denom, Amount: data.Amount, Memo: data.Memo,
	}
	if h, _ := ev.Get("packet_timeout_height"); h != "" && h != "0-0" {
		row.TimeoutHeight = h
	}
	if raw, _ := ev.Get("packet_timeout_timestamp"); raw != "" && raw != "0" {
		ns, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("packet timeout timestamp %q: %w", raw, err)
		}
		at := time.Unix(0, ns).UTC()
		row.TimeoutTime = &at
	}
	return row, nil
}

// eventPacketData decodes a packet event's data: packet_data_hex, or the
// deprecated packet_data an event without it carries.
func eventPacketData(ev node.Event) (ibctransfertypes.FungibleTokenPacketData, error) {
	if raw, ok := ev.Get("packet_data_hex"); ok {
		b, err := hex.DecodeString(raw)
		if err != nil {
			return ibctransfertypes.FungibleTokenPacketData{}, fmt.Errorf("packet_data_hex: %w", err)
		}
		return packetData(b)
	}
	raw, _ := ev.Get("packet_data")
	return packetData([]byte(raw))
}

// packetData decodes ICS-20 packet data.
func packetData(b []byte) (ibctransfertypes.FungibleTokenPacketData, error) {
	var d ibctransfertypes.FungibleTokenPacketData
	if err := json.Unmarshal(b, &d); err != nil {
		return d, fmt.Errorf("not ICS-20 packet data: %w", err)
	}
	if d.Denom == "" || d.Amount == "" {
		return d, fmt.Errorf("ICS-20 packet data without denom or amount")
	}
	return d, nil
}

// ackError is the error of a write_acknowledgement's error ack, empty for a
// success.
func ackError(ev node.Event) string {
	raw, ok := ev.Get("packet_ack")
	if !ok {
		if h, ok := ev.Get("packet_ack_hex"); ok {
			b, err := hex.DecodeString(h)
			if err != nil {
				return ""
			}
			raw = string(b)
		}
	}
	var ack struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(raw), &ack) != nil {
		return ""
	}
	return strings.TrimSpace(ack.Error)
}
