// Package daostate reads the DAOs of chain v8.3.0 (x/dao) live from the local
// node's gRPC: the DAO as the chain holds it, the account that ultimately
// controls it and, for a stake DAO, the stake its electorate sources hold.
// None of it is stored; each answer is cached in memory for a few seconds, so
// N open tabs of one DAO cost one node call per query, and concurrent misses
// of one key share a single call.
//
// A node that has not upgraded to v8.3.0 answers Unimplemented: every read
// then fails with ErrUnsupported, which callers treat as "no live data".
package daostate

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/math"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"
	rewardstypes "github.com/bze-alphateam/bze/x/rewards/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
)

// DefaultTTL is how long an answer is served from the cache.
const DefaultTTL = 5 * time.Second

var (
	// ErrNotFound means the chain has no such DAO.
	ErrNotFound = errors.New("dao not found")
	// ErrUnsupported means the node does not run x/dao (chain before v8.3.0).
	ErrUnsupported = errors.New("the node does not run x/dao")
)

// Client is the part of the x/dao query client the reader uses;
// daotypes.QueryClient satisfies it.
type Client interface {
	Params(ctx context.Context, in *daotypes.QueryParamsRequest, opts ...grpc.CallOption) (*daotypes.QueryParamsResponse, error)
	Dao(ctx context.Context, in *daotypes.QueryDaoRequest, opts ...grpc.CallOption) (*daotypes.QueryDaoResponse, error)
	DaoController(ctx context.Context, in *daotypes.QueryDaoControllerRequest, opts ...grpc.CallOption) (*daotypes.QueryDaoControllerResponse, error)
}

// Rewards is the part of the x/rewards query client the reader uses;
// rewardstypes.QueryClient satisfies it.
type Rewards interface {
	StakingReward(ctx context.Context, in *rewardstypes.QueryGetStakingRewardRequest, opts ...grpc.CallOption) (*rewardstypes.QueryGetStakingRewardResponse, error)
	DenomReward(ctx context.Context, in *rewardstypes.QueryDenomRewardRequest, opts ...grpc.CallOption) (*rewardstypes.QueryDenomRewardResponse, error)
}

// Deps are the reader's query clients.
type Deps struct {
	DAO     Client
	Rewards Rewards
}

// Config tunes the reader; the zero value is production.
type Config struct {
	// TTL is how long an answer is cached; zero is DefaultTTL.
	TTL time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Coin is an amount of a denom in base units.
type Coin struct {
	Denom  string
	Amount string
}

// Config of a DAO: its voting rules. Ratios are decimal strings, durations
// seconds.
type VotingConfig struct {
	MinQuorum         string
	Threshold         string
	VotingPeriodS     int64
	AllowExpedite     bool
	ExpeditedPeriodS  int64
	ExpediteQuorum    string
	ExpediteThreshold string
	OpenSubmission    bool
	// ProposalCost and ProposalDeposit are nil when the DAO has none.
	ProposalCost           *Coin
	ProposalDeposit        *Coin
	DepositRefundPolicy    string
	CostDestination        string
	KeptDepositDestination string
}

// Electorate names the stake sources of a stake DAO.
type Electorate struct {
	SrIDs   []string
	DrDenom string
}

// Dao is a DAO as the chain holds it.
type Dao struct {
	ID           uint64
	Address      string
	Admin        string
	PendingAdmin string
	// Type is "static_list" or "stake".
	Type        string
	Name        string
	Description string
	Config      VotingConfig
	Members     []string
	// Electorate is nil for a static-list DAO.
	Electorate *Electorate
	// CreatedAt is unix seconds.
	CreatedAt int64
	// AdminSeq and PendingAdminSeq are the chain's admin change counters.
	AdminSeq, PendingAdminSeq uint64
}

// Controller is the account that ultimately controls a DAO.
type Controller struct {
	Address string
	// EndReason is "account", "self_owned", "cycle" or "depth_cap".
	EndReason string
	Path      []string
}

// Source is one stake source of a stake DAO's electorate: a staking reward
// (SrID) or the denom reward of DrDenom.
type Source struct {
	SrID         string
	DrDenom      string
	StakingDenom string
	StakedAmount Coin
}

// ElectorateStake is the stake of a stake DAO's electorate: the chain's
// electorateStake, the sum of its sources' staked amounts.
type ElectorateStake struct {
	Sources     []Source
	VotingStake Coin
}

// Params are the module's parameters.
type Params struct {
	MaxSrPerDao        uint64
	MaxStaticMembers   uint64
	MaxOpenProposals   uint64
	MaxMsgsPerProposal uint64
	MaxProposalBytes   uint64
	MaxTitleLen        uint64
}

// Live is everything the DAO page reads live.
type Live struct {
	Dao        Dao
	Controller Controller
	// Stake is nil for a static-list DAO.
	Stake *ElectorateStake
}

// Reader reads DAOs live, through a short in-process cache.
type Reader struct {
	deps  Deps
	ttl   time.Duration
	now   func() time.Time
	group singleflight.Group

	mu      sync.Mutex
	entries map[string]entry

	// answered is set once the node has answered an x/dao query.
	answered atomic.Bool
	// failedAt is when Supported last found the node not answering, so a
	// node that does not run x/dao is asked once per TTL, not per request.
	failedAt atomic.Int64
}

type entry struct {
	value   any
	expires time.Time
}

// New returns a reader over deps.
func New(cfg Config, deps Deps) *Reader {
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultTTL
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Reader{deps: deps, ttl: cfg.TTL, now: cfg.Now, entries: map[string]entry{}}
}

// Supported reports whether the node runs x/dao: true once a query has
// answered, false for a node that answers Unimplemented (remembered for the
// TTL) or that cannot be reached and has never answered (asked again).
func (r *Reader) Supported(ctx context.Context) bool {
	if r.answered.Load() {
		return true
	}
	if at := r.failedAt.Load(); at != 0 && r.now().Sub(time.Unix(0, at)) < r.ttl {
		return false
	}
	_, err := r.Params(ctx)
	if errors.Is(err, ErrUnsupported) {
		r.failedAt.Store(r.now().UnixNano())
	}
	// Any other failure (a node outage) is not remembered: the next request
	// asks again.
	return err == nil
}

// Params reads the module's parameters.
func (r *Reader) Params(ctx context.Context) (Params, error) {
	return cached(ctx, r, "params", func(ctx context.Context) (Params, error) {
		resp, err := r.deps.DAO.Params(ctx, &daotypes.QueryParamsRequest{})
		if err != nil {
			return Params{}, r.fail("params", err)
		}
		r.answered.Store(true)
		p := resp.Params
		return Params{MaxSrPerDao: p.MaxSrPerDao, MaxStaticMembers: p.MaxStaticMembers, MaxOpenProposals: p.MaxOpenProposals,
			MaxMsgsPerProposal: p.MaxMsgsPerProposal, MaxProposalBytes: p.MaxProposalBytes, MaxTitleLen: p.MaxTitleLen}, nil
	})
}

// Dao reads DAO id. ErrNotFound when the chain has none.
func (r *Reader) Dao(ctx context.Context, id uint64) (Dao, error) {
	return cached(ctx, r, "dao|"+strconv.FormatUint(id, 10), func(ctx context.Context) (Dao, error) {
		resp, err := r.deps.DAO.Dao(ctx, &daotypes.QueryDaoRequest{Id: id})
		if err != nil {
			return Dao{}, r.fail(fmt.Sprintf("dao %d", id), err)
		}
		r.answered.Store(true)
		return daoOf(resp.Dao), nil
	})
}

// DaoController reads the account that ultimately controls DAO id.
func (r *Reader) DaoController(ctx context.Context, id uint64) (Controller, error) {
	return cached(ctx, r, "controller|"+strconv.FormatUint(id, 10), func(ctx context.Context) (Controller, error) {
		resp, err := r.deps.DAO.DaoController(ctx, &daotypes.QueryDaoControllerRequest{DaoId: id})
		if err != nil {
			return Controller{}, r.fail(fmt.Sprintf("dao %d controller", id), err)
		}
		return Controller{Address: resp.Controller, EndReason: endReason(resp.EndReason), Path: resp.Path}, nil
	})
}

// Stake reads the stake of a stake DAO's electorate: one rewards query per
// source (a staking reward per sr_id, the denom reward of dr_denom). A source
// the chain no longer has holds nothing.
func (r *Reader) Stake(ctx context.Context, e Electorate) (ElectorateStake, error) {
	key := "stake|" + e.DrDenom
	for _, id := range e.SrIDs {
		key += "|" + id
	}
	return cached(ctx, r, key, func(ctx context.Context) (ElectorateStake, error) {
		sources := make([]Source, len(e.SrIDs), len(e.SrIDs)+1)
		g, gctx := errgroup.WithContext(ctx)
		for i, id := range e.SrIDs {
			g.Go(func() error {
				s := Source{SrID: id}
				resp, err := r.deps.Rewards.StakingReward(gctx, &rewardstypes.QueryGetStakingRewardRequest{RewardId: id})
				switch {
				case status.Code(err) == codes.NotFound:
				case err != nil:
					return fmt.Errorf("staking reward %s: %w", id, err)
				default:
					s.StakingDenom = resp.StakingReward.StakingDenom
					s.StakedAmount = Coin{Denom: s.StakingDenom, Amount: zero(resp.StakingReward.StakedAmount)}
				}
				sources[i] = s
				return nil
			})
		}
		var dr *Source
		if e.DrDenom != "" {
			dr = &Source{DrDenom: e.DrDenom}
			g.Go(func() error {
				resp, err := r.deps.Rewards.DenomReward(gctx, &rewardstypes.QueryDenomRewardRequest{Denom: e.DrDenom})
				switch {
				case status.Code(err) == codes.NotFound:
				case err != nil:
					return fmt.Errorf("denom reward %s: %w", e.DrDenom, err)
				default:
					dr.StakingDenom = resp.DenomReward.StakingDenom
					dr.StakedAmount = Coin{Denom: dr.StakingDenom, Amount: resp.DenomReward.StakedAmount.String()}
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return ElectorateStake{}, err
		}
		if dr != nil {
			sources = append(sources, *dr)
		}
		total, denom := math.ZeroInt(), ""
		for _, s := range sources {
			if n, ok := math.NewIntFromString(s.StakedAmount.Amount); ok {
				total = total.Add(n)
			}
			// Every source of a stake DAO shares one staking denom.
			if denom == "" {
				denom = s.StakingDenom
			}
		}
		// A source the chain no longer has holds zero of that denom; when
		// every source is gone the bond denom stands in.
		if denom == "" {
			denom = chain.BondDenom
		}
		for i, s := range sources {
			if s.StakedAmount.Amount == "" {
				sources[i].StakedAmount = Coin{Denom: denom, Amount: "0"}
			}
		}
		return ElectorateStake{Sources: sources, VotingStake: Coin{Denom: denom, Amount: total.String()}}, nil
	})
}

// Live reads the DAO, its controller and, for a stake DAO, its electorate's
// stake. ErrNotFound when the chain has no DAO id; any other failure fails
// the whole read.
func (r *Reader) Live(ctx context.Context, id uint64) (*Live, error) {
	dao, err := r.Dao(ctx, id)
	if err != nil {
		return nil, err
	}
	out := Live{Dao: dao}
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		out.Controller, err = r.DaoController(gctx, id)
		return err
	})
	if dao.Electorate != nil {
		g.Go(func() error {
			s, err := r.Stake(gctx, *dao.Electorate)
			out.Stake = &s
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return &out, nil
}

// fail maps a gRPC error: NotFound and Unimplemented get their sentinel.
func (r *Reader) fail(what string, err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		// A NotFound answer is an answer: the module is there.
		r.answered.Store(true)
		return ErrNotFound
	case codes.Unimplemented:
		return ErrUnsupported
	}
	return fmt.Errorf("%s: %w", what, err)
}

func zero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

func daoOf(d daotypes.Dao) Dao {
	out := Dao{
		ID: d.Id, Address: d.Address, Admin: d.Admin, PendingAdmin: d.PendingAdmin, Type: TypeOf(d.Type),
		Name: d.Name, Description: d.Description, Members: d.Members, CreatedAt: d.CreatedAt,
		AdminSeq: d.AdminSeq, PendingAdminSeq: d.PendingAdminSeq,
		Config: VotingConfig{
			MinQuorum: d.Config.MinQuorum.String(), Threshold: d.Config.Threshold.String(),
			VotingPeriodS: int64(d.Config.VotingPeriod / time.Second), AllowExpedite: d.Config.AllowExpedite,
			ExpeditedPeriodS: int64(d.Config.ExpeditedPeriod / time.Second), ExpediteQuorum: d.Config.ExpediteQuorum.String(),
			ExpediteThreshold: d.Config.ExpediteThreshold.String(), OpenSubmission: d.Config.OpenSubmission,
			DepositRefundPolicy:    lower(d.Config.DepositRefundPolicy.String()),
			CostDestination:        lower(d.Config.CostDestination.String()),
			KeptDepositDestination: lower(d.Config.KeptDepositDestination.String()),
		},
	}
	if c := d.Config.ProposalCost; c != nil {
		out.Config.ProposalCost = &Coin{Denom: c.Denom, Amount: c.Amount.String()}
	}
	if c := d.Config.ProposalDeposit; c != nil {
		out.Config.ProposalDeposit = &Coin{Denom: c.Denom, Amount: c.Amount.String()}
	}
	if d.Electorate != nil {
		out.Electorate = &Electorate{SrIDs: d.Electorate.SrIds, DrDenom: d.Electorate.DrDenom}
	}
	return out
}

// TypeOf is the daos.type value of a DaoType.
func TypeOf(t daotypes.DaoType) string {
	if t == daotypes.DaoType_DAO_TYPE_STAKE {
		return "stake"
	}
	return "static_list"
}

func endReason(r daotypes.ControllerEndReason) string {
	switch r {
	case daotypes.ControllerEndReason_SELF_OWNED:
		return "self_owned"
	case daotypes.ControllerEndReason_CYCLE:
		return "cycle"
	case daotypes.ControllerEndReason_DEPTH_CAP:
		return "depth_cap"
	default:
		return "account"
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

func (r *Reader) get(key string) (any, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[key]
	if !ok || !r.now().Before(e.expires) {
		return nil, false
	}
	return e.value, true
}

// put stores value and drops the expired entries.
func (r *Reader) put(key string, value any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for k, e := range r.entries {
		if !now.Before(e.expires) {
			delete(r.entries, k)
		}
	}
	r.entries[key] = entry{value: value, expires: now.Add(r.ttl)}
}

// cached returns the cached answer of key, or loads it once for every
// concurrent caller. Errors are never cached. The load outlives a caller that
// gives up (the gRPC client bounds it).
func cached[T any](ctx context.Context, r *Reader, key string, load func(context.Context) (T, error)) (T, error) {
	var zeroT T
	if v, ok := r.get(key); ok {
		if t, ok := v.(T); ok {
			return t, nil
		}
	}
	ch := r.group.DoChan(key, func() (any, error) {
		v, err := load(context.WithoutCancel(ctx))
		if err != nil {
			return nil, err
		}
		r.put(key, v)
		return v, nil
	})
	select {
	case res := <-ch:
		if res.Err != nil {
			return zeroT, res.Err
		}
		t, _ := res.Val.(T)
		return t, nil
	case <-ctx.Done():
		return zeroT, ctx.Err()
	}
}
