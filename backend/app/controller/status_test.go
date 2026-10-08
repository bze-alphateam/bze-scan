package controller_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/internal/status"
)

type fakeStatus struct{ s status.Snapshot }

func (f fakeStatus) Snapshot() status.Snapshot { return f.s }

func serveStatus(t *testing.T, s status.Snapshot) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	e.GET("/api/v1/status", controller.NewStatusController(fakeStatus{s}).Status)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	return rec
}

func int64Ptr(v int64) *int64 { return &v }

func TestStatusBeforeTheFirstTick(t *testing.T) {
	rec := serveStatus(t, status.Snapshot{BackFill: status.BackFillFinished})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.JSONEq(t, `{
		"live_fill": {"healthy": false, "checked_at": null, "db_height": null, "node_height": null, "archive_height": null},
		"back_fill": {"status": "finished", "oldest_height": null}
	}`, rec.Body.String())
}

func TestStatusHealthy(t *testing.T) {
	rec := serveStatus(t, status.Snapshot{
		CheckedAt:     time.Date(2026, 10, 8, 14, 3, 5, 0, time.FixedZone("EEST", 3*3600)),
		Healthy:       true,
		DBHeight:      int64Ptr(25000894),
		NodeHeight:    int64Ptr(25000895),
		ArchiveHeight: int64Ptr(25000896),
		BackFill:      status.BackFillFinished,
		OldestHeight:  int64Ptr(24998316),
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{
		"live_fill": {"healthy": true, "checked_at": "2026-10-08T11:03:05Z", "db_height": 25000894,
			"node_height": 25000895, "archive_height": 25000896},
		"back_fill": {"status": "finished", "oldest_height": 24998316}
	}`, rec.Body.String())
}

// An unhealthy verdict is in the body, never in the HTTP status.
func TestStatusUnhealthyStillAnswers200(t *testing.T) {
	rec := serveStatus(t, status.Snapshot{
		CheckedAt: time.Date(2026, 10, 8, 11, 0, 0, 0, time.UTC),
		DBHeight:  int64Ptr(100),
		BackFill:  status.BackFillFinished,
	})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{
		"live_fill": {"healthy": false, "checked_at": "2026-10-08T11:00:00Z", "db_height": 100,
			"node_height": null, "archive_height": null},
		"back_fill": {"status": "finished", "oldest_height": null}
	}`, rec.Body.String())
}
