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
	// Proposer is null while no synced validator has the proposer's
	// consensus address.
	Proposer *Proposer `json:"proposer"`
}

// Proposer names the validator that proposed a block.
type Proposer struct {
	OperatorAddress string `json:"operator_address"`
	Moniker         string `json:"moniker"`
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
	// Transfers are the moves the chain made by itself in the block, the
	// routine minting and fee distribution left out.
	Transfers []Transfer `json:"transfers"`
	// Events are the block's first classified finalize-block events;
	// EventsNextCursor continues them on /blocks/{height}/events, null when
	// they are all here.
	Events           []BlockEvent `json:"events"`
	EventsNextCursor *string      `json:"events_next_cursor"`
}

// Transfer is one coin moved by a transfer, a mint or a burn, in base
// units. MsgIndex is null for the fee and for a block's own transfers;
// Sender is null for a mint, Recipient for a burn. Symbol and Exponent are
// null when the explorer does not know the denom, the labels when it does
// not know the address.
type Transfer struct {
	Seq            int64   `json:"seq"`
	MsgIndex       *int64  `json:"msg_index"`
	Kind           string  `json:"kind"`
	Sender         *string `json:"sender"`
	SenderLabel    *Label  `json:"sender_label"`
	Recipient      *string `json:"recipient"`
	RecipientLabel *Label  `json:"recipient_label"`
	Denom          string  `json:"denom"`
	Amount         string  `json:"amount"`
	Symbol         *string `json:"symbol"`
	Exponent       *int    `json:"exponent"`
}

// BlockEvent is a classified finalize-block event: its position in the
// block's events, its type and its attributes (typed-event values decoded).
type BlockEvent struct {
	Seq   int64           `json:"seq"`
	Type  string          `json:"type"`
	Attrs json.RawMessage `json:"attrs"`
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
	// Transfers are the coins the transaction moved, the fee first; a failed
	// transaction has its fee only.
	Transfers []Transfer `json:"transfers"`
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

// Validator is an item of GET /api/v1/validators. VotingPowerPct and
// Uptime are percentages with five decimals; Rank and VotingPowerPct are
// null for a validator outside the active set, Uptime before its first
// signing window is known.
type Validator struct {
	Rank            *int64  `json:"rank"`
	Moniker         string  `json:"moniker"`
	OperatorAddress string  `json:"operator_address"`
	Tokens          string  `json:"tokens"`
	VotingPowerPct  *string `json:"voting_power_pct"`
	CommissionRate  string  `json:"commission_rate"`
	Uptime          *string `json:"uptime"`
	Jailed          bool    `json:"jailed"`
	Status          string  `json:"status"`
}

// NewValidator maps a validator list row. Uptime is 1 − missed / window,
// from the slashing module's signing info, in percent.
func NewValidator(v repository.ValidatorSummary) Validator {
	return Validator{
		Rank: v.Rank, Moniker: v.Moniker, OperatorAddress: v.OperatorAddress, Tokens: v.Tokens,
		VotingPowerPct: v.VotingPowerPct, CommissionRate: v.CommissionRate, Jailed: v.Jailed, Status: v.Status,
		Uptime: uptime(v.MissedBlocks, v.SignedBlocksWindow),
	}
}

// uptime is 1 − missed / window in percent with five decimals, half up;
// nil while the window is unknown.
func uptime(missedBlocks, window *int64) *string {
	if missedBlocks == nil || window == nil || *window <= 0 {
		return nil
	}
	missed := min(max(*missedBlocks, 0), *window)
	// (window - missed) * 10^7 / window, half up.
	scaled := ((*window-missed)*20_000_000 + *window) / (2 * *window)
	s := strconv.FormatInt(scaled, 10)
	for len(s) < 6 {
		s = "0" + s
	}
	u := s[:len(s)-5] + "." + s[len(s)-5:]
	return &u
}

// ValidatorDetail is GET /api/v1/validators/{operator}: the list item, every
// other column, the last proposed blocks, the last events and the owner
// account's last governance votes.
type ValidatorDetail struct {
	Validator
	AccountAddress          string           `json:"account_address"`
	ConsensusAddress        *string          `json:"consensus_address"`
	ConsensusPubkey         *string          `json:"consensus_pubkey"`
	Identity                *string          `json:"identity"`
	Website                 *string          `json:"website"`
	SecurityContact         *string          `json:"security_contact"`
	Details                 *string          `json:"details"`
	Tombstoned              bool             `json:"tombstoned"`
	JailedUntil             *time.Time       `json:"jailed_until"`
	DelegatorShares         string           `json:"delegator_shares"`
	CommissionMaxRate       string           `json:"commission_max_rate"`
	CommissionMaxChangeRate string           `json:"commission_max_change_rate"`
	CommissionUpdateTime    *time.Time       `json:"commission_update_time"`
	MinSelfDelegation       *string          `json:"min_self_delegation"`
	SelfDelegation          *string          `json:"self_delegation"`
	DelegatorCount          *int64           `json:"delegator_count"`
	MissedBlocks            *int64           `json:"missed_blocks"`
	SignedBlocksWindow      *int64           `json:"signed_blocks_window"`
	FirstSeenHeight         *int64           `json:"first_seen_height"`
	FirstSeenTime           *time.Time       `json:"first_seen_time"`
	UpdatedAt               time.Time        `json:"updated_at"`
	RecentBlocks            []BlockSummary   `json:"recent_blocks"`
	Events                  []ValidatorEvent `json:"events"`
	Votes                   []ValidatorVote  `json:"votes"`
}

// ValidatorEvent is an event of the validator page. TxIndex is -1 and
// TxHash null for a block-level event (a slash) and for the transitions the
// state sync found (jailed, tombstoned, bonded, unbonded).
type ValidatorEvent struct {
	Height  int64           `json:"height"`
	TxIndex int64           `json:"tx_index"`
	TxHash  *string         `json:"tx_hash"`
	Kind    string          `json:"kind"`
	Details json.RawMessage `json:"details"`
	Time    time.Time       `json:"time"`
}

// ValidatorVote is a governance vote of the validator's owner account.
// Option is null for a weighted vote, whose split is in Options.
type ValidatorVote struct {
	ProposalID int64           `json:"proposal_id"`
	Title      *string         `json:"title"`
	Option     *string         `json:"option"`
	Options    json.RawMessage `json:"options"`
	Height     int64           `json:"height"`
	Time       time.Time       `json:"time"`
}

// NewValidatorDetail maps a validator row with its blocks, events and votes.
func NewValidatorDetail(v *repository.ValidatorDetail, blocks []repository.BlockSummary,
	events []repository.ValidatorEvent, votes []repository.ValidatorVote) ValidatorDetail {
	out := ValidatorDetail{
		Validator:      NewValidator(v.ValidatorSummary),
		AccountAddress: v.AccountAddress, ConsensusAddress: v.ConsensusAddress, ConsensusPubkey: v.ConsensusPubkey,
		Identity: v.Identity, Website: v.Website, SecurityContact: v.SecurityContact, Details: v.Details,
		Tombstoned: v.Tombstoned, JailedUntil: utc(v.JailedUntil), DelegatorShares: v.DelegatorShares,
		CommissionMaxRate: v.CommissionMaxRate, CommissionMaxChangeRate: v.CommissionMaxChangeRate,
		CommissionUpdateTime: utc(v.CommissionUpdateTime), MinSelfDelegation: v.MinSelfDelegation,
		SelfDelegation: v.SelfDelegation, DelegatorCount: v.DelegatorCount, MissedBlocks: v.MissedBlocks,
		SignedBlocksWindow: v.SignedBlocksWindow, FirstSeenHeight: v.FirstSeenHeight,
		FirstSeenTime: utc(v.FirstSeenTime), UpdatedAt: v.UpdatedAt.UTC(),
		RecentBlocks: make([]BlockSummary, 0, len(blocks)),
		Events:       make([]ValidatorEvent, 0, len(events)),
		Votes:        make([]ValidatorVote, 0, len(votes)),
	}
	for _, b := range blocks {
		out.RecentBlocks = append(out.RecentBlocks, NewBlockSummary(b))
	}
	for _, e := range events {
		out.Events = append(out.Events, ValidatorEvent{
			Height: e.Height, TxIndex: e.TxIndex, TxHash: e.TxHash, Kind: e.Kind,
			Details: jsonOrNull(e.Details), Time: e.Time.UTC(),
		})
	}
	for _, vote := range votes {
		out.Votes = append(out.Votes, ValidatorVote{
			ProposalID: vote.ProposalID, Title: vote.Title, Option: vote.Option,
			Options: jsonOr(vote.Options, "[]"), Height: vote.Height, Time: vote.Time.UTC(),
		})
	}
	return out
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
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
	out := BlockSummary{
		Height: b.Height, Time: b.Time.UTC(), Hash: b.Hash, TxCount: b.TxCount, TxFailedCount: b.TxFailedCount,
		ProposerConsAddress: b.ProposerConsAddress, BlockTimeMs: b.BlockTimeMs, SizeBytes: b.SizeBytes,
	}
	if b.Proposer != nil {
		out.Proposer = &Proposer{OperatorAddress: b.Proposer.OperatorAddress, Moniker: b.Proposer.Moniker}
	}
	return out
}

// NewBlock maps a block with its transactions, its own transfers and the
// first page of its events (eventsNext continues them, nil when complete).
func NewBlock(b *repository.Block, transfers []repository.Transfer, events []repository.BlockEvent, eventsNext *string) Block {
	out := Block{
		BlockSummary: NewBlockSummary(b.BlockSummary),
		Minted:       b.Minted, Inflation: b.Inflation, FeesDistributed: jsonOrNull(b.FeesDistributed),
		SignaturesCount: b.SignaturesCount, SignaturesPowerPct: b.SignaturesPowerPct,
		Transactions:     make([]BlockTx, 0, len(b.Transactions)),
		Transfers:        NewTransfers(transfers),
		Events:           make([]BlockEvent, 0, len(events)),
		EventsNextCursor: eventsNext,
	}
	for _, e := range events {
		out.Events = append(out.Events, NewBlockEvent(e))
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

// NewTx maps a transaction with its messages and transfers.
func NewTx(t *repository.Tx, transfers []repository.Transfer) Tx {
	out := Tx{
		TxSummary: NewTxSummary(t.TxSummary),
		Code:      t.Code, Codespace: t.Codespace, ErrorLog: t.ErrorLog, GasWanted: t.GasWanted, GasUsed: t.GasUsed,
		FeePayer: t.FeePayer, Signers: nonNil(t.Signers), Memo: t.Memo,
		Messages:  make([]Message, 0, len(t.Messages)),
		Transfers: NewTransfers(transfers),
	}
	for _, m := range t.Messages {
		out.Messages = append(out.Messages, Message{
			MsgIndex: m.MsgIndex, TypeURL: m.TypeURL, Sender: m.Sender, Module: m.Module,
			Body: jsonOrNull(m.Body), Events: jsonOr(m.Events, "[]"),
		})
	}
	return out
}

// NewTransfers maps transfers rows; none is an empty list.
func NewTransfers(rows []repository.Transfer) []Transfer {
	out := make([]Transfer, 0, len(rows))
	for _, t := range rows {
		out = append(out, Transfer{
			Seq: t.Seq, MsgIndex: t.MsgIndex, Kind: t.Kind,
			Sender: t.Sender, SenderLabel: newLabel(t.SenderLabel),
			Recipient: t.Recipient, RecipientLabel: newLabel(t.RecipientLabel),
			Denom: t.Denom, Amount: t.Amount, Symbol: t.Symbol, Exponent: t.Exponent,
		})
	}
	return out
}

// NewBlockEvent maps a block_events row.
func NewBlockEvent(e repository.BlockEvent) BlockEvent {
	return BlockEvent{Seq: e.Seq, Type: e.Type, Attrs: jsonOr(e.Attrs, "{}")}
}

func newLabel(l *repository.Label) *Label {
	if l == nil {
		return nil
	}
	return &Label{Name: l.Name, Kind: l.Kind}
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
