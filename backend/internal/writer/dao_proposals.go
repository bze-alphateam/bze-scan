package writer

import (
	"cmp"
	"encoding/json"
	"slices"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// The DAO proposal writes. The dao_proposals row is shared with the
// dao_proposals state-sync set, and its events can be indexed in any order
// (the backfill walks down; a submission may be below the live floor), so:
//
//   - every write is an upsert: a settle indexed before its submission
//     writes a row the submission fills later;
//   - the submission only fills what is missing, never the status;
//   - a status change applies only over an older one (status_height), or,
//     over a status the state sync set (no height), only forward.

// daoProposalRow is the JSON shape of the submissions upsert.
type daoProposalRow struct {
	ID          int64           `json:"id"`
	DaoID       int64           `json:"dao_id"`
	Proposer    *string         `json:"proposer"`
	Title       *string         `json:"title"`
	Summary     *string         `json:"summary"`
	Msgs        json.RawMessage `json:"msgs"`
	Expedited   bool            `json:"expedited"`
	Height      int64           `json:"height"`
	Time        time.Time       `json:"time"`
	TxHash      *string         `json:"tx_hash"`
	VotingEnd   *time.Time      `json:"voting_end"`
	Denominator string          `json:"denominator"`
}

// daoProposalsSQL fills the submission facts of the proposals; a row that
// exists (written by a later event or the state sync) keeps what it has.
const daoProposalsSQL = `INSERT INTO explorer.dao_proposals AS p (id, dao_id, proposer, title, summary, msgs, expedited,
		submit_height, submit_time, submit_tx_hash, voting_end, denominator, updated_at)
	SELECT r.id, r.dao_id, r.proposer, r.title, r.summary, NULLIF(r.msgs, 'null'::jsonb), r.expedited,
	       r.height, r.time, r.tx_hash, r.voting_end, r.denominator, now()
	  FROM jsonb_to_recordset($1::jsonb) AS r(id bigint, dao_id bigint, proposer text, title text, summary text,
	       msgs jsonb, expedited boolean, height bigint, time timestamptz, tx_hash text, voting_end timestamptz,
	       denominator numeric)
	 ORDER BY r.id
	ON CONFLICT (id) DO UPDATE SET
		dao_id = COALESCE(p.dao_id, EXCLUDED.dao_id), proposer = COALESCE(p.proposer, EXCLUDED.proposer),
		title = COALESCE(p.title, EXCLUDED.title), summary = COALESCE(p.summary, EXCLUDED.summary),
		msgs = COALESCE(p.msgs, EXCLUDED.msgs), expedited = COALESCE(p.expedited, EXCLUDED.expedited),
		submit_height = COALESCE(p.submit_height, EXCLUDED.submit_height),
		submit_time = COALESCE(p.submit_time, EXCLUDED.submit_time),
		submit_tx_hash = COALESCE(p.submit_tx_hash, EXCLUDED.submit_tx_hash),
		voting_end = COALESCE(p.voting_end, EXCLUDED.voting_end),
		denominator = COALESCE(p.denominator, EXCLUDED.denominator), updated_at = now()
	WHERE (p.dao_id, p.proposer, p.title, p.summary, p.msgs, p.expedited, p.submit_height, p.submit_time,
	       p.submit_tx_hash, p.voting_end, p.denominator) IS DISTINCT FROM
	      (COALESCE(p.dao_id, EXCLUDED.dao_id), COALESCE(p.proposer, EXCLUDED.proposer),
	       COALESCE(p.title, EXCLUDED.title), COALESCE(p.summary, EXCLUDED.summary), COALESCE(p.msgs, EXCLUDED.msgs),
	       COALESCE(p.expedited, EXCLUDED.expedited), COALESCE(p.submit_height, EXCLUDED.submit_height),
	       COALESCE(p.submit_time, EXCLUDED.submit_time), COALESCE(p.submit_tx_hash, EXCLUDED.submit_tx_hash),
	       COALESCE(p.voting_end, EXCLUDED.voting_end), COALESCE(p.denominator, EXCLUDED.denominator))`

// daoOutcomeRow is the JSON shape of the status changes upsert.
type daoOutcomeRow struct {
	ID             int64     `json:"id"`
	Status         string    `json:"status"`
	Height         int64     `json:"height"`
	TxHash         *string   `json:"tx_hash"`
	Cancelled      bool      `json:"cancelled"`
	Yes            *string   `json:"yes"`
	No             *string   `json:"no"`
	Abstain        *string   `json:"abstain"`
	Denominator    *string   `json:"denominator"`
	Time           time.Time `json:"time"`
	ExpeditedEarly *bool     `json:"expedited_early"`
	Executed       *bool     `json:"executed"`
	ExecutionError *string   `json:"execution_error"`
}

// daoOutcomesSQL applies the settles and cancels. settled_height and
// settled_tx_hash are those of the latest settle (the one that set the
// status: a retry's, an expiry's); a status change never goes back to an
// older one, and one the state sync set without a height only moves
// forward (voting → any, execution_failed → executed | expired).
const daoOutcomesSQL = `INSERT INTO explorer.dao_proposals AS p (id, status, status_height, settled_height,
		settled_tx_hash, yes, no, abstain, denominator, tally_updated_at, expedited_early, executed, execution_error,
		cancelled_height, updated_at)
	SELECT r.id, r.status, r.height,
	       CASE WHEN r.cancelled THEN NULL ELSE r.height END, CASE WHEN r.cancelled THEN NULL ELSE r.tx_hash END,
	       r.yes, r.no, r.abstain, r.denominator, CASE WHEN r.cancelled THEN NULL ELSE r.time END,
	       r.expedited_early, r.executed, r.execution_error, CASE WHEN r.cancelled THEN r.height END, now()
	  FROM jsonb_to_recordset($1::jsonb) AS r(id bigint, status text, height bigint, tx_hash text, cancelled boolean,
	       yes numeric, no numeric, abstain numeric, denominator numeric, time timestamptz, expedited_early boolean,
	       executed boolean, execution_error text)
	 ORDER BY r.id
	ON CONFLICT (id) DO UPDATE SET
		status = EXCLUDED.status, status_height = EXCLUDED.status_height,
		settled_height = COALESCE(EXCLUDED.settled_height, p.settled_height),
		settled_tx_hash = CASE WHEN EXCLUDED.settled_height IS NULL THEN p.settled_tx_hash ELSE EXCLUDED.settled_tx_hash END,
		yes = COALESCE(EXCLUDED.yes, p.yes), no = COALESCE(EXCLUDED.no, p.no),
		abstain = COALESCE(EXCLUDED.abstain, p.abstain), denominator = COALESCE(EXCLUDED.denominator, p.denominator),
		tally_updated_at = COALESCE(EXCLUDED.tally_updated_at, p.tally_updated_at),
		expedited_early = COALESCE(EXCLUDED.expedited_early, p.expedited_early),
		executed = COALESCE(EXCLUDED.executed, p.executed),
		execution_error = CASE WHEN EXCLUDED.executed IS NULL THEN p.execution_error ELSE EXCLUDED.execution_error END,
		cancelled_height = COALESCE(EXCLUDED.cancelled_height, p.cancelled_height),
		expedite_settleable = false,
		retry_deadline = CASE WHEN EXCLUDED.status = 'execution_failed' THEN p.retry_deadline END,
		updated_at = now()
	WHERE CASE WHEN p.status_height IS NOT NULL THEN p.status_height <= EXCLUDED.status_height
	           ELSE p.status IN ('voting', EXCLUDED.status)
	                OR (p.status = 'execution_failed' AND EXCLUDED.status IN ('executed', 'expired')) END
	  AND (p.status, p.status_height, p.settled_height, p.settled_tx_hash, p.yes, p.no, p.abstain, p.expedited_early,
	       p.executed, p.execution_error, p.cancelled_height) IS DISTINCT FROM
	      (EXCLUDED.status, EXCLUDED.status_height, COALESCE(EXCLUDED.settled_height, p.settled_height),
	       CASE WHEN EXCLUDED.settled_height IS NULL THEN p.settled_tx_hash ELSE EXCLUDED.settled_tx_hash END,
	       COALESCE(EXCLUDED.yes, p.yes), COALESCE(EXCLUDED.no, p.no), COALESCE(EXCLUDED.abstain, p.abstain),
	       COALESCE(EXCLUDED.expedited_early, p.expedited_early), COALESCE(EXCLUDED.executed, p.executed),
	       CASE WHEN EXCLUDED.executed IS NULL THEN p.execution_error ELSE EXCLUDED.execution_error END,
	       COALESCE(EXCLUDED.cancelled_height, p.cancelled_height))`

// daoDepositRow is the JSON shape of the deposit releases upsert.
type daoDepositRow struct {
	ID          int64  `json:"id"`
	Refunded    bool   `json:"refunded"`
	Destination string `json:"destination"`
	Amount      string `json:"amount"`
}

// daoDepositsSQL records the release of the proposals' deposits (once per
// proposal: at its first settle or its cancel).
const daoDepositsSQL = `INSERT INTO explorer.dao_proposals AS p (id, deposit_refunded, deposit_destination, deposit_amount, updated_at)
	SELECT r.id, r.refunded, r.destination, r.amount, now()
	  FROM jsonb_to_recordset($1::jsonb) AS r(id bigint, refunded boolean, destination text, amount text)
	 ORDER BY r.id
	ON CONFLICT (id) DO UPDATE SET deposit_refunded = EXCLUDED.deposit_refunded,
		deposit_destination = EXCLUDED.deposit_destination, deposit_amount = EXCLUDED.deposit_amount, updated_at = now()
	WHERE (p.deposit_refunded, p.deposit_destination, p.deposit_amount) IS DISTINCT FROM
	      (EXCLUDED.deposit_refunded, EXCLUDED.deposit_destination, EXCLUDED.deposit_amount)`

// daoCleanedRow is the JSON shape of the cleanups upsert.
type daoCleanedRow struct {
	ID     int64 `json:"id"`
	Height int64 `json:"height"`
}

// daoCleanedSQL records the cleanup that removed the proposals from the
// chain.
const daoCleanedSQL = `INSERT INTO explorer.dao_proposals AS p (id, cleaned_height, updated_at)
	SELECT r.id, r.height, now() FROM jsonb_to_recordset($1::jsonb) AS r(id bigint, height bigint) ORDER BY r.id
	ON CONFLICT (id) DO UPDATE SET cleaned_height = EXCLUDED.cleaned_height, updated_at = now()
	WHERE p.cleaned_height IS DISTINCT FROM EXCLUDED.cleaned_height`

// daoVoteRow is the JSON shape of the dao_votes upsert.
type daoVoteRow struct {
	ProposalID int64     `json:"proposal_id"`
	Voter      string    `json:"voter"`
	Option     string    `json:"option"`
	Weight     string    `json:"weight"`
	Height     int64     `json:"height"`
	TxIndex    int       `json:"tx_index"`
	MsgIndex   int       `json:"msg_index"`
	TxHash     *string   `json:"tx_hash"`
	Time       time.Time `json:"time"`
}

// daoVotesSQL keeps each voter's latest ballot: an earlier one (the backfill
// walks down) never replaces it, the same one written again (a reindex) only
// when it differs.
const daoVotesSQL = `INSERT INTO explorer.dao_votes AS v (proposal_id, voter, option, weight, height, tx_index, msg_index, tx_hash, time)
	SELECT r.proposal_id, r.voter, r.option, r.weight, r.height, r.tx_index, r.msg_index, r.tx_hash, r.time
	  FROM jsonb_to_recordset($1::jsonb) AS r(proposal_id bigint, voter text, option text, weight numeric,
	       height bigint, tx_index integer, msg_index integer, tx_hash text, time timestamptz)
	 ORDER BY r.proposal_id, r.voter
	ON CONFLICT (proposal_id, voter) DO UPDATE SET
		option = EXCLUDED.option, weight = EXCLUDED.weight, height = EXCLUDED.height, tx_index = EXCLUDED.tx_index,
		msg_index = EXCLUDED.msg_index, tx_hash = EXCLUDED.tx_hash, time = EXCLUDED.time
	WHERE (EXCLUDED.height, EXCLUDED.tx_index, EXCLUDED.msg_index) >= (v.height, v.tx_index, v.msg_index)
	  AND (v.option, v.weight, v.height, v.tx_index, v.msg_index, v.tx_hash, v.time) IS DISTINCT FROM
	      (EXCLUDED.option, EXCLUDED.weight, EXCLUDED.height, EXCLUDED.tx_index, EXCLUDED.msg_index,
	       EXCLUDED.tx_hash, EXCLUDED.time)`

// daoBallotReductionsTable is explorer.dao_ballot_reductions: insert-only
// history, so the reindex and the downward backfill stay idempotent.
var daoBallotReductionsTable = table{
	name: "explorer.dao_ballot_reductions",
	keys: []string{"height", "tx_index", "msg_index", "seq"},
	cols: []column{
		{name: "height", typ: "bigint"},
		{name: "tx_index", typ: "integer"},
		{name: "msg_index", typ: "integer"},
		{name: "seq", typ: "integer"},
		{name: "proposal_id", typ: "bigint"},
		{name: "voter", typ: "text"},
		{name: "source", typ: "text"},
		{name: "amount", typ: "numeric"},
		{name: "tx_hash", typ: "text"},
	},
}

type daoBallotReductionRow struct {
	Height     int64   `json:"height"`
	TxIndex    int     `json:"tx_index"`
	MsgIndex   int     `json:"msg_index"`
	Seq        int     `json:"seq"`
	ProposalID int64   `json:"proposal_id"`
	Voter      string  `json:"voter"`
	Source     string  `json:"source"`
	Amount     string  `json:"amount"`
	TxHash     *string `json:"tx_hash"`
}

// daoProposalStatements returns the DAO proposal writes of ents: the
// submissions, the status changes (after them, so a proposal submitted and
// settled in one flush ends settled), the deposits and cleanups, then the
// ballots.
func daoProposalStatements(top int64, ents *transform.Entities, mode Mode) ([]statement, error) {
	if len(ents.DaoProposals) == 0 && len(ents.DaoProposalOutcomes) == 0 && len(ents.DaoProposalDeposits) == 0 &&
		len(ents.DaoProposalsCleaned) == 0 && len(ents.DaoVotes) == 0 && len(ents.DaoBallotReductions) == 0 {
		return nil, nil
	}
	proposals := make([]daoProposalRow, 0, len(ents.DaoProposals))
	for _, p := range firstPerID(ents.DaoProposals, func(p transform.DaoProposal) int64 { return p.ID }) {
		row := daoProposalRow{
			ID: p.ID, DaoID: p.DaoID, Proposer: nullIfEmpty(p.Proposer), Expedited: p.Expedited, Height: p.Height,
			Time: p.Time, TxHash: nullIfEmpty(p.TxHash), Denominator: p.Denominator,
		}
		if p.HasBody {
			title, summary := p.Title, p.Summary
			row.Title, row.Summary, row.Msgs = &title, &summary, p.Msgs
		}
		if !p.VotingEnd.IsZero() {
			end := p.VotingEnd
			row.VotingEnd = &end
		}
		proposals = append(proposals, row)
	}
	outcomes := make([]daoOutcomeRow, 0, len(ents.DaoProposalOutcomes))
	for _, o := range latestOutcomes(ents.DaoProposalOutcomes) {
		row := daoOutcomeRow{ID: o.ID, Status: o.Status, Height: o.Height, TxHash: nullIfEmpty(o.TxHash),
			Cancelled: o.Cancelled, Time: o.Time, Executed: o.Executed}
		if !o.Cancelled {
			yes, no, abstain, den, early := o.Yes, o.No, o.Abstain, o.Denominator, o.ExpeditedEarly
			row.Yes, row.No, row.Abstain, row.Denominator, row.ExpeditedEarly = &yes, &no, &abstain, &den, &early
		}
		if o.Executed != nil {
			row.ExecutionError = nullIfEmpty(o.ExecutionError)
		}
		outcomes = append(outcomes, row)
	}
	deposits := make([]daoDepositRow, 0, len(ents.DaoProposalDeposits))
	for _, d := range lastPerID(ents.DaoProposalDeposits, func(d transform.DaoProposalDeposit) int64 { return d.ID }) {
		deposits = append(deposits, daoDepositRow{ID: d.ID, Refunded: d.Refunded, Destination: d.Destination, Amount: d.Amount})
	}
	cleaned := make([]daoCleanedRow, 0, len(ents.DaoProposalsCleaned))
	for _, c := range lastPerID(ents.DaoProposalsCleaned, func(c transform.DaoProposalCleaned) int64 { return c.ID }) {
		cleaned = append(cleaned, daoCleanedRow{ID: c.ID, Height: c.Height})
	}
	votes := make([]daoVoteRow, 0, len(ents.DaoVotes))
	for _, v := range latestDaoVotes(ents.DaoVotes) {
		votes = append(votes, daoVoteRow{
			ProposalID: v.ProposalID, Voter: v.Voter, Option: v.Option, Weight: v.Weight, Height: v.Height,
			TxIndex: v.TxIndex, MsgIndex: v.MsgIndex, TxHash: nullIfEmpty(v.TxHash), Time: v.Time,
		})
	}
	reductions := make([]daoBallotReductionRow, 0, len(ents.DaoBallotReductions))
	for _, r := range ents.DaoBallotReductions {
		reductions = append(reductions, daoBallotReductionRow{
			Height: r.Height, TxIndex: r.TxIndex, MsgIndex: r.MsgIndex, Seq: r.Seq, ProposalID: r.ProposalID,
			Voter: r.Voter, Source: r.Source, Amount: r.Amount, TxHash: nullIfEmpty(r.TxHash),
		})
	}

	var out []statement
	for _, build := range []func() ([]statement, error){
		func() ([]statement, error) { return plainStatements(top, "dao_proposals", daoProposalsSQL, proposals) },
		func() ([]statement, error) {
			return plainStatements(top, "dao_proposals outcomes", daoOutcomesSQL, outcomes)
		},
		func() ([]statement, error) {
			return plainStatements(top, "dao_proposals deposits", daoDepositsSQL, deposits)
		},
		func() ([]statement, error) {
			return plainStatements(top, "dao_proposals cleaned", daoCleanedSQL, cleaned)
		},
		func() ([]statement, error) { return plainStatements(top, "dao_votes", daoVotesSQL, votes) },
		func() ([]statement, error) {
			return chunked(top, "dao_ballot_reductions", daoBallotReductionsTable, mode, reductions, nil)
		},
	} {
		st, err := build()
		if err != nil {
			return nil, err
		}
		out = append(out, st...)
	}
	return out, nil
}

// firstPerID keeps the first row of each id, in id order (one statement
// cannot upsert a key twice).
func firstPerID[T any](rows []T, id func(T) int64) []T {
	seen := map[int64]bool{}
	var out []T
	for _, r := range rows {
		if !seen[id(r)] {
			seen[id(r)] = true
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b T) int { return cmp.Compare(id(a), id(b)) })
	return out
}

// lastPerID keeps the last row of each id, in id order.
func lastPerID[T any](rows []T, id func(T) int64) []T {
	last := map[int64]T{}
	for _, r := range rows {
		last[id(r)] = r
	}
	out := make([]T, 0, len(last))
	for _, r := range last {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b T) int { return cmp.Compare(id(a), id(b)) })
	return out
}

func outcomePos(o transform.DaoProposalOutcome) []int64 {
	return []int64{o.Height, int64(o.TxIndex), int64(o.MsgIndex), int64(o.Seq)}
}

// latestOutcomes keeps one status change per proposal: the latest by
// height, transaction, message and event. A later settle that ran nothing
// (an expiry) keeps the outcome of the earlier run in the same flush.
func latestOutcomes(outcomes []transform.DaoProposalOutcome) []transform.DaoProposalOutcome {
	sorted := slices.Clone(outcomes)
	slices.SortStableFunc(sorted, func(a, b transform.DaoProposalOutcome) int {
		return slices.Compare(outcomePos(a), outcomePos(b))
	})
	latest := map[int64]transform.DaoProposalOutcome{}
	for _, o := range sorted {
		if cur, ok := latest[o.ID]; ok && o.Executed == nil && !o.Cancelled {
			o.Executed, o.ExecutionError = cur.Executed, cur.ExecutionError
		}
		latest[o.ID] = o
	}
	out := make([]transform.DaoProposalOutcome, 0, len(latest))
	for _, o := range latest {
		out = append(out, o)
	}
	slices.SortFunc(out, func(a, b transform.DaoProposalOutcome) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// latestDaoVotes keeps one ballot per proposal and voter: the latest by
// height, transaction, message and event.
func latestDaoVotes(votes []transform.DaoVote) []transform.DaoVote {
	type key struct {
		id    int64
		voter string
	}
	latest := map[key]transform.DaoVote{}
	for _, v := range votes {
		k := key{v.ProposalID, v.Voter}
		cur, ok := latest[k]
		if !ok || cmp.Or(cmp.Compare(v.Height, cur.Height), cmp.Compare(v.TxIndex, cur.TxIndex),
			cmp.Compare(v.MsgIndex, cur.MsgIndex), cmp.Compare(v.Seq, cur.Seq)) > 0 {
			latest[k] = v
		}
	}
	out := make([]transform.DaoVote, 0, len(latest))
	for _, v := range latest {
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b transform.DaoVote) int {
		return cmp.Or(cmp.Compare(a.ProposalID, b.ProposalID), cmp.Compare(a.Voter, b.Voter))
	})
	return out
}
