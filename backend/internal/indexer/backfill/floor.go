package backfill

import (
	"context"
	"fmt"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// Floor is the lowest height the main job indexes: a height (1 is genesis),
// or the first block at or after a date.
type Floor struct {
	// Height is used when Date is zero.
	Height int64
	// Date, when set, is resolved to the first block whose header time is at
	// or after it.
	Date time.Time
}

// BlockReader reads a block by height; *node.Client satisfies it.
type BlockReader interface {
	Block(ctx context.Context, height int64) (*node.Block, []byte, error)
}

// ResolveFloor returns the floor height for a main job whose ceiling is
// ceiling. A date is resolved by a binary search over the header times of
// archive blocks in [1, ceiling], by height only (about 25 probes on
// mainnet); retry is asked when archive fails a probe. A date after the
// ceiling's block resolves to ceiling+1: nothing to index.
func ResolveFloor(ctx context.Context, f Floor, archive, retry BlockReader, ceiling int64) (int64, error) {
	if f.Date.IsZero() {
		return max(f.Height, 1), nil
	}
	if retry == nil {
		retry = archive
	}
	timeAt := func(h int64) (time.Time, error) {
		b, _, err := archive.Block(ctx, h)
		if err != nil {
			if b, _, err = retry.Block(ctx, h); err != nil {
				return time.Time{}, fmt.Errorf("resolve floor date: block %d: %w", h, err)
			}
		}
		return b.Time, nil
	}
	// Invariant: every height below lo is before the date, and the block at
	// hi+1 (or none) is at or after it.
	lo, hi := int64(1), ceiling
	for lo <= hi {
		mid := lo + (hi-lo)/2
		t, err := timeAt(mid)
		if err != nil {
			return 0, err
		}
		if t.Before(f.Date) {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return lo, nil
}
