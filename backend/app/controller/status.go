package controller

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/status"
)

// StatusReader returns the latest status snapshot without any I/O;
// implemented by status.Checker.
type StatusReader interface {
	Snapshot() status.Snapshot
}

// StatusController serves GET /api/v1/status.
type StatusController struct {
	status StatusReader
}

// NewStatusController returns a controller serving the snapshots of r.
func NewStatusController(r StatusReader) *StatusController {
	return &StatusController{status: r}
}

// Status answers 200 with the latest snapshot, whatever it says: the verdict
// is in the body (live_fill.healthy), never in the HTTP status. It touches
// neither a node nor the database.
func (h *StatusController) Status(c *echo.Context) error {
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.NewStatus(h.status.Snapshot()))
}
