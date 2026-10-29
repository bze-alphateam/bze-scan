// Package swaphistory serves an address's liquidity-pool swaps in display
// units, for the aggregator-compatible GET /aggregator/api/dex/swaps: the
// last Limit swaps the address made, newest first, read from explorer.swaps.
package swaphistory

import (
	"context"
	"fmt"

	sdkmath "cosmossdk.io/math"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/internal/dexunits"
)

// Limit is how many swaps an address's history holds, the aggregator's.
const Limit = 100

// MaxPages bounds the store reads of one history: swaps left out are
// replaced by older ones, from at most MaxPages × Limit rows.
const MaxPages = 10

// Swap is a stored swap in chain units.
type Swap struct {
	TradeID int64
	// TimeMs is the block time in unix milliseconds.
	TimeMs      int64
	PoolID      string
	Base        string
	Quote       string
	Side        string
	BaseAmount  string
	QuoteAmount string
	Creator     string
}

// Store reads the stored swaps; PGStore implements it.
type Store interface {
	// SwapsByCreator returns the last limit swaps creator made with a trade
	// id below before (no bound when 0), newest first.
	SwapsByCreator(ctx context.Context, creator string, before int64, limit int) ([]Swap, error)
}

// Trade is a swap in display units: Price is quote per base, the volumes
// are the base and quote sides.
type Trade struct {
	Swap
	Price       string
	BaseVolume  string
	QuoteVolume string
}

// Deps of the service; Log defaults to the standard logger.
type Deps struct {
	Store     Store
	Exponents dexunits.Exponents
	Log       log.FieldLogger
}

// Service converts the stored swaps to display units.
type Service struct {
	deps Deps
}

// New returns a service over deps.
func New(deps Deps) *Service {
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	return &Service{deps: deps}
}

// ByAddress returns address's last Limit swaps, newest first, in display
// units. A swap that cannot be converted (a denom without a known exponent,
// no base amount) is left out, as the aggregator never stores it, and the
// next older ones take its place: the store is read a page of Limit at a
// time, at most MaxPages pages.
func (s *Service) ByAddress(ctx context.Context, address string) ([]Trade, error) {
	out := make([]Trade, 0, Limit)
	exps := map[string]int{}
	known := map[string]bool{}
	before := int64(0)
	for range MaxPages {
		swaps, err := s.deps.Store.SwapsByCreator(ctx, address, before, Limit)
		if err != nil {
			return nil, err
		}
		var denoms []string
		for _, sw := range swaps {
			for _, d := range []string{sw.Base, sw.Quote} {
				if !known[d] {
					known[d] = true
					denoms = append(denoms, d)
				}
			}
		}
		if len(denoms) > 0 {
			got, err := s.deps.Exponents.Exponents(ctx, denoms)
			if err != nil {
				return nil, err
			}
			for d, e := range got {
				exps[d] = e
			}
		}
		for _, sw := range swaps {
			t, err := convert(sw, exps)
			if err != nil {
				s.deps.Log.WithError(err).WithField("trade_id", sw.TradeID).Warn("dex swaps: swap left out")
				continue
			}
			if out = append(out, t); len(out) == Limit {
				return out, nil
			}
		}
		if len(swaps) < Limit {
			break
		}
		before = swaps[len(swaps)-1].TradeID
	}
	return out, nil
}

// convert computes the display values the aggregator stores for a swap: the
// chain price is quote / base as an 18-decimal LegacyDec, then
// dexunits.TradePrice.
func convert(sw Swap, exps map[string]int) (t Trade, err error) {
	baseExp, okBase := exps[sw.Base]
	quoteExp, okQuote := exps[sw.Quote]
	if !okBase || !okQuote {
		return Trade{}, fmt.Errorf("exponents of %s and %s not both known", sw.Base, sw.Quote)
	}
	base, okB := sdkmath.NewIntFromString(sw.BaseAmount)
	quote, okQ := sdkmath.NewIntFromString(sw.QuoteAmount)
	if !okB || !okQ || base.IsZero() {
		return Trade{}, fmt.Errorf("amounts %q / %q do not make a price", sw.QuoteAmount, sw.BaseAmount)
	}
	// LegacyDec panics past its 256 bits; such an amount has no price.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("price of %s / %s: %v", sw.QuoteAmount, sw.BaseAmount, r)
		}
	}()
	chainPrice := sdkmath.LegacyNewDecFromInt(quote).Quo(sdkmath.LegacyNewDecFromInt(base))
	t = Trade{Swap: sw}
	if t.Price, err = dexunits.TradePrice(chainPrice.String(), baseExp, quoteExp); err != nil {
		return Trade{}, err
	}
	if t.BaseVolume, err = dexunits.Amount(sw.BaseAmount, baseExp); err != nil {
		return Trade{}, err
	}
	if t.QuoteVolume, err = dexunits.Amount(sw.QuoteAmount, quoteExp); err != nil {
		return Trade{}, err
	}
	return t, nil
}
