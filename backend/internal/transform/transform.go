// Package transform turns the node's answers for one height (/block,
// /block_results, /commit) into the rows the explorer stores. It is the only
// place where chain data is interpreted, shared by the live indexer and,
// later, the backfill and the reindex command. It does no I/O.
package transform

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// Input is everything the node answered for one height.
type Input struct {
	Block   *node.Block
	Results *node.BlockResults
	Commit  *node.Commit
}

// Entities are the rows derived from one or more heights, one slice per
// table. Later work adds a slice per table it fills.
type Entities struct {
	Blocks       []Block
	Transactions []Transaction
	Messages     []Message
	// ValidatorEvents are the validator_events rows of the heights' slash
	// events and validator messages.
	ValidatorEvents []ValidatorEvent
	// Dirty is the current state the heights changed, for the state sync.
	// The live indexer publishes it after the write; the backfill ignores it.
	Dirty statesync.Dirty
}

// Block is one explorer.blocks row. BlockTimeMs is not here: the writer
// derives it from the previous block's row.
type Block struct {
	Height              int64
	Time                time.Time
	TxCount             int
	TxFailedCount       int
	Hash                string
	ProposerConsAddress string
	SizeBytes           int
	// Minted is the mint event's amount (ubze), nil without a mint event.
	Minted *string
	// Inflation is the mint event's inflation, nil without a mint event.
	Inflation *string
	// FeesDistributed is the sum of the transfers from the fee collector to
	// the distribution module in the finalize-block events, nil when there is
	// none.
	FeesDistributed sdk.Coins
	SignaturesCount int
	// Signers are the consensus addresses (upper-case hex) of the commit's
	// signatures. The writers turn them into signatures_power_pct against
	// the current validators table: the voting power at the height is not in
	// /commit, so the share is exact for live blocks and approximate for
	// history.
	Signers []string
}

// FeesDistributedJSON is the fees_distributed column value: a JSON array of
// {"denom","amount"} objects, or nil.
func (b *Block) FeesDistributedJSON() ([]byte, error) {
	if b.FeesDistributed == nil {
		return nil, nil
	}
	return json.Marshal(b.FeesDistributed)
}

// Transaction is one explorer.transactions row.
type Transaction struct {
	Height    int64
	TxIndex   int
	Hash      string
	Time      time.Time
	Success   bool
	Code      uint32
	Codespace string
	// ErrorLog is the log of a failed transaction; empty on success.
	ErrorLog  string
	GasWanted int64
	GasUsed   int64
	Fee       []chain.Coin
	FeePayer  string
	Signers   []string
	Memo      string
	MsgCount  int
	MsgTypes  []string
}

// Message is one explorer.messages row.
type Message struct {
	Height   int64
	TxIndex  int
	MsgIndex int
	TypeURL  string
	Sender   string
	Module   string
	// Events are the transaction's events carrying this message's msg_index,
	// in emission order; empty for a failed transaction.
	Events []Event
	// Body is the message as proto JSON; nil when it could not be decoded.
	Body json.RawMessage
}

// Event is one event of a message as the explorer stores it. Attribute
// values of typed events (bze.*) are JSON-decoded; other values stay strings.
type Event struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
}

// Decoder decodes the raw bytes of a transaction; *chain.Codec satisfies it.
type Decoder interface {
	Decode(raw []byte) (*chain.Tx, error)
}

// Transformer holds the chain facts the rules need.
type Transformer struct {
	decoder      Decoder
	log          logrus.FieldLogger
	feeCollector string
	distribution string
}

// New returns a transformer for the BZE chain that decodes transactions with
// decoder and logs undecodable ones to log.
func New(decoder Decoder, log logrus.FieldLogger) *Transformer {
	return &Transformer{
		decoder:      decoder,
		log:          log,
		feeCollector: chain.ModuleAddress(chain.FeeCollector),
		distribution: chain.ModuleAddress(chain.Distribution),
	}
}

// Transform derives the entities of one height.
func (t *Transformer) Transform(in Input) (*Entities, error) {
	if in.Block == nil || in.Results == nil || in.Commit == nil {
		return nil, fmt.Errorf("transform: incomplete input")
	}
	h := in.Block.Height
	if in.Results.Height != h || in.Commit.Height != h {
		return nil, fmt.Errorf("transform %d: inputs of heights %d/%d/%d", h, h, in.Results.Height, in.Commit.Height)
	}
	if len(in.Results.TxsResults) != len(in.Block.Txs) {
		return nil, fmt.Errorf("transform %d: %d transactions but %d results", h, len(in.Block.Txs), len(in.Results.TxsResults))
	}

	b := Block{
		Height:              h,
		Time:                in.Block.Time.UTC(),
		TxCount:             len(in.Block.Txs),
		Hash:                in.Block.Hash,
		ProposerConsAddress: in.Block.ProposerAddress,
		SizeBytes:           len(in.Block.Raw),
	}
	for _, r := range in.Results.TxsResults {
		if r.Code != 0 {
			b.TxFailedCount++
		}
	}
	for _, s := range in.Commit.Signatures {
		if s.BlockIDFlag == node.BlockIDFlagCommit {
			b.SignaturesCount++
			b.Signers = append(b.Signers, strings.ToUpper(s.ValidatorAddress))
		}
	}

	ents := &Entities{}
	for _, ev := range in.Results.FinalizeBlockEvents {
		markValidators(&ents.Dirty, ev)
		switch ev.Type {
		case "mint":
			if b.Minted != nil {
				continue
			}
			if err := mintSummary(&b, ev); err != nil {
				return nil, fmt.Errorf("transform %d: %w", h, err)
			}
		case "transfer":
			sender, _ := ev.Get("sender")
			recipient, _ := ev.Get("recipient")
			if sender != t.feeCollector || recipient != t.distribution {
				continue
			}
			raw, _ := ev.Get("amount")
			coins, err := sdk.ParseCoinsNormalized(raw)
			if err != nil {
				return nil, fmt.Errorf("transform %d: fee distribution amount %q: %w", h, raw, err)
			}
			b.FeesDistributed = b.FeesDistributed.Add(coins...)
		}
	}

	markValidatorUpdates(&ents.Dirty, in.Results.ValidatorUpdates)

	ents.Blocks = []Block{b}
	slashEvents(ents, b, in.Results.FinalizeBlockEvents)
	for i, raw := range in.Block.Txs {
		if err := t.transaction(ents, b, i, raw, in.Results.TxsResults[i]); err != nil {
			return nil, fmt.Errorf("transform %d: tx %d: %w", h, i, err)
		}
	}
	return ents, nil
}

// transaction appends the transactions row and the messages rows of the
// transaction at index i.
func (t *Transformer) transaction(ents *Entities, b Block, i int, rawB64 string, res node.TxResult) error {
	raw, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil {
		return fmt.Errorf("raw bytes: %w", err)
	}
	sum := sha256.Sum256(raw)
	tx := Transaction{
		Height:    b.Height,
		TxIndex:   i,
		Hash:      strings.ToUpper(hex.EncodeToString(sum[:])),
		Time:      b.Time,
		Success:   res.Code == 0,
		Code:      res.Code,
		Codespace: res.Codespace,
		GasWanted: res.GasWanted,
		GasUsed:   res.GasUsed,
		Fee:       []chain.Coin{},
		Signers:   []string{},
		MsgTypes:  []string{},
	}
	if !tx.Success {
		tx.ErrorLog = res.Log
	}
	logger := t.log.WithFields(logrus.Fields{"height": b.Height, "tx_index": i, "hash": tx.Hash})

	decoded, err := t.decoder.Decode(raw)
	if err != nil {
		logger.WithError(err).Warn("transaction not decodable, stored from its results only")
		decoded = &chain.Tx{}
	}
	tx.Memo = decoded.Memo

	// Fee, fee payer and signers: the tx events of the ante handler, which a
	// failed transaction emits too; the decoded transaction otherwise.
	feeSeen := false
	for _, ev := range res.Events {
		if ev.Type != "tx" {
			continue
		}
		for _, a := range ev.Attributes {
			switch a.Key {
			case "fee":
				coins, err := chain.ParseCoins(a.Value)
				if err != nil {
					return fmt.Errorf("tx.fee: %w", err)
				}
				tx.Fee, feeSeen = coins, true
			case "fee_payer":
				tx.FeePayer = a.Value
			case "acc_seq":
				if j := strings.LastIndexByte(a.Value, '/'); j > 0 {
					tx.Signers = append(tx.Signers, a.Value[:j])
				}
			}
		}
	}
	if !feeSeen {
		tx.Fee = chain.CoinsOf(decoded.Fee)
	}
	if tx.FeePayer == "" {
		tx.FeePayer = decoded.FeePayer
	}
	if len(tx.Signers) == 0 && len(decoded.Signers) > 0 {
		tx.Signers = decoded.Signers
	}

	if tx.Success {
		for _, ev := range res.Events {
			markValidators(&ents.Dirty, ev)
		}
		for _, m := range decoded.Msgs {
			markValidatorMsg(&ents.Dirty, m)
		}
	}

	tx.MsgCount = len(decoded.Msgs)
	byIndex := eventsByMsgIndex(res.Events)
	if tx.Success {
		txValidatorEvents(ents, b, i, decoded.Msgs, byIndex)
	}
	for j, m := range decoded.Msgs {
		tx.MsgTypes = append(tx.MsgTypes, m.TypeURL)
		if m.Err != nil {
			logger.WithError(m.Err).WithFields(logrus.Fields{"msg_index": j, "type_url": m.TypeURL}).
				Warn("message not decodable, stored without a body")
		}
		msg := Message{
			Height:   b.Height,
			TxIndex:  i,
			MsgIndex: j,
			TypeURL:  m.TypeURL,
			Events:   []Event{},
			Body:     m.Body,
		}
		first := ""
		if len(tx.Signers) > 0 {
			first = tx.Signers[0]
		}
		msg.Sender = first
		messageSeen := false
		for _, ev := range byIndex[j] {
			if ev.Type == "message" && !messageSeen {
				messageSeen = true
				if s, ok := ev.Get("sender"); ok && s != "" {
					msg.Sender = s
				}
				msg.Module, _ = ev.Get("module")
			}
			msg.Events = append(msg.Events, storedEvent(ev))
		}
		ents.Messages = append(ents.Messages, msg)
	}
	ents.Transactions = append(ents.Transactions, tx)
	return nil
}

// eventsByMsgIndex groups a transaction's events by their msg_index
// attribute, keeping emission order. Events without one (the ante handler's)
// belong to no message.
func eventsByMsgIndex(events []node.Event) map[int][]node.Event {
	out := map[int][]node.Event{}
	for _, ev := range events {
		v, ok := ev.Get("msg_index")
		if !ok {
			continue
		}
		j, err := strconv.Atoi(v)
		if err != nil {
			continue
		}
		out[j] = append(out[j], ev)
	}
	return out
}

// storedEvent converts an event to its stored form: msg_index dropped (the
// row carries it), typed-event values decoded, the first of repeated keys
// kept.
func storedEvent(ev node.Event) Event {
	typed := strings.HasPrefix(ev.Type, "bze.")
	attrs := make(map[string]any, len(ev.Attributes))
	for _, a := range ev.Attributes {
		if a.Key == "msg_index" {
			continue
		}
		if _, dup := attrs[a.Key]; dup {
			continue
		}
		attrs[a.Key] = a.Value
		if typed {
			if v, ok := decodeJSON(a.Value); ok {
				attrs[a.Key] = v
			}
		}
	}
	return Event{Type: ev.Type, Attrs: attrs}
}

// decodeJSON decodes a typed-event attribute value, keeping numbers exact.
func decodeJSON(s string) (any, bool) {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil || dec.More() {
		return nil, false
	}
	return v, true
}

func mintSummary(b *Block, ev node.Event) error {
	if raw, ok := ev.Get("amount"); ok {
		if _, ok := sdkmath.NewIntFromString(raw); !ok {
			return fmt.Errorf("mint amount %q is not an integer", raw)
		}
		b.Minted = &raw
	}
	if raw, ok := ev.Get("inflation"); ok {
		if _, err := sdkmath.LegacyNewDecFromStr(raw); err != nil {
			return fmt.Errorf("mint inflation %q: %w", raw, err)
		}
		b.Inflation = &raw
	}
	return nil
}
