package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// BondDenom is the denom whose price, supply and community pool the stats
// show.
const BondDenom = "ubze"

// TopValidators is how many of the largest bonded validators the
// validators header's top share sums.
const TopValidators = 5

// StatsReader reads the home tiles and the validators header. Implemented
// by repository.Explorer.
type StatsReader interface {
	BlockStats(ctx context.Context) (*repository.BlockStats, error)
	ChainState(ctx context.Context) (map[string]json.RawMessage, error)
	TopBondedTokens(ctx context.Context, n int) (string, error)
}

// StatsController serves GET /api/v1/stats.
type StatsController struct {
	repo StatsReader
}

// NewStatsController returns a controller over repo.
func NewStatsController(repo StatsReader) *StatsController {
	return &StatsController{repo: repo}
}

// Stats serves the home tiles: indexed columns and the chain_state rows,
// no node call.
func (h *StatsController) Stats(c *echo.Context) error {
	ctx := c.Request().Context()
	blocks, err := h.repo.BlockStats(ctx)
	if err != nil {
		return err
	}
	state, err := h.repo.ChainState(ctx)
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.NewStats(blocks, state, BondDenom))
}

// validatorsSummary reads the validators header.
func validatorsSummary(ctx context.Context, repo StatsReader) (*dto.ValidatorsSummary, error) {
	state, err := repo.ChainState(ctx)
	if err != nil {
		return nil, err
	}
	top, err := repo.TopBondedTokens(ctx, TopValidators)
	if err != nil {
		return nil, err
	}
	return dto.NewValidatorsSummary(state, top, BondDenom), nil
}

// LiveParams reads every module's live parameters as proto JSON; a module
// that failed is missing from the map and its error is returned with the
// others. Implemented by params.Cached.
type LiveParams interface {
	Params(ctx context.Context) (map[string]json.RawMessage, error)
}

// ParamChangeReader reads each module's latest parameter change.
// Implemented by repository.Explorer.
type ParamChangeReader interface {
	ParamChanges(ctx context.Context) (map[string]repository.ParamChange, error)
}

// ParamsController serves GET /api/v1/params.
type ParamsController struct {
	live    LiveParams
	changes ParamChangeReader
}

// NewParamsController returns a controller over the live parameters and
// the recorded changes.
func NewParamsController(live LiveParams, changes ParamChangeReader) *ParamsController {
	return &ParamsController{live: live, changes: changes}
}

// ModuleParams is one module of GET /api/v1/params.
type ModuleParams struct {
	Params     json.RawMessage `json:"params"`
	LastChange *LastChange     `json:"last_change"`
}

// LastChange is when a module's parameters last changed, and the proposal
// that changed them (null for a change no proposal explains).
type LastChange struct {
	Height     int64     `json:"height"`
	Time       time.Time `json:"time"`
	ProposalID *int64    `json:"proposal_id"`
}

// Params serves every module's live parameters, keyed by module, with its
// last change. A module the node failed to answer is left out; when none
// answered it is a 502.
func (h *ParamsController) Params(c *echo.Context) error {
	ctx := c.Request().Context()
	live, err := h.live.Params(ctx)
	if len(live) == 0 {
		msg := "the node answered no parameters"
		if err != nil {
			msg += ": " + err.Error()
		}
		return echo.NewHTTPError(http.StatusBadGateway, msg)
	}
	changes, err := h.changes.ParamChanges(ctx)
	if err != nil {
		return err
	}
	out := make(map[string]ModuleParams, len(live))
	for m, raw := range live {
		mp := ModuleParams{Params: raw}
		if ch, ok := changes[m]; ok {
			mp.LastChange = &LastChange{Height: ch.Height, Time: ch.Time.UTC(), ProposalID: ch.ProposalID}
		}
		out[m] = mp
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, out)
}
