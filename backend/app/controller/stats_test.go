package controller_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

type fakeStats struct {
	blocks *repository.BlockStats
	state  map[string]json.RawMessage
	top    string
	gotTop int
	err    error
}

func (f *fakeStats) BlockStats(context.Context) (*repository.BlockStats, error) {
	return f.blocks, f.err
}

func (f *fakeStats) ChainState(context.Context) (map[string]json.RawMessage, error) {
	return f.state, f.err
}

func (f *fakeStats) TopBondedTokens(_ context.Context, n int) (string, error) {
	f.gotTop = n
	return f.top, f.err
}

func chainState() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"staking_pool":     json.RawMessage(`{"bonded_tokens":"600","not_bonded_tokens":"0"}`),
		"supply":           json.RawMessage(`{"denom":"ubze","amount":"1000"}`),
		"mint":             json.RawMessage(`{"inflation":"0.06","annual_provisions":"60","params":{}}`),
		"chain_params":     json.RawMessage(`{"max_validators":40,"unbonding_time_s":1814400,"community_tax":"0.1"}`),
		"validator_counts": json.RawMessage(`{"bonded":22,"jailed":1,"total":30}`),
	}
}

func TestStatsRoute(t *testing.T) {
	h := int64(9)
	f := &fakeStats{blocks: &repository.BlockStats{LatestHeight: &h, Txs24h: 3}, state: chainState()}
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	e.GET("/stats", controller.NewStatsController(f).Stats)

	rec := get(t, e, "/stats")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	body := decodeRaw(t, rec)
	assert.JSONEq(t, `9`, string(body["latest_height"]))
	assert.JSONEq(t, `"60.00000"`, string(body["staked_share"]))
	assert.JSONEq(t, `"9.00000"`, string(body["reward_rate"]), "6% × 0.9 ÷ 60%")
	assert.JSONEq(t, `{"active":22,"total":30}`, string(body["validators"]))
	assert.JSONEq(t, `40`, string(body["max_validators"]))
	assert.JSONEq(t, `null`, string(body["price_usd"]))

	f.err = errors.New("db down")
	assert.Equal(t, http.StatusInternalServerError, get(t, e, "/stats").Code)
}

func TestValidatorsCarryTheSummary(t *testing.T) {
	f := &fakeStats{state: chainState(), top: "300"}
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	e.GET("/validators", controller.NewExplorerController(&fakeReader{vals: validatorRows(1)}, f).Validators)

	rec := get(t, e, "/validators")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.TopValidators, f.gotTop)
	assert.JSONEq(t, `{"bonded_tokens":"600","staked_share":"60.00000","reward_rate":"9.00000",
		"unbonding_period":1814400,"top5_share":"50.00000","max_validators":40}`, string(decodeRaw(t, rec)["summary"]))

	f.err = errors.New("db down")
	assert.Equal(t, http.StatusInternalServerError, get(t, e, "/validators").Code)

	// Without a stats reader the header is null.
	e.GET("/plain", controller.NewExplorerController(&fakeReader{}, nil).Validators)
	assert.JSONEq(t, `null`, string(decodeRaw(t, get(t, e, "/plain"))["summary"]))
}

type fakeLive struct {
	params map[string]json.RawMessage
	err    error
}

func (f fakeLive) Params(context.Context) (map[string]json.RawMessage, error) { return f.params, f.err }

type fakeChanges struct {
	changes map[string]repository.ParamChange
	err     error
}

func (f fakeChanges) ParamChanges(context.Context) (map[string]repository.ParamChange, error) {
	return f.changes, f.err
}

func paramsAPI(live controller.LiveParams, changes controller.ParamChangeReader) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	e.GET("/params", controller.NewParamsController(live, changes).Params)
	return e
}

func TestParamsRoute(t *testing.T) {
	p44 := int64(44)
	live := fakeLive{
		params: map[string]json.RawMessage{
			"tradebin": json.RawMessage(`{"marketTakerFee":{"denom":"ubze","amount":"100000"}}`),
			"burner":   json.RawMessage(`{"periodic_burning_weeks":"4"}`),
		},
		err: errors.New("gov params: unavailable"),
	}
	changes := fakeChanges{changes: map[string]repository.ParamChange{
		"tradebin": {Height: 20237800, Time: time.Date(2025, 11, 24, 10, 0, 0, 0, time.FixedZone("x", 7200)), ProposalID: &p44},
		"gov":      {Height: 1, Time: time.Unix(0, 0)},
	}}
	rec := get(t, paramsAPI(live, changes), "/params")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.JSONEq(t, `{
		"tradebin": {"params": {"marketTakerFee":{"denom":"ubze","amount":"100000"}},
		             "last_change": {"height":20237800,"time":"2025-11-24T08:00:00Z","proposal_id":44}},
		"burner": {"params": {"periodic_burning_weeks":"4"}, "last_change": null}
	}`, rec.Body.String(), "a module the node did not answer is left out")
}

func TestParamsRouteFailures(t *testing.T) {
	rec := get(t, paramsAPI(fakeLive{err: errors.New("connection refused")}, fakeChanges{}), "/params")
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Equal(t, "upstream_error", errorCode(t, rec))

	ok := fakeLive{params: map[string]json.RawMessage{"burner": json.RawMessage(`{}`)}}
	assert.Equal(t, http.StatusInternalServerError,
		get(t, paramsAPI(ok, fakeChanges{err: errors.New("db down")}), "/params").Code)
}
