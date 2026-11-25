package controller

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/emergency"
)

// EmergencyState reads the emergency switches live from the node;
// *emergency.Reader implements it.
type EmergencyState interface {
	State(ctx context.Context) (*emergency.State, error)
}

// EmergencyIndex reads what the explorer stores about the emergency
// switches; *repository.Explorer implements it.
type EmergencyIndex interface {
	EmergencyActions(ctx context.Context, before *repository.EmergencyKey, limit int) ([]repository.EmergencyAction, error)
	EmergencyDenoms(ctx context.Context, denoms []string) (map[string]repository.EmergencyDenom, error)
	EmergencyChannels(ctx context.Context, ids []string) (map[string]repository.EmergencyChannel, error)
	EmergencyLabel(ctx context.Context, address string) (*string, error)
}

// EmergencyController serves the emergency status routes.
type EmergencyController struct {
	state EmergencyState
	index EmergencyIndex
}

// NewEmergencyController returns a controller over the live state and the
// index.
func NewEmergencyController(state EmergencyState, index EmergencyIndex) *EmergencyController {
	return &EmergencyController{state: state, index: index}
}

// State serves GET /api/v1/emergency: whether any emergency measure is in
// force and which. A node that does not answer is a 502.
func (h *EmergencyController) State(c *echo.Context) error {
	ctx := c.Request().Context()
	s, err := h.state.State(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "the node did not answer the emergency state").Wrap(err)
	}
	denoms, err := h.index.EmergencyDenoms(ctx, append(append([]string{}, s.BlockedDenoms...), s.HaltedDenoms...))
	if err != nil {
		return err
	}
	channels, err := h.index.EmergencyChannels(ctx, s.ClosedChannels)
	if err != nil {
		return err
	}
	var label *string
	if s.Guardian != "" {
		if label, err = h.index.EmergencyLabel(ctx, s.Guardian); err != nil {
			return err
		}
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.NewEmergency(s, denoms, channels, label))
}

// Actions serves GET /api/v1/emergency/actions?cursor&limit: the emergency
// actions and their undos, newest first.
func (h *EmergencyController) Actions(c *echo.Context) error {
	limit, err := parseLimit(c)
	if err != nil {
		return err
	}
	var before *repository.EmergencyKey
	if keys, err := parseCursor(c, 2); err != nil {
		return err
	} else if keys != nil {
		before = &repository.EmergencyKey{Height: keys[0], Order: keys[1]}
	}
	rows, err := h.index.EmergencyActions(c.Request().Context(), before, limit+1)
	if err != nil {
		return err
	}
	resp := dto.List[dto.EmergencyAction]{Items: make([]dto.EmergencyAction, 0, min(len(rows), limit))}
	for i, r := range rows {
		if i == limit {
			next := dto.EncodeCursor(rows[i-1].Height, rows[i-1].Order)
			resp.NextCursor = &next
			break
		}
		if a, ok := dto.NewEmergencyAction(r); ok {
			resp.Items = append(resp.Items, a)
		}
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, resp)
}
