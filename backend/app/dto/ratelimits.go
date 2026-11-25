package dto

import (
	"time"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/ratelimits"
)

// RateLimitDirection is one direction of a rate limit in its current
// window. Amounts are base units (strings); net_flow is negative when more
// went the other way. used_pct is a percent with one decimal, null for a
// threshold of 0. enforced is false when the module never refuses (a channel
// value of 0).
type RateLimitDirection struct {
	MaxPercent string   `json:"max_percent"`
	Threshold  string   `json:"threshold"`
	NetFlow    string   `json:"net_flow"`
	Used       string   `json:"used"`
	Remaining  string   `json:"remaining"`
	UsedPct    *float64 `json:"used_pct"`
	Enforced   bool     `json:"enforced"`
}

// RateLimit is an item of GET /api/v1/ibc/rate-limits and of the token's
// rate_limits: the limit of a denom over a channel with the denom's symbol,
// exponent and logo, the channel's counterparty chain, and the send and
// receive sides of its current window. next_reset is null when the window's
// phase is not derivable (a duration that does not divide 24 hours).
// channel_value is the denom's supply when the window started.
type RateLimit struct {
	Denom        string             `json:"denom"`
	Symbol       *string            `json:"symbol"`
	Exponent     int                `json:"exponent"`
	LogoURL      *string            `json:"logo_url"`
	ChannelID    string             `json:"channel_id"`
	Counterparty Chain              `json:"counterparty"`
	WindowHours  uint64             `json:"window_hours"`
	NextReset    *time.Time         `json:"next_reset"`
	ChannelValue string             `json:"channel_value"`
	Inflow       string             `json:"inflow"`
	Outflow      string             `json:"outflow"`
	Send         RateLimitDirection `json:"send"`
	Recv         RateLimitDirection `json:"recv"`
}

// RateLimitList is GET /api/v1/ibc/rate-limits. supported is false (and
// items empty) for a node from before chain v8.3.0.
type RateLimitList struct {
	Supported         bool        `json:"supported"`
	Items             []RateLimit `json:"items"`
	BlacklistedDenoms []string    `json:"blacklisted_denoms"`
}

func newRateLimitDirection(d ratelimits.Direction) RateLimitDirection {
	return RateLimitDirection{
		MaxPercent: d.MaxPercent.String(), Threshold: d.Threshold.String(), NetFlow: d.NetFlow.String(),
		Used: d.Used.String(), Remaining: d.Remaining.String(), UsedPct: d.UsedPct, Enforced: d.Enforced,
	}
}

// NewRateLimit maps a limit with its denom's brief (zero when the denom is
// not known) and its channel's counterparty (zero when the channel is not).
func NewRateLimit(l ratelimits.Limit, denom repository.DenomBrief, counterparty repository.Chain) RateLimit {
	return RateLimit{
		Denom: l.Denom, Symbol: denom.Symbol, Exponent: denom.Exponent, LogoURL: denom.LogoURL,
		ChannelID: l.ChannelID, Counterparty: newChain(counterparty),
		WindowHours: l.WindowHours, NextReset: l.NextReset,
		ChannelValue: l.ChannelValue.String(), Inflow: l.Inflow.String(), Outflow: l.Outflow.String(),
		Send: newRateLimitDirection(l.Send), Recv: newRateLimitDirection(l.Recv),
	}
}
