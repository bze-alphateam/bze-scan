package controller_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/internal/stakeflow"
)

const (
	flowOperator = "bzevaloper1prm55vzlp5u6excqdunwlm4tw254cq943m6e6m"
	flowOther    = "bzevaloper10dcm7hqv9a5ajdjr9ctcu5pgg2969ag7qx0nyg"
	flowGone     = "bzevaloper1gone"
)

type fakeFlows struct {
	changes   stakeflow.Changes
	page      stakeflow.Page
	gotOp     string
	gotOffset int
	gotLimit  int
}

func (f *fakeFlows) Changes() stakeflow.Changes { return f.changes }

func (f *fakeFlows) Movements(validator string, offset, limit int) stakeflow.Page {
	f.gotOp, f.gotOffset, f.gotLimit = validator, offset, limit
	return f.page
}

type fakeNames struct {
	monikers map[string]string
	got      []string
	err      error
}

func (f *fakeNames) ValidatorMonikers(_ context.Context, operators []string) (map[string]string, error) {
	f.got = operators
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]string{}
	for _, op := range operators {
		if m, ok := f.monikers[op]; ok {
			out[op] = m
		}
	}
	return out, nil
}

func newFlowAPI(flows *fakeFlows, names *fakeNames) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	h := controller.NewStakeFlowController(flows, names)
	e.GET("/validators/stake-changes", h.StakeChanges)
	e.GET("/validators/:operator/movements", h.Movements)
	return e
}

func TestStakeChanges(t *testing.T) {
	flows := &fakeFlows{changes: stakeflow.Changes{Complete: true, AsOfHeight: 25142082, Items: map[string]stakeflow.Change{
		flowOperator: {Day: -1640000000000, Week: -2928000000000},
		flowOther:    {Day: 0, Week: 12},
	}}}
	rec := get(t, newFlowAPI(flows, &fakeNames{}), "/validators/stake-changes")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.JSONEq(t, `{"complete": true, "as_of_height": 25142082, "items": {
		"`+flowOperator+`": {"24h": "-1640000000000", "7d": "-2928000000000"},
		"`+flowOther+`": {"24h": "0", "7d": "12"}}}`, rec.Body.String())

	// Before the load: nothing yet, and not complete.
	flows.changes = stakeflow.Changes{Items: map[string]stakeflow.Change{}}
	rec = get(t, newFlowAPI(flows, &fakeNames{}), "/validators/stake-changes")
	assert.JSONEq(t, `{"complete": false, "as_of_height": 0, "items": {}}`, rec.Body.String())
}

func TestMovementsPage(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 12, 0, 0, time.FixedZone("EEST", 3*3600))
	flows := &fakeFlows{page: stakeflow.Page{Complete: true, WindowFrom: at.Add(-stakeflow.Window), Total: 214, Items: []stakeflow.Movement{
		{Height: 25142010, TxHash: "AB12", Time: at, Kind: stakeflow.KindRedelegateOut, Validator: flowOperator,
			Delegator: "bze1d", Counterparty: flowOther, Amount: 640000000000},
		{Height: 25142000, TxHash: "CD34", Time: at, Kind: stakeflow.KindRedelegateIn, Validator: flowOperator,
			Delegator: "bze1d", Counterparty: flowGone, Amount: 5},
		{Height: 25141990, TxHash: "EF56", Time: at, Kind: stakeflow.KindDelegate, Validator: flowOperator,
			Delegator: "bze1e", Amount: 7},
	}}}
	names := &fakeNames{monikers: map[string]string{flowOperator: "Thamar", flowOther: "ChainTools"}}
	rec := get(t, newFlowAPI(flows, names), "/validators/"+flowOperator+"/movements?page=3&limit=20")

	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.Equal(t, flowOperator, flows.gotOp)
	assert.Equal(t, 40, flows.gotOffset)
	assert.Equal(t, 20, flows.gotLimit)
	assert.Equal(t, []string{flowOperator, flowOther, flowGone}, names.got)
	assert.JSONEq(t, `{"complete": true, "window_from": "2026-10-14T06:12:00Z", "page": 3, "total": 214, "items": [
		{"height": 25142010, "tx_hash": "AB12", "time": "2026-10-21T06:12:00Z", "kind": "redelegate_out", "delegator": "bze1d",
			"counterparty_validator": {"operator_address": "`+flowOther+`", "moniker": "ChainTools"}, "amount": "640000000000"},
		{"height": 25142000, "tx_hash": "CD34", "time": "2026-10-21T06:12:00Z", "kind": "redelegate_in", "delegator": "bze1d",
			"counterparty_validator": {"operator_address": "`+flowGone+`", "moniker": null}, "amount": "5"},
		{"height": 25141990, "tx_hash": "EF56", "time": "2026-10-21T06:12:00Z", "kind": "delegate", "delegator": "bze1e",
			"counterparty_validator": null, "amount": "7"}
	]}`, rec.Body.String())
}

func TestMovementsDefaultsAndEmptyBeforeTheLoad(t *testing.T) {
	flows := &fakeFlows{page: stakeflow.Page{Items: []stakeflow.Movement{}}}
	rec := get(t, newFlowAPI(flows, &fakeNames{monikers: map[string]string{flowOperator: "Thamar"}}),
		"/validators/"+flowOperator+"/movements")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 0, flows.gotOffset)
	assert.Equal(t, controller.MovementsDefaultLimit, flows.gotLimit)
	assert.JSONEq(t, `{"complete": false, "window_from": null, "page": 1, "total": 0, "items": []}`, rec.Body.String())
}

func TestMovementsErrors(t *testing.T) {
	names := &fakeNames{monikers: map[string]string{flowOperator: "Thamar"}}
	for path, want := range map[string]int{
		"/validators/bze1qly96sy3yklkwv6cf4he5ves4qu8yn588vltqp/movements": http.StatusBadRequest,
		"/validators/nope/movements":                                       http.StatusBadRequest,
		"/validators/" + flowOperator + "/movements?page=0":                http.StatusBadRequest,
		"/validators/" + flowOperator + "/movements?page=x":                http.StatusBadRequest,
		"/validators/" + flowOperator + "/movements?limit=0":               http.StatusBadRequest,
		"/validators/" + flowOperator + "/movements?limit=51":              http.StatusBadRequest,
		"/validators/" + flowOperator + "/movements?limit=+5":              http.StatusBadRequest,
		"/validators/" + flowOther + "/movements":                          http.StatusNotFound,
	} {
		rec := get(t, newFlowAPI(&fakeFlows{}, names), path)
		assert.Equal(t, want, rec.Code, path)
		assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl), path)
	}

	rec := get(t, newFlowAPI(&fakeFlows{}, &fakeNames{err: errors.New("db down")}), "/validators/"+flowOperator+"/movements")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "internal", errorCode(t, rec))
}
