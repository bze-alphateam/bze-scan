// Package dto holds the JSON shapes of the /api/v1 routes and the keyset
// cursor. Field names are snake_case; amounts and other big decimals are
// strings; times are RFC 3339 in UTC; an absent value is null, never a
// missing field.
package dto

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// List is the shape of every list response. NextCursor is null on the last
// page.
type List[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// ErrInvalidCursor is returned for a cursor this API did not produce.
var ErrInvalidCursor = errors.New("invalid cursor")

// EncodeCursor turns the key tuple of the last item of a page into the
// opaque cursor of the next page: base64 (URL alphabet, no padding) of the
// keys joined by ":".
func EncodeCursor(keys ...int64) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = strconv.FormatInt(k, 10)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, ":")))
}

// DecodeCursor returns the n keys of a cursor made by EncodeCursor. Every key
// must be a non-negative integer.
func DecodeCursor(cursor string, n int) ([]int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != n {
		return nil, ErrInvalidCursor
	}
	keys := make([]int64, n)
	for i, p := range parts {
		k, err := strconv.ParseInt(p, 10, 64)
		if err != nil || k < 0 || strconv.FormatInt(k, 10) != p {
			return nil, ErrInvalidCursor
		}
		keys[i] = k
	}
	return keys, nil
}

// BlockSummary is an item of GET /api/v1/blocks.
type BlockSummary struct {
	Height              int64     `json:"height"`
	Time                time.Time `json:"time"`
	Hash                string    `json:"hash"`
	TxCount             int64     `json:"tx_count"`
	TxFailedCount       int64     `json:"tx_failed_count"`
	ProposerConsAddress *string   `json:"proposer_cons_address"`
	BlockTimeMs         *int64    `json:"block_time_ms"`
	SizeBytes           *int64    `json:"size_bytes"`
}

// Block is GET /api/v1/blocks/{height}.
type Block struct {
	BlockSummary
	Minted             *string         `json:"minted"`
	Inflation          *string         `json:"inflation"`
	FeesDistributed    json.RawMessage `json:"fees_distributed"`
	SignaturesCount    *int64          `json:"signatures_count"`
	SignaturesPowerPct *string         `json:"signatures_power_pct"`
	Transactions       []BlockTx       `json:"transactions"`
}

// BlockTx is a transaction of the block page.
type BlockTx struct {
	Height   int64           `json:"height"`
	TxIndex  int64           `json:"tx_index"`
	Hash     string          `json:"hash"`
	Success  bool            `json:"success"`
	MsgTypes []string        `json:"msg_types"`
	Fee      json.RawMessage `json:"fee"`
	Signer   *string         `json:"signer"`
}

// TxSummary is an item of GET /api/v1/txs.
type TxSummary struct {
	Height   int64           `json:"height"`
	TxIndex  int64           `json:"tx_index"`
	Hash     string          `json:"hash"`
	Time     time.Time       `json:"time"`
	Success  bool            `json:"success"`
	MsgCount int64           `json:"msg_count"`
	MsgTypes []string        `json:"msg_types"`
	Fee      json.RawMessage `json:"fee"`
	Signer   *string         `json:"signer"`
}

// Tx is GET /api/v1/txs/{hash}.
type Tx struct {
	TxSummary
	Code      int64     `json:"code"`
	Codespace *string   `json:"codespace"`
	ErrorLog  *string   `json:"error_log"`
	GasWanted *int64    `json:"gas_wanted"`
	GasUsed   *int64    `json:"gas_used"`
	FeePayer  *string   `json:"fee_payer"`
	Signers   []string  `json:"signers"`
	Memo      *string   `json:"memo"`
	Messages  []Message `json:"messages"`
}

// Message is a message of the transaction page. Body is the decoded message
// as proto JSON, null when it could not be decoded; Events are the message's
// events in emission order.
type Message struct {
	MsgIndex int64           `json:"msg_index"`
	TypeURL  string          `json:"type_url"`
	Sender   *string         `json:"sender"`
	Module   *string         `json:"module"`
	Body     json.RawMessage `json:"body"`
	Events   json.RawMessage `json:"events"`
}

// Search result types.
const (
	ResultBlock       = "block"
	ResultTransaction = "transaction"
	ResultAccount     = "account"
	ResultValidator   = "validator"
)

// SearchResponse is GET /api/v1/search.
type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

// SearchResult is one match. ID is what the UI routes with (a height, a
// hash, an address); Indexed is set for accounts only.
type SearchResult struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Label   string `json:"label"`
	Indexed *bool  `json:"indexed,omitempty"`
}

// NewBlockSummary maps a block list row.
func NewBlockSummary(b repository.BlockSummary) BlockSummary {
	return BlockSummary{
		Height: b.Height, Time: b.Time.UTC(), Hash: b.Hash, TxCount: b.TxCount, TxFailedCount: b.TxFailedCount,
		ProposerConsAddress: b.ProposerConsAddress, BlockTimeMs: b.BlockTimeMs, SizeBytes: b.SizeBytes,
	}
}

// NewBlock maps a block with its transactions.
func NewBlock(b *repository.Block) Block {
	out := Block{
		BlockSummary: NewBlockSummary(b.BlockSummary),
		Minted:       b.Minted, Inflation: b.Inflation, FeesDistributed: jsonOrNull(b.FeesDistributed),
		SignaturesCount: b.SignaturesCount, SignaturesPowerPct: b.SignaturesPowerPct,
		Transactions: make([]BlockTx, 0, len(b.Transactions)),
	}
	for _, t := range b.Transactions {
		out.Transactions = append(out.Transactions, BlockTx{
			Height: b.Height, TxIndex: t.TxIndex, Hash: t.Hash, Success: t.Success,
			MsgTypes: nonNil(t.MsgTypes), Fee: jsonOr(t.Fee, "[]"), Signer: t.Signer,
		})
	}
	return out
}

// NewTxSummary maps a transaction list row.
func NewTxSummary(t repository.TxSummary) TxSummary {
	return TxSummary{
		Height: t.Height, TxIndex: t.TxIndex, Hash: t.Hash, Time: t.Time.UTC(), Success: t.Success,
		MsgCount: t.MsgCount, MsgTypes: nonNil(t.MsgTypes), Fee: jsonOr(t.Fee, "[]"), Signer: t.Signer,
	}
}

// NewTx maps a transaction with its messages.
func NewTx(t *repository.Tx) Tx {
	out := Tx{
		TxSummary: NewTxSummary(t.TxSummary),
		Code:      t.Code, Codespace: t.Codespace, ErrorLog: t.ErrorLog, GasWanted: t.GasWanted, GasUsed: t.GasUsed,
		FeePayer: t.FeePayer, Signers: nonNil(t.Signers), Memo: t.Memo,
		Messages: make([]Message, 0, len(t.Messages)),
	}
	for _, m := range t.Messages {
		out.Messages = append(out.Messages, Message{
			MsgIndex: m.MsgIndex, TypeURL: m.TypeURL, Sender: m.Sender, Module: m.Module,
			Body: jsonOrNull(m.Body), Events: jsonOr(m.Events, "[]"),
		})
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// jsonOr returns raw, or fallback when the column was NULL.
func jsonOr(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

func jsonOrNull(raw json.RawMessage) json.RawMessage {
	return jsonOr(raw, "null")
}
