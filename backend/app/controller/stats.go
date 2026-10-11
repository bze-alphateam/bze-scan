package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/dailystats"
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

// DefaultDays and MaxDays bound the closed days of the stats' daily
// series (?days=).
const (
	DefaultDays = 30
	MaxDays     = 365
)

// DailyStatsReader reads the home charts' days. Implemented by
// repository.Explorer.
type DailyStatsReader interface {
	// DailyStats returns the stored rows of the last days closed UTC days,
	// oldest first.
	DailyStats(ctx context.Context, days int) ([]dailystats.Stats, error)
	// TodayStats computes the current UTC day so far; nil while it has no
	// indexed block.
	TodayStats(ctx context.Context) (*dailystats.Stats, error)
}

// StatsController serves GET /api/v1/stats.
type StatsController struct {
	repo  StatsReader
	daily DailyStatsReader
}

// NewStatsController returns a controller over repo; a nil daily answers
// an empty series and no today.
func NewStatsController(repo StatsReader, daily DailyStatsReader) *StatsController {
	return &StatsController{repo: repo, daily: daily}
}

// Stats serves the home tiles and the daily series: indexed columns, the
// chain_state rows and daily_stats, no node call.
func (h *StatsController) Stats(c *echo.Context) error {
	ctx := c.Request().Context()
	days := DefaultDays
	if s := c.QueryParam("days"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > MaxDays {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("days must be an integer from 1 to %d", MaxDays))
		}
		days = n
	}
	blocks, err := h.repo.BlockStats(ctx)
	if err != nil {
		return err
	}
	state, err := h.repo.ChainState(ctx)
	if err != nil {
		return err
	}
	out := dto.NewStats(blocks, state, BondDenom)
	if h.daily != nil {
		series, err := h.daily.DailyStats(ctx, days)
		if err != nil {
			return err
		}
		for _, d := range series {
			out.Daily = append(out.Daily, dto.NewDayStats(d))
		}
		today, err := h.daily.TodayStats(ctx)
		if err != nil {
			return err
		}
		if today != nil {
			t := dto.NewDayStats(*today)
			out.Today = &t
		}
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, out)
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
