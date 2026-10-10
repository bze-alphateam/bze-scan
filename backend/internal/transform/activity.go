package transform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/classify"
	"github.com/bze-alphateam/bze-scan/backend/internal/labels"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// Activity is one explorer.account_activity row: what one transaction (or,
// with TxIndex BlockTxIndex, one block's finalize events) did to one
// address.
type Activity struct {
	Address  string    `json:"address"`
	Height   int64     `json:"height"`
	TxIndex  int       `json:"tx_index"`
	Time     time.Time `json:"time"`
	Category string    `json:"category"`
	Kind     string    `json:"kind"`
	IsSigner bool      `json:"is_signer"`
	Success  bool      `json:"success"`
	// Counterparty is the other party when there is exactly one (an
	// account, a validator operator or a module account); empty otherwise.
	Counterparty string `json:"counterparty"`
	// Deltas is the net balance change of the address per denom, by denom,
	// zero entries dropped; the fee is part of it.
	Deltas []chain.Coin `json:"deltas"`
	// MsgTypes are the type URLs of the messages naming the address, first
	// one first (explorer.reclassify_unknown() reads it); every message of
	// the transaction for a signer.
	MsgTypes []string `json:"msg_types"`
	// Details are the typed events naming the address; block-level rows
	// only.
	Details []Event `json:"details"`
}

// Address prefixes the activity rules recognise.
const valoperPrefix = chain.Bech32Prefix + "valoper"

// ibcTransferPort is the port of the ICS-20 escrow accounts.
const ibcTransferPort = "transfer"

// moduleAccounts are the module accounts the labels seed names; they never
// get activity rows (the fee collector would get one per block).
var moduleAccounts = func() map[string]bool {
	out := map[string]bool{}
	for _, l := range labels.Modules() {
		out[l.Address] = true
	}
	return out
}()

// isAccount reports whether s is a bze1 account address with a valid
// checksum, so random strings never match.
func isAccount(s string) bool {
	return hasPrefix(s, chain.Bech32Prefix)
}

func hasPrefix(s, hrp string) bool {
	if !strings.HasPrefix(s, hrp+"1") {
		return false
	}
	got, data, err := bech32.DecodeAndConvert(s)
	return err == nil && got == hrp && (len(data) == 20 || len(data) == 32)
}

// walkStrings calls f for every string in v (a decoded JSON value),
// depth first.
func walkStrings(v any, f func(string)) {
	switch x := v.(type) {
	case string:
		f(x)
	case []any:
		for _, e := range x {
			walkStrings(e, f)
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkStrings(x[k], f)
		}
	}
}

// accountsIn returns the distinct accounts named by the values of events,
// in order of appearance.
func accountsIn(events []Event) []string {
	var out []string
	seen := map[string]bool{}
	for _, ev := range events {
		keys := make([]string, 0, len(ev.Attrs))
		for k := range ev.Attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkStrings(ev.Attrs[k], func(s string) {
				if !seen[s] && isAccount(s) {
					seen[s] = true
					out = append(out, s)
				}
			})
		}
	}
	return out
}

// accountsInBody returns the distinct accounts named by a message body
// (proto JSON).
func accountsInBody(body json.RawMessage) []string {
	if len(body) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil
	}
	var out []string
	walkStrings(v, func(s string) {
		if isAccount(s) && !slices.Contains(out, s) {
			out = append(out, s)
		}
	})
	return out
}

// balances are net balance changes per address and denom.
type balances map[string]map[string]sdkmath.Int

// add sums the coin_received (plus) and coin_spent (minus) events of
// events; skip leaves an event out.
func (b balances) add(events []node.Event, skip func(node.Event) bool) error {
	for _, ev := range events {
		var who string
		var sign int64
		switch ev.Type {
		case "coin_received":
			who, _ = ev.Get("receiver")
			sign = 1
		case "coin_spent":
			who, _ = ev.Get("spender")
			sign = -1
		default:
			continue
		}
		if who == "" || (skip != nil && skip(ev)) {
			continue
		}
		raw, _ := ev.Get("amount")
		coins, err := chain.ParseCoins(raw)
		if err != nil {
			return fmt.Errorf("%s amount: %w", ev.Type, err)
		}
		for _, c := range coins {
			n, ok := sdkmath.NewIntFromString(c.Amount)
			if !ok {
				return fmt.Errorf("%s amount %q", ev.Type, c.Amount)
			}
			if b[who] == nil {
				b[who] = map[string]sdkmath.Int{}
			}
			cur, ok := b[who][c.Denom]
			if !ok {
				cur = sdkmath.ZeroInt()
			}
			b[who][c.Denom] = cur.Add(n.MulRaw(sign))
		}
	}
	return nil
}

// of returns the non-zero changes of address, by denom.
func (b balances) of(address string) []chain.Coin {
	out := []chain.Coin{}
	for denom, n := range b[address] {
		if !n.IsZero() {
			out = append(out, chain.Coin{Denom: denom, Amount: n.String()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Denom < out[j].Denom })
	return out
}

func hasPositive(deltas []chain.Coin) bool {
	for _, d := range deltas {
		if !strings.HasPrefix(d.Amount, "-") {
			return true
		}
	}
	return false
}

// escrowAccounts returns the ICS-20 escrow accounts of the channels the
// events name. Like the module accounts they would get a row for every
// cross-chain transfer, so they get none.
func escrowAccounts(events []node.Event) map[string]bool {
	out := map[string]bool{}
	for _, ev := range events {
		for _, a := range ev.Attributes {
			if (a.Key == "packet_src_channel" || a.Key == "packet_dst_channel") && a.Value != "" {
				addr, err := bech32.ConvertAndEncode(chain.Bech32Prefix, ibctransfertypes.GetEscrowAddress(ibcTransferPort, a.Value))
				if err == nil {
					out[addr] = true
				}
			}
		}
	}
	return out
}

func distinct(s []string) []string {
	out := make([]string, 0, len(s))
	for _, v := range s {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// txActivity appends the account_activity rows of a transaction whose
// transactions row is tx and whose messages rows are msgs: one row per
// address the transaction names (in its events or message bodies) or that
// signed it, except module and escrow accounts; non-signers named only by
// relay messages only when their balance changed. A failed transaction
// yields one fee_only row for its fee payer.
func txActivity(ents *Entities, tx *Transaction, msgs []Message, events []node.Event) error {
	signers := map[string]bool{}
	for _, s := range tx.Signers {
		if s != "" {
			signers[s] = true
		}
	}
	row := func(address string) Activity {
		return Activity{
			Address: address, Height: tx.Height, TxIndex: tx.TxIndex, Time: tx.Time,
			IsSigner: signers[address], Success: tx.Success, Deltas: []chain.Coin{}, MsgTypes: []string{},
		}
	}
	ante := func(ev node.Event) bool {
		_, ok := ev.Get("msg_index")
		return ok
	}

	if !tx.Success {
		payer := tx.FeePayer
		if payer == "" && len(tx.Signers) > 0 {
			payer = tx.Signers[0]
		}
		if payer == "" || moduleAccounts[payer] {
			return nil
		}
		book := balances{}
		if err := book.add(events, ante); err != nil {
			return fmt.Errorf("activity: %w", err)
		}
		a := row(payer)
		a.Kind, a.Category = classify.KindFeeOnly, classify.Other
		a.Deltas = book.of(payer)
		a.MsgTypes = distinct(tx.MsgTypes)
		ents.Activity = append(ents.Activity, a)
		return nil
	}

	book := balances{}
	if err := book.add(events, nil); err != nil {
		return fmt.Errorf("activity: %w", err)
	}
	// named[address] are the indexes of the messages naming it, in order.
	named := map[string][]int{}
	for j, m := range msgs {
		for _, addr := range append(accountsIn(m.Events), accountsInBody(m.Body)...) {
			if js := named[addr]; len(js) == 0 || js[len(js)-1] != j {
				named[addr] = append(named[addr], j)
			}
		}
	}
	addresses := map[string]bool{}
	for s := range signers {
		addresses[s] = true
	}
	for addr := range named {
		addresses[addr] = true
	}
	var anteEvents []Event
	for _, ev := range events {
		if !ante(ev) {
			anteEvents = append(anteEvents, storedEvent(ev))
		}
	}
	for _, addr := range accountsIn(anteEvents) {
		addresses[addr] = true
	}
	escrows := escrowAccounts(events)

	for _, addr := range slices.Sorted(maps.Keys(addresses)) {
		if moduleAccounts[addr] || escrows[addr] {
			continue
		}
		a := row(addr)
		a.Deltas = book.of(addr)
		var entry classify.MessageKind
		var involved []int // the messages naming the address
		switch js := named[addr]; {
		case a.IsSigner:
			for j := range msgs {
				involved = append(involved, j)
			}
			entry = classify.OtherMessage
			if len(msgs) > 0 {
				entry = classify.Lookup(msgs[0].TypeURL)
			}
			a.Kind, a.Category = entry.Kind, entry.SignerCategory
		case len(js) > 0:
			involved = js
			entry = classify.Lookup(msgs[js[0]].TypeURL)
			a.Kind, a.Category = entry.Kind, entry.ParticipantCategory
			relayOnly := true
			for _, j := range js {
				relayOnly = relayOnly && classify.IsRelayParticipantMsg(msgs[j].TypeURL)
			}
			if relayOnly && len(a.Deltas) == 0 {
				continue
			}
			if hasPositive(a.Deltas) {
				switch msgs[js[0]].TypeURL {
				case classify.MsgRecvPacket:
					a.Kind = classify.KindIBCIn
				case classify.MsgAcknowledgement, classify.MsgTimeout, classify.MsgTimeoutOnClose:
					a.Kind = classify.KindIBCRefund
				}
			}
		default:
			// Named by the ante handler's events only: a fee granter.
			a.Kind, a.Category = classify.KindFeeOnly, classify.Other
		}
		for _, j := range involved {
			a.MsgTypes = append(a.MsgTypes, msgs[j].TypeURL)
		}
		a.MsgTypes = distinct(a.MsgTypes)
		a.Counterparty = counterparty(entry, msgs, involved, addr, a.IsSigner)
		ents.Activity = append(ents.Activity, a)
	}
	return nil
}

// counterparty resolves the entry's counterparty attribute over the
// messages of the entry's type among involved: the one account, validator
// operator or module account it names other than address. A non-signer
// whose attribute names nobody else (the recipient of a send names
// itself) falls back to the one sender of those messages, unless they are
// relay messages. Empty when there is not exactly one.
func counterparty(entry classify.MessageKind, msgs []Message, involved []int, address string, signer bool) string {
	var same []Message
	for _, j := range involved {
		if msgs[j].TypeURL == entry.TypeURL {
			same = append(same, msgs[j])
		}
	}
	valid := func(s string) bool {
		return s != address && (isAccount(s) || hasPrefix(s, valoperPrefix))
	}
	if i := strings.LastIndexByte(entry.CounterpartyAttr, '.'); i > 0 {
		evType, attr := entry.CounterpartyAttr[:i], entry.CounterpartyAttr[i+1:]
		var found []string
		for _, m := range same {
			for _, ev := range m.Events {
				if ev.Type != evType {
					continue
				}
				walkStrings(ev.Attrs[attr], func(s string) {
					if valid(s) && !slices.Contains(found, s) {
						found = append(found, s)
					}
				})
			}
		}
		if len(found) == 1 {
			return found[0]
		}
		if len(found) > 1 {
			return ""
		}
	}
	// The sender of a relay message is the relayer, never the
	// counterparty of the transfer it carries.
	if signer || entry.Kind == classify.KindRelay {
		return ""
	}
	var senders []string
	for _, m := range same {
		if valid(m.Sender) && !slices.Contains(senders, m.Sender) {
			senders = append(senders, m.Sender)
		}
	}
	if len(senders) == 1 {
		return senders[0]
	}
	return ""
}

// blockActivity appends the block-level account_activity rows of a
// height: one per account named by the address attributes of its stored
// block events, kind and category from the first such event, details the
// events naming it, deltas from the block's coin_spent/coin_received
// events.
func blockActivity(ents *Entities, b Block, events []node.Event) error {
	book := balances{}
	if err := book.add(events, nil); err != nil {
		return fmt.Errorf("block activity: %w", err)
	}
	rows := map[string]*Activity{}
	var order []string
	for _, e := range ents.BlockEvents {
		if e.Height != b.Height {
			continue
		}
		kind, _ := classify.LookupBlockEvent(e.Type)
		var named []string
		for _, attr := range kind.AddressAttrs {
			walkStrings(e.Attrs[attr], func(s string) {
				if isAccount(s) && !moduleAccounts[s] && !slices.Contains(named, s) {
					named = append(named, s)
				}
			})
		}
		for _, addr := range named {
			a, ok := rows[addr]
			if !ok {
				a = &Activity{
					Address: addr, Height: b.Height, TxIndex: BlockTxIndex, Time: b.Time,
					Kind: kind.Kind, Category: kind.Category, Success: true,
					Deltas: book.of(addr), MsgTypes: []string{}, Details: []Event{},
				}
				rows[addr] = a
				order = append(order, addr)
			}
			a.Details = append(a.Details, Event{Type: e.Type, Attrs: e.Attrs})
		}
	}
	sort.Strings(order)
	for _, addr := range order {
		ents.Activity = append(ents.Activity, *rows[addr])
	}
	return nil
}
