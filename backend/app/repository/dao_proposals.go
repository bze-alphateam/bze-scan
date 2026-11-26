package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// DaoProposal is an explorer.dao_proposals row with its DAO's name and type
// and the proposer's label. Every column but the id and the status is NULL
// on a row whose submission is not indexed (a settle indexed first, a
// proposal submitted below the live floor and gone from the chain).
type DaoProposal struct {
	ID            int64
	DaoID         *int64
	DaoName       *string
	DaoType       *string
	Proposer      *string
	ProposerLabel *Label
	Title         *string
	Summary       *string
	Msgs          json.RawMessage
	Expedited     *bool
	SubmitHeight  *int64
	SubmitTime    *time.Time
	SubmitTxHash  *string
	VotingEnd     *time.Time
	Denominator   *string
	PinnedConfig  json.RawMessage
	// Status is nil on a placeholder row (a deposit release or a cleanup
	// indexed without the submission and the settle).
	Status             *string
	Yes                *string
	No                 *string
	Abstain            *string
	TallyUpdatedAt     *time.Time
	ExpediteSettleable bool
	RetryDeadline      *time.Time
	SettledHeight      *int64
	SettledTxHash      *string
	ExpeditedEarly     *bool
	Executed           *bool
	ExecutionError     *string
	DepositRefunded    *bool
	DepositDestination *string
	DepositAmount      *string
	CancelledHeight    *int64
	CleanedHeight      *int64
}

const daoProposalCols = `p.id, p.dao_id, d.name, d.type, p.proposer, lp.name, lp.kind, p.title, p.summary, p.msgs,
	p.expedited, p.submit_height, p.submit_time, p.submit_tx_hash, p.voting_end, p.denominator::text, p.pinned_config,
	p.status, p.yes::text, p.no::text, p.abstain::text, p.tally_updated_at, p.expedite_settleable, p.retry_deadline,
	p.settled_height, p.settled_tx_hash, p.expedited_early, p.executed, p.execution_error, p.deposit_refunded,
	p.deposit_destination, p.deposit_amount, p.cancelled_height, p.cleaned_height`

const daoProposalFrom = ` FROM explorer.dao_proposals p
	LEFT JOIN explorer.daos d ON d.id = p.dao_id
	LEFT JOIN explorer.labels lp ON lp.address = p.proposer`

func scanDaoProposal(row pgx.Row) (DaoProposal, error) {
	var p DaoProposal
	var labelName, labelKind *string
	err := row.Scan(&p.ID, &p.DaoID, &p.DaoName, &p.DaoType, &p.Proposer, &labelName, &labelKind, &p.Title, &p.Summary,
		&p.Msgs, &p.Expedited, &p.SubmitHeight, &p.SubmitTime, &p.SubmitTxHash, &p.VotingEnd, &p.Denominator,
		&p.PinnedConfig, &p.Status, &p.Yes, &p.No, &p.Abstain, &p.TallyUpdatedAt, &p.ExpediteSettleable,
		&p.RetryDeadline, &p.SettledHeight, &p.SettledTxHash, &p.ExpeditedEarly, &p.Executed, &p.ExecutionError,
		&p.DepositRefunded, &p.DepositDestination, &p.DepositAmount, &p.CancelledHeight, &p.CleanedHeight)
	if err != nil {
		return DaoProposal{}, err
	}
	p.ProposerLabel = label(p.Proposer, labelName, labelKind)
	return p, nil
}

// DaoProposals returns up to limit proposals of DAO daoID, newest (highest
// id) first, below before when not nil, only those in status when not nil.
func (r *Explorer) DaoProposals(ctx context.Context, daoID int64, status *string, before *int64, limit int) ([]DaoProposal, error) {
	rows, err := r.db.Query(ctx, `SELECT `+daoProposalCols+daoProposalFrom+`
		WHERE p.dao_id = $1 AND ($2::text IS NULL OR p.status = $2) AND ($3::bigint IS NULL OR p.id < $3)
		ORDER BY p.id DESC LIMIT $4`, daoID, status, before, limit)
	if err != nil {
		return nil, fmt.Errorf("dao %d proposals: %w", daoID, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (DaoProposal, error) { return scanDaoProposal(row) })
}

// DaoProposal returns DAO proposal id and its number of votes; ErrNotFound
// when the index has none.
func (r *Explorer) DaoProposal(ctx context.Context, id int64) (*DaoProposal, int64, error) {
	p, err := scanDaoProposal(r.db.QueryRow(ctx, `SELECT `+daoProposalCols+daoProposalFrom+` WHERE p.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, fmt.Errorf("dao proposal %d: %w", id, err)
	}
	var votes int64
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM explorer.dao_votes WHERE proposal_id = $1`, id).Scan(&votes); err != nil {
		return nil, 0, fmt.Errorf("dao proposal %d votes: %w", id, err)
	}
	return &p, votes, nil
}

// DaoVote is a dao_votes row with its effective weight: the ballot's weight
// less the ballot reductions indexed after it, floored at 0.
type DaoVote struct {
	Voter           string
	VoterLabel      *Label
	Option          string
	Weight          string
	EffectiveWeight string
	Height          int64
	TxIndex         int64
	MsgIndex        int64
	TxHash          *string
	Time            time.Time
}

// DaoVoteKey is the keyset position of a proposal's votes: effective weight
// descending, then voter ascending.
type DaoVoteKey struct {
	// EffectiveWeight is a decimal integer.
	EffectiveWeight string
	Voter           string
}

// inBlock is the position of a row of alias within its block: its
// transaction index, block-level rows (tx_index -1, the EndBlocker) after
// every transaction.
func inBlock(alias string) string {
	return `COALESCE(NULLIF(` + alias + `.tx_index, -1), 2147483647)`
}

// reductionsJoin adds, to a query over explorer.dao_votes v, the sum of the
// ballot reductions that count against each ballot as red.amount.
var reductionsJoin = `
		          LEFT JOIN LATERAL (SELECT sum(b.amount) AS amount FROM explorer.dao_ballot_reductions b
		                WHERE b.proposal_id = v.proposal_id AND b.voter = v.voter AND b.height >= v.height
		                  AND (b.height, ` + inBlock("b") + `, b.msg_index) > (v.height, ` + inBlock("v") + `, v.msg_index)) red ON true`

// DaoVotes returns up to limit votes of DAO proposal id, heaviest effective
// weight first, then by voter, after when not nil, only option when not nil.
// A reduction counts against a ballot when it comes after it (height,
// transaction, message; the block's own events after its transactions): a revote's weight already reflects the earlier
// ones.
func (r *Explorer) DaoVotes(ctx context.Context, id int64, option *string, after *DaoVoteKey, limit int) ([]DaoVote, error) {
	var weight, voter *string
	if after != nil {
		weight, voter = &after.EffectiveWeight, &after.Voter
	}
	rows, err := r.db.Query(ctx, `SELECT x.voter, l.name, l.kind, x.option, x.weight::text, x.eff::text, x.height,
		       x.tx_index, x.msg_index, x.tx_hash, x.time
		  FROM (SELECT v.voter, v.option, v.weight, GREATEST(v.weight - COALESCE(red.amount, 0), 0) AS eff, v.height,
		               v.tx_index, v.msg_index, v.tx_hash, v.time
		          FROM explorer.dao_votes v`+reductionsJoin+`
		         WHERE v.proposal_id = $1 AND ($2::text IS NULL OR v.option = $2)) x
		  LEFT JOIN explorer.labels l ON l.address = x.voter
		 WHERE $3::numeric IS NULL OR x.eff < $3 OR (x.eff = $3 AND x.voter > $4)
		 ORDER BY x.eff DESC, x.voter ASC LIMIT $5`, id, option, weight, voter, limit)
	if err != nil {
		return nil, fmt.Errorf("dao proposal %d votes: %w", id, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (DaoVote, error) {
		var v DaoVote
		var name, kind *string
		err := row.Scan(&v.Voter, &name, &kind, &v.Option, &v.Weight, &v.EffectiveWeight, &v.Height, &v.TxIndex,
			&v.MsgIndex, &v.TxHash, &v.Time)
		v.VoterLabel = label(&v.Voter, name, kind)
		return v, err
	})
}

// OpenDaoProposalKey is the keyset position of the voting proposals list:
// voting_end ascending (microseconds since the epoch), then id ascending.
type OpenDaoProposalKey struct {
	VotingEndMicros int64
	ID              int64
}

// DaoProposalsAcross returns up to limit proposals of every DAO whose status
// is one of statuses (every real proposal when empty; a placeholder row,
// whose status is NULL, never is). When voting is true (statuses is exactly
// "voting") the order is voting_end ascending then id ascending, after
// afterVoting when not nil; otherwise id descending, below before when not
// nil. A proposal in voting whose rows the chain cleaned up is not listed.
func (r *Explorer) DaoProposalsAcross(ctx context.Context, statuses []string, voting bool, before *int64,
	afterVoting *OpenDaoProposalKey, limit int) ([]DaoProposal, error) {
	if statuses == nil {
		statuses = []string{}
	}
	where := `p.status IS NOT NULL AND (cardinality($1::text[]) = 0 OR p.status = ANY($1))
		  AND (p.status <> 'voting' OR p.cleaned_height IS NULL)`
	var rows pgx.Rows
	var err error
	if voting {
		var endAt *time.Time
		var id *int64
		if afterVoting != nil {
			t := time.UnixMicro(afterVoting.VotingEndMicros).UTC()
			endAt, id = &t, &afterVoting.ID
		}
		rows, err = r.db.Query(ctx, `SELECT `+daoProposalCols+daoProposalFrom+`
			WHERE `+where+` AND p.voting_end IS NOT NULL
			  AND ($2::timestamptz IS NULL OR (p.voting_end, p.id) > ($2, $3::bigint))
			ORDER BY p.voting_end ASC, p.id ASC LIMIT $4`, statuses, endAt, id, limit)
	} else {
		rows, err = r.db.Query(ctx, `SELECT `+daoProposalCols+daoProposalFrom+`
			WHERE `+where+` AND ($2::bigint IS NULL OR p.id < $2)
			ORDER BY p.id DESC LIMIT $3`, statuses, before, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("dao proposals: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (DaoProposal, error) { return scanDaoProposal(row) })
}

// VoterBallot is a ballot of an address with its proposal and DAO. The
// proposal's and the DAO's columns are nil on a ballot whose proposal or DAO
// the index does not hold.
type VoterBallot struct {
	DaoVote
	ProposalID     int64
	ProposalTitle  *string
	ProposalStatus *string
	VotingEnd      *time.Time
	DaoID          *int64
	DaoName        *string
}

// VoterBallotKey is the keyset position of a voter's ballots: height
// descending, then proposal id descending (a voter has one ballot per
// proposal).
type VoterBallotKey struct {
	Height     int64
	ProposalID int64
}

// DaoVotesByVoter returns up to limit ballots of voter, newest first, below
// before when not nil, with the effective weight as DaoVotes derives it.
func (r *Explorer) DaoVotesByVoter(ctx context.Context, voter string, before *VoterBallotKey, limit int) ([]VoterBallot, error) {
	var height, proposal *int64
	if before != nil {
		height, proposal = &before.Height, &before.ProposalID
	}
	rows, err := r.db.Query(ctx, `SELECT v.proposal_id, v.voter, v.option, v.weight::text,
		       GREATEST(v.weight - COALESCE(red.amount, 0), 0)::text, v.height, v.tx_index, v.msg_index, v.tx_hash, v.time,
		       p.title, p.status, p.voting_end, d.id, d.name
		  FROM explorer.dao_votes v`+reductionsJoin+`
		  LEFT JOIN explorer.dao_proposals p ON p.id = v.proposal_id
		  LEFT JOIN explorer.daos d ON d.id = p.dao_id
		 WHERE v.voter = $1 AND ($2::bigint IS NULL OR (v.height, v.proposal_id) < ($2, $3::bigint))
		 ORDER BY v.height DESC, v.proposal_id DESC LIMIT $4`, voter, height, proposal, limit)
	if err != nil {
		return nil, fmt.Errorf("dao votes of %s: %w", voter, err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (VoterBallot, error) {
		var b VoterBallot
		err := row.Scan(&b.ProposalID, &b.Voter, &b.Option, &b.Weight, &b.EffectiveWeight, &b.Height, &b.TxIndex,
			&b.MsgIndex, &b.TxHash, &b.Time, &b.ProposalTitle, &b.ProposalStatus, &b.VotingEnd, &b.DaoID, &b.DaoName)
		return b, err
	})
}
