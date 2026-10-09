package dto

import (
	"encoding/base64"
	"encoding/json"
	"time"
	"unicode/utf8"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// TokenSummary is one row of the tokens list. The holders count and the
// price fields are null until the holders and prices job fills them.
type TokenSummary struct {
	Denom             string  `json:"denom"`
	Symbol            *string `json:"symbol"`
	Name              *string `json:"name"`
	Kind              string  `json:"kind"`
	Exponent          int     `json:"exponent"`
	Supply            *string `json:"supply"`
	HoldersCount      *int64  `json:"holders_count"`
	PriceUSD          *string `json:"price_usd"`
	PriceChange24hPct *string `json:"price_change_24h_pct"`
	Halted            bool    `json:"halted"`
	LogoURL           *string `json:"logo_url"`
}

// Token is the token page: the denom with its origin, first sight, admin,
// markets and raw bank metadata, its newest events and the cursor to the
// rest of them.
type Token struct {
	TokenSummary
	Description      *string         `json:"description"`
	OriginChainID    *string         `json:"origin_chain_id"`
	IBCBaseDenom     *string         `json:"ibc_base_denom"`
	IBCPath          *string         `json:"ibc_path"`
	Creator          *string         `json:"creator"`
	CreatorLabel     *Label          `json:"creator_label"`
	Admin            *string         `json:"admin"`
	AdminLabel       *Label          `json:"admin_label"`
	CreatedHeight    *int64          `json:"created_height"`
	CreatedTxHash    *string         `json:"created_tx_hash"`
	CreatedTime      *time.Time      `json:"created_time"`
	Website          *string         `json:"website"`
	Markets          []string        `json:"markets"`
	PriceUpdatedAt   *time.Time      `json:"price_updated_at"`
	Metadata         json.RawMessage `json:"metadata"`
	UpdatedAt        time.Time       `json:"updated_at"`
	Events           []TokenEvent    `json:"events"`
	EventsNextCursor *string         `json:"events_next_cursor"`
}

// TokenEvent is one event in a denom's history. TxHash is null for a
// block-level event (a halt enacted by governance).
type TokenEvent struct {
	Height     int64           `json:"height"`
	TxIndex    int64           `json:"tx_index"`
	Seq        int64           `json:"seq"`
	TxHash     *string         `json:"tx_hash"`
	Kind       string          `json:"kind"`
	Actor      *string         `json:"actor"`
	ActorLabel *Label          `json:"actor_label"`
	Amount     *string         `json:"amount"`
	Details    json.RawMessage `json:"details"`
	Time       time.Time       `json:"time"`
}

// TokenTransfer is one move of a denom, with where it happened.
type TokenTransfer struct {
	Height  int64     `json:"height"`
	TxIndex int64     `json:"tx_index"`
	TxHash  *string   `json:"tx_hash"`
	Time    time.Time `json:"time"`
	Transfer
}

// NewTokenSummary maps a tokens list row.
func NewTokenSummary(t repository.TokenSummary) TokenSummary {
	return TokenSummary{
		Denom: t.Denom, Symbol: t.Symbol, Name: t.Name, Kind: t.Kind, Exponent: t.Exponent, Supply: t.Supply,
		HoldersCount: t.HoldersCount, PriceUSD: t.PriceUSD, PriceChange24hPct: t.PriceChange24hPct,
		Halted: t.Halted, LogoURL: t.LogoURL,
	}
}

// NewToken maps the token page.
func NewToken(t *repository.Token, events []repository.TokenEvent, eventsNext *string) Token {
	out := Token{
		TokenSummary: NewTokenSummary(t.TokenSummary),
		Description:  t.Description, OriginChainID: t.OriginChainID, IBCBaseDenom: t.IBCBaseDenom, IBCPath: t.IBCPath,
		Creator: t.Creator, CreatorLabel: newLabel(t.CreatorLabel), Admin: t.Admin, AdminLabel: newLabel(t.AdminLabel),
		CreatedHeight: t.CreatedHeight, CreatedTxHash: t.CreatedTxHash, CreatedTime: utc(t.CreatedTime),
		Website: t.Website, Markets: nonNil(t.Markets), PriceUpdatedAt: utc(t.PriceUpdatedAt),
		Metadata: jsonOrNull(t.Metadata), UpdatedAt: t.UpdatedAt.UTC(),
		Events: make([]TokenEvent, 0, len(events)), EventsNextCursor: eventsNext,
	}
	for _, e := range events {
		out.Events = append(out.Events, NewTokenEvent(e))
	}
	return out
}

// NewTokenEvent maps a token_events row.
func NewTokenEvent(e repository.TokenEvent) TokenEvent {
	return TokenEvent{
		Height: e.Height, TxIndex: e.TxIndex, Seq: e.Seq, TxHash: e.TxHash, Kind: e.Kind,
		Actor: e.Actor, ActorLabel: newLabel(e.ActorLabel), Amount: e.Amount,
		Details: jsonOrNull(e.Details), Time: e.Time.UTC(),
	}
}

// NewTokenTransfer maps a transfers row of one denom.
func NewTokenTransfer(x repository.DenomTransfer) TokenTransfer {
	return TokenTransfer{
		Height: x.Height, TxIndex: x.TxIndex, TxHash: x.TxHash, Time: x.Time.UTC(),
		Transfer: NewTransfers([]repository.Transfer{x.Transfer})[0],
	}
}

// EncodeEventCursor is the cursor after a row keyed by (height, tx_index,
// seq). tx_index is shifted by one: a block-level row's is -1 and cursor
// keys are non-negative.
func EncodeEventCursor(k repository.EventKey) string {
	return EncodeCursor(k.Height, k.TxIndex+1, k.Seq)
}

// DecodeEventCursor reverses EncodeEventCursor.
func DecodeEventCursor(cursor string) (*repository.EventKey, error) {
	keys, err := DecodeCursor(cursor, 3)
	if err != nil {
		return nil, err
	}
	return &repository.EventKey{Height: keys[0], TxIndex: keys[1] - 1, Seq: keys[2]}, nil
}

// tokenCursor is the JSON of a tokens list cursor.
type tokenCursor struct {
	Rank  int    `json:"r"`
	Sort  string `json:"s"`
	Denom string `json:"d"`
}

// EncodeTokenCursor is the cursor after a tokens list row: base64 (URL
// alphabet, no padding) of its keyset position, which holds text.
func EncodeTokenCursor(k repository.TokenKey) string {
	raw, _ := json.Marshal(tokenCursor{Rank: k.KindRank, Sort: k.SortKey, Denom: k.Denom}) // strings and an int always marshal
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeTokenCursor reverses EncodeTokenCursor.
func DecodeTokenCursor(cursor string) (*repository.TokenKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || !utf8.Valid(raw) {
		return nil, ErrInvalidCursor
	}
	var c tokenCursor
	if err := json.Unmarshal(raw, &c); err != nil || c.Denom == "" || c.Rank < 0 {
		return nil, ErrInvalidCursor
	}
	return &repository.TokenKey{KindRank: c.Rank, SortKey: c.Sort, Denom: c.Denom}, nil
}

// TokenKeyOf is the keyset position of a tokens list row.
func TokenKeyOf(t repository.TokenSummary) repository.TokenKey {
	return repository.TokenKey{KindRank: t.KindRank, SortKey: t.SortKey, Denom: t.Denom}
}
