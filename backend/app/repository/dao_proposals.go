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
		          FROM explorer.dao_votes v
		          LEFT JOIN LATERAL (SELECT sum(b.amount) AS amount FROM explorer.dao_ballot_reductions b
		                WHERE b.proposal_id = v.proposal_id AND b.voter = v.voter AND b.height >= v.height
		                  AND (b.height, `+inBlock("b")+`, b.msg_index) > (v.height, `+inBlock("v")+`, v.msg_index)) red ON true
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
