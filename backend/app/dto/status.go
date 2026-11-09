package dto

import (
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/status"
)

// Status is the body of GET /api/v1/status.
type Status struct {
	LiveFill LiveFill `json:"live_fill"`
	BackFill BackFill `json:"back_fill"`
}

// LiveFill is whether the explorer keeps up with the chain. CheckedAt is
// null before the first check completed. SafeHeight is the height a reader
// may read up to: no height at or below it is waiting for a repair.
type LiveFill struct {
	Healthy       bool       `json:"healthy"`
	CheckedAt     *time.Time `json:"checked_at"`
	DBHeight      *int64     `json:"db_height"`
	SafeHeight    *int64     `json:"safe_height"`
	NodeHeight    *int64     `json:"node_height"`
	ArchiveHeight *int64     `json:"archive_height"`
}

// BackFill is the progress of the history back-fill: Status is "finished"
// or "in_progress"; OldestHeight is the lowest indexed height.
type BackFill struct {
	Status       string `json:"status"`
	OldestHeight *int64 `json:"oldest_height"`
}

// NewStatus maps a checker snapshot to the response.
func NewStatus(s status.Snapshot) Status {
	out := Status{
		LiveFill: LiveFill{
			Healthy:       s.Healthy,
			DBHeight:      s.DBHeight,
			SafeHeight:    s.SafeHeight,
			NodeHeight:    s.NodeHeight,
			ArchiveHeight: s.ArchiveHeight,
		},
		BackFill: BackFill{Status: s.BackFill, OldestHeight: s.OldestHeight},
	}
	if !s.CheckedAt.IsZero() {
		at := s.CheckedAt.UTC()
		out.LiveFill.CheckedAt = &at
	}
	return out
}
