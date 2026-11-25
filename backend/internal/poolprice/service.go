package poolprice

import (
	"context"
	"encoding/json"
	"fmt"

	sdkmath "cosmossdk.io/math"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/dexunits"
	"github.com/bze-alphateam/bze-scan/backend/internal/tickers"
)

// Averages reads a pool's price average; Cached implements it.
type Averages interface {
	Average(ctx context.Context, poolID string) (*Average, error)
}

// Pools reads every liquidity pool; tickers.NodePools implements it. Its
// reserves are cached for a few seconds, so the explorer's spot can lag the
// chain by that much.
type Pools interface {
	Pools(ctx context.Context) ([]tickers.Pool, error)
}

// Params reads the live module parameters; params.Cached implements it.
type Params interface {
	Params(ctx context.Context) (map[string]json.RawMessage, error)
}

// Meta is what the explorer knows about a denom for display.
type Meta struct {
	Symbol   string
	Exponent int
}

// Metas reads the display metadata of denoms; a denom it does not know is
// missing from the map.
type Metas interface {
	Metas(ctx context.Context, denoms []string) (map[string]Meta, error)
}

// Asset is one side of the pool with its reserve in base units.
type Asset struct {
	Denom    string  `json:"denom"`
	Symbol   *string `json:"symbol"`
	Exponent *int    `json:"exponent"`
	Reserve  string  `json:"reserve"`
}

// Reserves are the two sides of the pool.
type Reserves struct {
	Base  Asset `json:"base"`
	Quote Asset `json:"quote"`
}

// Price is a pool price: Chain in chain units (quote base units per base
// base unit), Display in display units (null when an exponent is unknown).
type Price struct {
	Chain   string  `json:"chain"`
	Display *string `json:"display"`
}

// Pool is the price state of a liquidity pool. AvgPrice is null (and both
// flags with it) when the node has no average for the pool. DriftPct is
// null without a price or with an average of 0. The flags are null for a
// pool without the native denom, or when the parameters could not be read:
// FeesInBZE = drifted against feeMaxPriceDrift (fees the payer would pay in
// the pool's token are charged in BZE), ModuleSwapsHeld = drifted against
// moduleMaxPriceDrift (the fee collector's swap and the burner's liquidity
// move are refused with ErrPoolPriceDrift).
type Pool struct {
	PoolID          string   `json:"pool_id"`
	Reserves        Reserves `json:"reserves"`
	Price           *Price   `json:"price"`
	AvgPrice        *Price   `json:"avg_price"`
	AvgLastHeight   *int64   `json:"avg_last_height"`
	DriftPct        *string  `json:"drift_pct"`
	HalfLifeBlocks  *int64   `json:"half_life_blocks"`
	FeeMaxDriftPct  *string  `json:"fee_max_drift_pct"`
	ModuleMaxDrift  *string  `json:"module_max_drift_pct"`
	FeesInBZE       *bool    `json:"fees_in_bze"`
	ModuleSwapsHeld *bool    `json:"module_swaps_held"`
}

// Deps are what a Service reads.
type Deps struct {
	Pools    Pools
	Averages Averages
	Params   Params
	Metas    Metas
	// Log receives the failures the page survives; nil is the standard logger.
	Log log.FieldLogger
}

// Service builds the Pool of an LP denom.
type Service struct {
	deps   Deps
	native string
	log    log.FieldLogger
}

// New returns a Service; native is the chain's native denom.
func New(deps Deps, native string) *Service {
	lg := deps.Log
	if lg == nil {
		lg = log.StandardLogger()
	}
	return &Service{deps: deps, native: native, log: lg}
}

// Pool returns the price state of the pool whose LP denom is lpDenom; nil
// when no such pool exists. A failed average read is an error; a missing
// average, missing parameters or missing metadata only null the fields that
// depend on them.
func (s *Service) Pool(ctx context.Context, lpDenom string) (*Pool, error) {
	pools, err := s.deps.Pools.Pools(ctx)
	if err != nil {
		return nil, fmt.Errorf("pools: %w", err)
	}
	var p *tickers.Pool
	for i := range pools {
		if pools[i].LPDenom == lpDenom {
			p = &pools[i]
			break
		}
	}
	if p == nil {
		return nil, nil
	}
	avg, err := s.deps.Averages.Average(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	metas, err := s.deps.Metas.Metas(ctx, []string{p.Base, p.Quote})
	if err != nil {
		// Symbols and display units are an extra: the pool is shown without them.
		s.log.WithError(err).WithField("pool", p.ID).Warn("pool price: denom metadata")
		metas = nil
	}
	out := &Pool{
		PoolID: p.ID,
		Reserves: Reserves{
			Base:  asset(p.Base, p.ReserveBase, metas),
			Quote: asset(p.Quote, p.ReserveQuote, metas),
		},
	}
	sp, hasSpot := spot(p.ReserveBase, p.ReserveQuote)
	if hasSpot {
		out.Price = price(sp, out.Reserves)
	}
	if avg == nil {
		return out, nil
	}
	out.AvgPrice = price(avg.AvgPrice, out.Reserves)
	h := avg.LastHeight
	out.AvgLastHeight = &h
	if pct, ok := driftPct(sp, hasSpot, avg.AvgPrice); ok {
		out.DriftPct = &pct
	}
	if p.Base != s.native && p.Quote != s.native {
		return out, nil
	}
	limits, ok := s.limits(ctx, out)
	if !ok {
		return out, nil
	}
	fees := drifted(sp, hasSpot, avg.AvgPrice, limits.fee)
	held := drifted(sp, hasSpot, avg.AvgPrice, limits.module)
	out.FeesInBZE, out.ModuleSwapsHeld = &fees, &held
	return out, nil
}

type limits struct{ fee, module sdkmath.LegacyDec }

// limits reads the drift parameters and fills their display fields of out.
func (s *Service) limits(ctx context.Context, out *Pool) (limits, bool) {
	all, err := s.deps.Params.Params(ctx)
	if err != nil && all == nil {
		return limits{}, false
	}
	raw, ok := all["tradebin"]
	if !ok {
		return limits{}, false
	}
	var doc struct {
		HalfLife string `json:"poolPriceHalfLifeBlocks"`
		Fee      string `json:"feeMaxPriceDrift"`
		Module   string `json:"moduleMaxPriceDrift"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return limits{}, false
	}
	fee, err1 := sdkmath.LegacyNewDecFromStr(doc.Fee)
	module, err2 := sdkmath.LegacyNewDecFromStr(doc.Module)
	if err1 != nil || err2 != nil {
		return limits{}, false
	}
	var half int64
	if _, err := fmt.Sscan(doc.HalfLife, &half); err == nil {
		out.HalfLifeBlocks = &half
	}
	f, m := pct(fee), pct(module)
	out.FeeMaxDriftPct, out.ModuleMaxDrift = &f, &m
	return limits{fee: fee, module: module}, true
}

// pct renders a fraction as a percentage without trailing zeros.
func pct(d sdkmath.LegacyDec) string {
	return dexunits.TrimTrailingZeros(d.MulInt64(100).String())
}

func asset(denom string, reserve sdkmath.Int, metas map[string]Meta) Asset {
	a := Asset{Denom: denom, Reserve: "0"}
	if !reserve.IsNil() {
		a.Reserve = reserve.String()
	}
	if m, ok := metas[denom]; ok {
		sym, exp := m.Symbol, m.Exponent
		if sym != "" {
			a.Symbol = &sym
		}
		a.Exponent = &exp
	}
	return a
}

func price(chain sdkmath.LegacyDec, r Reserves) *Price {
	out := &Price{Chain: chain.String()}
	if r.Base.Exponent != nil && r.Quote.Exponent != nil {
		if d, err := dexunits.Price(out.Chain, *r.Base.Exponent, *r.Quote.Exponent); err == nil {
			out.Display = &d
		}
	}
	return out
}
