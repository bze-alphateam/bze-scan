package dto

import (
	"encoding/json"
	"math/big"
	"strings"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/chainstate"
)

// Stats is GET /api/v1/stats: the home page's tiles. Ratios (staked_share,
// inflation, reward_rate) are percentages with five decimals; amounts are
// base units of the bond denom. A field is null until what it is read from
// exists (the first block, the first chain_state run, the first prices
// run).
type Stats struct {
	LatestHeight   *int64     `json:"latest_height"`
	LatestTime     *time.Time `json:"latest_time"`
	AvgBlockTimeMs *int64     `json:"avg_block_time_ms"`
	Txs24h         int64      `json:"txs_24h"`
	BondedTokens   *string    `json:"bonded_tokens"`
	StakedShare    *string    `json:"staked_share"`
	Inflation      *string    `json:"inflation"`
	// RewardRate is the yearly staking reward before commission:
	// inflation × (1 − community tax) ÷ staked share.
	RewardRate        *string          `json:"reward_rate"`
	Validators        *ValidatorCounts `json:"validators"`
	MaxValidators     *int64           `json:"max_validators"`
	PriceUSD          *string          `json:"price_usd"`
	PriceChange24hPct *string          `json:"price_change_24h_pct"`
	// UnbondingPeriod is in seconds.
	UnbondingPeriod *int64 `json:"unbonding_period"`
	// CommunityPool is the pool's bond-denom amount, whole base units.
	CommunityPool *string `json:"community_pool"`
	Supply        *string `json:"supply"`
	// Daily is reserved for the daily series; always empty for now.
	Daily []json.RawMessage `json:"daily"`
}

// ValidatorCounts are the validators in the active set and every known one.
type ValidatorCounts struct {
	Active int64 `json:"active"`
	Total  int64 `json:"total"`
}

// ValidatorsSummary is the header of the validators list. Ratios are
// percentages with five decimals; top5_share is the five largest bonded
// validators' share of the bonded tokens.
type ValidatorsSummary struct {
	BondedTokens    *string `json:"bonded_tokens"`
	StakedShare     *string `json:"staked_share"`
	RewardRate      *string `json:"reward_rate"`
	UnbondingPeriod *int64  `json:"unbonding_period"`
	Top5Share       *string `json:"top5_share"`
	MaxValidators   *int64  `json:"max_validators"`
}

// ValidatorList is GET /api/v1/validators: the page and the header.
// Summary is null when the route has no chain state to read.
type ValidatorList struct {
	List[Validator]
	Summary *ValidatorsSummary `json:"summary"`
}

// chainNumbers are the chain_state rows the stats read, parsed; a row that
// is missing or does not parse is nil.
type chainNumbers struct {
	pool   *chainstate.StakingPool
	supply *chainstate.Coin
	mint   *chainstate.MintState
	params *chainstate.ChainParams
	counts *chainstate.ValidatorCounts
	price  *chainstate.Price
	cpool  []chainstate.Coin
}

func row[T any](state map[string]json.RawMessage, key string) *T {
	raw, ok := state[key]
	if !ok {
		return nil
	}
	var v T
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return &v
}

func parseChainState(state map[string]json.RawMessage, denom string) chainNumbers {
	n := chainNumbers{
		pool:   row[chainstate.StakingPool](state, chainstate.KeyStakingPool),
		supply: row[chainstate.Coin](state, chainstate.KeySupply),
		mint:   row[chainstate.MintState](state, chainstate.KeyMint),
		params: row[chainstate.ChainParams](state, chainstate.KeyChainParams),
		counts: row[chainstate.ValidatorCounts](state, chainstate.KeyValidatorCounts),
		price:  row[chainstate.Price](state, chainstate.PriceKey(denom)),
	}
	if c := row[[]chainstate.Coin](state, chainstate.KeyCommunityPool); c != nil {
		n.cpool = *c
	}
	return n
}

func (n chainNumbers) bonded() *big.Int {
	if n.pool == nil {
		return nil
	}
	return intOf(n.pool.BondedTokens)
}

func (n chainNumbers) stakedShare() *string {
	bonded := n.bonded()
	if bonded == nil || n.supply == nil {
		return nil
	}
	return pctOf(bonded, intOf(n.supply.Amount))
}

// rewardRate is inflation × (1 − tax) × supply ÷ bonded.
func (n chainNumbers) rewardRate() *string {
	bonded := n.bonded()
	if bonded == nil || bonded.Sign() <= 0 || n.supply == nil || n.mint == nil || n.params == nil {
		return nil
	}
	supply := intOf(n.supply.Amount)
	infl, ok1 := new(big.Rat).SetString(n.mint.Inflation)
	tax, ok2 := new(big.Rat).SetString(n.params.CommunityTax)
	if supply == nil || !ok1 || !ok2 {
		return nil
	}
	r := new(big.Rat).Mul(infl, new(big.Rat).Sub(big.NewRat(1, 1), tax))
	r.Mul(r, new(big.Rat).SetInt(supply)).Quo(r, new(big.Rat).SetInt(bonded))
	if r.Sign() < 0 {
		return nil
	}
	s := pct(r.Num(), r.Denom())
	return &s
}

func (n chainNumbers) inflation() *string {
	if n.mint == nil {
		return nil
	}
	r, ok := new(big.Rat).SetString(n.mint.Inflation)
	if !ok || r.Sign() < 0 {
		return nil
	}
	s := pct(r.Num(), r.Denom())
	return &s
}

func (n chainNumbers) maxValidators() *int64 {
	if n.params == nil {
		return nil
	}
	v := int64(n.params.MaxValidators)
	return &v
}

func (n chainNumbers) unbonding() *int64 {
	if n.params == nil {
		return nil
	}
	return &n.params.UnbondingTimeS
}

func (n chainNumbers) communityPool(denom string) *string {
	if n.cpool == nil {
		return nil
	}
	for _, c := range n.cpool {
		if c.Denom == denom {
			whole, _, _ := strings.Cut(c.Amount, ".")
			return &whole
		}
	}
	zero := "0"
	return &zero
}

// NewStats maps the block tiles and the chain_state rows; denom is the
// bond denom, whose price, supply and community pool are shown.
func NewStats(b *repository.BlockStats, state map[string]json.RawMessage, denom string) Stats {
	n := parseChainState(state, denom)
	out := Stats{
		LatestHeight: b.LatestHeight, LatestTime: utc(b.LatestTime), AvgBlockTimeMs: b.AvgBlockTimeMs, Txs24h: b.Txs24h,
		StakedShare: n.stakedShare(), Inflation: n.inflation(), RewardRate: n.rewardRate(),
		MaxValidators: n.maxValidators(), UnbondingPeriod: n.unbonding(), CommunityPool: n.communityPool(denom),
		Daily: []json.RawMessage{},
	}
	if n.pool != nil {
		out.BondedTokens = &n.pool.BondedTokens
	}
	if n.supply != nil {
		out.Supply = &n.supply.Amount
	}
	if n.counts != nil {
		out.Validators = &ValidatorCounts{Active: n.counts.Bonded, Total: n.counts.Total}
	}
	if n.price != nil {
		out.PriceUSD, out.PriceChange24hPct = n.price.PriceUSD, n.price.PriceChange24hPct
	}
	return out
}

// NewValidatorsSummary maps the validators header; top5 is the sum of the
// five largest bonded validators' tokens.
func NewValidatorsSummary(state map[string]json.RawMessage, top5, denom string) *ValidatorsSummary {
	n := parseChainState(state, denom)
	out := &ValidatorsSummary{
		StakedShare: n.stakedShare(), RewardRate: n.rewardRate(), UnbondingPeriod: n.unbonding(),
		MaxValidators: n.maxValidators(),
	}
	if bonded := n.bonded(); bonded != nil {
		out.BondedTokens = &n.pool.BondedTokens
		out.Top5Share = pctOf(intOf(top5), bonded)
	}
	return out
}

// intOf parses a base-units amount; nil when it is not one.
func intOf(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil
	}
	return v
}

// pctOf is part over whole in percent, nil when either is missing, negative
// or whole is zero.
func pctOf(part, whole *big.Int) *string {
	if part == nil || whole == nil || whole.Sign() <= 0 || part.Sign() < 0 {
		return nil
	}
	s := pct(part, whole)
	return &s
}
