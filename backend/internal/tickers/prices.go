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
// for the denom and never finds it).
func (s *Service) USD(ctx context.Context, denom string) *big.Rat {
	now := s.deps.Clock.Now()
	s.pricesMu.Lock()
	c, ok := s.prices[denom]
	s.pricesMu.Unlock()
	if ok && now.Before(c.expires) {
		return c.price
	}
	p := s.usd(ctx, denom)
	s.pricesMu.Lock()
	s.prices[denom] = cachedPrice{price: p, expires: now.Add(s.cfg.PriceTTL)}
	s.pricesMu.Unlock()
	return p
}

func (s *Service) usd(ctx context.Context, denom string) *big.Rat {
	native, usdc := s.cfg.NativeDenom, s.cfg.USDCDenom
	switch {
	case usdc != "" && denom == usdc:
		return big.NewRat(1, 1)
	case strings.HasPrefix(denom, LPPrefix):
		return s.lpPrice(ctx, denom)
	case denom == native:
		if p := s.last(ctx, native, usdc); p.Sign() > 0 {
			return p
		}
		return s.coinGecko(ctx)
	}
	inUSD := s.last(ctx, denom, usdc)
	inNative := s.last(ctx, denom, native)
	if inNative.Sign() <= 0 {
		return inUSD
	}
	nativeUSD := s.USD(ctx, native)
	if nativeUSD.Sign() <= 0 {
		return inUSD
	}
	fromNative := new(big.Rat).Mul(inNative, nativeUSD)
	if inUSD.Sign() <= 0 {
		return fromNative
	}
	avg := fromNative.Add(fromNative, inUSD)
	return avg.Quo(avg, big.NewRat(2, 1))
}

// last is base's price in quote: the spot price of their pool when it has
// one, else the last trade of the order book market base/quote (the
// aggregator's "last 24 hours" and "last trade ever" steps, which the newest
// trade answers both); zero when neither has one.
func (s *Service) last(ctx context.Context, base, quote string) *big.Rat {
	if base == "" || quote == "" {
		return new(big.Rat)
	}
	if p := s.poolPrice(ctx, base, quote); p.Sign() > 0 {
		return p
	}
	trades, err := s.deps.Trades.Trades(ctx, tradehistory.Filter{MarketID: base + "/" + quote}, 1)
	if err != nil {
		s.deps.Log.WithError(err).WithField("market", base+"/"+quote).Warn("tickers: last trade of a USD price")
		return new(big.Rat)
	}
	if len(trades) == 0 {
		return new(big.Rat)
	}
	p, ok := new(big.Rat).SetString(trades[0].Price)
	if !ok {
		return new(big.Rat)
	}
	return p
}

// poolPrice is base's display price in quote from the reserves of their
// pool, either way round; zero without the pool, a reserve or an exponent.
func (s *Service) poolPrice(ctx context.Context, base, quote string) *big.Rat {
	id := base + "_" + quote
	if quote < base {
		id = quote + "_" + base
	}
	p := s.pool(ctx, id)
	if p == nil || p.ReserveBase.Sign() <= 0 || p.ReserveQuote.Sign() <= 0 {
		return new(big.Rat)
	}
	exps, err := s.deps.Exponents.Exponents(ctx, []string{p.Base, p.Quote})
	if err != nil {
		s.deps.Log.WithError(err).WithField("pool", id).Warn("tickers: exponents of a pool")
		return new(big.Rat)
	}
	baseExp, okB := exps[p.Base]
	quoteExp, okQ := exps[p.Quote]
	if !okB || !okQ {
		return new(big.Rat)
	}
	priced, other := p.ReserveBase, p.ReserveQuote
	pricedExp, otherExp := baseExp, quoteExp
	if base != p.Base {
		priced, other, pricedExp, otherExp = other, priced, otherExp, pricedExp
	}
	// other / priced, in display units.
	out := new(big.Rat).SetFrac(new(big.Int).Mul(other.BigInt(), pow10(max(pricedExp-otherExp, 0))),
		new(big.Int).Mul(priced.BigInt(), pow10(max(otherExp-pricedExp, 0))))
	return out
}

// lpPrice is an LP token's price: its pool's reserves at their USD prices,
// per whole LP token; zero when a side has no price or the supply is not
// positive.
func (s *Service) lpPrice(ctx context.Context, denom string) *big.Rat {
	id := strings.TrimPrefix(denom, LPPrefix)
	p := s.pool(ctx, id)
	if p == nil {
		return new(big.Rat)
	}
	baseUSD := s.USD(ctx, p.Base)
	quoteUSD := s.USD(ctx, p.Quote)
	if baseUSD.Sign() <= 0 || quoteUSD.Sign() <= 0 {
		return new(big.Rat)
	}
	exps, err := s.deps.Exponents.Exponents(ctx, []string{p.Base, p.Quote})
	if err != nil {
		s.deps.Log.WithError(err).WithField("pool", id).Warn("tickers: exponents of a pool")
		return new(big.Rat)
	}
	baseExp, okB := exps[p.Base]
	quoteExp, okQ := exps[p.Quote]
	if !okB || !okQ {
		return new(big.Rat)
	}
	supply, err := s.deps.Pools.Supply(ctx, denom)
	if err != nil {
		s.deps.Log.WithError(err).WithField("denom", denom).Warn("tickers: LP supply")
		return new(big.Rat)
	}
	if supply.Sign() <= 0 {
		return new(big.Rat)
	}
	value := new(big.Rat).Mul(baseUSD, new(big.Rat).SetFrac(p.ReserveBase.BigInt(), pow10(baseExp)))
	value.Add(value, new(big.Rat).Mul(quoteUSD, new(big.Rat).SetFrac(p.ReserveQuote.BigInt(), pow10(quoteExp))))
	return value.Quo(value, new(big.Rat).SetFrac(supply.BigInt(), pow10(LPExponent)))
}

// pool returns the pool id, nil when there is none or the pools cannot be
// read.
func (s *Service) pool(ctx context.Context, id string) *Pool {
	pools, err := s.deps.Pools.Pools(ctx)
	if err != nil {
		return nil
	}
	for i := range pools {
		if pools[i].ID == id {
			return &pools[i]
		}
	}
	return nil
}

// coinGecko is CoinGecko's USD price of the native denom's id; zero
// without one.
func (s *Service) coinGecko(ctx context.Context) *big.Rat {
	if s.deps.CoinGecko == nil {
		return new(big.Rat)
	}
	prices, err := s.deps.CoinGecko.USD(ctx)
	if err != nil {
		s.deps.Log.WithError(err).Warn("tickers: CoinGecko price of the native denom")
		return new(big.Rat)
	}
	p, ok := new(big.Rat).SetString(prices[s.cfg.NativeCoinGeckoID])
	if !ok {
		return new(big.Rat)
	}
	return p
}
