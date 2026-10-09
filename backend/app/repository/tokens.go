package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// TokenSummary is a denoms row as the tokens list shows it.
type TokenSummary struct {
	Denom             string
	Symbol            *string
	Name              *string
	Kind              string
	Exponent          int
	Supply            *string
	HoldersCount      *int64
	PriceUSD          *string
	PriceChange24hPct *string
	Halted            bool
	LogoURL           *string
	// SortKey is the row's position within its kind (the lower-case symbol,
	// else the denom), for the keyset cursor.
	SortKey string
	// KindRank orders the kinds: native, factory, ibc, lp, then the rest.
	KindRank int
}

// Token is a whole denoms row.
type Token struct {
	TokenSummary
	Description    *string
	OriginChainID  *string
	IBCBaseDenom   *string
	IBCPath        *string
	Creator        *string
	Admin          *string
	CreatedHeight  *int64
	CreatedTxHash  *string
	CreatedTime    *time.Time
	Website        *string
	Markets        []string
	PriceUpdatedAt *time.Time
	Metadata       json.RawMessage
	UpdatedAt      time.Time
	CreatorLabel   *Label
	AdminLabel     *Label
}

// TokenKey is the keyset position of the tokens list.
type TokenKey struct {
	KindRank int
	SortKey  string
	Denom    string
}

// TokenEvent is a token_events row with the hash of its transaction (nil
// for a block-level event) and the label of its actor.
type TokenEvent struct {
	Height     int64
	TxIndex    int64
	Seq        int64
	TxHash     *string
	Kind       string
	Actor      *string
	ActorLabel *Label
	Amount     *string
	Details    json.RawMessage
	Time       time.Time
}

// EventKey is the keyset position of a list of rows keyed by (height,
// tx_index, seq), newest first.
type EventKey struct {
	Height  int64
	TxIndex int64
	Seq     int64
}

// DenomTransfer is a transfers row of one denom with its height, its
// transaction's hash (nil for a block-level row) and its block's time.
type DenomTransfer struct {
	Transfer
	Height  int64
	TxIndex int64
	TxHash  *string
	Time    time.Time
}

// kindRank is the SQL rank of a denoms row's kind.
const kindRank = `CASE d.kind WHEN 'native' THEN 0 WHEN 'factory' THEN 1 WHEN 'ibc' THEN 2 WHEN 'lp' THEN 3 ELSE 4 END`

// sortKey is the SQL position of a denoms row within its kind.
const sortKey = `lower(coalesce(d.symbol, d.denom))`

const tokenSummaryCols = `d.denom, d.symbol, d.name, d.kind, d.exponent, d.supply::text, d.holders_count,
		d.price_usd::text, d.price_change_24h_pct::text, d.halted, d.logo_url, ` + sortKey + `, ` + kindRank

func tokenSummaryDest(t *TokenSummary) []any {
	return []any{&t.Denom, &t.Symbol, &t.Name, &t.Kind, &t.Exponent, &t.Supply, &t.HoldersCount,
		&t.PriceUSD, &t.PriceChange24hPct, &t.Halted, &t.LogoURL, &t.SortKey, &t.KindRank}
}

// Tokens returns up to limit denoms of kind (every kind when nil), by kind
// (native, factory, ibc, lp, the rest) then symbol, after the key after.
func (r *Explorer) Tokens(ctx context.Context, kind *string, after *TokenKey, limit int) ([]TokenSummary, error) {
	args := []any{kind, limit}
	keyset := ""
	if after != nil {
		args = append(args, after.KindRank, after.SortKey, after.Denom)
		keyset = ` AND (` + kindRank + `, ` + sortKey + `, d.denom) > ($3, $4, $5)`
	}
	rows, err := r.db.Query(ctx, `SELECT `+tokenSummaryCols+` FROM explorer.denoms d
		WHERE ($1::text IS NULL OR d.kind = $1)`+keyset+`
		ORDER BY `+kindRank+`, `+sortKey+`, d.denom LIMIT $2`, args...)
	if err != nil {
		return nil, fmt.Errorf("tokens: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (TokenSummary, error) {
		var t TokenSummary
		return t, row.Scan(tokenSummaryDest(&t)...)
	})
	if err != nil {
		return nil, fmt.Errorf("tokens: %w", err)
	}
	return out, nil
}

// Token returns the denoms row of denom, or ErrNotFound.
func (r *Explorer) Token(ctx context.Context, denom string) (*Token, error) {
	var t Token
	var creatorName, creatorKind, adminName, adminKind *string
	dest := append(tokenSummaryDest(&t.TokenSummary), &t.Description, &t.OriginChainID, &t.IBCBaseDenom, &t.IBCPath,
		&t.Creator, &t.Admin, &t.CreatedHeight, &t.CreatedTxHash, &t.CreatedTime, &t.Website, &t.Markets,
		&t.PriceUpdatedAt, &t.Metadata, &t.UpdatedAt, &creatorName, &creatorKind, &adminName, &adminKind)
	err := r.db.QueryRow(ctx, `SELECT `+tokenSummaryCols+`, d.description, d.origin_chain_id, d.ibc_base_denom,
			d.ibc_path, d.creator, d.admin, d.created_height, d.created_tx_hash, d.created_time, d.website,
			d.markets, d.price_updated_at, d.metadata, d.updated_at, lc.name, lc.kind, la.name, la.kind
		FROM explorer.denoms d
		LEFT JOIN explorer.labels lc ON lc.address = d.creator
		LEFT JOIN explorer.labels la ON la.address = d.admin
		WHERE d.denom = $1`, denom).Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("token %s: %w", denom, err)
	}
	t.CreatorLabel = label(t.Creator, creatorName, creatorKind)
	t.AdminLabel = label(t.Admin, adminName, adminKind)
	return &t, nil
}

// TokenEvents returns up to limit token_events rows of denom, newest first,
// before the key before (from the newest when nil).
func (r *Explorer) TokenEvents(ctx context.Context, denom string, before *EventKey, limit int) ([]TokenEvent, error) {
	args := []any{denom, limit}
	keyset := ""
	if before != nil {
		args = append(args, before.Height, before.TxIndex, before.Seq)
		keyset = ` AND (e.height, e.tx_index, e.seq) < ($3, $4, $5)`
	}
	rows, err := r.db.Query(ctx, `SELECT e.height, e.tx_index, e.seq, t.hash, e.kind, e.actor, l.name, l.kind,
			e.amount::text, e.details, e.time
		FROM explorer.token_events e
		LEFT JOIN explorer.transactions t ON t.height = e.height AND t.tx_index = e.tx_index
		LEFT JOIN explorer.labels l ON l.address = e.actor
		WHERE e.denom = $1`+keyset+`
		ORDER BY e.height DESC, e.tx_index DESC, e.seq DESC LIMIT $2`, args...)
	if err != nil {
		return nil, fmt.Errorf("token events %s: %w", denom, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (TokenEvent, error) {
		var e TokenEvent
		var name, kind *string
		err := row.Scan(&e.Height, &e.TxIndex, &e.Seq, &e.TxHash, &e.Kind, &e.Actor, &name, &kind,
			&e.Amount, &e.Details, &e.Time)
		e.ActorLabel = label(e.Actor, name, kind)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("token events %s: %w", denom, err)
	}
	return out, nil
}

// DenomTransfers returns up to limit transfers rows of denom, newest first,
// before the key before (from the newest when nil).
func (r *Explorer) DenomTransfers(ctx context.Context, denom string, before *EventKey, limit int) ([]DenomTransfer, error) {
	args := []any{denom, limit}
	keyset := ""
	if before != nil {
		args = append(args, before.Height, before.TxIndex, before.Seq)
		keyset = ` AND (x.height, x.tx_index, x.seq) < ($3, $4, $5)`
	}
	rows, err := r.db.Query(ctx, `SELECT x.height, x.tx_index, t.hash, b.time, x.seq, x.msg_index, x.kind,
			x.sender, x.recipient, x.denom, x.amount::text, d.symbol, d.exponent, ls.name, ls.kind, lr.name, lr.kind
		FROM explorer.transfers x
		JOIN explorer.blocks b ON b.height = x.height
		LEFT JOIN explorer.transactions t ON t.height = x.height AND t.tx_index = x.tx_index
		LEFT JOIN explorer.denoms d ON d.denom = x.denom
		LEFT JOIN explorer.labels ls ON ls.address = x.sender
		LEFT JOIN explorer.labels lr ON lr.address = x.recipient
		WHERE x.denom = $1`+keyset+`
		ORDER BY x.height DESC, x.tx_index DESC, x.seq DESC LIMIT $2`, args...)
	if err != nil {
		return nil, fmt.Errorf("denom transfers %s: %w", denom, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (DenomTransfer, error) {
		var x DenomTransfer
		var senderName, senderKind, recipientName, recipientKind *string
		err := row.Scan(&x.Height, &x.TxIndex, &x.TxHash, &x.Time, &x.Seq, &x.MsgIndex, &x.Kind,
			&x.Sender, &x.Recipient, &x.Denom, &x.Amount, &x.Symbol, &x.Exponent,
			&senderName, &senderKind, &recipientName, &recipientKind)
		x.SenderLabel = label(x.Sender, senderName, senderKind)
		x.RecipientLabel = label(x.Recipient, recipientName, recipientKind)
		return x, err
	})
	if err != nil {
		return nil, fmt.Errorf("denom transfers %s: %w", denom, err)
	}
	return out, nil
}
