// Package proposals is the proposals kind of snapshot: for every resolved
// proposal that reached its voting period, the state of the archive node
// just before its resolution (height H - 1, H being the block whose
// active_proposal event tallied it), which the gov module deletes at H:
//
//   - the bonded tokens, for the turnout: the ubze balance of the
//     bonded_tokens_pool module account, which equals the staking pool's
//     bonded tokens at every height (the staking Pool query fails before v8);
//   - the votes: a stored vote gets its options rewritten and keeps its event
//     height; a voter without a row gets one at H - 1 (tx_index -1, source
//     archive); a stored voter the state does not list is left alone;
//   - the deposits: a depositor without a row gets one with its total at
//     H - 1 (tx_index -1, source archive); depositors with event rows are
//     left alone (the state holds a total, the rows are per transaction).
package proposals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/snapshots"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// Name of the kind.
const Name = "proposals"

// PageLimit is the page size of the votes and deposits queries.
const PageLimit = 1000

// The gRPC methods read through abci_query.
const (
	PathBalance  = "/cosmos.bank.v1beta1.Query/Balance"
	PathVotes    = "/cosmos.gov.v1.Query/Votes"
	PathDeposits = "/cosmos.gov.v1.Query/Deposits"
)

// BondedPool is the bonded_tokens_pool module account.
var BondedPool = chain.ModuleAddress("bonded_tokens_pool")

// Proposal is a stored resolved proposal to snapshot.
type Proposal struct {
	ID uint64
	// ResolvedHeight is nil when the explorer never indexed the resolution.
	ResolvedHeight *int64
	// SubmitHeight, when known, bounds the resolution search from below.
	SubmitHeight  *int64
	VotingEndTime time.Time
}

// Vote is a voter's vote in the state.
type Vote struct {
	Voter string
	// Options are the weighted options in the explorer's encoding (the
	// numeric option, as the event encodes it).
	Options json.RawMessage
	// Option is yes, no, abstain or no_with_veto for an unsplit vote, ""
	// otherwise.
	Option string
}

// Deposit is a depositor's total in the state.
type Deposit struct {
	Depositor string
	// Amount is a JSON array of coins.
	Amount json.RawMessage
}

// Snapshot is what the kind writes for one proposal.
type Snapshot struct {
	ID uint64
	// ResolvedHeight is the height found by the search, to store when the
	// column is null; 0 when the row has one.
	ResolvedHeight int64
	// Height is H - 1, the height the values are read at; Time its block's
	// time.
	Height       int64
	Time         time.Time
	BondedTokens string
	Votes        []Vote
	Deposits     []Deposit
}

// Store reads the targets and writes the snapshots.
type Store interface {
	// Resolved lists the proposals passed, rejected or failed after a
	// voting period (every one, or those of ids), by id.
	Resolved(ctx context.Context, ids []uint64) ([]Proposal, error)
	// Save writes s in one transaction, never deleting a row.
	Save(ctx context.Context, s Snapshot) error
}

// Kind is the proposals kind.
type Kind struct {
	store Store
}

var _ snapshots.Kind = (*Kind)(nil)

// New returns the kind over store.
func New(store Store) *Kind {
	return &Kind{store: store}
}

// Name is Name.
func (k *Kind) Name() string { return Name }

// Targets lists the resolved proposals that reached their voting period.
func (k *Kind) Targets(ctx context.Context, ids []string) ([]snapshots.Target, error) {
	var nums []uint64
	for _, id := range ids {
		n, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("proposals: %q is not a proposal id", id)
		}
		nums = append(nums, n)
	}
	ps, err := k.store.Resolved(ctx, nums)
	if err != nil {
		return nil, err
	}
	out := make([]snapshots.Target, 0, len(ps))
	for _, p := range ps {
		out = append(out, snapshots.Target{ID: strconv.FormatUint(p.ID, 10), Value: p})
	}
	return out, nil
}

// Snapshot reads the proposal's bonded tokens, votes and deposits at H - 1
// and saves them.
func (k *Kind) Snapshot(ctx context.Context, r snapshots.Reader, t snapshots.Target) (string, error) {
	p, ok := t.Value.(Proposal)
	if !ok {
		return "", fmt.Errorf("proposals: target %s is a %T", t.ID, t.Value)
	}
	s := Snapshot{ID: p.ID}
	h := int64(0)
	if p.ResolvedHeight != nil {
		h = *p.ResolvedHeight
	} else {
		found, err := resolution(ctx, r, p)
		if err != nil {
			return "", err
		}
		h, s.ResolvedHeight = found, found
	}
	if h < 2 {
		return "", fmt.Errorf("proposal %d: resolution height %d has no height before it", p.ID, h)
	}
	s.Height = h - 1

	b, err := r.Block(ctx, s.Height)
	if err != nil {
		return "", fmt.Errorf("proposal %d: block %d: %w", p.ID, s.Height, err)
	}
	s.Time = b.Time.UTC()

	var bal banktypes.QueryBalanceResponse
	if err := r.Query(ctx, PathBalance, &banktypes.QueryBalanceRequest{Address: BondedPool, Denom: chain.BondDenom}, &bal, s.Height); err != nil {
		return "", fmt.Errorf("proposal %d: bonded tokens: %w", p.ID, err)
	}
	if bal.Balance == nil {
		return "", fmt.Errorf("proposal %d: bonded tokens: no balance at %d", p.ID, s.Height)
	}
	s.BondedTokens = bal.Balance.Amount.String()

	if s.Votes, err = votes(ctx, r, p.ID, s.Height); err != nil {
		return "", err
	}
	if s.Deposits, err = deposits(ctx, r, p.ID, s.Height); err != nil {
		return "", err
	}
	if err := k.store.Save(ctx, s); err != nil {
		return "", fmt.Errorf("proposal %d: %w", p.ID, err)
	}
	return fmt.Sprintf("at height %d: bonded %s, %d votes, %d deposits", s.Height, s.BondedTokens, len(s.Votes), len(s.Deposits)), nil
}

func votes(ctx context.Context, r snapshots.Reader, id uint64, height int64) ([]Vote, error) {
	var out []Vote
	var next []byte
	for {
		var resp govv1.QueryVotesResponse
		req := &govv1.QueryVotesRequest{ProposalId: id, Pagination: &query.PageRequest{Key: next, Limit: PageLimit}}
		if err := r.Query(ctx, PathVotes, req, &resp, height); err != nil {
			return nil, fmt.Errorf("proposal %d: votes: %w", id, err)
		}
		for _, v := range resp.Votes {
			vote, err := voteOf(v)
			if err != nil {
				return nil, fmt.Errorf("proposal %d: vote of %s: %w", id, v.Voter, err)
			}
			out = append(out, vote)
		}
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			return out, nil
		}
		next = resp.Pagination.NextKey
	}
}

// voteOf encodes a state vote's options as the events do, so a vote read
// from the archive and one read from its transaction are the same row.
func voteOf(v *govv1.Vote) (Vote, error) {
	type option struct {
		Option int32  `json:"option"`
		Weight string `json:"weight"`
	}
	opts := make([]option, 0, len(v.Options))
	for _, o := range v.Options {
		opts = append(opts, option{Option: int32(o.Option), Weight: o.Weight})
	}
	raw, err := json.Marshal(opts)
	if err != nil {
		return Vote{}, err
	}
	options, single, err := transform.VoteOptions(string(raw))
	if err != nil {
		return Vote{}, err
	}
	return Vote{Voter: v.Voter, Options: options, Option: single}, nil
}

func deposits(ctx context.Context, r snapshots.Reader, id uint64, height int64) ([]Deposit, error) {
	var out []Deposit
	var next []byte
	for {
		var resp govv1.QueryDepositsResponse
		req := &govv1.QueryDepositsRequest{ProposalId: id, Pagination: &query.PageRequest{Key: next, Limit: PageLimit}}
		if err := r.Query(ctx, PathDeposits, req, &resp, height); err != nil {
			return nil, fmt.Errorf("proposal %d: deposits: %w", id, err)
		}
		for _, d := range resp.Deposits {
			coins := make([]chain.Coin, 0, len(d.Amount))
			for _, c := range d.Amount {
				coins = append(coins, chain.Coin{Denom: c.Denom, Amount: c.Amount.String()})
			}
			amount, err := json.Marshal(coins)
			if err != nil {
				return nil, err
			}
			out = append(out, Deposit{Depositor: d.Depositor, Amount: amount})
		}
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			return out, nil
		}
		next = resp.Pagination.NextKey
	}
}

// ErrNoResolution is returned when the archive has no block at or after the
// proposal's voting end, or that block did not tally it.
var ErrNoResolution = errors.New("resolution not found")

// resolution finds the block that tallied p: the first block whose time is
// at or after its voting end (binary search over the archive's blocks, from
// its submit height or the archive's earliest), which must carry p's
// active_proposal event.
func resolution(ctx context.Context, r snapshots.Reader, p Proposal) (int64, error) {
	st, err := r.Status(ctx)
	if err != nil {
		return 0, fmt.Errorf("proposal %d: archive status: %w", p.ID, err)
	}
	lo, hi := max(st.EarliestBlockHeight, 1), st.LatestBlockHeight
	if p.SubmitHeight != nil && *p.SubmitHeight > lo {
		lo = *p.SubmitHeight
	}
	if lo > hi {
		return 0, fmt.Errorf("proposal %d: %w: no block between %d and %d", p.ID, ErrNoResolution, lo, hi)
	}
	// The smallest height in [lo, hi] whose time is >= the voting end;
	// hi + 1 when there is none.
	top := hi + 1
	for lo < top {
		mid := lo + (top-lo)/2
		b, err := r.Block(ctx, mid)
		if err != nil {
			return 0, fmt.Errorf("proposal %d: block %d: %w", p.ID, mid, err)
		}
		if b.Time.Before(p.VotingEndTime) {
			lo = mid + 1
		} else {
			top = mid
		}
	}
	if lo > hi {
		return 0, fmt.Errorf("proposal %d: %w: no block at or after %s", p.ID, ErrNoResolution, p.VotingEndTime.UTC().Format(time.RFC3339))
	}
	res, err := r.BlockResults(ctx, lo)
	if err != nil {
		return 0, fmt.Errorf("proposal %d: block results %d: %w", p.ID, lo, err)
	}
	if !tallies(res, p.ID) {
		return 0, fmt.Errorf("proposal %d: %w: block %d has no active_proposal event for it", p.ID, ErrNoResolution, lo)
	}
	return lo, nil
}

// tallies reports whether res carries the active_proposal event of id.
func tallies(res *node.BlockResults, id uint64) bool {
	want := strconv.FormatUint(id, 10)
	return slices.ContainsFunc(slices.Concat(res.FinalizeBlockEvents, res.EndBlockEvents), func(ev node.Event) bool {
		v, _ := ev.Get("proposal_id")
		return ev.Type == "active_proposal" && v == want
	})
}
