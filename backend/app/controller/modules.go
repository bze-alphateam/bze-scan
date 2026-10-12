package controller

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
)

// Modules serves GET /api/v1/modules: the module accounts' labels. Only the
// labels seed of a migration changes them, so the answer is cached like
// what the state sync fills.
func (h *ExplorerController) Modules(c *echo.Context) error {
	rows, err := h.repo.ModuleAccounts(c.Request().Context())
	if err != nil {
		return err
	}
	resp := dto.ModuleList{Items: make([]dto.ModuleAccount, 0, len(rows))}
	for _, m := range rows {
		resp.Items = append(resp.Items, dto.NewModuleAccount(m))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheIndexed)
	return c.JSON(http.StatusOK, resp)
}
