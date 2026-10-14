package controller_test

import (
	"context"
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

// fakeUpgrades knows one upgrade.
type fakeUpgrades struct {
	upgrade    *repository.Upgrade
	members    []repository.UpgradeMember
	err        error
	membersErr error
}

func (f *fakeUpgrades) Upgrade(_ context.Context, height int64) (*repository.Upgrade, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.upgrade == nil || f.upgrade.Height != height {
		return nil, repository.ErrNotFound
	}
	return f.upgrade, nil
}

func (f *fakeUpgrades) UpgradeMembers(context.Context, int64) ([]repository.UpgradeMember, error) {
	return f.members, f.membersErr
}

func upgradeServer(f *fakeUpgrades) *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = middleware.ErrorHandler
	e.GET("/api/v1/upgrades/:height", controller.NewUpgradeController(f).Upgrade)
	return e
}

func TestUpgrade(t *testing.T) {
	halt, resume := time.Date(2026, 4, 28, 15, 52, 31, 683549605, time.UTC), time.Date(2026, 4, 28, 16, 4, 39, 802993053, time.UTC)
	back, signed := time.Date(2026, 4, 28, 16, 4, 45, 770401227, time.UTC), time.Date(2026, 4, 28, 16, 4, 45, 776698023, time.UTC)
	size, end, needed, first, secs := 23, int64(22551815), int64(1), int64(22551812), 734
	op, moniker, source := "bzevaloper1x", "Thamar", "commit"
	f := &fakeUpgrades{
		upgrade: &repository.Upgrade{Height: 22551810, ProposalID: 40, Name: "v8.1.0", HaltTime: &halt, ResumeTime: &resume,
			ActiveSetSize: &size, WindowEndHeight: &end, ComputedAt: &resume},
		members: []repository.UpgradeMember{
			{ConsensusAddress: "2AD8E07AA7B80F9A5C7229AFE353F5C69F28AA67", OperatorAddress: &op, Moniker: &moniker,
				VotingPower: 5644211, PowerPct: "4.12000", BackTime: &back, BackSource: &source, VoteTime: &signed,
				FirstSignedHeight: &first, BlocksNeeded: &needed, SecondsBack: &secs},
			{ConsensusAddress: "18E08A311C5111C975C1E85D5B91AE167E79B1EF", VotingPower: 1986504, PowerPct: "1.45000"},
		},
	}
	e := upgradeServer(f)

	rec := get(t, e, "/api/v1/upgrades/22551810")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.JSONEq(t, `{
		"height": 22551810, "proposal_id": 40, "name": "v8.1.0",
		"halt_time": "2026-04-28T15:52:31.683549605Z", "resume_time": "2026-04-28T16:04:39.802993053Z",
		"downtime_seconds": 728, "watched": false, "active_set_size": 23, "window_end_height": 22551815,
		"computed_at": "2026-04-28T16:04:39.802993053Z",
		"members": [
			{"operator_address": "bzevaloper1x", "consensus_address": "2AD8E07AA7B80F9A5C7229AFE353F5C69F28AA67", "moniker": "Thamar",
			 "voting_power": 5644211, "power_pct": "4.12000", "back_time": "2026-04-28T16:04:45.770401227Z", "back_source": "commit",
			 "vote_time": "2026-04-28T16:04:45.776698023Z", "seconds_back": 734, "first_signed_height": 22551812, "blocks_needed": 1},
			{"operator_address": null, "consensus_address": "18E08A311C5111C975C1E85D5B91AE167E79B1EF", "moniker": null,
			 "voting_power": 1986504, "power_pct": "1.45000", "back_time": null, "back_source": null, "vote_time": null,
			 "seconds_back": null, "first_signed_height": null, "blocks_needed": null}
		]}`, rec.Body.String())

	// Pending: no times, no members yet.
	f.upgrade = &repository.Upgrade{Height: 22551810, ProposalID: 40, Name: "v8.1.0"}
	f.members = nil
	rec = get(t, e, "/api/v1/upgrades/22551810")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"downtime_seconds":null`)
	assert.Contains(t, rec.Body.String(), `"members":[]`)

	assert.Equal(t, http.StatusNotFound, get(t, e, "/api/v1/upgrades/22551811").Code)
	for _, path := range []string{"/api/v1/upgrades/0", "/api/v1/upgrades/x", "/api/v1/upgrades/-1"} {
		assert.Equal(t, http.StatusBadRequest, get(t, e, path).Code, path)
	}
	f.membersErr = errors.New("boom")
	assert.Equal(t, http.StatusInternalServerError, get(t, e, "/api/v1/upgrades/22551810").Code)
	f.err = errors.New("boom")
	assert.Equal(t, http.StatusInternalServerError, get(t, e, "/api/v1/upgrades/22551810").Code)
}
