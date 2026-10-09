package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ProposalSummary is a proposals row as the proposals list shows it.
type ProposalSummary struct {
	ID              int64
	Title           string
	Kind            string
	Status          string
	Expedited       bool
	SubmitTime      time.Time
	DepositEndTime  *time.Time
	VotingStartTime *time.Time
	VotingEndTime   *time.Time
	// The tally columns are nil until the state sync fills them.
	TallyYes, TallyNo, TallyAbstain, TallyVeto *string
	TallyBondedTokens                          *string
}

// Proposal is a whole proposals row, with how many bonded validators voted.
type Proposal struct {
	ProposalSummary
	Summary        *string
	Metadata       *string
	Proposer       *string
	ProposerLabel  *Label
	MessageTypes   []string
	Messages       json.RawMessage
	TotalDeposit   json.RawMessage
	TallyUpdatedAt *time.Time
	SubmitHeight   *int64
	SubmitTxHash   *string
	ResolvedHeight *int64
	UpdatedAt      time.Time
	// ValidatorsVoted of ValidatorsTotal bonded validators have an owner
	// that voted on the proposal.
	ValidatorsVoted int
	ValidatorsTotal int
}

// ProposalVote is a proposal_votes row with its transaction's hash, the
// voter's label and, when the voter owns a validator, that validator.
type ProposalVote struct {
	Voter      string
	VoterLabel *Label
	// Option is nil for a split vote.
	Option            *string
	Options           json.RawMessage
	Height            int64
	TxIndex           int64
	TxHash            *string
	Time              time.Time
	ValidatorMoniker  *string
	ValidatorOperator *string
	VotingPowerPct    *string
}

// VoteKey is the keyset position of a proposal's votes, newest first.
type VoteKey struct {
	Height  int64
	TxIndex int64
	Voter   string
}

// ProposalDeposit is a proposal_deposits row with its transaction's hash
// and the depositor's label.
type ProposalDeposit struct {
	Depositor      string
	DepositorLabel *Label
	Amount         json.RawMessage
	Height         int64
	TxIndex        int64
	TxHash         *string
	Time           time.Time
}

// DepositKey is the keyset position of a proposal's deposits, newest first.
type DepositKey struct {
	Height    int64
	TxIndex   int64
	Depositor string
}

const proposalSummaryCols = `p.id, p.title, p.kind, p.status, p.expedited, p.submit_time, p.deposit_end_time,
		p.voting_start_time, p.voting_end_time, p.tally_yes::text, p.tally_no::text, p.tally_abstain::text,
		p.tally_veto::text, p.tally_bonded_tokens::text`

func proposalSummaryDest(p *ProposalSummary) []any {
	return []any{&p.ID, &p.Title, &p.Kind, &p.Status, &p.Expedited, &p.SubmitTime, &p.DepositEndTime,
		&p.VotingStartTime, &p.VotingEndTime, &p.TallyYes, &p.TallyNo, &p.TallyAbstain, &p.TallyVeto, &p.TallyBondedTokens}
}

// Proposals returns up to limit proposals with status (every status when
// nil), newest first, below the id before (from the newest when nil).
func (r *Explorer) Proposals(ctx context.Context, status *string, before *int64, limit int) ([]ProposalSummary, error) {
	rows, err := r.db.Query(ctx, `SELECT `+proposalSummaryCols+` FROM explorer.proposals p
		WHERE ($1::text IS NULL OR p.status = $1) AND ($2::bigint IS NULL OR p.id < $2)
		ORDER BY p.id DESC LIMIT $3`, status, before, limit)
	if err != nil {
		return nil, fmt.Errorf("proposals: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ProposalSummary, error) {
		var p ProposalSummary
		return p, row.Scan(proposalSummaryDest(&p)...)
	})
	if err != nil {
		return nil, fmt.Errorf("proposals: %w", err)
	}
	return out, nil
}

// Proposal returns proposal id, or ErrNotFound.
func (r *Explorer) Proposal(ctx context.Context, id int64) (*Proposal, error) {
	var p Proposal
	var name, kind *string
	dest := append(proposalSummaryDest(&p.ProposalSummary), &p.Summary, &p.Metadata, &p.Proposer, &name, &kind,
		&p.MessageTypes, &p.Messages, &p.TotalDeposit, &p.TallyUpdatedAt, &p.SubmitHeight, &p.SubmitTxHash,
		&p.ResolvedHeight, &p.UpdatedAt, &p.ValidatorsVoted, &p.ValidatorsTotal)
	err := r.db.QueryRow(ctx, `SELECT `+proposalSummaryCols+`, p.summary, p.metadata, p.proposer, l.name, l.kind,
			p.message_types, p.messages, p.total_deposit, p.tally_updated_at, p.submit_height, p.submit_tx_hash,
			p.resolved_height, p.updated_at,
			(SELECT count(*) FROM explorer.validators v WHERE v.status = 'bonded'
			   AND EXISTS (SELECT 1 FROM explorer.proposal_votes pv WHERE pv.proposal_id = p.id AND pv.voter = v.account_address)),
			(SELECT count(*) FROM explorer.validators v WHERE v.status = 'bonded')
		FROM explorer.proposals p
		LEFT JOIN explorer.labels l ON l.address = p.proposer
		WHERE p.id = $1`, id).Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("proposal %d: %w", id, err)
	}
	p.ProposerLabel = label(p.Proposer, name, kind)
	return &p, nil
}

// ProposalVotes returns up to limit votes on proposal id, newest first,
// before the key before (from the newest when nil). option filters by the
// single option (yes, no, abstain, no_with_veto) or, "weighted", the split
// votes; nil keeps every vote.
func (r *Explorer) ProposalVotes(ctx context.Context, id int64, option *string, before *VoteKey, limit int) ([]ProposalVote, error) {
	args := []any{id, option, limit}
	keyset := ""
	if before != nil {
		args = append(args, before.Height, before.TxIndex, before.Voter)
		keyset = ` AND (pv.height, pv.tx_index, pv.voter) < ($4, $5, $6)`
	}
	rows, err := r.db.Query(ctx, `SELECT pv.voter, l.name, l.kind, pv.option, pv.options, pv.height, pv.tx_index,
			t.hash, pv.time, v.moniker, v.operator_address, v.voting_power_pct::text
		FROM explorer.proposal_votes pv
		LEFT JOIN explorer.transactions t ON t.height = pv.height AND t.tx_index = pv.tx_index
		LEFT JOIN explorer.labels l ON l.address = pv.voter
		LEFT JOIN LATERAL (SELECT moniker, operator_address, voting_power_pct FROM explorer.validators
		                    WHERE account_address = pv.voter ORDER BY tokens DESC LIMIT 1) v ON true
		WHERE pv.proposal_id = $1
		  AND ($2::text IS NULL OR ($2 = 'weighted' AND pv.option IS NULL) OR pv.option = $2)`+keyset+`
		ORDER BY pv.height DESC, pv.tx_index DESC, pv.voter DESC LIMIT $3`, args...)
	if err != nil {
		return nil, fmt.Errorf("proposal %d votes: %w", id, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ProposalVote, error) {
		var v ProposalVote
		var name, kind *string
		err := row.Scan(&v.Voter, &name, &kind, &v.Option, &v.Options, &v.Height, &v.TxIndex, &v.TxHash, &v.Time,
			&v.ValidatorMoniker, &v.ValidatorOperator, &v.VotingPowerPct)
		v.VoterLabel = label(&v.Voter, name, kind)
		return v, err
	})
	if err != nil {
		return nil, fmt.Errorf("proposal %d votes: %w", id, err)
	}
	return out, nil
}

// ProposalDeposits returns up to limit deposits on proposal id, newest
// first, before the key before (from the newest when nil).
func (r *Explorer) ProposalDeposits(ctx context.Context, id int64, before *DepositKey, limit int) ([]ProposalDeposit, error) {
	args := []any{id, limit}
	keyset := ""
	if before != nil {
		args = append(args, before.Height, before.TxIndex, before.Depositor)
		keyset = ` AND (d.height, d.tx_index, d.depositor) < ($3, $4, $5)`
	}
	rows, err := r.db.Query(ctx, `SELECT d.depositor, l.name, l.kind, d.amount, d.height, d.tx_index, t.hash, d.time
		FROM explorer.proposal_deposits d
		LEFT JOIN explorer.transactions t ON t.height = d.height AND t.tx_index = d.tx_index
		LEFT JOIN explorer.labels l ON l.address = d.depositor
		WHERE d.proposal_id = $1`+keyset+`
		ORDER BY d.height DESC, d.tx_index DESC, d.depositor DESC LIMIT $2`, args...)
	if err != nil {
		return nil, fmt.Errorf("proposal %d deposits: %w", id, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ProposalDeposit, error) {
		var d ProposalDeposit
		var name, kind *string
		err := row.Scan(&d.Depositor, &name, &kind, &d.Amount, &d.Height, &d.TxIndex, &d.TxHash, &d.Time)
		d.DepositorLabel = label(&d.Depositor, name, kind)
		return d, err
	})
	if err != nil {
		return nil, fmt.Errorf("proposal %d deposits: %w", id, err)
	}
	return out, nil
}
