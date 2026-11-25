package controller_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/internal/activeset"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/upgrades"
)

type fakeLiveHalt struct {
	live upgrades.Live
	ok   bool
}

func (f *fakeLiveHalt) Live() (upgrades.Live, bool) { return f.live, f.ok }

func newHaltAPI(live controller.LiveHaltReader, names *fakeNames) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	e.GET("/upgrades/live", controller.NewHaltController(live, names).Live)
	return e
}

func TestLiveHaltNotHalted(t *testing.T) {
	for name, live := range map[string]controller.LiveHaltReader{
		"no watch":  nil,
		"not armed": &fakeLiveHalt{},
	} {
		t.Run(name, func(t *testing.T) {
			rec := get(t, newHaltAPI(live, &fakeNames{}), "/upgrades/live")
			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
			assert.JSONEq(t, `{"halted": false}`, rec.Body.String())
		})
	}
}

var proposal48 = int64(48)

func TestLiveHaltHalted(t *testing.T) {
	since := time.Date(2026, 10, 26, 17, 0, 0, 0, time.UTC)
	live := &fakeLiveHalt{ok: true, live: upgrades.Live{
		Height: 25263600, ProposalID: &proposal48, Name: "v8.2.0", Kind: upgrades.KindGovernance, Round: 2,
		HaltedSince: since, CheckedAt: since.Add(5 * time.Minute),
		Members: []upgrades.LiveMember{
			{Member: activeset.Member{ConsensusAddress: "AAAA", VotingPower: 60, Operator: flowOperator}, Back: true, Prevote: true, Precommit: true},
			{Member: activeset.Member{ConsensusAddress: "BBBB", VotingPower: 30, Operator: flowOther}, Back: true},
			{Member: activeset.Member{ConsensusAddress: "CCCC", VotingPower: 10}},
		},
	}}
	names := &fakeNames{monikers: map[string]string{flowOperator: "Alpha"}}
	rec := get(t, newHaltAPI(live, names), "/upgrades/live")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.Equal(t, []string{flowOperator, flowOther}, names.got)
	assert.JSONEq(t, `{"halted": true, "height": 25263600, "halt_height": 25263601, "proposal_id": 48, "name": "v8.2.0",
		"kind": "governance", "reason": null, "scheduled_by": null, "round": 2, "halted_since": "2026-10-26T17:00:00Z", "checked_at": "2026-10-26T17:05:00Z",
		"total_power": "100", "back_power": "90", "prevote_power": "60", "precommit_power": "60",
		"members": [
			{"consensus_address": "AAAA", "operator_address": "`+flowOperator+`", "moniker": "Alpha", "voting_power": "60",
			 "back": true, "prevote": true, "precommit": true},
			{"consensus_address": "BBBB", "operator_address": "`+flowOther+`", "moniker": null, "voting_power": "30",
			 "back": true, "prevote": false, "precommit": false},
			{"consensus_address": "CCCC", "operator_address": null, "moniker": null, "voting_power": "10",
			 "back": false, "prevote": false, "precommit": false}]}`, rec.Body.String())
}

// An emergency halt has no proposal: the kind, the reason and the signer
// say what it is.
func TestLiveHaltEmergency(t *testing.T) {
	since := time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)
	live := &fakeLiveHalt{ok: true, live: upgrades.Live{
		Height: 5000000, Name: "emergency-halt-5000000", Kind: upgrades.KindEmergency,
		Reason: "pool drain under way", ScheduledBy: flowOperator, Round: 0,
		HaltedSince: since, CheckedAt: since,
		Members: []upgrades.LiveMember{{Member: activeset.Member{ConsensusAddress: "AAAA", VotingPower: 1}}},
	}}
	rec := get(t, newHaltAPI(live, &fakeNames{}), "/upgrades/live")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"halted": true, "height": 5000000, "halt_height": 5000001, "proposal_id": null,
		"name": "emergency-halt-5000000", "kind": "emergency", "reason": "pool drain under way",
		"scheduled_by": "`+flowOperator+`", "round": 0, "halted_since": "2026-12-01T09:00:00Z",
		"checked_at": "2026-12-01T09:00:00Z", "total_power": "1", "back_power": "0", "prevote_power": "0",
		"precommit_power": "0", "members": [{"consensus_address": "AAAA", "operator_address": null, "moniker": null,
		"voting_power": "1", "back": false, "prevote": false, "precommit": false}]}`, rec.Body.String())
}

func TestLiveHaltNamesFail(t *testing.T) {
	live := &fakeLiveHalt{ok: true, live: upgrades.Live{Members: []upgrades.LiveMember{
		{Member: activeset.Member{ConsensusAddress: "AAAA", VotingPower: 1, Operator: flowOperator}},
	}}}
	rec := get(t, newHaltAPI(live, &fakeNames{err: errors.New("db down")}), "/upgrades/live")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
