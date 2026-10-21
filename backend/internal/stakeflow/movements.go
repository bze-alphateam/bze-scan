// Package stakeflow keeps the validators' stake movements of the last seven
// days in memory: every delegation, undelegation, redelegation, cancelled
// unbonding and initial self-stake the indexer stored, with each validator's
// net change over the last 24 hours and the last 7 days kept as running
// totals.
//
// The set is loaded once at start from the stored message events and then
// kept current by the writers' PostFlush hook; nothing is ever read again
// from the database after the load. The window is anchored on the newest
// indexed block, so movements leave the totals as new blocks arrive, never
// by a clock.
package stakeflow

import (
	"strconv"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/types/bech32"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// Window is how far back the set reaches; Day is the short window of the
// totals.
const (
	Window = 7 * 24 * time.Hour
	Day    = 24 * time.Hour
)

// Movement kinds.
const (
	KindDelegate        = "delegate"
	KindUndelegate      = "undelegate"
	KindRedelegateIn    = "redelegate_in"
	KindRedelegateOut   = "redelegate_out"
	KindCancelUnbonding = "cancel_unbonding"
	KindCreate          = "create"
)

// EventTypes are the stored event types a movement comes from.
var EventTypes = []string{"delegate", "unbond", "redelegate", "cancel_unbonding_delegation", "create_validator"}

// Ref locates a message: its key, its transaction's hash and the block time.
type Ref struct {
	Height   int64
	TxIndex  int
	MsgIndex int
	TxHash   string
	Time     time.Time
}

// Movement is one change of a validator's delegated stake.
type Movement struct {
	Height   int64
	TxIndex  int
	MsgIndex int
	// Seq is the position of the event among the message's events; both
	// sides of a redelegation share it.
	Seq    int
	TxHash string
	Time   time.Time
	Kind   string
	// Validator is the operator address whose stake moved.
	Validator string
	// Delegator is the account that moved it.
	Delegator string
	// Counterparty is the other validator of a redelegation, empty
	// otherwise.
	Counterparty string
	// Amount is the bond denom amount moved, always positive.
	Amount int64
}

// Delta is the movement's signed effect on the validator's stake.
func (m Movement) Delta() int64 {
	switch m.Kind {
	case KindUndelegate, KindRedelegateOut:
		return -m.Amount
	}
	return m.Amount
}

// FromEvents returns the movements of one message's stored events, in event
// order (a redelegation gives its out side, then its in side). Events of
// other types, amounts of another denom and events missing their validator
// are skipped.
func FromEvents(ref Ref, events []transform.Event) []Movement {
	var out []Movement
	for seq, e := range events {
		amount, ok := bondAmount(attr(e, "amount"))
		if !ok {
			continue
		}
		m := Movement{
			Height: ref.Height, TxIndex: ref.TxIndex, MsgIndex: ref.MsgIndex, Seq: seq,
			TxHash: ref.TxHash, Time: ref.Time, Delegator: attr(e, "delegator"), Amount: amount,
		}
		switch e.Type {
		case "delegate":
			m.Kind, m.Validator = KindDelegate, attr(e, "validator")
		case "unbond":
			m.Kind, m.Validator = KindUndelegate, attr(e, "validator")
		case "cancel_unbonding_delegation":
			m.Kind, m.Validator = KindCancelUnbonding, attr(e, "validator")
		case "create_validator":
			m.Kind, m.Validator = KindCreate, attr(e, "validator")
			if m.Delegator == "" {
				m.Delegator = operatorAccount(m.Validator)
			}
		case "redelegate":
			src, dst := attr(e, "source_validator"), attr(e, "destination_validator")
			if src == "" || dst == "" {
				continue
			}
			outSide, inSide := m, m
			outSide.Kind, outSide.Validator, outSide.Counterparty = KindRedelegateOut, src, dst
			inSide.Kind, inSide.Validator, inSide.Counterparty = KindRedelegateIn, dst, src
			out = append(out, outSide, inSide)
			continue
		default:
			continue
		}
		if m.Validator == "" {
			continue
		}
		out = append(out, m)
	}
	return out
}

// Block is an indexed height and its time.
type Block struct {
	Height int64
	Time   time.Time
}

// FromEntities returns the blocks of a flush and the movements of their
// messages, the same movements the load reads back from the stored events.
func FromEntities(ents *transform.Entities) ([]Block, []Movement) {
	if ents == nil {
		return nil, nil
	}
	blocks := make([]Block, 0, len(ents.Blocks))
	for _, b := range ents.Blocks {
		blocks = append(blocks, Block{Height: b.Height, Time: b.Time})
	}
	type txKey struct {
		height int64
		index  int
	}
	txs := make(map[txKey]*transform.Transaction, len(ents.Transactions))
	for i := range ents.Transactions {
		t := &ents.Transactions[i]
		txs[txKey{t.Height, t.TxIndex}] = t
	}
	var out []Movement
	for _, m := range ents.Messages {
		t := txs[txKey{m.Height, m.TxIndex}]
		if t == nil || !t.Success || len(m.Events) == 0 {
			continue
		}
		out = append(out, FromEvents(Ref{
			Height: m.Height, TxIndex: m.TxIndex, MsgIndex: m.MsgIndex, TxHash: t.Hash, Time: t.Time,
		}, m.Events)...)
	}
	return blocks, out
}

func attr(e transform.Event, key string) string {
	s, _ := e.Attrs[key].(string)
	return s
}

// bondAmount reads a single bond denom coin ("772573ubze"); a bare integer
// is taken as the bond denom too.
func bondAmount(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	amount := s
	if strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		coins, err := chain.ParseCoins(s)
		if err != nil || len(coins) != 1 || coins[0].Denom != chain.BondDenom {
			return 0, false
		}
		amount = coins[0].Amount
	}
	// An amount above int64 would be more than the whole supply.
	n, err := strconv.ParseInt(amount, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// operatorAccount is the account of a validator's operator address: the
// same bytes under the account prefix. Empty when it does not decode.
func operatorAccount(operator string) string {
	_, bz, err := bech32.DecodeAndConvert(operator)
	if err != nil {
		return ""
	}
	acc, err := bech32.ConvertAndEncode(chain.Bech32Prefix, bz)
	if err != nil {
		return ""
	}
	return acc
}
