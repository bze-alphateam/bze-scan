package controller

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/ratelimits"
)

// RateLimitReader reads the live IBC rate limits; implemented by
// ratelimits.Service.
type RateLimitReader interface {
	Snapshot(ctx context.Context) (*ratelimits.Snapshot, error)
	// Peek is the last good read, never a node call.
	Peek() (*ratelimits.Snapshot, bool)
}

// TokenRateLimits gives the token page its denom's limits without waiting
// on the node; implemented by RateLimitController.
type TokenRateLimits interface {
	TokenRateLimits(ctx context.Context, denom string) []dto.RateLimit
}

// RateLimitCatalog reads what names a limit's denom and channel;
// implemented by repository.Explorer.
type RateLimitCatalog interface {
	IBCChannels(ctx context.Context) ([]repository.IBCChannel, error)
	DenomBriefs(ctx context.Context, denoms []string) (map[string]repository.DenomBrief, error)
}

// RateLimitController serves GET /api/v1/ibc/rate-limits and builds the
// token page's rate_limits.
type RateLimitController struct {
	limits  RateLimitReader
	catalog RateLimitCatalog
}

// NewRateLimitController returns a controller over limits and catalog.
func NewRateLimitController(limits RateLimitReader, catalog RateLimitCatalog) *RateLimitController {
	return &RateLimitController{limits: limits, catalog: catalog}
}

// RateLimits serves GET /api/v1/ibc/rate-limits?denom&channel: every limit
// (a few, so one page) with its usage in the current window, and the
// blacklisted denoms. A node from before chain v8.3.0 answers supported
// false and no items.
func (h *RateLimitController) RateLimits(c *echo.Context) error {
	ctx := c.Request().Context()
	snap, err := h.limits.Snapshot(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "the node did not answer the rate limits").Wrap(err)
	}
	denom, channel := c.QueryParam("denom"), c.QueryParam("channel")
	items, err := h.items(ctx, snap.Limits, func(l ratelimits.Limit) bool {
		return (denom == "" || l.Denom == denom) && (channel == "" || l.ChannelID == channel)
	})
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusOK, dto.RateLimitList{Supported: snap.Supported, Items: items, BlacklistedDenoms: snap.Blacklisted})
}

// items names the limits keep selects.
func (h *RateLimitController) items(ctx context.Context, limits []ratelimits.Limit, keep func(ratelimits.Limit) bool) ([]dto.RateLimit, error) {
	var picked []ratelimits.Limit
	var denoms []string
	seen := map[string]bool{}
	for _, l := range limits {
		if !keep(l) {
			continue
		}
		picked = append(picked, l)
		if !seen[l.Denom] {
			seen[l.Denom] = true
			denoms = append(denoms, l.Denom)
		}
	}
	out := make([]dto.RateLimit, 0, len(picked))
	if len(picked) == 0 {
		return out, nil
	}
	briefs, err := h.catalog.DenomBriefs(ctx, denoms)
	if err != nil {
		return nil, err
	}
	channels, err := h.catalog.IBCChannels(ctx)
	if err != nil {
		return nil, err
	}
	counterparty := make(map[string]repository.Chain, len(channels))
	for _, ch := range channels {
		if _, ok := counterparty[ch.ChannelID]; !ok {
			counterparty[ch.ChannelID] = ch.Counterparty
		}
	}
	for _, l := range picked {
		out = append(out, dto.NewRateLimit(l, briefs[l.Denom], counterparty[l.ChannelID]))
	}
	return out, nil
}

// TokenRateLimits is the token page's rate_limits: the denom's limits from
// the last good read, never a node call. No read yet, or a database
// failure, is no limits: the page never waits or fails for them.
func (h *RateLimitController) TokenRateLimits(ctx context.Context, denom string) []dto.RateLimit {
	snap, ok := h.limits.Peek()
	if !ok {
		return []dto.RateLimit{}
	}
	items, err := h.items(ctx, snap.Limits, func(l ratelimits.Limit) bool { return l.Denom == denom })
	if err != nil {
		log.WithError(err).WithField("denom", denom).Warn("token rate limits unavailable")
		return []dto.RateLimit{}
	}
	return items
}

// WithRateLimits adds the rate limits of the denom to the token page; without
// it rate_limits is always empty.
func (h *TokenController) WithRateLimits(r TokenRateLimits) *TokenController {
	h.rates = r
	return h
}

// rateLimits is the token page's rate_limits.
func (h *TokenController) rateLimits(ctx context.Context, denom string) []dto.RateLimit {
	if h.rates == nil {
		return []dto.RateLimit{}
	}
	return h.rates.TokenRateLimits(ctx, denom)
}
