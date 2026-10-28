package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/labstack/echo/v5"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/server"
	"github.com/bze-alphateam/bze-scan/backend/internal/swaphistory"
)

const (
	swapAddr = "bze1n4qjms7gdam0k8f06eadgn3ecggvwy042casm5"
	swapAtom = "ibc/27394FB092D2ECCD56123C74F36E4C1F926001CEADA9CA97EA622B25F41E5EB2"
	swapEth  = "ibc/EA1D43981D5C9A1C4AAEA9C23BB1D4FA126BA9BC7020A25E0AE4AA841EA25DC5"
)

// swapStore answers SwapsByCreator from swaps (newest first) as the
// database pages them and records what it was asked.
type swapStore struct {
	swaps  []swaphistory.Swap
	fail   bool
	asked  []string
	limit  int
	before []int64
}

func (s *swapStore) SwapsByCreator(_ context.Context, creator string, before int64, limit int) ([]swaphistory.Swap, error) {
	s.asked, s.limit, s.before = append(s.asked, creator), limit, append(s.before, before)
	if s.fail {
		return nil, errors.New("database is down")
	}
	var out []swaphistory.Swap
	for _, sw := range s.swaps {
		if (before == 0 || sw.TradeID < before) && len(out) < limit {
			out = append(out, sw)
		}
	}
	return out, nil
}

type swapExponents map[string]int

func (e swapExponents) Exponents(_ context.Context, denoms []string) (map[string]int, error) {
	out := map[string]int{}
	for _, d := range denoms {
		if exp, ok := e[d]; ok {
			out[d] = exp
		}
	}
	return out, nil
}

func swapServer(store *swapStore, exps swapExponents) *echo.Echo {
	lg, _ := logtest.NewNullLogger()
	return server.New(server.Deps{CompatSwaps: swaphistory.New(swaphistory.Deps{Store: store, Exponents: exps, Log: lg})})
}

func swapRow(id int64, base, quote, side, baseAmount, quoteAmount string) swaphistory.Swap {
	return swaphistory.Swap{TradeID: id, TimeMs: 1777391551683, PoolID: base + "_" + quote, Base: base, Quote: quote,
		Side: side, BaseAmount: baseAmount, QuoteAmount: quoteAmount, Creator: swapAddr}
}

// The aggregator's shape byte for byte: the trade id as a number, display
// units as strings, executed_at in unix milliseconds as a string, an empty
// maker, the store's order kept.
func TestCompatSwapsAnswerTheAggregatorsShape(t *testing.T) {
	store := &swapStore{swaps: []swaphistory.Swap{
		swapRow(22551811000002, swapAtom, "ubze", "buy", "40", "1000"),
		swapRow(22551811000001, swapAtom, "ubze", "sell", "3", "1"),
	}}
	e := swapServer(store, swapExponents{swapAtom: 6, "ubze": 6})
	rec := serve(t, e, http.MethodGet, "/aggregator/api/dex/swaps?address="+swapAddr)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, `[{"order_id":22551811000002,"price":"25","base_volume":"0.00004","quote_volume":"0.001",`+
		`"executed_at":"1777391551683","order_type":"buy","maker":"","taker":"`+swapAddr+`",`+
		`"pool_id":"`+swapAtom+`_ubze","base":"`+swapAtom+`","quote":"ubze"},`+
		`{"order_id":22551811000001,"price":"0.333333333333333333","base_volume":"0.000003","quote_volume":"0.000001",`+
		`"executed_at":"1777391551683","order_type":"sell","maker":"","taker":"`+swapAddr+`",`+
		`"pool_id":"`+swapAtom+`_ubze","base":"`+swapAtom+`","quote":"ubze"}]`+"\n", rec.Body.String(),
		"the price is quote / base as the chain's 18-decimal division rounds it")
	assert.Equal(t, echo.MIMEApplicationJSON, rec.Header().Get(echo.HeaderContentType))
	assert.Empty(t, rec.Header().Get(echo.HeaderCacheControl), "the aggregator sends none")
	assert.Equal(t, []string{swapAddr}, store.asked)
	assert.Equal(t, 100, store.limit, "the aggregator's last 100")

	rec = corsRequest(e, http.MethodGet, "/aggregator/api/dex/swaps?address="+swapAddr, "https://dex.getbze.com")
	assert.Equal(t, "*", rec.Header().Get(echo.HeaderAccessControlAllowOrigin))
}

func TestCompatSwapsWithoutSwapsAnswerAnEmptyList(t *testing.T) {
	e := swapServer(&swapStore{}, swapExponents{})
	rec := serve(t, e, http.MethodGet, "/aggregator/api/dex/swaps?address="+swapAddr)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]\n", rec.Body.String())
}

// Allowed difference: the aggregator never validates the address and, without
// one, answers everyone's last 100 swaps.
func TestCompatSwapsNeedAnAddress(t *testing.T) {
	store := &swapStore{swaps: []swaphistory.Swap{swapRow(1, swapAtom, "ubze", "buy", "1", "1")}}
	e := swapServer(store, swapExponents{swapAtom: 6, "ubze": 6})
	for _, path := range []string{"/aggregator/api/dex/swaps", "/aggregator/api/dex/swaps?address="} {
		rec := serve(t, e, http.MethodGet, path)
		assert.Equal(t, http.StatusBadRequest, rec.Code, path)
		assert.Equal(t, `{"message":"invalid request"}`+"\n", rec.Body.String(), path)
	}
	assert.Empty(t, store.asked)
}

func TestCompatSwapsStoreErrorIsTheAggregators500(t *testing.T) {
	e := swapServer(&swapStore{fail: true}, swapExponents{})
	rec := serve(t, e, http.MethodGet, "/aggregator/api/dex/swaps?address="+swapAddr)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, `{"message":"failed to get swap history"}`+"\n", rec.Body.String())
}

// A swap whose denoms have no known exponent is left out, as the aggregator
// never stores one.
func TestCompatSwapsLeaveOutWhatCannotBeConverted(t *testing.T) {
	e := swapServer(&swapStore{swaps: []swaphistory.Swap{
		swapRow(3, "factory/bze1x/unknown", "ubze", "buy", "1", "1"),
		swapRow(2, swapAtom, "ubze", "sell", "0", "1"),
		swapRow(1, swapAtom, "ubze", "sell", "2", "1"),
	}}, swapExponents{swapAtom: 6, "ubze": 6})
	rec := serve(t, e, http.MethodGet, "/aggregator/api/dex/swaps?address="+swapAddr)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"order_id":1,"price":"0.5"`)
	assert.NotContains(t, rec.Body.String(), `"order_id":2`, "no price without a base amount")
	assert.NotContains(t, rec.Body.String(), `"order_id":3`)
}

// Exponents shift the price: up when the base has more decimals, down when
// the quote has. Allowed difference (BZE-133): the aggregator's %.2f power
// of ten is 0 once the quote has three or more decimals more, Scan's price
// is exact.
func TestCompatSwapsPriceInDisplayUnits(t *testing.T) {
	for _, tc := range []struct {
		name               string
		baseExp, quoteExp  int
		baseAmt, quoteAmt  string
		price, base, quote string
	}{
		{"base has two more decimals", 8, 6, "100000000", "5000000", "5", "1", "5"},
		// 10^8 / (3×10^6) = 33.333333333333333333, times 0.01 rounded back to
		// 18 decimals as the aggregator's LegacyDec does (an exact shift has
		// two more 3s).
		{"quote has two more decimals", 6, 8, "3000000", "100000000", "0.333333333333333333", "3", "1"},
		{"quote has one more decimal", 6, 7, "3000000", "20000000", "0.666666666666666667", "3", "2"},
		{"quote has twelve more decimals", 6, 18, "2000000", "3000000000000000000", "1.5", "2", "3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := swapServer(&swapStore{swaps: []swaphistory.Swap{swapRow(7, swapAtom, swapEth, "buy", tc.baseAmt, tc.quoteAmt)}},
				swapExponents{swapAtom: tc.baseExp, swapEth: tc.quoteExp})
			rec := serve(t, e, http.MethodGet, "/aggregator/api/dex/swaps?address="+swapAddr)
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Body.String(),
				`"price":"`+tc.price+`","base_volume":"`+tc.base+`","quote_volume":"`+tc.quote+`"`)
		})
	}
}

func TestCompatSwapsNeedAService(t *testing.T) {
	rec := serve(t, server.New(server.Deps{}), http.MethodGet, "/aggregator/api/dex/swaps?address="+swapAddr)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// Swaps left out are replaced by older ones, as the aggregator, which never
// stored them, still answers 100: the store is paged by trade id.
func TestCompatSwapsAnswer100WhenSomeAreLeftOut(t *testing.T) {
	var swaps []swaphistory.Swap
	for id := int64(250); id > 0; id-- {
		base := swapAtom
		if id > 200 && id%2 == 0 {
			base = "factory/bze1x/unknown" // 25 of the newest 50 cannot be converted
		}
		swaps = append(swaps, swapRow(id, base, "ubze", "buy", "1", "1"))
	}
	store := &swapStore{swaps: swaps}
	e := swapServer(store, swapExponents{swapAtom: 6, "ubze": 6})
	rec := serve(t, e, http.MethodGet, "/aggregator/api/dex/swaps?address="+swapAddr)
	require.Equal(t, http.StatusOK, rec.Code)
	var got []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 100)
	assert.Equal(t, float64(249), got[0]["order_id"])
	assert.Equal(t, float64(126), got[99]["order_id"], "25 dropped, so 25 older ones fill in")
	assert.Equal(t, []int64{0, 151}, store.before, "the second page starts below the first")

	// Pages stop at MaxPages, and when the store has no more.
	store = &swapStore{swaps: swaps}
	serve(t, swapServer(store, swapExponents{"ubze": 6}), http.MethodGet, "/aggregator/api/dex/swaps?address="+swapAddr)
	assert.Equal(t, []int64{0, 151, 51}, store.before, "250 rows: the third page is short")

	var many []swaphistory.Swap
	for id := int64(5000); id > 0; id-- {
		many = append(many, swapRow(id, swapAtom, "ubze", "buy", "1", "1"))
	}
	store = &swapStore{swaps: many}
	rec = serve(t, swapServer(store, swapExponents{"ubze": 6}), http.MethodGet, "/aggregator/api/dex/swaps?address="+swapAddr)
	assert.Equal(t, "[]\n", rec.Body.String())
	assert.Len(t, store.before, swaphistory.MaxPages)
}
