package poolprice_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/poolprice"
	"github.com/bze-alphateam/bze-scan/backend/internal/tickers"
)

func dec(s string) sdkmath.LegacyDec { return sdkmath.LegacyMustNewDecFromStr(s) }

func TestDriftedBoundaries(t *testing.T) {
	avg := dec("2")
	limit := dec("0.1") // 10%: the band is [1.8, 2.2]
	for name, tc := range map[string]struct {
		spot    string
		drifted bool
	}{
		"equal":              {"2", false},
		"exactly above":      {"2.2", false},
		"exactly below":      {"1.8", false},
		"just above":         {"2.200000000000000001", true},
		"just below":         {"1.799999999999999999", true},
		"far":                {"5", true},
		"inside the band":    {"2.1", false},
		"another side limit": {"0.1", true},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.drifted, poolprice.Drifted(dec(tc.spot), true, avg, limit))
		})
	}
}

func TestDriftedWithoutAPriceOrAnAverage(t *testing.T) {
	assert.True(t, poolprice.Drifted(sdkmath.LegacyDec{}, false, dec("2"), dec("0.1")), "no price")
	assert.True(t, poolprice.Drifted(dec("2"), true, dec("0"), dec("0.1")), "average 0")
	assert.True(t, poolprice.Drifted(dec("2"), true, sdkmath.LegacyDec{}, dec("0.1")), "average nil")
}

func TestSpotMirrorsTheChain(t *testing.T) {
	p, ok := poolprice.Spot(sdkmath.NewInt(3), sdkmath.NewInt(1))
	require.True(t, ok)
	assert.Equal(t, "0.333333333333333333", p.String())
	_, ok = poolprice.Spot(sdkmath.ZeroInt(), sdkmath.NewInt(1))
	assert.False(t, ok)
	_, ok = poolprice.Spot(sdkmath.NewInt(1), sdkmath.ZeroInt())
	assert.False(t, ok)
	_, ok = poolprice.Spot(sdkmath.Int{}, sdkmath.NewInt(1))
	assert.False(t, ok)
}

func TestDriftPct(t *testing.T) {
	pct, ok := poolprice.DriftPct(dec("2"), true, dec("1.8"))
	require.True(t, ok)
	assert.Equal(t, "11.1", pct)
	pct, _ = poolprice.DriftPct(dec("2"), true, dec("2"))
	assert.Equal(t, "0.0", pct)
	pct, _ = poolprice.DriftPct(dec("1.8"), true, dec("2"))
	assert.Equal(t, "10.0", pct)
	_, ok = poolprice.DriftPct(dec("2"), true, dec("0"))
	assert.False(t, ok, "average 0")
	_, ok = poolprice.DriftPct(sdkmath.LegacyDec{}, false, dec("2"))
	assert.False(t, ok, "no price")
}

type fakeNode struct {
	res   *tradebintypes.QueryPoolPriceAverageResponse
	err   error
	calls int
}

func (f *fakeNode) PoolPriceAverage(_ context.Context, _ *tradebintypes.QueryPoolPriceAverageRequest,
	_ ...grpc.CallOption) (*tradebintypes.QueryPoolPriceAverageResponse, error) {
	f.calls++
	return f.res, f.err
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func TestCachedReadsOncePerTTLAndServesTheLastOnFailure(t *testing.T) {
	node := &fakeNode{res: &tradebintypes.QueryPoolPriceAverageResponse{AvgPrice: dec("1.5"), LastHeight: 7}}
	clk := &clock{now: time.Unix(1000, 0)}
	c := poolprice.NewCached(node, 10*time.Second, clk)
	for range 3 {
		avg, err := c.Average(context.Background(), "p")
		require.NoError(t, err)
		assert.Equal(t, "1.500000000000000000", avg.AvgPrice.String())
		assert.EqualValues(t, 7, avg.LastHeight)
	}
	assert.Equal(t, 1, node.calls)
	clk.now = clk.now.Add(11 * time.Second)
	node.res, node.err = nil, status.Error(codes.Unavailable, "down")
	avg, err := c.Average(context.Background(), "p")
	require.NoError(t, err, "the last good read is served")
	assert.EqualValues(t, 7, avg.LastHeight)
	_, err = c.Average(context.Background(), "other")
	assert.Error(t, err, "nothing held for another pool")
}

func TestCachedNoAverageForNotFoundAndUnimplemented(t *testing.T) {
	for _, code := range []codes.Code{codes.NotFound, codes.Unimplemented} {
		node := &fakeNode{err: status.Error(code, "x")}
		avg, err := poolprice.NewCached(node, 0, nil).Average(context.Background(), "p")
		require.NoError(t, err, code.String())
		assert.Nil(t, avg, code.String())
	}
}

type fakePools struct {
	pools []tickers.Pool
	err   error
}

func (f fakePools) Pools(context.Context) ([]tickers.Pool, error) { return f.pools, f.err }

type fakeAverages struct {
	avg *poolprice.Average
	err error
}

func (f fakeAverages) Average(context.Context, string) (*poolprice.Average, error) {
	return f.avg, f.err
}

type fakeParams struct {
	doc map[string]json.RawMessage
	err error
}

func (f fakeParams) Params(context.Context) (map[string]json.RawMessage, error) { return f.doc, f.err }

type fakeMetas map[string]poolprice.Meta

func (f fakeMetas) Metas(context.Context, []string) (map[string]poolprice.Meta, error) { return f, nil }

const (
	tradebinParams = `{"poolPriceHalfLifeBlocks":"20","feeMaxPriceDrift":"0.100000000000000000","moduleMaxPriceDrift":"0.030000000000000000"}`
	lp             = "ulp_x_ubze"
)

func service(pool tickers.Pool, avg *poolprice.Average, avgErr error) *poolprice.Service {
	pool.LPDenom = lp
	return poolprice.New(poolprice.Deps{
		Pools:    fakePools{pools: []tickers.Pool{pool}},
		Averages: fakeAverages{avg: avg, err: avgErr},
		Params:   fakeParams{doc: map[string]json.RawMessage{"tradebin": json.RawMessage(tradebinParams)}},
		Metas:    fakeMetas{"x": {Symbol: "X", Exponent: 6}, "ubze": {Symbol: "BZE", Exponent: 6}},
	}, "ubze")
}

func nativePool(base, quote int64) tickers.Pool {
	return tickers.Pool{ID: "x_ubze", Base: "x", Quote: "ubze", ReserveBase: sdkmath.NewInt(base), ReserveQuote: sdkmath.NewInt(quote)}
}

func flags(t *testing.T, p *poolprice.Pool) (bool, bool) {
	t.Helper()
	require.NotNil(t, p.FeesInBZE)
	require.NotNil(t, p.ModuleSwapsHeld)
	return *p.FeesInBZE, *p.ModuleSwapsHeld
}

func TestPoolFlagsAtTheBoundaries(t *testing.T) {
	// Average 1: the spot is the quote reserve over the base reserve, so the
	// reserves put it at exact distances from the average.
	for name, tc := range map[string]struct {
		base, quote int64
		avg         string
		fees, held  bool
	}{
		"on the average":      {100, 100, "1", false, false},
		"exactly 3%":          {100, 103, "1", false, false},
		"just past 3%":        {10000, 10301, "1", false, true},
		"exactly 10%":         {100, 110, "1", false, true},
		"just past 10%":       {10000, 11001, "1", true, true},
		"exactly 3% below":    {100, 97, "1", false, false},
		"just past 3% below":  {10000, 9699, "1", false, true},
		"exactly 10% below":   {100, 90, "1", false, true},
		"just past 10% below": {10000, 8999, "1", true, true},
	} {
		t.Run(name, func(t *testing.T) {
			s := service(nativePool(tc.base, tc.quote), &poolprice.Average{AvgPrice: dec(tc.avg), LastHeight: 9}, nil)
			p, err := s.Pool(context.Background(), lp)
			require.NoError(t, err)
			fees, held := flags(t, p)
			assert.Equal(t, tc.fees, fees, "fees in BZE")
			assert.Equal(t, tc.held, held, "module swaps held")
		})
	}
}

func TestPoolFields(t *testing.T) {
	s := service(nativePool(1000, 2000), &poolprice.Average{AvgPrice: dec("1.8"), LastHeight: 55}, nil)
	p, err := s.Pool(context.Background(), lp)
	require.NoError(t, err)
	assert.Equal(t, "x_ubze", p.PoolID)
	assert.Equal(t, "1000", p.Reserves.Base.Reserve)
	require.NotNil(t, p.Reserves.Base.Symbol)
	assert.Equal(t, "X", *p.Reserves.Base.Symbol)
	assert.Equal(t, "2.000000000000000000", p.Price.Chain)
	require.NotNil(t, p.Price.Display)
	assert.Equal(t, "2", *p.Price.Display, "same exponents")
	assert.Equal(t, "1.800000000000000000", p.AvgPrice.Chain)
	assert.EqualValues(t, 55, *p.AvgLastHeight)
	assert.Equal(t, "11.1", *p.DriftPct)
	assert.EqualValues(t, 20, *p.HalfLifeBlocks)
	assert.Equal(t, "10", *p.FeeMaxDriftPct)
	assert.Equal(t, "3", *p.ModuleMaxDrift)
	fees, held := flags(t, p)
	assert.True(t, fees)
	assert.True(t, held)
}

func TestPoolNonNativeHasNullFlags(t *testing.T) {
	pool := tickers.Pool{ID: "x_y", Base: "x", Quote: "y", ReserveBase: sdkmath.NewInt(100), ReserveQuote: sdkmath.NewInt(200)}
	p, err := service(pool, &poolprice.Average{AvgPrice: dec("1"), LastHeight: 1}, nil).Pool(context.Background(), lp)
	require.NoError(t, err)
	assert.Nil(t, p.FeesInBZE)
	assert.Nil(t, p.ModuleSwapsHeld)
	require.NotNil(t, p.DriftPct, "the drift itself is still shown")
	assert.Equal(t, "100.0", *p.DriftPct)
}

func TestPoolAverageZeroCountsAsDrifted(t *testing.T) {
	p, err := service(nativePool(100, 100), &poolprice.Average{AvgPrice: dec("0"), LastHeight: 1}, nil).Pool(context.Background(), lp)
	require.NoError(t, err)
	assert.Nil(t, p.DriftPct)
	fees, held := flags(t, p)
	assert.True(t, fees)
	assert.True(t, held)
}

func TestPoolWithoutAverageHasNullAverageAndFlags(t *testing.T) {
	p, err := service(nativePool(100, 100), nil, nil).Pool(context.Background(), lp)
	require.NoError(t, err)
	assert.Nil(t, p.AvgPrice)
	assert.Nil(t, p.AvgLastHeight)
	assert.Nil(t, p.DriftPct)
	assert.Nil(t, p.FeesInBZE)
	assert.Nil(t, p.ModuleSwapsHeld)
	require.NotNil(t, p.Price)
}

func TestPoolWithoutAPriceIsDrifted(t *testing.T) {
	p, err := service(nativePool(0, 0), &poolprice.Average{AvgPrice: dec("1"), LastHeight: 1}, nil).Pool(context.Background(), lp)
	require.NoError(t, err)
	assert.Nil(t, p.Price)
	assert.Nil(t, p.DriftPct)
	fees, held := flags(t, p)
	assert.True(t, fees)
	assert.True(t, held)
}

func TestPoolDisplayPriceUsesTheExponents(t *testing.T) {
	s := poolprice.New(poolprice.Deps{
		Pools:    fakePools{pools: []tickers.Pool{{ID: "x_ubze", Base: "x", Quote: "ubze", LPDenom: lp, ReserveBase: sdkmath.NewInt(1_000_000), ReserveQuote: sdkmath.NewInt(2_000_000_000)}}},
		Averages: fakeAverages{avg: &poolprice.Average{AvgPrice: dec("2000"), LastHeight: 1}},
		Params:   fakeParams{doc: map[string]json.RawMessage{"tradebin": json.RawMessage(tradebinParams)}},
		Metas:    fakeMetas{"x": {Symbol: "X", Exponent: 6}, "ubze": {Symbol: "BZE", Exponent: 9}},
	}, "ubze")
	p, err := s.Pool(context.Background(), lp)
	require.NoError(t, err)
	assert.Equal(t, "2000.000000000000000000", p.Price.Chain)
	require.NotNil(t, p.Price.Display)
	assert.Equal(t, "2", *p.Price.Display, "2000 × 10^(6−9)")
}

func TestPoolParamsMissingNullsTheFlagsOnly(t *testing.T) {
	s2 := poolprice.New(poolprice.Deps{
		Pools:    fakePools{pools: []tickers.Pool{{ID: "x_ubze", Base: "x", Quote: "ubze", LPDenom: lp, ReserveBase: sdkmath.NewInt(100), ReserveQuote: sdkmath.NewInt(150)}}},
		Averages: fakeAverages{avg: &poolprice.Average{AvgPrice: dec("1"), LastHeight: 1}},
		Params:   fakeParams{err: errors.New("down")},
		Metas:    fakeMetas{},
	}, "ubze")
	p, err := s2.Pool(context.Background(), lp)
	require.NoError(t, err)
	require.NotNil(t, p.AvgPrice)
	assert.Nil(t, p.FeesInBZE)
	assert.Nil(t, p.ModuleSwapsHeld)
	assert.Nil(t, p.Reserves.Base.Symbol, "no metadata")
}

func TestPoolUnknownLPAndErrors(t *testing.T) {
	p, err := service(nativePool(1, 1), nil, nil).Pool(context.Background(), "ulp_other")
	require.NoError(t, err)
	assert.Nil(t, p)
	_, err = service(nativePool(1, 1), nil, errors.New("down")).Pool(context.Background(), lp)
	assert.Error(t, err)
}
