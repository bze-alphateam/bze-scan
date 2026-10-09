package transform

import (
	"fmt"
	"strconv"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/classify"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// Kinds of the transfers rows.
const (
	TransferKindTransfer = "transfer"
	TransferKindMint     = "mint"
	TransferKindBurn     = "burn"
)

// BlockTxIndex is the tx_index of the rows of a block's finalize-block
// events.
const BlockTxIndex = -1

// Transfer is one explorer.transfers row: one coin moved by a transfer,
// coinbase or burn event.
type Transfer struct {
	Height int64
	// TxIndex is BlockTxIndex for a block-level row.
	TxIndex int
	// Seq orders the rows of one transaction (or of the block's finalize
	// events), from 0.
	Seq int
	// MsgIndex is the message that moved the coin; nil for the ante
	// handler's fee transfer and for block-level rows.
	MsgIndex *int
	Kind     string
	// Sender is empty for a mint, Recipient for a burn.
	Sender    string
	Recipient string
	Denom     string
	Amount    string
}

// BlockEvent is one explorer.block_events row: a finalize-block event of a
// type the block-event classification lists.
type BlockEvent struct {
	Height int64
	// Seq is the event's position in the block's finalize-block events.
	Seq  int
	Type string
	// Attrs are the attributes, typed-event values JSON-decoded.
	Attrs map[string]any
}

// moves turns the transfer, coinbase and burn events of events into
// transfers rows of (height, txIndex), one per coin, numbered from 0 in
// emission order. skip leaves an event out. A transfer without a sender
// (a MsgMultiSend output) takes the sender of the message event before it.
func moves(height int64, txIndex int, events []node.Event, skip func(node.Event) bool) ([]Transfer, error) {
	var out []Transfer
	lastSender := map[string]string{} // by msg_index attribute
	for _, ev := range events {
		idx, hasIdx := ev.Get("msg_index")
		if ev.Type == "message" {
			if s, ok := ev.Get("sender"); ok && s != "" {
				lastSender[idx] = s
			}
			continue
		}
		var kind, sender, recipient string
		switch ev.Type {
		case "transfer":
			kind = TransferKindTransfer
			sender, _ = ev.Get("sender")
			recipient, _ = ev.Get("recipient")
			if sender == "" {
				sender = lastSender[idx]
			}
		case "coinbase":
			kind = TransferKindMint
			recipient, _ = ev.Get("minter")
		case "burn":
			kind = TransferKindBurn
			sender, _ = ev.Get("burner")
		default:
			continue
		}
		if skip != nil && skip(ev) {
			continue
		}
		var msgIndex *int
		if hasIdx {
			j, err := strconv.Atoi(idx)
			if err != nil {
				return nil, fmt.Errorf("%s: msg_index %q: %w", ev.Type, idx, err)
			}
			msgIndex = &j
		}
		raw, _ := ev.Get("amount")
		coins, err := chain.ParseCoins(raw)
		if err != nil {
			return nil, fmt.Errorf("%s amount: %w", ev.Type, err)
		}
		for _, c := range coins {
			out = append(out, Transfer{
				Height: height, TxIndex: txIndex, Seq: len(out), MsgIndex: msgIndex, Kind: kind,
				Sender: sender, Recipient: recipient, Denom: c.Denom, Amount: c.Amount,
			})
		}
	}
	return out, nil
}

// txTransfers appends the transfers rows of a transaction. A failed
// transaction keeps its ante handler's events (no msg_index) only, so it
// yields its fee row.
func txTransfers(ents *Entities, height int64, txIndex int, res node.TxResult) error {
	var skip func(node.Event) bool
	if res.Code != 0 {
		skip = func(ev node.Event) bool {
			_, ok := ev.Get("msg_index")
			return ok
		}
	}
	rows, err := moves(height, txIndex, res.Events, skip)
	if err != nil {
		return fmt.Errorf("transfers: %w", err)
	}
	ents.Transfers = append(ents.Transfers, rows...)
	return nil
}

// blockTransfers appends the transfers rows of the block's finalize events,
// leaving out the routine moves the blocks row summarises: the mint
// module's coinbase, its transfer to the fee collector and the fee
// collector's transfer to the distribution module, recognised by the module
// addresses.
func (t *Transformer) blockTransfers(ents *Entities, height int64, events []node.Event) error {
	rows, err := moves(height, BlockTxIndex, events, func(ev node.Event) bool {
		switch ev.Type {
		case "coinbase":
			minter, _ := ev.Get("minter")
			return minter == t.mint
		case "transfer":
			sender, _ := ev.Get("sender")
			recipient, _ := ev.Get("recipient")
			return (sender == t.mint && recipient == t.feeCollector) ||
				(sender == t.feeCollector && recipient == t.distribution)
		}
		return false
	})
	if err != nil {
		return fmt.Errorf("block transfers: %w", err)
	}
	ents.Transfers = append(ents.Transfers, rows...)
	return nil
}

// blockEvents appends the block_events rows: the finalize events whose type
// the block-event classification lists.
func blockEvents(ents *Entities, height int64, events []node.Event) {
	for i, ev := range events {
		if _, ok := classify.LookupBlockEvent(ev.Type); !ok {
			continue
		}
		ents.BlockEvents = append(ents.BlockEvents, BlockEvent{
			Height: height, Seq: i, Type: ev.Type, Attrs: storedEvent(ev).Attrs,
		})
	}
}
