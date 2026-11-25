package controller_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/emergency"
)

type fakeEmergencyState struct {
	s   *emergency.State
	err error
}

func (f fakeEmergencyState) State(context.Context) (*emergency.State, error) { return f.s, f.err }

type fakeEmergencyIndex struct {
	rows   []repository.EmergencyAction
	before *repository.EmergencyKey
	limit  int
}

func (f *fakeEmergencyIndex) EmergencyActions(_ context.Context, before *repository.EmergencyKey, limit int) ([]repository.EmergencyAction, error) {
	f.before, f.limit = before, limit
	return f.rows, nil
}

func (f *fakeEmergencyIndex) EmergencyDenoms(context.Context, []string) (map[string]repository.EmergencyDenom, error) {
	sym := "UTOK"
	return map[string]repository.EmergencyDenom{"factory/bze1x/utok": {Symbol: &sym, Exponent: 6}}, nil
}

func (f *fakeEmergencyIndex) EmergencyChannels(context.Context, []string) (map[string]repository.EmergencyChannel, error) {
	id, name := "cosmoshub-4", "Cosmos Hub"
	return map[string]repository.EmergencyChannel{"channel-3": {CounterpartyChainID: &id, CounterpartyChainName: &name}}, nil
}

func (f *fakeEmergencyIndex) EmergencyLabel(context.Context, string) (*repository.EmergencyLabel, error) {
	return &repository.EmergencyLabel{Name: "DAO #3 · Ops", Kind: "dao"}, nil
}

func serveEmergency(t *testing.T, state controller.EmergencyState, index controller.EmergencyIndex, path string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	h := controller.NewEmergencyController(state, index)
	e.GET("/api/v1/emergency", h.State)
	e.GET("/api/v1/emergency/actions", h.Actions)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestEmergencyStateNamesDenomsChannelsAndGuardian(t *testing.T) {
	s := &emergency.State{
		Supported: true, Height: 10, Guardian: "bze1guardian", AutomationPaused: true,
		ClosedChannels: []string{"channel-3"}, BlockedDenoms: []string{"ibc/AAA"},
		DisabledMessages: []string{"/bze.tradebin.MsgCreateOrder"}, HaltedDenoms: []string{"factory/bze1x/utok"},
		Plan: &emergency.Plan{Name: "emergency-halt-12", Height: 12, Info: "why", Emergency: true},
	}
	rec := serveEmergency(t, fakeEmergencyState{s: s}, &fakeEmergencyIndex{}, "/api/v1/emergency")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.JSONEq(t, `{
		"supported": true, "height": 10, "active": true,
		"guardian": {"address": "bze1guardian", "label": "DAO #3 · Ops", "label_kind": "dao"},
		"automation_paused": true,
		"closed_channels": [{"channel_id": "channel-3", "counterparty_chain_id": "cosmoshub-4", "counterparty_chain_name": "Cosmos Hub"}],
		"blocked_denoms": [{"denom": "ibc/AAA", "symbol": null, "exponent": 0, "logo_url": null}],
		"disabled_messages": ["/bze.tradebin.MsgCreateOrder"],
		"halted_denoms": [{"denom": "factory/bze1x/utok", "symbol": "UTOK", "exponent": 6, "logo_url": null}],
		"plan": {"name": "emergency-halt-12", "height": 12, "info": "why", "emergency": true}
	}`, rec.Body.String())
}

func TestEmergencyStateOfAnOldNodeIsEmpty(t *testing.T) {
	s := &emergency.State{Height: 3, ClosedChannels: []string{}, BlockedDenoms: []string{}, DisabledMessages: []string{}, HaltedDenoms: []string{}}
	rec := serveEmergency(t, fakeEmergencyState{s: s}, &fakeEmergencyIndex{}, "/api/v1/emergency")

	assert.JSONEq(t, `{
		"supported": false, "height": 3, "active": false, "guardian": null, "automation_paused": false,
		"closed_channels": [], "blocked_denoms": [], "disabled_messages": [], "halted_denoms": [], "plan": null
	}`, rec.Body.String())
}

func TestEmergencyStateIsA502WhenTheNodeFails(t *testing.T) {
	rec := serveEmergency(t, fakeEmergencyState{err: errors.New("down")}, &fakeEmergencyIndex{}, "/api/v1/emergency")
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestEmergencyActionsMapRowsAndPage(t *testing.T) {
	tm := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	gov := chain.ModuleAddress("gov")
	idx := &fakeEmergencyIndex{rows: []repository.EmergencyAction{
		{Height: 30, Order: 1_000_000_000_002, Time: tm, Kind: "bze.guardian.v1.DenomUnblockedEvent", Source: repository.EmergencyFromEvent,
			Details: []byte(`{"authority":"` + gov + `","denom":"ibc/AAA"}`)},
		{Height: 20, Order: 10_001, Time: tm, TxHash: "AB12", Kind: "/bze.guardian.v1.MsgBlockDenom", Signer: "bze1guardian",
			Details: []byte(`{"denom":"ibc/AAA"}`)},
		{Height: 10, Order: 0, Time: tm, TxHash: "CD34", Kind: "/bze.guardian.v1.MsgCloseChannel", Signer: "bze1guardian", Details: []byte(`{}`)},
	}}
	rec := serveEmergency(t, fakeEmergencyState{}, idx, "/api/v1/emergency/actions?limit=2")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 3, idx.limit, "one more than the page")
	assert.Nil(t, idx.before)
	assert.Contains(t, rec.Body.String(), `"next_cursor":"MjA6MTAwMDE"`) // 20:10001
	assert.JSONEq(t, `{"items": [
		{"height": 30, "time": "2026-11-25T10:00:00Z", "tx_hash": null, "action": "unblock_denom", "by": "governance",
		 "signer": "`+gov+`", "details": {"authority": "`+gov+`", "denom": "ibc/AAA"}, "proposal_id": null},
		{"height": 20, "time": "2026-11-25T10:00:00Z", "tx_hash": "AB12", "action": "block_denom", "by": "guardian",
		 "signer": "bze1guardian", "details": {"denom": "ibc/AAA"}, "proposal_id": null}
	], "next_cursor": "MjA6MTAwMDE"}`, rec.Body.String())
}

func TestEmergencyActionsReadTheCursorAndRejectABadOne(t *testing.T) {
	idx := &fakeEmergencyIndex{}
	rec := serveEmergency(t, fakeEmergencyState{}, idx, "/api/v1/emergency/actions?cursor=MjA6MTAwMDE")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, &repository.EmergencyKey{Height: 20, Order: 10001}, idx.before)
	assert.JSONEq(t, `{"items": [], "next_cursor": null}`, rec.Body.String())

	assert.Equal(t, http.StatusBadRequest, serveEmergency(t, fakeEmergencyState{}, idx, "/api/v1/emergency/actions?cursor=x").Code)
	assert.Equal(t, http.StatusBadRequest, serveEmergency(t, fakeEmergencyState{}, idx, "/api/v1/emergency/actions?limit=0").Code)
}

func TestEmergencyActionsOfAGovernanceMessageSignerAreGovernance(t *testing.T) {
	gov := chain.ModuleAddress("gov")
	idx := &fakeEmergencyIndex{rows: []repository.EmergencyAction{
		{Height: 5, Time: time.Unix(0, 0), TxHash: "EF", Kind: "/cosmos.circuit.v1.MsgResetCircuitBreaker", Signer: gov, Details: []byte(`{}`)},
	}}
	rec := serveEmergency(t, fakeEmergencyState{}, idx, "/api/v1/emergency/actions")
	assert.Contains(t, rec.Body.String(), `"action":"enable_messages","by":"governance"`)
}

func TestEmergencyActionOfAPassedProposalIsGovernancesWithoutATxHash(t *testing.T) {
	gov := chain.ModuleAddress("gov")
	id := int64(48)
	idx := &fakeEmergencyIndex{rows: []repository.EmergencyAction{
		{Height: 5, Order: 2_000_000_048_000, Time: time.Unix(0, 0), Kind: "/bze.tradebin.MsgUnhaltDenoms",
			Source: repository.EmergencyFromProposal, ProposalID: &id, Details: []byte(`{"denoms":["x"],"proposal_id":48}`)},
	}}
	rec := serveEmergency(t, fakeEmergencyState{}, idx, "/api/v1/emergency/actions")
	assert.JSONEq(t, `{"items": [{"height": 5, "time": "1970-01-01T00:00:00Z", "tx_hash": null, "action": "unhalt_trading",
		"by": "governance", "signer": "`+gov+`", "details": {"denoms": ["x"], "proposal_id": 48}, "proposal_id": 48}],
		"next_cursor": null}`, rec.Body.String())
}
