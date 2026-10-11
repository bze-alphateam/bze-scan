package dto

import (
	"encoding/json"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// ChainEvent is an item of the global block-events list: a stored
// finalize-block event, the block it happened in, its classification and
// its attributes (typed-event values decoded).
type ChainEvent struct {
	Height   int64           `json:"height"`
	Seq      int64           `json:"seq"`
	Time     time.Time       `json:"time"`
	Type     string          `json:"type"`
	Kind     string          `json:"kind"`
	Category string          `json:"category"`
	Attrs    json.RawMessage `json:"attrs"`
}

// NewChainEvent maps a stored block event.
func NewChainEvent(e repository.ChainEvent) ChainEvent {
	return ChainEvent{
		Height: e.Height, Seq: e.Seq, Time: e.Time.UTC(), Type: e.Type, Kind: e.Kind, Category: e.Category,
		Attrs: jsonOr(e.Attrs, "{}"),
	}
}
