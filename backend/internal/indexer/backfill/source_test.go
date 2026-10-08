package backfill_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/backfill"
)

func drain(t *testing.T, s backfill.HeightSource) []int64 {
	t.Helper()
	var out []int64
	for {
		h, ok, err := s.Next(context.Background())
		require.NoError(t, err)
		if !ok {
			return out
		}
		out = append(out, h)
	}
}

// presenceLog records the windows asked for.
type presenceLog struct {
	*mockStore
	windows [][2]int64
}

func (p *presenceLog) PresentHeights(ctx context.Context, lo, hi int64) ([]int64, error) {
	p.windows = append(p.windows, [2]int64{lo, hi})
	return p.mockStore.PresentHeights(ctx, lo, hi)
}

func TestDescendingSkipsThePresentHeights(t *testing.T) {
	store := newMockStore(0)
	for _, h := range []int64{9, 7, 1} {
		store.present[h] = true
	}
	assert.Equal(t, []int64{10, 8, 6, 5, 4, 3, 2}, drain(t, backfill.Descending(10, 1, store)))
}

func TestAscendingSkipsThePresentHeights(t *testing.T) {
	store := newMockStore(0)
	store.present[3] = true
	assert.Equal(t, []int64{1, 2, 4}, drain(t, backfill.Ascending(1, 4, store)))
}

func TestPresenceIsReadOneWindowAtATime(t *testing.T) {
	p := &presenceLog{mockStore: newMockStore(0)}
	got := drain(t, backfill.Descending(2500, 1, p))
	assert.Len(t, got, 2500)
	assert.Equal(t, [][2]int64{{1501, 2500}, {501, 1500}, {1, 500}}, p.windows)

	p = &presenceLog{mockStore: newMockStore(0)}
	drain(t, backfill.Ascending(10, 1200, p))
	assert.Equal(t, [][2]int64{{10, 1009}, {1010, 1200}}, p.windows)
}

func TestEmptyRanges(t *testing.T) {
	assert.Empty(t, drain(t, backfill.Descending(4, 5, nil)))
	assert.Empty(t, drain(t, backfill.Ascending(5, 4, nil)))
	assert.Equal(t, []int64{5}, drain(t, backfill.Descending(5, 5, nil)))
}
