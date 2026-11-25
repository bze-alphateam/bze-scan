package controller_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/ratelimits"
)

type fakeLimits struct {
	snap *ratelimits.Snapshot
	err  error
}

func (f fakeLimits) Snapshot(context.Context) (*ratelimits.Snapshot, error) { return f.snap, f.err }

type fakeCatalog struct {
	briefs   map[string]repository.DenomBrief
	channels []repository.IBCChannel
	err      error
	calls    int
}

func (f *fakeCatalog) IBCChannels(context.Context) ([]repository.IBCChannel, error) {
	f.calls++
	return f.channels, f.err
}

func (f *fakeCatalog) DenomBriefs(context.Context, []string) (map[string]repository.DenomBrief, error) {
	return f.briefs, f.err
}

func direction(threshold, net int64) ratelimits.Direction {
	remaining := max(threshold-net, 0)
	return ratelimits.Direction{MaxPercent: math.NewInt(2), Threshold: math.NewInt(threshold), NetFlow: math.NewInt(net),
		Used: math.NewInt(max(net, 0)), Remaining: math.NewInt(remaining), Enforced: true}
}

func rl(denom, channel string) ratelimits.Limit {
	reset := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	return ratelimits.Limit{Denom: denom, ChannelID: channel, WindowHours: 24, NextReset: &reset,
		ChannelValue: math.NewInt(1000), Inflow: math.NewInt(0), Outflow: math.NewInt(5),
		Send: direction(20, 5), Recv: direction(20, -5)}
}

func rateLimitAPI(limits controller.RateLimitReader, cat controller.RateLimitCatalog) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	e.GET("/ibc/rate-limits", controller.NewRateLimitController(limits, cat).RateLimits)
	return e
}

func getJSON(t *testing.T, e *echo.Echo, path string) (*httptest.ResponseRecorder, dto.RateLimitList) {
	t.Helper()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var list dto.RateLimitList
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list), rec.Body.String())
	}
	return rec, list
}

func TestRateLimitsNamesDenomsAndChannelsAndFilters(t *testing.T) {
	sym := "BZE"
	cat := &fakeCatalog{
		briefs:   map[string]repository.DenomBrief{"ubze": {Denom: "ubze", Symbol: &sym, Exponent: 6}},
		channels: []repository.IBCChannel{{ChannelID: "channel-3", Counterparty: noble()}},
	}
	snap := &ratelimits.Snapshot{Supported: true, Blacklisted: []string{"ibc/X"},
		Limits: []ratelimits.Limit{rl("ubze", "channel-0"), rl("ubze", "channel-3"), rl("ibc/A", "channel-3")}}
	e := rateLimitAPI(fakeLimits{snap: snap}, cat)

	rec, list := getJSON(t, e, "/ibc/rate-limits")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get("Cache-Control"))
	assert.True(t, list.Supported)
	require.Len(t, list.Items, 3)
	assert.Equal(t, []string{"ibc/X"}, list.BlacklistedDenoms)
	assert.Equal(t, "BZE", *list.Items[0].Symbol)
	assert.Equal(t, 6, list.Items[0].Exponent)
	assert.Nil(t, list.Items[0].Counterparty.PrettyName, "channel-0 is not in the channels table")
	assert.Equal(t, "Noble", *list.Items[1].Counterparty.PrettyName)
	assert.Equal(t, "15", list.Items[0].Send.Remaining)
	assert.Equal(t, "25", list.Items[0].Recv.Remaining)
	assert.Equal(t, "-5", list.Items[0].Recv.NetFlow)

	_, list = getJSON(t, e, "/ibc/rate-limits?denom=ubze")
	assert.Len(t, list.Items, 2)
	_, list = getJSON(t, e, "/ibc/rate-limits?channel=channel-3")
	assert.Len(t, list.Items, 2)
	_, list = getJSON(t, e, "/ibc/rate-limits?denom=ubze&channel=channel-3")
	require.Len(t, list.Items, 1)
	assert.Equal(t, "channel-3", list.Items[0].ChannelID)
	before := cat.calls
	_, list = getJSON(t, e, "/ibc/rate-limits?denom=none")
	assert.NotNil(t, list.Items)
	assert.Empty(t, list.Items)
	assert.Equal(t, before, cat.calls, "no limit, no lookup")
}

func TestRateLimitsBeforeV830(t *testing.T) {
	e := rateLimitAPI(fakeLimits{snap: &ratelimits.Snapshot{Blacklisted: []string{}}}, &fakeCatalog{})
	rec, _ := getJSON(t, e, "/ibc/rate-limits")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"supported":false,"items":[],"blacklisted_denoms":[]}`, rec.Body.String())
}

func TestRateLimitsNodeFailureIsBadGateway(t *testing.T) {
	e := rateLimitAPI(fakeLimits{err: errors.New("down")}, &fakeCatalog{})
	rec, _ := getJSON(t, e, "/ibc/rate-limits")
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestRateLimitsDatabaseFailureIsAnError(t *testing.T) {
	snap := &ratelimits.Snapshot{Supported: true, Limits: []ratelimits.Limit{rl("ubze", "channel-0")}}
	e := rateLimitAPI(fakeLimits{snap: snap}, &fakeCatalog{err: errors.New("db")})
	rec, _ := getJSON(t, e, "/ibc/rate-limits")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func tokenWithLimits(limits controller.RateLimitReader, cat controller.RateLimitCatalog) *echo.Echo {
	f := &fakeTokens{token: honeyToken(), events: tokenEvents(1)}
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	h := controller.NewTokenController(f)
	if limits != nil {
		h.WithRateLimits(controller.NewRateLimitController(limits, cat))
	}
	e.GET("/api/v1/tokens/:denom", h.Token)
	return e
}

func TestTokenCarriesItsRateLimits(t *testing.T) {
	snap := &ratelimits.Snapshot{Supported: true,
		Limits: []ratelimits.Limit{rl("ubze", "channel-0"), rl(honeyDenom, "channel-3")}}
	e := tokenWithLimits(fakeLimits{snap: snap}, &fakeCatalog{channels: []repository.IBCChannel{{ChannelID: "channel-3", Counterparty: noble()}}})
	rec := get(t, e, "/api/v1/tokens/"+url.PathEscape(honeyDenom))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	tok := decode[dto.Token](t, rec)
	require.Len(t, tok.RateLimits, 1, "only this denom's limits")
	assert.Equal(t, "channel-3", tok.RateLimits[0].ChannelID)
	assert.Equal(t, "Noble", *tok.RateLimits[0].Counterparty.PrettyName)
}

func TestTokenRateLimitsAreEmptyWhenThereAreNoneOrTheNodeFails(t *testing.T) {
	for name, e := range map[string]*echo.Echo{
		"not wired":   tokenWithLimits(nil, nil),
		"none":        tokenWithLimits(fakeLimits{snap: &ratelimits.Snapshot{Supported: true}}, &fakeCatalog{}),
		"unsupported": tokenWithLimits(fakeLimits{snap: &ratelimits.Snapshot{}}, &fakeCatalog{}),
		"node down":   tokenWithLimits(fakeLimits{err: errors.New("down")}, &fakeCatalog{}),
	} {
		rec := get(t, e, "/api/v1/tokens/"+url.PathEscape(honeyDenom))
		require.Equal(t, http.StatusOK, rec.Code, name)
		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
		assert.JSONEq(t, "[]", string(raw["rate_limits"]), name)
	}
}
