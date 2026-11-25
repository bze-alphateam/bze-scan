package poolprice_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/poolprice"
	"github.com/bze-alphateam/bze-scan/backend/internal/tickers"
)

func dec(s string) sdkmath.LegacyDec { return sdkmath.LegacyMustNewDecFromStr(s) }

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

func bigPool(base, quote string) tickers.Pool {
	b, _ := sdkmath.NewIntFromString(base)
	q, _ := sdkmath.NewIntFromString(quote)
	return tickers.Pool{ID: "x_ubze", Base: "x", Quote: "ubze", ReserveBase: b, ReserveQuote: q}
}

// The chain compares in LegacyDec (18 decimals): one unit of the last decimal
// decides, which the reserves of 10^18 reach.
func TestPoolFlagsAtTheLastDecimal(t *testing.T) {
	for name, tc := range map[string]struct {
		base, quote string
		avg         string
		fees, held  bool
	}{
		"exactly 10%":             {"1000000000000000000", "1100000000000000000", "1", false, true},
		"10% plus 1e-18":          {"1000000000000000000", "1100000000000000001", "1", true, true},
		"10% minus 1e-18":         {"1000000000000000000", "1099999999999999999", "1", false, true},
		"exactly 3%":              {"1000000000000000000", "1030000000000000000", "1", false, false},
		"3% plus 1e-18":           {"1000000000000000000", "1030000000000000001", "1", false, true},
		"3% minus 1e-18":          {"1000000000000000000", "1029999999999999999", "1", false, false},
		"below by 3% + 1e-18":     {"1000000000000000000", "969999999999999999", "1", false, true},
		"below by exactly 3%":     {"1000000000000000000", "970000000000000000", "1", false, false},
		"average 1/3, on 3%":      {"1000000000000000000", "343333333333333333", "0.333333333333333333", false, false},
		"average 1/3, 3% + 1e-18": {"1000000000000000000", "343333333333333334", "0.333333333333333333", false, true},
	} {
		t.Run(name, func(t *testing.T) {
			s := service(bigPool(tc.base, tc.quote), &poolprice.Average{AvgPrice: dec(tc.avg), LastHeight: 9}, nil)
			p, err := s.Pool(context.Background(), lp)
			require.NoError(t, err)
			fees, held := flags(t, p)
			assert.Equal(t, tc.fees, fees, "fees in BZE")
			assert.Equal(t, tc.held, held, "module swaps held")
		})
	}
}

func TestPoolDriftPctRoundsHalfUpToOneDecimal(t *testing.T) {
	for name, tc := range map[string]struct {
		base, quote string
		avg, want   string
	}{
		"0.05% rounds up":    {"10000", "10005", "1", "0.1"},
		"0.04% rounds down":  {"10000", "10004", "1", "0.0"},
		"below the average":  {"10000", "9995", "1", "0.1"},
		"11.11%":             {"1000", "2000", "1.8", "11.1"},
		"a third as average": {"1000000", "400000", "0.333333333333333333", "20.0"},
		"on the average":     {"100", "100", "1", "0.0"},
	} {
		t.Run(name, func(t *testing.T) {
			base, _ := sdkmath.NewIntFromString(tc.base)
			quote, _ := sdkmath.NewIntFromString(tc.quote)
			pool := tickers.Pool{ID: "x_ubze", Base: "x", Quote: "ubze", ReserveBase: base, ReserveQuote: quote}
			p, err := service(pool, &poolprice.Average{AvgPrice: dec(tc.avg), LastHeight: 1}, nil).Pool(context.Background(), lp)
			require.NoError(t, err)
			require.NotNil(t, p.DriftPct)
			assert.Equal(t, tc.want, *p.DriftPct)
		})
	}
}

type blockingNode struct {
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (b *blockingNode) PoolPriceAverage(context.Context, *tradebintypes.QueryPoolPriceAverageRequest,
	...grpc.CallOption) (*tradebintypes.QueryPoolPriceAverageResponse, error) {
	if b.calls.Add(1) == 1 {
		close(b.entered)
	}
	<-b.release
	return &tradebintypes.QueryPoolPriceAverageResponse{AvgPrice: dec("1.5"), LastHeight: 3}, nil
}

func TestCachedConcurrentCallersShareOneFlight(t *testing.T) {
	node := &blockingNode{entered: make(chan struct{}), release: make(chan struct{})}
	c := poolprice.NewCached(node, 0, nil)
	const callers = 8
	var wg sync.WaitGroup
	results := make([]*poolprice.Average, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			avg, err := c.Average(context.Background(), "p")
			assert.NoError(t, err)
			results[i] = avg
		}()
	}
	<-node.entered
	time.Sleep(100 * time.Millisecond) // the other callers join the flight
	close(node.release)
	wg.Wait()
	assert.EqualValues(t, 1, node.calls.Load(), "one call on the node for every caller")
	for _, r := range results {
		require.NotNil(t, r)
		assert.EqualValues(t, 3, r.LastHeight)
	}
}

type failingMetas struct{}

func (failingMetas) Metas(context.Context, []string) (map[string]poolprice.Meta, error) {
	return nil, errors.New("db down")
}

func TestPoolWithoutMetadataIsStillServedAndTheFailureLogged(t *testing.T) {
	logger, hook := logtest.NewNullLogger()
	s := poolprice.New(poolprice.Deps{
		Pools:    fakePools{pools: []tickers.Pool{func() tickers.Pool { p := nativePool(1000, 2000); p.LPDenom = lp; return p }()}},
		Averages: fakeAverages{avg: &poolprice.Average{AvgPrice: dec("1.8"), LastHeight: 1}},
		Params:   fakeParams{doc: map[string]json.RawMessage{"tradebin": json.RawMessage(tradebinParams)}},
		Metas:    failingMetas{},
		Log:      logger,
	}, "ubze")
	p, err := s.Pool(context.Background(), lp)
	require.NoError(t, err)
	assert.Nil(t, p.Reserves.Base.Symbol)
	assert.Nil(t, p.Price.Display, "no exponents: chain units only")
	require.Len(t, hook.AllEntries(), 1)
	assert.Equal(t, logrus.WarnLevel, hook.LastEntry().Level)
}
