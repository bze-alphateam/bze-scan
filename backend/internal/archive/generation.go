// Package archive normalises what an archive node answers for an old height
// into the shape the transformer expects: the event format of the current
// chain generation (Cosmos SDK 0.50, CometBFT 0.38). It runs on the archive
// path only, before the transformer: the backfill and the reindex of old
// heights. The live path never calls it.
//
// # Generations of mainnet beezee-1
//
// Every software upgrade of the chain, with the height it was applied at and
// the library lines of its release tag:
//
//	release  from height  Cosmos SDK  consensus                 ibc-go  event format
//	v5.0     1            0.44.3      Tendermint 0.34.14        1.2.2   legacy
//	v5.1.2   3,646,700    0.45.9      Tendermint 0.34.21        1.2.2   legacy
//	v6.0.0   4,875,460    0.45.10     Tendermint 0.34.22        2.4.2   legacy
//	v6.1.0   9,079,079    0.45.16     CometBFT 0.34.27          4.5.1   legacy
//	v7.0.0   12,723,000   0.45.16     CometBFT 0.34.27          4.6.0   legacy
//	v7.1.0   13,710,000   0.45.16     CometBFT 0.34.27          4.6.0   legacy
//	v7.1.1   14,182,500   0.45.16     CometBFT 0.34.27          4.6.0   legacy
//	v7.2.0   15,951,000   0.45.16     CometBFT 0.34.27          4.6.0   legacy
//	v8.0.0   20,237,800   0.50.14     CometBFT 0.38.17          8.7.0   current
//	v8.1.0   22,551,810   0.50.15     CometBFT 0.38.21          8.7.0   current
//	v8.1.1   23,855,000   0.50.15     CometBFT 0.38.23          8.8.0   current
//
// How it was verified (2026-10-08): the upgrade names are the handlers in
// the chain's app/upgrades; each height is the chain's own record of the
// applied plan (/cosmos/upgrade/v1beta1/applied_plan/<name> on mainnet REST);
// the library versions are the go.mod of each release tag. v8.2.0 had not
// been applied yet: when it is, add its row (same event format as v8.0.0).
// Note that v7 stayed on SDK 0.45 and CometBFT 0.34: there is no 0.47
// generation on mainnet, and the only format break is v8.0.0.
//
// # What the legacy format differs in, and what Normalise does
//
// The archive node runs CometBFT 0.38 and serves old heights through its
// conversion of the legacy ABCI responses: begin- and end-block events
// arrive merged into finalize_block_events (each with its mode attribute,
// as today), deliver_tx results arrive as txs_results, and attribute keys
// and values arrive as plain strings. An archive node of an older line
// would answer begin_block_events and end_block_events instead, with base64
// attributes on 0.34 and plain ones on 0.37, which the response does not
// tell apart: such an answer fails with ErrUnknownGeneration rather than
// being guessed at. Point ARCHIVE_RPC_URL at a CometBFT 0.38 node. Within
// that layout a legacy height differs as follows:
//
//   - msg_index: absent. Reconstructed from message order: each message's
//     events start with a message event carrying the action, and the
//     decoded transaction gives the message count, which must match.
//   - The leading message event carries only the action. Rebuilt as SDK 0.50
//     builds it: action, then sender (the message's first signer), then
//     module (from the type URL, only when none of the message's events
//     carries a module attribute already), then msg_index on every event.
//   - message.action: SDK messages carry their type URL; BZE messages of the
//     legacy router carry a short name (create_order, add_article…), and
//     BZE type URLs carry the old proto package (/bze.tradebin.v1.…). Both
//     are mapped to the current type URL and checked against the decoded
//     message.
//   - Typed events: BZE event types carry the old package
//     (bze.tradebin.v1.OrderCreateMessageEvent, bze.rewards.v1.…) and are
//     renamed to the current one; their attribute values were already JSON
//     on every generation (EmitTypedEvent), and Normalise checks they are.
//     Their field names did not change.
//   - proposal_vote: no voter attribute, and the option is one JSON object
//     per line instead of a JSON array. voter is added from the decoded
//     MsgVote or MsgVoteWeighted, the option rewritten as the array.
//   - proposal_deposit (the one carrying the amount): no depositor, added
//     from MsgDeposit or MsgSubmitProposal. submit_proposal (the one
//     carrying proposal_id): no proposer, added from MsgSubmitProposal.
//     SDK 0.50's proposal_messages attribute stays absent: legacy proposals
//     carried a content, not messages.
//   - ibc_transfer: only sender and receiver. amount, denom and memo are
//     added from the decoded MsgTransfer (memo empty before ibc-go 4).
//   - IBC packet events carry packet_data and packet_data_hex on every
//     generation (ibc-go 1.2.2 already emitted both): nothing to do. The
//     connection_id attribute that ibc-go 8 adds stays absent.
//   - Events the legacy modules emitted and 0.50 no longer does (the
//     message{module, sender} event of the distribution, staking and gov
//     handlers, submit_proposal{proposal_type}, proposer_reward) are kept:
//     they are true and the transformer does not depend on their absence.
//   - Failed transactions carry their ante-handler events only, as today.
//
// Facts a legacy height never recorded stay absent, never guessed. A height
// whose shape does not match its generation fails with ErrUnknownGeneration
// instead of being misparsed.
package archive

import "sort"

// Format is the event format of a generation.
type Format int

// Event formats.
const (
	// FormatLegacy is Cosmos SDK 0.44/0.45 on Tendermint/CometBFT 0.34.
	FormatLegacy Format = iota + 1
	// FormatCurrent is Cosmos SDK 0.50 on CometBFT 0.38, the shape the
	// transformer reads.
	FormatCurrent
)

func (f Format) String() string {
	switch f {
	case FormatLegacy:
		return "legacy"
	case FormatCurrent:
		return "current"
	}
	return "unknown"
}

// Generation is one release line of mainnet beezee-1.
type Generation struct {
	// Release is the chain release, e.g. "v7.2.0".
	Release string
	// From is the first height the release produced.
	From      int64
	SDK       string
	Consensus string
	IBC       string
	Format    Format
}

// Generations lists the releases of mainnet beezee-1 by height, oldest
// first (see the package documentation for how it was verified).
var Generations = []Generation{
	{Release: "v5.0", From: 1, SDK: "0.44.3", Consensus: "Tendermint 0.34.14", IBC: "1.2.2", Format: FormatLegacy},
	{Release: "v5.1.2", From: 3_646_700, SDK: "0.45.9", Consensus: "Tendermint 0.34.21", IBC: "1.2.2", Format: FormatLegacy},
	{Release: "v6.0.0", From: 4_875_460, SDK: "0.45.10", Consensus: "Tendermint 0.34.22", IBC: "2.4.2", Format: FormatLegacy},
	{Release: "v6.1.0", From: 9_079_079, SDK: "0.45.16", Consensus: "CometBFT 0.34.27", IBC: "4.5.1", Format: FormatLegacy},
	{Release: "v7.0.0", From: 12_723_000, SDK: "0.45.16", Consensus: "CometBFT 0.34.27", IBC: "4.6.0", Format: FormatLegacy},
	{Release: "v7.1.0", From: 13_710_000, SDK: "0.45.16", Consensus: "CometBFT 0.34.27", IBC: "4.6.0", Format: FormatLegacy},
	{Release: "v7.1.1", From: 14_182_500, SDK: "0.45.16", Consensus: "CometBFT 0.34.27", IBC: "4.6.0", Format: FormatLegacy},
	{Release: "v7.2.0", From: 15_951_000, SDK: "0.45.16", Consensus: "CometBFT 0.34.27", IBC: "4.6.0", Format: FormatLegacy},
	{Release: "v8.0.0", From: 20_237_800, SDK: "0.50.14", Consensus: "CometBFT 0.38.17", IBC: "8.7.0", Format: FormatCurrent},
	{Release: "v8.1.0", From: 22_551_810, SDK: "0.50.15", Consensus: "CometBFT 0.38.21", IBC: "8.7.0", Format: FormatCurrent},
	{Release: "v8.1.1", From: 23_855_000, SDK: "0.50.15", Consensus: "CometBFT 0.38.23", IBC: "8.8.0", Format: FormatCurrent},
}

// GenerationAt returns the generation that produced height (≥ 1).
func GenerationAt(height int64) Generation {
	i := sort.Search(len(Generations), func(i int) bool { return Generations[i].From > height })
	if i == 0 {
		return Generations[0]
	}
	return Generations[i-1]
}
