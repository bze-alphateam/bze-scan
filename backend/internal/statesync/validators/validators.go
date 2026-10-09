// Package validators is the validators set of the state sync: it rewrites
// explorer.validators (and the validator_owner rows of explorer.labels) from
// the local node's staking and slashing queries.
package validators

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"time"

	sdkmath "cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/cosmos/cosmos-sdk/types/query"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
)

// DefaultInterval is the safety-net period of the set.
const DefaultInterval = time.Minute

// PageLimit is the page size of the list queries.
const PageLimit = 200

// Statuses as the validators table stores them.
const (
	StatusBonded    = "bonded"
	StatusUnbonding = "unbonding"
	StatusUnbonded  = "unbonded"
)

// Bech32 prefix of consensus addresses, under which signing infos are kept.
const consPrefix = "bzevalcons"

// Staking is the part of the staking query client the set uses;
// stakingtypes.QueryClient satisfies it.
type Staking interface {
	Validators(ctx context.Context, in *stakingtypes.QueryValidatorsRequest, opts ...grpc.CallOption) (*stakingtypes.QueryValidatorsResponse, error)
	Validator(ctx context.Context, in *stakingtypes.QueryValidatorRequest, opts ...grpc.CallOption) (*stakingtypes.QueryValidatorResponse, error)
	Delegation(ctx context.Context, in *stakingtypes.QueryDelegationRequest, opts ...grpc.CallOption) (*stakingtypes.QueryDelegationResponse, error)
	ValidatorDelegations(ctx context.Context, in *stakingtypes.QueryValidatorDelegationsRequest, opts ...grpc.CallOption) (*stakingtypes.QueryValidatorDelegationsResponse, error)
}

// Slashing is the part of the slashing query client the set uses;
// slashingtypes.QueryClient satisfies it.
type Slashing interface {
	Params(ctx context.Context, in *slashingtypes.QueryParamsRequest, opts ...grpc.CallOption) (*slashingtypes.QueryParamsResponse, error)
	SigningInfo(ctx context.Context, in *slashingtypes.QuerySigningInfoRequest, opts ...grpc.CallOption) (*slashingtypes.QuerySigningInfoResponse, error)
	SigningInfos(ctx context.Context, in *slashingtypes.QuerySigningInfosRequest, opts ...grpc.CallOption) (*slashingtypes.QuerySigningInfosResponse, error)
}

// Validator is one explorer.validators row as the node describes it; rank,
// voting power, first sight and updated_at are the store's. Numbers are
// decimal text; empty strings are stored as NULL.
type Validator struct {
	OperatorAddress         string
	AccountAddress          string
	ConsensusAddress        string // upper-case hex, as /block reports the proposer
	ConsensusPubkey         string // base64 of the key bytes
	Moniker                 string
	Identity                string
	Website                 string
	SecurityContact         string
	Details                 string
	Status                  string
	Jailed                  bool
	Tombstoned              bool
	JailedUntil             *time.Time
	Tokens                  string
	DelegatorShares         string
	CommissionRate          string
	CommissionMaxRate       string
	CommissionMaxChangeRate string
	CommissionUpdateTime    *time.Time
	MinSelfDelegation       string
	SelfDelegation          string
	DelegatorCount          int64
	MissedBlocks            *int64
	SignedBlocksWindow      *int64
}

// Standing is what ranking needs of a stored validator.
type Standing struct {
	Operator string
	Status   string
	Tokens   sdkmath.Int
}

// Ranking is the rank and voting power of a bonded validator.
type Ranking struct {
	Operator string
	Rank     int
	// VotingPowerPct is the share of the bonded tokens, in percent with five
	// decimals.
	VotingPowerPct string
}

// Snapshot is one write of the set.
type Snapshot struct {
	// Validators are upserted, with their validator_owner labels.
	Validators []Validator
	// Full marks every stored validator absent from Validators unbonded.
	Full bool
	// Unbonded are operators to mark unbonded (gone from the node).
	Unbonded []string
	// Ranks are the bonded validators' standings; every other row loses its
	// rank and voting power.
	Ranks []Ranking
}

// Store persists the set.
type Store interface {
	// Standings lists every stored validator.
	Standings(ctx context.Context) ([]Standing, error)
	// Save writes a snapshot in one transaction.
	Save(ctx context.Context, s Snapshot) error
}

// Deps of the set.
type Deps struct {
	Staking  Staking
	Slashing Slashing
	Store    Store
	// Keys resolves the validators' consensus keys (Any values the query
	// responses leave packed); the chain codec's interface registry.
	Keys codectypes.AnyUnpacker
}

// Set is the validators set. Runs are serialised: a single resync reads the
// stored standings to rank, so two runs must not interleave.
type Set struct {
	deps     Deps
	interval time.Duration
	mu       sync.Mutex
}

// New returns the set; interval zero is DefaultInterval.
func New(deps Deps, interval time.Duration) *Set {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Set{deps: deps, interval: interval}
}

var _ statesync.Set = (*Set)(nil)

// Name is statesync.Validators.
func (s *Set) Name() string { return statesync.Validators }

// Interval is the safety-net period.
func (s *Set) Interval() time.Duration { return s.interval }

// FullResync reads every validator of every status, page by page, with the
// signing infos, and rewrites the table; stored validators the node no
// longer lists become unbonded.
func (s *Set) FullResync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var vals []stakingtypes.Validator
	var next []byte
	for {
		resp, err := s.deps.Staking.Validators(ctx, &stakingtypes.QueryValidatorsRequest{
			Pagination: &query.PageRequest{Key: next, Limit: PageLimit},
		})
		if err != nil {
			return fmt.Errorf("staking Validators: %w", err)
		}
		vals = append(vals, resp.Validators...)
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			break
		}
		next = resp.Pagination.NextKey
	}

	infos := map[string]slashingtypes.ValidatorSigningInfo{}
	next = nil
	for {
		resp, err := s.deps.Slashing.SigningInfos(ctx, &slashingtypes.QuerySigningInfosRequest{
			Pagination: &query.PageRequest{Key: next, Limit: PageLimit},
		})
		if err != nil {
			return fmt.Errorf("slashing SigningInfos: %w", err)
		}
		for _, info := range resp.Info {
			infos[info.Address] = info
		}
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			break
		}
		next = resp.Pagination.NextKey
	}
	window, err := s.window(ctx)
	if err != nil {
		return err
	}

	snap := Snapshot{Full: true}
	standings := make([]Standing, 0, len(vals))
	for _, v := range vals {
		row, err := s.row(ctx, v, window, func(cons string) (*slashingtypes.ValidatorSigningInfo, error) {
			if info, ok := infos[cons]; ok {
				return &info, nil
			}
			return nil, nil
		})
		if err != nil {
			return err
		}
		snap.Validators = append(snap.Validators, row)
		standings = append(standings, Standing{Operator: row.OperatorAddress, Status: row.Status, Tokens: v.Tokens})
	}
	snap.Ranks = Rank(standings)
	return s.deps.Store.Save(ctx, snap)
}

// ResyncOne rewrites the validator with operator address key and re-ranks
// every bonded validator over the stored standings. A validator the node no
// longer knows becomes unbonded.
func (s *Set) ResyncOne(ctx context.Context, key string) error {
	if key == statesync.All {
		return s.FullResync(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	standings, err := s.deps.Store.Standings(ctx)
	if err != nil {
		return err
	}
	standings = slices.DeleteFunc(standings, func(st Standing) bool { return st.Operator == key })

	resp, err := s.deps.Staking.Validator(ctx, &stakingtypes.QueryValidatorRequest{ValidatorAddr: key})
	if status.Code(err) == codes.NotFound {
		return s.deps.Store.Save(ctx, Snapshot{Unbonded: []string{key}, Ranks: Rank(standings)})
	}
	if err != nil {
		return fmt.Errorf("staking Validator %s: %w", key, err)
	}
	window, err := s.window(ctx)
	if err != nil {
		return err
	}
	row, err := s.row(ctx, resp.Validator, window, func(cons string) (*slashingtypes.ValidatorSigningInfo, error) {
		r, err := s.deps.Slashing.SigningInfo(ctx, &slashingtypes.QuerySigningInfoRequest{ConsAddress: cons})
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("slashing SigningInfo %s: %w", cons, err)
		}
		return &r.ValSigningInfo, nil
	})
	if err != nil {
		return err
	}
	standings = append(standings, Standing{Operator: row.OperatorAddress, Status: row.Status, Tokens: resp.Validator.Tokens})
	return s.deps.Store.Save(ctx, Snapshot{Validators: []Validator{row}, Ranks: Rank(standings)})
}

func (s *Set) window(ctx context.Context) (int64, error) {
	resp, err := s.deps.Slashing.Params(ctx, &slashingtypes.QueryParamsRequest{})
	if err != nil {
		return 0, fmt.Errorf("slashing Params: %w", err)
	}
	return resp.Params.SignedBlocksWindow, nil
}

// row maps one validator, querying its self-delegation and delegator count.
func (s *Set) row(ctx context.Context, v stakingtypes.Validator, window int64,
	signingInfo func(cons string) (*slashingtypes.ValidatorSigningInfo, error)) (Validator, error) {
	_, opBytes, err := bech32.DecodeAndConvert(v.OperatorAddress)
	if err != nil {
		return Validator{}, fmt.Errorf("validator %q: operator address: %w", v.OperatorAddress, err)
	}
	account, err := bech32.ConvertAndEncode(chain.Bech32Prefix, opBytes)
	if err != nil {
		return Validator{}, fmt.Errorf("validator %s: account address: %w", v.OperatorAddress, err)
	}
	row := Validator{
		OperatorAddress:         v.OperatorAddress,
		AccountAddress:          account,
		Moniker:                 v.Description.Moniker,
		Identity:                v.Description.Identity,
		Website:                 v.Description.Website,
		SecurityContact:         v.Description.SecurityContact,
		Details:                 v.Description.Details,
		Status:                  bondStatus(v.Status),
		Jailed:                  v.Jailed,
		Tokens:                  v.Tokens.String(),
		DelegatorShares:         v.DelegatorShares.String(),
		CommissionRate:          v.Commission.Rate.String(),
		CommissionMaxRate:       v.Commission.MaxRate.String(),
		CommissionMaxChangeRate: v.Commission.MaxChangeRate.String(),
		CommissionUpdateTime:    nonZeroTime(v.Commission.UpdateTime),
		MinSelfDelegation:       v.MinSelfDelegation.String(),
		SignedBlocksWindow:      &window,
	}

	if err := v.UnpackInterfaces(s.deps.Keys); err != nil {
		return Validator{}, fmt.Errorf("validator %s: consensus key: %w", v.OperatorAddress, err)
	}
	pk, err := v.ConsPubKey()
	if err != nil {
		return Validator{}, fmt.Errorf("validator %s: consensus key: %w", v.OperatorAddress, err)
	}
	row.ConsensusAddress = strings.ToUpper(hex.EncodeToString(pk.Address()))
	row.ConsensusPubkey = base64.StdEncoding.EncodeToString(pk.Bytes())
	cons, err := bech32.ConvertAndEncode(consPrefix, pk.Address())
	if err != nil {
		return Validator{}, fmt.Errorf("validator %s: consensus address: %w", v.OperatorAddress, err)
	}
	info, err := signingInfo(cons)
	if err != nil {
		return Validator{}, err
	}
	if info != nil {
		missed := info.MissedBlocksCounter
		row.MissedBlocks = &missed
		row.Tombstoned = info.Tombstoned
		row.JailedUntil = nonZeroTime(info.JailedUntil)
	}

	del, err := s.deps.Staking.Delegation(ctx, &stakingtypes.QueryDelegationRequest{
		DelegatorAddr: account, ValidatorAddr: v.OperatorAddress,
	})
	switch {
	case status.Code(err) == codes.NotFound:
		row.SelfDelegation = "0"
	case err != nil:
		return Validator{}, fmt.Errorf("staking Delegation %s: %w", v.OperatorAddress, err)
	case del.DelegationResponse == nil:
		row.SelfDelegation = "0"
	default:
		row.SelfDelegation = del.DelegationResponse.Balance.Amount.String()
	}

	dels, err := s.deps.Staking.ValidatorDelegations(ctx, &stakingtypes.QueryValidatorDelegationsRequest{
		ValidatorAddr: v.OperatorAddress, Pagination: &query.PageRequest{Limit: 1, CountTotal: true},
	})
	if err != nil {
		return Validator{}, fmt.Errorf("staking ValidatorDelegations %s: %w", v.OperatorAddress, err)
	}
	if dels.Pagination != nil {
		row.DelegatorCount = int64(dels.Pagination.Total) //nolint:gosec // a delegator count fits
	}
	return row, nil
}

// Rank ranks the bonded validators by tokens (operator address breaking
// ties) and gives each its share of the bonded tokens in percent, rounded
// half up to five decimals. Other statuses are not ranked.
func Rank(standings []Standing) []Ranking {
	bonded := slices.DeleteFunc(slices.Clone(standings), func(s Standing) bool { return s.Status != StatusBonded })
	slices.SortFunc(bonded, func(a, b Standing) int {
		if c := b.Tokens.BigInt().Cmp(a.Tokens.BigInt()); c != 0 {
			return c
		}
		return strings.Compare(a.Operator, b.Operator)
	})
	total := new(big.Int)
	for _, s := range bonded {
		total.Add(total, s.Tokens.BigInt())
	}
	out := make([]Ranking, len(bonded))
	for i, s := range bonded {
		out[i] = Ranking{Operator: s.Operator, Rank: i + 1, VotingPowerPct: percent(s.Tokens.BigInt(), total)}
	}
	return out
}

// percent is part/total in percent with five decimals, rounded half up.
func percent(part, total *big.Int) string {
	if total.Sign() == 0 {
		return "0.00000"
	}
	// part * 100 * 10^5 / total, half up: (2 * part * 10^7 + total) / (2 * total).
	num := new(big.Int).Mul(part, big.NewInt(20_000_000))
	num.Add(num, total)
	q := num.Quo(num, new(big.Int).Mul(total, big.NewInt(2)))
	s := fmt.Sprintf("%06s", q.String())
	return s[:len(s)-5] + "." + s[len(s)-5:]
}

func bondStatus(s stakingtypes.BondStatus) string {
	switch s {
	case stakingtypes.Bonded:
		return StatusBonded
	case stakingtypes.Unbonding:
		return StatusUnbonding
	default:
		return StatusUnbonded
	}
}

// nonZeroTime is nil for the zero time and the Unix epoch, which the SDK
// uses for "never".
func nonZeroTime(t time.Time) *time.Time {
	if t.IsZero() || t.Unix() == 0 {
		return nil
	}
	u := t.UTC()
	return &u
}
