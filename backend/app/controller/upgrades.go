package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// UpgradeReader is the read side of the upgrade tables. Implemented by
// repository.Explorer.
type UpgradeReader interface {
	Upgrade(ctx context.Context, height int64) (*repository.Upgrade, error)
	UpgradeMembers(ctx context.Context, height int64) ([]repository.UpgradeMember, error)
}

// UpgradeController serves /api/v1/upgrades. The rows fill in while the
// state sync reads the blocks after an upgrade, so nothing is cached.
type UpgradeController struct {
	repo UpgradeReader
}

// NewUpgradeController returns a controller over repo.
func NewUpgradeController(repo UpgradeReader) *UpgradeController {
	return &UpgradeController{repo: repo}
}

// Upgrade serves GET /api/v1/upgrades/{height}: the software upgrade
// planned at that height and when each validator came back after its halt.
func (h *UpgradeController) Upgrade(c *echo.Context) error {
	height, err := strconv.ParseInt(c.Param("height"), 10, 64)
	if err != nil || height <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "height must be a positive integer")
	}
	ctx := c.Request().Context()
	u, err := h.repo.Upgrade(ctx, height)
	if errors.Is(err, repository.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("no software upgrade is known at height %d", height))
	}
	if err != nil {
		return err
	}
	members, err := h.repo.UpgradeMembers(ctx, height)
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.NewUpgrade(u, members))
}
