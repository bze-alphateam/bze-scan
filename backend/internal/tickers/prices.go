package tickers

import (
	"context"
	"math/big"
	"strings"

	"github.com/bze-alphateam/bze-scan/backend/internal/tradehistory"
)

// USD is denom's internal USD price, the aggregator's, kept PriceTTL; zero
// when there is none:
//   - the USDC denom is $1;
//   - an LP token is its pool's reserves at their USD prices per whole LP
//     token;
//   - the native denom is its USDC price (see last), else CoinGecko's price
//     of its id;
//   - any other denom is the average of its USDC price and its native
//     price times the native denom's USD price, or whichever of the two
//     exists.
//
// Unlike the aggregator, no price is cut to six decimals, and the native
// denom's CoinGecko fallback reads its CoinGecko id (the aggregator looks
// for the denom and never finds it). A price computed after a failed read
// (a store, the node, CoinGecko) is answered but not kept.
func (s *Service) USD(ctx context.Context, denom string) *big.Rat {
	p, _ := s.cachedUSD(ctx, denom)
	return p
}

// cachedUSD is USD; failed reports a failed read behind the price, which is
// then not kept.
func (s *Service) cachedUSD(ctx context.Context, denom string) (p *big.Rat, failed bool) {
	now := s.deps.Clock.Now()
	s.pricesMu.Lock()
	c, ok := s.prices[denom]
	s.pricesMu.Unlock()
	if ok && now.Before(c.expires) {
		return c.price, false
	}
	p, failed = s.usd(ctx, denom)
	if !failed {
		s.pricesMu.Lock()
		s.prices[denom] = cachedPrice{price: p, expires: now.Add(s.cfg.PriceTTL)}
		s.pricesMu.Unlock()
	}
	return p, failed
}

func (s *Service) usd(ctx context.Context, denom string) (*big.Rat, bool) {
	native, usdc := s.cfg.NativeDenom, s.cfg.USDCDenom
	switch {
	case usdc != "" && denom == usdc:
		return big.NewRat(1, 1), false
	case strings.HasPrefix(denom, LPPrefix):
		return s.lpPrice(ctx, denom)
	case denom == native:
		p, failed := s.last(ctx, native, usdc)
		if p.Sign() > 0 {
			return p, failed
		}
		cg, cgFailed := s.coinGecko(ctx)
		return cg, failed || cgFailed
	}
	inUSD, failedUSD := s.last(ctx, denom, usdc)
	inNative, failed := s.last(ctx, denom, native)
	failed = failed || failedUSD
	if inNative.Sign() <= 0 {
		return inUSD, failed
	}
	nativeUSD, failedNative := s.cachedUSD(ctx, native)
	failed = failed || failedNative
	if nativeUSD.Sign() <= 0 {
		return inUSD, failed
	}
	fromNative := new(big.Rat).Mul(inNative, nativeUSD)
	if inUSD.Sign() <= 0 {
		return fromNative, failed
	}
	avg := fromNative.Add(fromNative, inUSD)
	return avg.Quo(avg, big.NewRat(2, 1)), failed
}

// last is base's price in quote: the spot price of their pool when it has
// one, else the last trade of the order book market base/quote (the
// aggregator's "last 24 hours" and "last trade ever" steps, which the newest
// trade answers both); zero when neither has one. failed reports a failed
// read.
func (s *Service) last(ctx context.Context, base, quote string) (*big.Rat, bool) {
	if base == "" || quote == "" {
		return new(big.Rat), false
	}
	p, failed := s.poolPrice(ctx, base, quote)
	if p.Sign() > 0 {
		return p, failed
	}
	trades, err := s.deps.Trades.Trades(ctx, tradehistory.Filter{MarketID: base + "/" + quote}, 1)
	if err != nil {
		s.deps.Log.WithError(err).WithField("market", base+"/"+quote).Warn("tickers: last trade of a USD price")
		return new(big.Rat), true
	}
	if len(trades) == 0 {
		return new(big.Rat), failed
	}
	if p, ok := new(big.Rat).SetString(trades[0].Price); ok {
		return p, failed
	}
	return new(big.Rat), failed
}

// poolPrice is base's display price in quote from the reserves of their
// pool, either way round; zero without the pool, a reserve or an exponent.
// failed reports a failed read.
func (s *Service) poolPrice(ctx context.Context, base, quote string) (*big.Rat, bool) {
	id := base + "_" + quote
	if quote < base {
		id = quote + "_" + base
	}
	p, failed := s.pool(ctx, id)
	if p == nil || p.ReserveBase.Sign() <= 0 || p.ReserveQuote.Sign() <= 0 {
		return new(big.Rat), failed
	}
	exps, err := s.deps.Exponents.Exponents(ctx, []string{p.Base, p.Quote})
	if err != nil {
		s.deps.Log.WithError(err).WithField("pool", id).Warn("tickers: exponents of a pool")
		return new(big.Rat), true
	}
	baseExp, okB := exps[p.Base]
	quoteExp, okQ := exps[p.Quote]
	if !okB || !okQ {
		return new(big.Rat), false
	}
	priced, other := p.ReserveBase, p.ReserveQuote
	pricedExp, otherExp := baseExp, quoteExp
	if base != p.Base {
		priced, other, pricedExp, otherExp = other, priced, otherExp, pricedExp
	}
	// other / priced, in display units.
	out := new(big.Rat).SetFrac(new(big.Int).Mul(other.BigInt(), pow10(max(pricedExp-otherExp, 0))),
		new(big.Int).Mul(priced.BigInt(), pow10(max(otherExp-pricedExp, 0))))
	return out, false
}

// lpPrice is an LP token's price: its pool's reserves at their USD prices,
// per whole LP token; zero when a side has no price or the supply is not
// positive. failed reports a failed read.
func (s *Service) lpPrice(ctx context.Context, denom string) (*big.Rat, bool) {
	id := strings.TrimPrefix(denom, LPPrefix)
	p, failed := s.pool(ctx, id)
	if p == nil {
		return new(big.Rat), failed
	}
	baseUSD, failedB := s.cachedUSD(ctx, p.Base)
	quoteUSD, failedQ := s.cachedUSD(ctx, p.Quote)
	failed = failedB || failedQ
	if baseUSD.Sign() <= 0 || quoteUSD.Sign() <= 0 {
		return new(big.Rat), failed
	}
	exps, err := s.deps.Exponents.Exponents(ctx, []string{p.Base, p.Quote})
	if err != nil {
		s.deps.Log.WithError(err).WithField("pool", id).Warn("tickers: exponents of a pool")
		return new(big.Rat), true
	}
	baseExp, okB := exps[p.Base]
	quoteExp, okQ := exps[p.Quote]
	if !okB || !okQ {
		return new(big.Rat), failed
	}
	supply, err := s.deps.Pools.Supply(ctx, denom)
	if err != nil {
		s.deps.Log.WithError(err).WithField("denom", denom).Warn("tickers: LP supply")
		return new(big.Rat), true
	}
	if supply.Sign() <= 0 {
		return new(big.Rat), failed
	}
	value := new(big.Rat).Mul(baseUSD, new(big.Rat).SetFrac(p.ReserveBase.BigInt(), pow10(baseExp)))
	value.Add(value, new(big.Rat).Mul(quoteUSD, new(big.Rat).SetFrac(p.ReserveQuote.BigInt(), pow10(quoteExp))))
	return value.Quo(value, new(big.Rat).SetFrac(supply.BigInt(), pow10(LPExponent))), failed
}

// pool returns the pool id, nil when there is none; failed when the pools
// cannot be read.
func (s *Service) pool(ctx context.Context, id string) (*Pool, bool) {
	pools, err := s.deps.Pools.Pools(ctx)
	if err != nil {
		return nil, true
	}
	for i := range pools {
		if pools[i].ID == id {
			return &pools[i], false
		}
	}
	return nil, false
}

// coinGecko is CoinGecko's USD price of the native denom's id; zero
// without one. failed reports a failed read.
func (s *Service) coinGecko(ctx context.Context) (*big.Rat, bool) {
	if s.deps.CoinGecko == nil {
		return new(big.Rat), false
	}
	prices, err := s.deps.CoinGecko.USD(ctx)
	if err != nil {
		s.deps.Log.WithError(err).Warn("tickers: CoinGecko price of the native denom")
		return new(big.Rat), true
	}
	if p, ok := new(big.Rat).SetString(prices[s.cfg.NativeCoinGeckoID]); ok {
		return p, false
	}
	return new(big.Rat), false
}
