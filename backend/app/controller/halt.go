package controller

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/upgrades"
)

// LiveHaltReader returns what the halt watch saw at its latest poll, no
// I/O; implemented by upgrades.Watch.
type LiveHaltReader interface {
	Live() (upgrades.Live, bool)
}

// HaltController serves GET /api/v1/upgrades/live.
type HaltController struct {
	live  LiveHaltReader
	names ValidatorNames
}

// NewHaltController returns a controller over live (nil: the watch is not
// running, never halted), naming validators through names.
func NewHaltController(live LiveHaltReader, names ValidatorNames) *HaltController {
	return &HaltController{live: live, names: names}
}

// Live serves GET /api/v1/upgrades/live: while the chain is halted for a
// software upgrade, the votes of the round consensus waits in, member by
// member; {"halted": false} otherwise. A stall that is not an upgrade's
// halt is never reported here (the status endpoint says the explorer is
// behind).
func (h *HaltController) Live(c *echo.Context) error {
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	if h.live == nil {
		return c.JSON(http.StatusOK, dto.NotHalted{})
	}
	l, ok := h.live.Live()
	if !ok {
		return c.JSON(http.StatusOK, dto.NotHalted{})
	}
	var ops []string
	for _, m := range l.Members {
		if m.Operator != "" {
			ops = append(ops, m.Operator)
		}
	}
	monikers := map[string]string{}
	if h.names != nil && len(ops) > 0 {
		var err error
		if monikers, err = h.names.ValidatorMonikers(c.Request().Context(), ops); err != nil {
			return err
		}
	}
	return c.JSON(http.StatusOK, dto.NewLiveHalt(l, monikers))
}
