package archive

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// ErrUnknownGeneration is returned for a height whose shape does not match
// the event format of its generation.
var ErrUnknownGeneration = errors.New("unknown event generation")

// Decoder decodes the raw bytes of a transaction; *chain.Codec satisfies it.
type Decoder interface {
	Decode(raw []byte) (*chain.Tx, error)
}

// Adapter normalises archive input of any generation to the current event
// format. It satisfies the backfill pipeline's Adapter.
type Adapter struct {
	decoder Decoder
}

// New returns an adapter that decodes transactions with decoder.
func New(decoder Decoder) *Adapter {
	return &Adapter{decoder: decoder}
}

// Adapt normalises in, the archive's answers for height, in place. A height
// of the current generation is left untouched.
func (a *Adapter) Adapt(height int64, in *transform.Input) error {
	if in.Block == nil || in.Results == nil {
		return fmt.Errorf("adapt %d: incomplete input", height)
	}
	if len(in.Results.BeginBlockEvents) > 0 || len(in.Results.EndBlockEvents) > 0 {
		return fmt.Errorf("%w: height %d answered in the layout of a node older than CometBFT 0.38 (begin/end block events)",
			ErrUnknownGeneration, height)
	}
	switch g := GenerationAt(height); g.Format {
	case FormatCurrent:
		return nil
	case FormatLegacy:
		if err := a.legacy(in); err != nil {
			return fmt.Errorf("height %d (%s): %w", height, g.Release, err)
		}
		return nil
	default:
		return fmt.Errorf("%w: height %d", ErrUnknownGeneration, height)
	}
}

// legacy normalises a height of the SDK 0.44/0.45 format.
func (a *Adapter) legacy(in *transform.Input) error {
	for i := range in.Results.FinalizeBlockEvents {
		if err := typedEvent(&in.Results.FinalizeBlockEvents[i]); err != nil {
			return err
		}
	}
	if len(in.Block.Txs) != len(in.Results.TxsResults) {
		return fmt.Errorf("%d transactions but %d results", len(in.Block.Txs), len(in.Results.TxsResults))
	}
	for i := range in.Results.TxsResults {
		if err := a.transaction(in.Block.Txs[i], &in.Results.TxsResults[i]); err != nil {
			return fmt.Errorf("tx %d: %w", i, err)
		}
	}
	return nil
}

// transaction normalises the events of one transaction result.
func (a *Adapter) transaction(rawB64 string, res *node.TxResult) error {
	var starts []int
	for k := range res.Events {
		ev := &res.Events[k]
		if _, ok := ev.Get("msg_index"); ok {
			return fmt.Errorf("%w: msg_index on a legacy height", ErrUnknownGeneration)
		}
		if err := typedEvent(ev); err != nil {
			return err
		}
		if _, ok := ev.Get("action"); ok && ev.Type == "message" {
			starts = append(starts, k)
		}
	}
	if res.Code != 0 {
		// A failed transaction keeps its ante-handler events only.
		if len(starts) > 0 {
			return fmt.Errorf("%w: message events in a failed transaction", ErrUnknownGeneration)
		}
		return nil
	}

	raw, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil {
		return fmt.Errorf("raw bytes: %w", err)
	}
	tx, err := a.decoder.Decode(raw)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if len(starts) != len(tx.Msgs) {
		return fmt.Errorf("%w: %d message events for %d messages", ErrUnknownGeneration, len(starts), len(tx.Msgs))
	}

	for j, msg := range tx.Msgs {
		end := len(res.Events)
		if j+1 < len(starts) {
			end = starts[j+1]
		}
		seg := res.Events[starts[j]:end]
		if err := message(j, msg, seg); err != nil {
			return fmt.Errorf("msg %d: %w", j, err)
		}
	}
	return nil
}

// message normalises the events of message j: seg[0] is its message event
// with the action, the rest are the events the message emitted.
func message(j int, msg chain.Msg, seg []node.Event) error {
	action, _ := seg[0].Get("action")
	if !actionMatches(action, msg.TypeURL) {
		return fmt.Errorf("%w: action %q for a %s", ErrUnknownGeneration, action, msg.TypeURL)
	}

	// The leading event as SDK 0.50 builds it (baseapp createEvents).
	lead := []node.Attribute{{Key: "action", Value: msg.TypeURL}}
	if msg.Signer != "" {
		lead = append(lead, node.Attribute{Key: "sender", Value: msg.Signer})
	}
	if !hasAttribute(seg[1:], "module") {
		if m := sdk.GetModuleNameFromTypeURL(msg.TypeURL); m != "" {
			lead = append(lead, node.Attribute{Key: "module", Value: m})
		}
	}
	seg[0].Attributes = lead

	if err := fillFacts(msg, seg[1:]); err != nil {
		return err
	}
	idx := node.Attribute{Key: "msg_index", Value: fmt.Sprint(j)}
	for k := range seg {
		seg[k].Attributes = append(seg[k].Attributes, idx)
	}
	return nil
}

// actionMatches reports whether a legacy message.action names typeURL: a
// type URL (possibly of an old proto package) or a legacy router name.
func actionMatches(action, typeURL string) bool {
	if strings.HasPrefix(action, "/") {
		return chain.CanonicalTypeURL(action) == typeURL
	}
	return slices.Contains(legacyActions[action], typeURL)
}

// typedEvent renames a BZE typed event to its current type and checks its
// attribute values are JSON, as every generation encoded them (except mode,
// the plain BeginBlock/EndBlock marker of block-level events, then and now).
func typedEvent(ev *node.Event) error {
	if !strings.HasPrefix(ev.Type, "bze.") {
		return nil
	}
	current, ok := legacyEventTypes[ev.Type]
	if !ok {
		return fmt.Errorf("%w: typed event %s", ErrUnknownGeneration, ev.Type)
	}
	for _, at := range ev.Attributes {
		if at.Key != "mode" && !json.Valid([]byte(at.Value)) {
			return fmt.Errorf("%w: %s.%s is not JSON", ErrUnknownGeneration, ev.Type, at.Key)
		}
	}
	ev.Type = current
	return nil
}

// actor is one message a message event belongs to: the message itself, or
// one executed by an authz MsgExec (at any depth), in execution order.
type actor struct {
	typeURL string
	body    map[string]any
}

func actors(msg chain.Msg) ([]actor, error) {
	if msg.Body == nil {
		return []actor{{typeURL: msg.TypeURL}}, nil
	}
	var body map[string]any
	if err := json.Unmarshal(msg.Body, &body); err != nil {
		return nil, fmt.Errorf("message body: %w", err)
	}
	return flatten(msg.TypeURL, body), nil
}

func flatten(typeURL string, body map[string]any) []actor {
	if typeURL != execType {
		return []actor{{typeURL: typeURL, body: body}}
	}
	out := []actor{{typeURL: typeURL, body: body}}
	inner, _ := body["msgs"].([]any)
	for _, m := range inner {
		mb, ok := m.(map[string]any)
		if !ok {
			continue
		}
		t, _ := mb["@type"].(string)
		out = append(out, flatten(t, mb)...)
	}
	return out
}

// fillFacts adds the attributes SDK 0.50 and ibc-go 8 emit and the legacy
// generation lacks, taken from the messages that emitted the events.
func fillFacts(msg chain.Msg, events []node.Event) error {
	var votes, deposits, submits, transfers []*node.Event
	for k := range events {
		ev := &events[k]
		switch {
		case ev.Type == "proposal_vote":
			votes = append(votes, ev)
		case ev.Type == "proposal_deposit" && has(ev, "amount"):
			deposits = append(deposits, ev)
		case ev.Type == "submit_proposal" && has(ev, "proposal_id"):
			submits = append(submits, ev)
		case ev.Type == "ibc_transfer" && has(ev, "sender"):
			transfers = append(transfers, ev)
		}
	}
	if len(votes)+len(deposits)+len(submits)+len(transfers) == 0 {
		return nil
	}
	acts, err := actors(msg)
	if err != nil {
		return err
	}
	pick := func(types ...string) []actor {
		var out []actor
		for _, a := range acts {
			if slices.Contains(types, a.typeURL) {
				out = append(out, a)
			}
		}
		return out
	}

	voters := pick(voteTypes...)
	if err := pair("proposal_vote", votes, voters, func(ev *node.Event, a actor) error {
		if has(ev, "voter") {
			return fmt.Errorf("%w: proposal_vote already has a voter", ErrUnknownGeneration)
		}
		opt, _ := ev.Get("option")
		options, err := voteOptions(opt)
		if err != nil {
			return err
		}
		voter, _ := a.body["voter"].(string)
		attrs := []node.Attribute{{Key: "voter", Value: voter}}
		for _, at := range ev.Attributes {
			if at.Key == "option" {
				at.Value = options
			}
			attrs = append(attrs, at)
		}
		ev.Attributes = attrs
		return nil
	}); err != nil {
		return err
	}

	depositors := pick(append(slices.Clone(depositTypes), submitTypes...)...)
	if err := pair("proposal_deposit", deposits, depositors, func(ev *node.Event, a actor) error {
		who, _ := a.body["depositor"].(string)
		if slices.Contains(submitTypes, a.typeURL) {
			who, _ = a.body["proposer"].(string)
		}
		ev.Attributes = append([]node.Attribute{{Key: "depositor", Value: who}}, ev.Attributes...)
		return nil
	}); err != nil {
		return err
	}

	if err := pair("submit_proposal", submits, pick(submitTypes...), func(ev *node.Event, a actor) error {
		proposer, _ := a.body["proposer"].(string)
		ev.Attributes = append(ev.Attributes, node.Attribute{Key: "proposer", Value: proposer})
		return nil
	}); err != nil {
		return err
	}

	return pair("ibc_transfer", transfers, pick(transferType), func(ev *node.Event, a actor) error {
		token, _ := a.body["token"].(map[string]any)
		amount, _ := token["amount"].(string)
		denom, _ := token["denom"].(string)
		memo, _ := a.body["memo"].(string)
		ev.Attributes = append(ev.Attributes,
			node.Attribute{Key: "amount", Value: amount},
			node.Attribute{Key: "denom", Value: denom},
			node.Attribute{Key: "memo", Value: memo})
		return nil
	})
}

// pair applies fill to the k-th event and the k-th actor; the counts must
// match, as each of those messages emits exactly one such event.
func pair(kind string, events []*node.Event, acts []actor, fill func(*node.Event, actor) error) error {
	if len(events) != len(acts) {
		return fmt.Errorf("%w: %d %s events for %d messages", ErrUnknownGeneration, len(events), kind, len(acts))
	}
	for k, ev := range events {
		if acts[k].body == nil {
			return fmt.Errorf("%w: %s from an undecodable message", ErrUnknownGeneration, kind)
		}
		if err := fill(ev, acts[k]); err != nil {
			return err
		}
	}
	return nil
}

// voteOptions turns the legacy option value (one JSON object per line) into
// the JSON array SDK 0.50 emits.
func voteOptions(v string) (string, error) {
	var parts []string
	for line := range strings.SplitSeq(strings.TrimSpace(v), "\n") {
		line = strings.TrimSpace(line)
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			return "", fmt.Errorf("%w: vote option %q", ErrUnknownGeneration, v)
		}
		var buf bytes.Buffer
		if err := json.Compact(&buf, []byte(line)); err != nil {
			return "", err
		}
		parts = append(parts, buf.String())
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}

func has(ev *node.Event, key string) bool {
	_, ok := ev.Get(key)
	return ok
}

func hasAttribute(events []node.Event, key string) bool {
	for k := range events {
		if has(&events[k], key) {
			return true
		}
	}
	return false
}
