package controller

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/stakeflow"
)

// Page sizes of the stake movements.
const (
	MovementsDefaultLimit = 10
	MovementsMaxLimit     = 50
)

// StakeFlowReader serves the stake movements from memory; implemented by
// stakeflow.Set.
type StakeFlowReader interface {
	Changes() stakeflow.Changes
	Movements(validator string, offset, limit int) stakeflow.Page
}

// ValidatorNames names validators: the monikers of the known ones among
// operators.
type ValidatorNames interface {
	ValidatorMonikers(ctx context.Context, operators []string) (map[string]string, error)
}

// StakeFlowController serves the validators' stake changes and movements.
type StakeFlowController struct {
	flows StakeFlowReader
	names ValidatorNames
}

// NewStakeFlowController returns a controller over the movement set flows,
// naming validators through names.
func NewStakeFlowController(flows StakeFlowReader, names ValidatorNames) *StakeFlowController {
	return &StakeFlowController{flows: flows, names: names}
}

// StakeChanges serves GET /api/v1/validators/stake-changes: every
// validator's net change over the last 24 hours and 7 days, from memory.
func (h *StakeFlowController) StakeChanges(c *echo.Context) error {
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.NewStakeChanges(h.flows.Changes()))
}

// Movements serves GET /api/v1/validators/{operator}/movements?page=&limit=:
// the validator's movements of the last 7 days, newest first, from memory.
// The validator must be known; its counterparties are named.
func (h *StakeFlowController) Movements(c *echo.Context) error {
	hrp, operator, ok := bech32Address(c.Param("operator"))
	if !ok || hrp != validatorPrefix {
		return echo.NewHTTPError(http.StatusBadRequest, "operator must be a bzevaloper1 address")
	}
	page, err := parsePage(c)
	if err != nil {
		return err
	}
	if page == 0 {
		page = 1
	}
	limit := MovementsDefaultLimit
	if s := c.QueryParam("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || !isDigits(s) || n < 1 || n > MovementsMaxLimit {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("limit must be an integer from 1 to %d", MovementsMaxLimit))
		}
		limit = n
	}
	p := h.flows.Movements(operator, int(page-1)*limit, limit)

	ops := []string{operator}
	for _, m := range p.Items {
		if m.Counterparty != "" {
			ops = append(ops, m.Counterparty)
		}
	}
	monikers, err := h.names.ValidatorMonikers(c.Request().Context(), ops)
	if err != nil {
		return err
	}
	if _, ok := monikers[operator]; !ok {
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("validator %s is not known", operator))
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.NewStakeMovements(p, page, monikers))
}
