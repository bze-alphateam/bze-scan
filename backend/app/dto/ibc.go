package dto

import (
	"strconv"
	"strings"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
)

// IBCDirections are the direction filters of an account's cross-chain
// transfers.
var IBCDirections = []string{repository.IBCDirectionOut, repository.IBCDirectionIn}

// Chain is a counterparty chain as the chain registry names it. chain_id
// is null when the channel's chain is not known yet; the other fields are
// null when the registry does not know the chain. explorer_account_url and
// explorer_tx_url are the registry's templates (${accountAddress},
// ${txHash}).
type Chain struct {
	ChainID            *string `json:"chain_id"`
	PrettyName         *string `json:"pretty_name"`
	LogoURL            *string `json:"logo_url"`
	ExplorerAccountURL *string `json:"explorer_account_url"`
	ExplorerTxURL      *string `json:"explorer_tx_url"`
}

// IBCTransfer is a cross-chain transfer as BZE's own events know it: an
// item of ibc_transfers on GET /api/v1/txs/{hash} and of GET
// /api/v1/accounts/{address}/ibc. Status is pending, delivered,
// refunded_rejected or refunded_timeout for direction out, received or
// rejected for in; expired is a pending transfer past its timeout time,
// still waiting for the relayer to bring the timeout back. created_* is
// the transaction that created the row (the MsgTransfer, the
// MsgRecvPacket, or the acknowledgement when the transfer itself is not
// indexed), result_* the one that settled it (null while pending).
// timeout_height is "revision-height", null for none.
type IBCTransfer struct {
	ChannelID     string     `json:"channel_id"`
	Sequence      int64      `json:"sequence"`
	Direction     string     `json:"direction"`
	Status        string     `json:"status"`
	Expired       bool       `json:"expired"`
	Sender        string     `json:"sender"`
	Receiver      string     `json:"receiver"`
	Denom         string     `json:"denom"`
	Amount        string     `json:"amount"`
	Memo          *string    `json:"memo"`
	Counterparty  Chain      `json:"counterparty"`
	CreatedHeight int64      `json:"created_height"`
	CreatedTxHash string     `json:"created_tx_hash"`
	CreatedTime   time.Time  `json:"created_time"`
	TimeoutHeight *string    `json:"timeout_height"`
	TimeoutTime   *time.Time `json:"timeout_time"`
	ResultHeight  *int64     `json:"result_height"`
	ResultTxHash  *string    `json:"result_tx_hash"`
	ResultTime    *time.Time `json:"result_time"`
	AckError      *string    `json:"ack_error"`
}

// IBCChannel is an item of GET /api/v1/ibc/channels: a transfer channel,
// its state (open, closed, init, tryopen…), its counterparty and the
// escrow account holding the BZE tokens sent over it.
type IBCChannel struct {
	ChannelID             string    `json:"channel_id"`
	PortID                string    `json:"port_id"`
	State                 *string   `json:"state"`
	ClientStatus          *string   `json:"client_status"`
	ClientID              *string   `json:"client_id"`
	ConnectionID          *string   `json:"connection_id"`
	CounterpartyChannelID *string   `json:"counterparty_channel_id"`
	Counterparty          Chain     `json:"counterparty"`
	EscrowAddress         string    `json:"escrow_address"`
	UpdatedAt             time.Time `json:"updated_at"`
}

func newChain(c repository.Chain) Chain {
	return Chain{ChainID: c.ChainID, PrettyName: c.PrettyName, LogoURL: c.LogoURL,
		ExplorerAccountURL: c.ExplorerAccountURL, ExplorerTxURL: c.ExplorerTxURL}
}

// NewIBCTransfer maps an ibc_transfers row.
func NewIBCTransfer(t repository.IBCTransfer) IBCTransfer {
	return IBCTransfer{
		ChannelID: t.ChannelID, Sequence: t.Sequence, Direction: t.Direction, Status: t.Status, Expired: t.Expired,
		Sender: t.Sender, Receiver: t.Receiver, Denom: t.Denom, Amount: t.Amount, Memo: t.Memo,
		Counterparty: newChain(t.Counterparty), CreatedHeight: t.CreatedHeight, CreatedTxHash: t.CreatedTxHash,
		CreatedTime: t.CreatedTime.UTC(), TimeoutHeight: t.TimeoutHeight, TimeoutTime: utc(t.TimeoutTime),
		ResultHeight: t.ResultHeight, ResultTxHash: t.ResultTxHash, ResultTime: utc(t.ResultTime), AckError: t.AckError,
	}
}

// NewTxIBCTransfers maps a transaction's cross-chain transfers; none is
// null.
func NewTxIBCTransfers(rows []repository.IBCTransfer) []IBCTransfer {
	if len(rows) == 0 {
		return nil
	}
	out := make([]IBCTransfer, 0, len(rows))
	for _, t := range rows {
		out = append(out, NewIBCTransfer(t))
	}
	return out
}

// NewIBCChannel maps an ibc_channels row.
func NewIBCChannel(c repository.IBCChannel) IBCChannel {
	return IBCChannel{
		ChannelID: c.ChannelID, PortID: c.PortID, State: c.State, ClientStatus: c.ClientStatus, ClientID: c.ClientID, ConnectionID: c.ConnectionID,
		CounterpartyChannelID: c.CounterpartyChannelID, Counterparty: newChain(c.Counterparty),
		EscrowAddress: chain.EscrowAddress(c.PortID, c.ChannelID), UpdatedAt: c.UpdatedAt.UTC(),
	}
}

// EncodeIBCCursor is the cursor after a transfer of an account's list: its
// creating transaction, channel number and sequence.
func EncodeIBCCursor(t repository.IBCTransfer) string {
	n, _ := strconv.ParseInt(strings.TrimPrefix(t.ChannelID, "channel-"), 10, 64) // ibc-go names every channel channel-N
	return EncodeCursor(t.CreatedHeight, t.CreatedTxIndex, n, t.Sequence)
}

// DecodeIBCCursor reverses EncodeIBCCursor.
func DecodeIBCCursor(cursor string) (*repository.IBCKey, error) {
	keys, err := DecodeCursor(cursor, 4)
	if err != nil {
		return nil, err
	}
	return &repository.IBCKey{Height: keys[0], TxIndex: keys[1], Channel: keys[2], Sequence: keys[3]}, nil
}
