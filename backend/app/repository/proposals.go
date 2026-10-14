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
	// TallySource is where the bonded tokens come from: sync or archive.
	TallySource *string
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
	// ValidatorsVoted validators, whatever their status today, have an owner
	// that voted on the proposal. ValidatorsTotal is the bonded validators
	// while the proposal is in its voting period, nil once voting ended:
	// today's active set is not the one that could vote then.
	ValidatorsVoted int
	ValidatorsTotal *int
	// VotesCount is how many accounts voted (one vote each, the latest).
	VotesCount int
	// ActiveSet is the set the tally counted, nil until the state sync
	// wrote the proposal's non-voters.
	ActiveSet *ActiveSet
}

// ActiveSet is the validator set a proposal's tally counted.
type ActiveSet struct {
	// Height is the height of the CometBFT set read.
	Height int64
	// Size is how many validators the tally counted.
	Size int
	// NonVotersCount is how many of them did not vote.
	NonVotersCount int
}

// ProposalNonVoter is a proposal_non_voters row with the moniker of its
// validator.
type ProposalNonVoter struct {
	ConsensusAddress string
	// OperatorAddress is nil when no validator was known with the
	// consensus address.
	OperatorAddress *string
	Moniker         *string
	VotingPower     int64
	PowerPct        string
}

// NonVoterKey is the keyset position of a proposal's non-voters, by power
// descending.
type NonVoterKey struct {
	VotingPower      int64
	ConsensusAddress string
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
	// Source is event, or archive for a vote read from the state before
	// the tally (no transaction).
	Source string
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
	// Source is event, or archive for a depositor's total read from the
	// state before the tally (no transaction).
	Source string
}

// DepositKey is the keyset position of a proposal's deposits, newest first.
type DepositKey struct {
	Height    int64
	TxIndex   int64
	Depositor string
}

const proposalSummaryCols = `p.id, p.title, p.kind, p.status, p.expedited, p.submit_time, p.deposit_end_time,
		p.voting_start_time, p.voting_end_time, p.tally_yes::text, p.tally_no::text, p.tally_abstain::text,
		p.tally_veto::text, p.tally_bonded_tokens::text, p.tally_source`

func proposalSummaryDest(p *ProposalSummary) []any {
	return []any{&p.ID, &p.Title, &p.Kind, &p.Status, &p.Expedited, &p.SubmitTime, &p.DepositEndTime,
		&p.VotingStartTime, &p.VotingEndTime, &p.TallyYes, &p.TallyNo, &p.TallyAbstain, &p.TallyVeto, &p.TallyBondedTokens, &p.TallySource}
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
	var setHeight *int64
	var setSize, nonVoters *int
	dest := append(proposalSummaryDest(&p.ProposalSummary), &p.Summary, &p.Metadata, &p.Proposer, &name, &kind,
		&p.MessageTypes, &p.Messages, &p.TotalDeposit, &p.TallyUpdatedAt, &p.SubmitHeight, &p.SubmitTxHash,
		&p.ResolvedHeight, &p.UpdatedAt, &p.ValidatorsVoted, &p.ValidatorsTotal, &p.VotesCount,
		&setHeight, &setSize, &nonVoters)
	err := r.db.QueryRow(ctx, `SELECT `+proposalSummaryCols+`, p.summary, p.metadata, p.proposer, l.name, l.kind,
			p.message_types, p.messages, p.total_deposit, p.tally_updated_at, p.submit_height, p.submit_tx_hash,
			p.resolved_height, p.updated_at,
			(SELECT count(*) FROM explorer.validators v
			  WHERE EXISTS (SELECT 1 FROM explorer.proposal_votes pv WHERE pv.proposal_id = p.id AND pv.voter = v.account_address)),
			CASE WHEN p.status = 'voting_period' THEN (SELECT count(*) FROM explorer.validators v WHERE v.status = 'bonded') END,
			(SELECT count(*) FROM explorer.proposal_votes pv WHERE pv.proposal_id = p.id),
			p.active_set_height, p.active_set_size,
			CASE WHEN p.non_voters_at IS NOT NULL THEN
			  (SELECT count(*) FROM explorer.proposal_non_voters n WHERE n.proposal_id = p.id) END
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
	if setHeight != nil && setSize != nil && nonVoters != nil {
		p.ActiveSet = &ActiveSet{Height: *setHeight, Size: *setSize, NonVotersCount: *nonVoters}
	}
	return &p, nil
}

// ProposalNonVoters returns up to limit of the validators of proposal id's
// tally set that did not vote, by voting power descending then consensus
// address, after the key after (from the first when nil).
func (r *Explorer) ProposalNonVoters(ctx context.Context, id int64, after *NonVoterKey, limit int) ([]ProposalNonVoter, error) {
	args := []any{id, limit}
	keyset := ""
	if after != nil {
		args = append(args, after.VotingPower, after.ConsensusAddress)
		keyset = ` AND (n.voting_power < $3 OR (n.voting_power = $3 AND n.consensus_address > $4))`
	}
	rows, err := r.db.Query(ctx, `SELECT n.consensus_address, n.operator_address, v.moniker, n.voting_power, n.power_pct::text
		FROM explorer.proposal_non_voters n
		LEFT JOIN explorer.validators v ON v.operator_address = n.operator_address
		WHERE n.proposal_id = $1`+keyset+`
		ORDER BY n.voting_power DESC, n.consensus_address LIMIT $2`, args...)
	if err != nil {
		return nil, fmt.Errorf("proposal %d non-voters: %w", id, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ProposalNonVoter, error) {
		var n ProposalNonVoter
		err := row.Scan(&n.ConsensusAddress, &n.OperatorAddress, &n.Moniker, &n.VotingPower, &n.PowerPct)
		return n, err
	})
	if err != nil {
		return nil, fmt.Errorf("proposal %d non-voters: %w", id, err)
	}
	return out, nil
}

// ProposalVotes returns up to limit votes on proposal id, newest first,
// before the key before (from the newest when nil). option filters by the
// single option (yes, no, abstain, no_with_veto) or, "weighted", the split
// votes; nil keeps every vote. validators keeps only the votes of accounts
// that own a validator.
func (r *Explorer) ProposalVotes(ctx context.Context, id int64, option *string, validators bool, before *VoteKey, limit int) ([]ProposalVote, error) {
	args := []any{id, option, limit, validators}
	keyset := ""
	if before != nil {
		args = append(args, before.Height, before.TxIndex, before.Voter)
		keyset = ` AND (pv.height, pv.tx_index, pv.voter) < ($5, $6, $7)`
	}
	rows, err := r.db.Query(ctx, `SELECT pv.voter, l.name, l.kind, pv.option, pv.options, pv.height, pv.tx_index,
			t.hash, pv.time, v.moniker, v.operator_address, v.voting_power_pct::text, pv.source
		FROM explorer.proposal_votes pv
		LEFT JOIN explorer.transactions t ON t.height = pv.height AND t.tx_index = pv.tx_index
		LEFT JOIN explorer.labels l ON l.address = pv.voter
		LEFT JOIN LATERAL (SELECT moniker, operator_address, voting_power_pct FROM explorer.validators
		                    WHERE account_address = pv.voter ORDER BY tokens DESC LIMIT 1) v ON true
		WHERE pv.proposal_id = $1
		  AND ($2::text IS NULL OR ($2 = 'weighted' AND pv.option IS NULL) OR pv.option = $2)
		  AND (NOT $4::boolean OR v.operator_address IS NOT NULL)`+keyset+`
		ORDER BY pv.height DESC, pv.tx_index DESC, pv.voter DESC LIMIT $3`, args...)
	if err != nil {
		return nil, fmt.Errorf("proposal %d votes: %w", id, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ProposalVote, error) {
		var v ProposalVote
		var name, kind *string
		err := row.Scan(&v.Voter, &name, &kind, &v.Option, &v.Options, &v.Height, &v.TxIndex, &v.TxHash, &v.Time,
			&v.ValidatorMoniker, &v.ValidatorOperator, &v.VotingPowerPct, &v.Source)
		v.VoterLabel = label(&v.Voter, name, kind)
		return v, err
	})
	if err != nil {
		return nil, fmt.Errorf("proposal %d votes: %w", id, err)
	}
	return out, nil
}

// ProposalDeposits returns up to limit deposits on proposal id, newest
// first, before the key before (from the newest when nil). A depositor's
// archive total is left out once its transactions are indexed.
func (r *Explorer) ProposalDeposits(ctx context.Context, id int64, before *DepositKey, limit int) ([]ProposalDeposit, error) {
	args := []any{id, limit}
	keyset := ""
	if before != nil {
		args = append(args, before.Height, before.TxIndex, before.Depositor)
		keyset = ` AND (d.height, d.tx_index, d.depositor) < ($3, $4, $5)`
	}
	rows, err := r.db.Query(ctx, `SELECT d.depositor, l.name, l.kind, d.amount, d.height, d.tx_index, t.hash, d.time, d.source
		FROM explorer.proposal_deposits d
		LEFT JOIN explorer.transactions t ON t.height = d.height AND t.tx_index = d.tx_index
		LEFT JOIN explorer.labels l ON l.address = d.depositor
		WHERE d.proposal_id = $1
		  AND (d.source = 'event' OR NOT EXISTS (SELECT 1 FROM explorer.proposal_deposits e
		       WHERE e.proposal_id = d.proposal_id AND e.depositor = d.depositor AND e.source = 'event'))`+keyset+`
		ORDER BY d.height DESC, d.tx_index DESC, d.depositor DESC LIMIT $2`, args...)
	if err != nil {
		return nil, fmt.Errorf("proposal %d deposits: %w", id, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ProposalDeposit, error) {
		var d ProposalDeposit
		var name, kind *string
		err := row.Scan(&d.Depositor, &name, &kind, &d.Amount, &d.Height, &d.TxIndex, &d.TxHash, &d.Time, &d.Source)
		d.DepositorLabel = label(&d.Depositor, name, kind)
		return d, err
	})
	if err != nil {
		return nil, fmt.Errorf("proposal %d deposits: %w", id, err)
	}
	return out, nil
}
