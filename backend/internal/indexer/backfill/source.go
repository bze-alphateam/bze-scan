package backfill

import (
	"context"
)

// PresenceWindow is the number of heights one presence query covers.
const PresenceWindow = 1000

// Presence lists the heights in [lo, hi] already present in explorer.blocks.
type Presence interface {
	PresentHeights(ctx context.Context, lo, hi int64) ([]int64, error)
}

// rangeSource walks [low, high] in one direction and skips the heights
// present, reading presence one window at a time.
type rangeSource struct {
	next, end int64
	step      int64
	present   Presence
	// loaded is the far end of the window already read; skip holds its
	// present heights.
	loaded int64
	skip   map[int64]bool
}

// Descending walks from high down to low, both included, skipping the
// heights present (nil present skips nothing). Empty when high < low.
func Descending(high, low int64, present Presence) HeightSource {
	return &rangeSource{next: high, end: low, step: -1, present: present, loaded: high + 1}
}

// Ascending walks from low up to high, both included, skipping the heights
// present (nil present skips nothing). Empty when high < low.
func Ascending(low, high int64, present Presence) HeightSource {
	return &rangeSource{next: low, end: high, step: 1, present: present, loaded: low - 1}
}

// Next implements HeightSource.
func (s *rangeSource) Next(ctx context.Context) (int64, bool, error) {
	for s.within(s.next) {
		h := s.next
		if s.present != nil && !s.isLoaded(h) {
			if err := s.load(ctx, h); err != nil {
				return 0, false, err
			}
		}
		s.next += s.step
		if s.skip[h] {
			continue
		}
		return h, true, nil
	}
	return 0, false, nil
}

func (s *rangeSource) within(h int64) bool {
	if s.step < 0 {
		return h >= s.end
	}
	return h <= s.end
}

func (s *rangeSource) isLoaded(h int64) bool {
	if s.step < 0 {
		return h >= s.loaded
	}
	return h <= s.loaded
}

// load reads the presence of the window starting at h in walking order.
func (s *rangeSource) load(ctx context.Context, h int64) error {
	far := h + s.step*(PresenceWindow-1)
	if s.step < 0 {
		far = max(far, s.end)
	} else {
		far = min(far, s.end)
	}
	lo, hi := min(h, far), max(h, far)
	heights, err := s.present.PresentHeights(ctx, lo, hi)
	if err != nil {
		return err
	}
	s.skip = make(map[int64]bool, len(heights))
	for _, p := range heights {
		s.skip[p] = true
	}
	s.loaded = far
	return nil
}
