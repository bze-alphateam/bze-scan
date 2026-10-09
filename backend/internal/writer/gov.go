package writer

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// The governance writes. The proposals row is shared with the state sync
// (which writes what the node knows), so these statements only fill what
// events know and move the status forward, in both modes.

// proposalRow is the JSON shape of the proposals insert.
type proposalRow struct {
	ID           uint64          `json:"id"`
	Title        string          `json:"title"`
	Summary      *string         `json:"summary"`
	Metadata     *string         `json:"metadata"`
	Proposer     *string         `json:"proposer"`
	Kind         string          `json:"kind"`
	MessageTypes []string        `json:"message_types"`
	Messages     json.RawMessage `json:"messages"`
	Status       string          `json:"status"`
	Expedited    bool            `json:"expedited"`
	Time         time.Time       `json:"time"`
	Height       int64           `json:"height"`
	TxHash       string          `json:"tx_hash"`
}

// proposalsSQL inserts the submitted proposals; a row the state sync wrote
// first gets its submit height, transaction and the event fields it lacks.
const proposalsSQL = `INSERT INTO explorer.proposals AS p (
		id, title, summary, metadata, proposer, kind, message_types, messages, status, expedited,
		submit_time, submit_height, submit_tx_hash, updated_at)
	SELECT r.id, r.title, r.summary, r.metadata, r.proposer, r.kind, r.message_types,
		NULLIF(r.messages, 'null'::jsonb), r.status, r.expedited, r.time, r.height, r.tx_hash, now()
	  FROM jsonb_to_recordset($1::jsonb) AS r(id bigint, title text, summary text, metadata text, proposer text,
		kind text, message_types text[], messages jsonb, status text, expedited boolean, time timestamptz,
		height bigint, tx_hash text)
	ON CONFLICT (id) DO UPDATE SET
		submit_height = COALESCE(p.submit_height, EXCLUDED.submit_height),
		submit_tx_hash = COALESCE(p.submit_tx_hash, EXCLUDED.submit_tx_hash),
		proposer = COALESCE(p.proposer, EXCLUDED.proposer),
		updated_at = now()
	WHERE p.submit_height IS NULL OR p.submit_tx_hash IS NULL OR p.proposer IS NULL`

// proposalVoteRow is the JSON shape of the proposal_votes upsert.
type proposalVoteRow struct {
	ProposalID uint64          `json:"proposal_id"`
	Voter      string          `json:"voter"`
	Options    json.RawMessage `json:"options"`
	Option     *string         `json:"option"`
	Height     int64           `json:"height"`
	TxIndex    int             `json:"tx_index"`
	Time       time.Time       `json:"time"`
}

// proposalVotesSQL keeps each voter's latest vote: an earlier one (the
// backfill walks down) never replaces it, the same one written again (a
// reindex) only when it differs.
const proposalVotesSQL = `INSERT INTO explorer.proposal_votes AS v (proposal_id, voter, options, option, height, tx_index, time)
	SELECT r.proposal_id, r.voter, r.options, r.option, r.height, r.tx_index, r.time
	  FROM jsonb_to_recordset($1::jsonb) AS r(proposal_id bigint, voter text, options jsonb, option text,
		height bigint, tx_index integer, time timestamptz)
	ON CONFLICT (proposal_id, voter) DO UPDATE SET
		options = EXCLUDED.options, option = EXCLUDED.option, height = EXCLUDED.height,
		tx_index = EXCLUDED.tx_index, time = EXCLUDED.time
	WHERE (EXCLUDED.height, EXCLUDED.tx_index) >= (v.height, v.tx_index)
	  AND (v.options, v.option, v.height, v.tx_index, v.time) IS DISTINCT FROM
	      (EXCLUDED.options, EXCLUDED.option, EXCLUDED.height, EXCLUDED.tx_index, EXCLUDED.time)`

// proposalStatusRow is the JSON shape of the status update.
type proposalStatusRow struct {
	ID           uint64 `json:"id"`
	Status       string `json:"status"`
	Height       int64  `json:"height"`
	Resolved     bool   `json:"resolved"`
	ExpeditedOff bool   `json:"expedited_off"`
}

// proposalStatusSQL applies status changes to the stored proposals (one
// submitted before the live floor has no row yet: the state sync writes it,
// with its resolution height from block_events). A resolution never goes
// back to an earlier one; an open status never undoes a resolution; the
// voting period opening only moves a proposal out of its deposit period.
const proposalStatusSQL = `UPDATE explorer.proposals AS p SET
		status = r.status,
		resolved_height = CASE WHEN r.resolved THEN r.height ELSE p.resolved_height END,
		expedited = CASE WHEN r.expedited_off THEN false ELSE p.expedited END,
		updated_at = now()
	  FROM jsonb_to_recordset($1::jsonb) AS r(id bigint, status text, height bigint, resolved boolean, expedited_off boolean)
	 WHERE p.id = r.id
	   AND CASE WHEN r.resolved THEN p.resolved_height IS NULL OR p.resolved_height <= r.height
	            WHEN r.expedited_off THEN p.resolved_height IS NULL
	            ELSE p.resolved_height IS NULL AND p.status = 'deposit_period' END
	   AND (p.status, p.resolved_height, p.expedited) IS DISTINCT FROM
	       (r.status, CASE WHEN r.resolved THEN r.height ELSE p.resolved_height END,
	        CASE WHEN r.expedited_off THEN false ELSE p.expedited END)`

var proposalDepositsTable = table{
	name: "explorer.proposal_deposits",
	keys: []string{"proposal_id", "depositor", "height", "tx_index"},
	cols: []column{
		{name: "proposal_id", typ: "bigint"},
		{name: "depositor", typ: "text"},
		{name: "height", typ: "bigint"},
		{name: "tx_index", typ: "integer"},
		{name: "amount", typ: "jsonb"},
		{name: "time", typ: "timestamptz"},
	},
}

// proposalDepositRow is the JSON shape of the proposal_deposits bulk write.
type proposalDepositRow struct {
	ProposalID uint64          `json:"proposal_id"`
	Depositor  string          `json:"depositor"`
	Height     int64           `json:"height"`
	TxIndex    int             `json:"tx_index"`
	Amount     json.RawMessage `json:"amount"`
	Time       time.Time       `json:"time"`
}

// govStatements returns the governance writes of ents: the proposals, then
// the deposits and votes, then the status changes (after the inserts, so a
// proposal submitted and resolved in one flush ends resolved).
func govStatements(top int64, ents *transform.Entities, mode Mode) ([]statement, error) {
	proposals := make([]proposalRow, 0, len(ents.Proposals))
	for _, p := range ents.Proposals {
		proposals = append(proposals, proposalRow{
			ID: p.ID, Title: p.Title, Summary: nullIfEmpty(p.Summary), Metadata: nullIfEmpty(p.Metadata),
			Proposer: nullIfEmpty(p.Proposer), Kind: p.Kind, MessageTypes: nonNil(p.MessageTypes),
			Messages: p.Messages, Status: p.Status, Expedited: p.Expedited, Time: p.Time, Height: p.Height, TxHash: p.TxHash,
		})
	}
	deposits := make([]proposalDepositRow, 0, len(ents.ProposalDeposits))
	for _, d := range ents.ProposalDeposits {
		amount, err := json.Marshal(nonNil(d.Amount))
		if err != nil {
			return nil, fmt.Errorf("write %d: proposal_deposits: amount: %w", top, err)
		}
		deposits = append(deposits, proposalDepositRow{
			ProposalID: d.ProposalID, Depositor: d.Depositor, Height: d.Height, TxIndex: d.TxIndex, Amount: amount, Time: d.Time,
		})
	}
	votes := make([]proposalVoteRow, 0, len(ents.ProposalVotes))
	for _, v := range latestVotes(ents.ProposalVotes) {
		votes = append(votes, proposalVoteRow{
			ProposalID: v.ProposalID, Voter: v.Voter, Options: v.Options, Option: nullIfEmpty(v.Option),
			Height: v.Height, TxIndex: v.TxIndex, Time: v.Time,
		})
	}
	statuses := make([]proposalStatusRow, 0, len(ents.ProposalStatuses))
	for _, s := range latestStatuses(ents.ProposalStatuses) {
		statuses = append(statuses, proposalStatusRow{
			ID: s.ProposalID, Status: s.Status, Height: s.Height, Resolved: s.Resolved, ExpeditedOff: s.ExpeditedOff,
		})
	}

	props, err := plainStatements(top, "proposals", proposalsSQL, proposals)
	if err != nil {
		return nil, err
	}
	deps, err := chunked(top, "proposal_deposits", proposalDepositsTable, mode, deposits, nil)
	if err != nil {
		return nil, err
	}
	vs, err := plainStatements(top, "proposal_votes", proposalVotesSQL, votes)
	if err != nil {
		return nil, err
	}
	sts, err := plainStatements(top, "proposal statuses", proposalStatusSQL, statuses)
	if err != nil {
		return nil, err
	}
	return slices.Concat(props, deps, vs, sts), nil
}

// plainStatements is one statement of sql per chunk of rows, the same in
// both modes.
func plainStatements[T any](top int64, label, sql string, rows []T) ([]statement, error) {
	payloads, err := jsonChunks(top, label, rows)
	if err != nil {
		return nil, err
	}
	out := make([]statement, 0, len(payloads))
	for _, p := range payloads {
		out = append(out, statement{table: label, sql: sql, args: []any{p}})
	}
	return out, nil
}

// latestVotes keeps one vote per proposal and voter: the latest by height,
// transaction and message. One statement cannot upsert a key twice, and a
// flush may hold several heights.
func latestVotes(votes []transform.ProposalVote) []transform.ProposalVote {
	type key struct {
		id    uint64
		voter string
	}
	latest := map[key]transform.ProposalVote{}
	order := []key{}
	for _, v := range votes {
		k := key{v.ProposalID, v.Voter}
		cur, ok := latest[k]
		if !ok {
			order = append(order, k)
		}
		if !ok || cmp.Or(cmp.Compare(v.Height, cur.Height), cmp.Compare(v.TxIndex, cur.TxIndex), cmp.Compare(v.MsgIndex, cur.MsgIndex)) > 0 {
			latest[k] = v
		}
	}
	out := make([]transform.ProposalVote, 0, len(order))
	for _, k := range order {
		out = append(out, latest[k])
	}
	return out
}

// latestStatuses keeps one status change per proposal: the highest height,
// a resolution over an open status at the same height (EndBlock runs after
// the transactions).
func latestStatuses(statuses []transform.ProposalStatus) []transform.ProposalStatus {
	latest := map[uint64]transform.ProposalStatus{}
	for _, s := range statuses {
		cur, ok := latest[s.ProposalID]
		if !ok || s.Height > cur.Height || (s.Height == cur.Height && s.Resolved && !cur.Resolved) {
			latest[s.ProposalID] = s
		}
	}
	out := make([]transform.ProposalStatus, 0, len(latest))
	for _, s := range latest {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b transform.ProposalStatus) int { return cmp.Compare(a.ProposalID, b.ProposalID) })
	return out
}
