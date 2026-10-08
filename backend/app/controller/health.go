// Package controller holds the thin HTTP handlers of the API.
package controller

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

// HealthController serves GET /health.
type HealthController struct{}

// NewHealthController returns a HealthController.
func NewHealthController() *HealthController {
	return &HealthController{}
}

// Health answers 200 with an empty body whenever the process serves HTTP. It
// is the probe of the deploy's blue/green gate and deliberately checks
// nothing else: no database, no node. Whether the indexed data keeps up with
// the chain is the status endpoint's job.
func (h *HealthController) Health(c *echo.Context) error {
	return c.NoContent(http.StatusOK)
}
