package transform

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/denoms"
)

// Kinds of the token_events rows: the history of a denom that the chain
// state forgets.
const (
	TokenCreated         = "created"
	TokenMinted          = "minted"
	TokenBurned          = "burned"
	TokenAdminChanged    = "admin_changed"
	TokenMetadataChanged = "metadata_changed"
	TokenBrandingChanged = "branding_changed"
	TokenHalted          = "halted"
	TokenUnhalted        = "unhalted"
	TokenMarketCreated   = "market_created"
	TokenPoolCreated     = "pool_created"
)

// Type URLs of the tokenfactory messages.
const (
	msgCreateDenom      = "/bze.tokenfactory.MsgCreateDenom"
	msgMint             = "/bze.tokenfactory.MsgMint"
	msgBurn             = "/bze.tokenfactory.MsgBurn"
	msgChangeAdmin      = "/bze.tokenfactory.MsgChangeAdmin"
	msgSetDenomMetadata = "/bze.tokenfactory.MsgSetDenomMetadata"
	msgSetDenomBranding = "/bze.tokenfactory.MsgSetDenomBranding"
)

// Typed events that name denoms.
const (
	evDenomAdminChange    = "bze.tokenfactory.DenomAdminChangeEvent"
	evDenomBrandingChange = "bze.tokenfactory.DenomBrandingChangeEvent"
	evDenomMetadataChange = "bze.tokenfactory.DenomMetadataChangeEvent"
	evMarketCreated       = "bze.tradebin.MarketCreatedEvent"
	evPoolCreated         = "bze.tradebin.PoolCreatedEvent"
	evDenomHalted         = "bze.tradebin.DenomHaltedEvent"
	evDenomUnhalted       = "bze.tradebin.DenomUnhaltedEvent"
)

// TokenEvent is one explorer.token_events row.
type TokenEvent struct {
	Height int64
	// TxIndex is -1 for a block-level event (a halt enacted by governance).
	TxIndex int
	// Seq orders the token events of one transaction (or of the block's
	// finalize events), from 0.
	Seq   int
	Denom string
	Kind  string
	// Actor is the message's sender; empty for a block-level event.
	Actor string
	// Amount is in base units; empty for kinds without one.
	Amount string
	// Details is the row's details object; nil is NULL.
	Details map[string]any
	Time    time.Time
}

// tokenRows appends the token events of one transaction (or of the block)
// with consecutive seq.
type tokenRows struct {
	ents    *Entities
	height  int64
	txIndex int
	time    time.Time
	seq     int
}

func (r *tokenRows) add(denom, kind, actor, amount string, details map[string]any) {
	if denom == "" {
		return
	}
	r.ents.TokenEvents = append(r.ents.TokenEvents, TokenEvent{
		Height: r.height, TxIndex: r.txIndex, Seq: r.seq, Denom: denom, Kind: kind,
		Actor: actor, Amount: amount, Details: details, Time: r.time,
	})
	r.seq++
	r.ents.Dirty.Mark(statesync.Denoms, denom)
}

// txTokenEvents appends the token events of a successful transaction: the
// tokenfactory messages, then the typed events of each message. byIndex are
// its events by message.
func txTokenEvents(ents *Entities, b Block, txIndex int, msgs []chain.Msg, byIndex map[int][]node.Event) error {
	rows := &tokenRows{ents: ents, height: b.Height, txIndex: txIndex, time: b.Time}
	for j, m := range msgs {
		var body map[string]any
		_ = json.Unmarshal(m.Body, &body) // an undecodable body leaves it nil
		actor := m.Signer
		if actor == "" {
			actor = str(body, "creator")
		}
		switch m.TypeURL {
		case msgCreateDenom:
			creator, sub := str(body, "creator"), str(body, "subdenom")
			if creator != "" && sub != "" {
				rows.add("factory/"+creator+"/"+sub, TokenCreated, actor, "", map[string]any{"subdenom": sub})
			}
		case msgMint, msgBurn:
			kind, details := TokenBurned, map[string]any(nil)
			if m.TypeURL == msgMint {
				// The tokenfactory mints to the admin who signs.
				kind, details = TokenMinted, map[string]any{"recipient": actor}
			}
			coins, err := chain.ParseCoins(str(body, "coins"))
			if err != nil {
				return fmt.Errorf("msg %d: %s coins: %w", j, m.TypeURL, err)
			}
			for _, c := range coins {
				rows.add(c.Denom, kind, actor, c.Amount, details)
			}
		case msgChangeAdmin:
			details := map[string]any{"to": str(body, "newAdmin")}
			for _, ev := range byIndex[j] {
				if ev.Type == evDenomAdminChange {
					attrs := storedEvent(ev).Attrs
					details["from"], details["to"] = attrs["admin"], attrs["new_admin"]
					break
				}
			}
			rows.add(str(body, "denom"), TokenAdminChanged, actor, "", details)
		case msgSetDenomMetadata:
			md, _ := body["metadata"].(map[string]any)
			rows.add(str(md, "base"), TokenMetadataChanged, actor, "",
				map[string]any{"symbol": str(md, "symbol"), "name": str(md, "name"), "display": str(md, "display")})
		case msgSetDenomBranding:
			rows.add(str(body, "denom"), TokenBrandingChanged, actor, "", nil)
		}
		for _, ev := range byIndex[j] {
			typedTokenEvents(rows, ev, actor)
		}
	}
	return nil
}

// blockTokenEvents appends the token events of the block's finalize events:
// the halts governance enacts in EndBlock.
func blockTokenEvents(ents *Entities, b Block, events []node.Event) {
	rows := &tokenRows{ents: ents, height: b.Height, txIndex: -1, time: b.Time}
	for _, ev := range events {
		typedTokenEvents(rows, ev, "")
	}
}

// typedTokenEvents appends the rows of one tradebin typed event, and marks
// the denoms of the tokenfactory ones dirty (their messages are rows
// already).
func typedTokenEvents(rows *tokenRows, ev node.Event, actor string) {
	switch ev.Type {
	case evDenomHalted, evDenomUnhalted:
		kind := TokenHalted
		if ev.Type == evDenomUnhalted {
			kind = TokenUnhalted
		}
		rows.add(attrString(ev, "denom"), kind, actor, "", nil)
	case evMarketCreated:
		attrs := storedEvent(ev).Attrs
		base, quote := anyString(attrs["base"]), anyString(attrs["quote"])
		if actor == "" {
			actor = anyString(attrs["creator"])
		}
		details := map[string]any{"market_id": base + "/" + quote, "base": base, "quote": quote}
		rows.add(base, TokenMarketCreated, actor, "", details)
		rows.add(quote, TokenMarketCreated, actor, "", details)
	case evPoolCreated:
		attrs := storedEvent(ev).Attrs
		base, quote, lp := anyString(attrs["base"]), anyString(attrs["quote"]), anyString(attrs["lp_denom"])
		if actor == "" {
			actor = anyString(attrs["creator"])
		}
		details := map[string]any{"base": base, "quote": quote}
		if lp != "" {
			details["lp_denom"] = lp
		}
		rows.add(base, TokenPoolCreated, actor, "", details)
		rows.add(quote, TokenPoolCreated, actor, "", details)
		rows.add(lp, TokenPoolCreated, actor, "", details)
	case evDenomAdminChange, evDenomBrandingChange, evDenomMetadataChange:
		rows.ents.Dirty.Mark(statesync.Denoms, attrString(ev, "denom"))
	}
}

// markSeenDenoms marks every denom the heights' transfers moved, for the
// denoms set to resync the ones it does not hold yet.
func markSeenDenoms(ents *Entities) {
	for _, t := range ents.Transfers {
		ents.Dirty.Mark(statesync.Denoms, denoms.SeenKey(t.Denom))
	}
}

// attrString is a typed event's attribute decoded to a string.
func attrString(ev node.Event, key string) string {
	return anyString(storedEvent(ev).Attrs[key])
}

func anyString(v any) string {
	s, _ := v.(string)
	return s
}
