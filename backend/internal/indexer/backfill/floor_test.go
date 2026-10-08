package backfill_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/backfill"
)

func TestFloorHeightAndGenesis(t *testing.T) {
	n := newMockNode()
	for _, c := range []struct{ in, want int64 }{{1, 1}, {0, 1}, {19_560_001, 19_560_001}} {
		got, err := backfill.ResolveFloor(context.Background(), backfill.Floor{Height: c.in}, n, nil, 25_000_000)
		require.NoError(t, err)
		assert.Equal(t, c.want, got)
	}
	assert.Empty(t, n.blockCalls(), "a height needs no probe")
}

func TestFloorDateIsTheFirstBlockAtOrAfterIt(t *testing.T) {
	const ceiling = 25_000_000
	cases := map[string]struct {
		date time.Time
		want int64
	}{
		"exactly a block's time":   {blockTime(12_345_678), 12_345_678},
		"between two blocks":       {blockTime(12_345_678).Add(-time.Second), 12_345_678},
		"just after a block":       {blockTime(12_345_678).Add(time.Second), 12_345_679},
		"before genesis":           {genesisTime.Add(-24 * time.Hour), 1},
		"genesis itself":           {genesisTime, 1},
		"the ceiling's block":      {blockTime(ceiling), ceiling},
		"after the ceiling's time": {blockTime(ceiling).Add(time.Second), ceiling + 1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			n := newMockNode()
			got, err := backfill.ResolveFloor(context.Background(), backfill.Floor{Date: c.date}, n, nil, ceiling)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
			assert.LessOrEqual(t, len(n.blockCalls()), 26, "a binary search: about 25 probes")
		})
	}
}

func TestFloorDateProbeFallsBackToTheRetryNode(t *testing.T) {
	archive, retry := newMockNode(), newMockNode()
	archive.fail(500, errUnavailable)
	got, err := backfill.ResolveFloor(context.Background(), backfill.Floor{Date: blockTime(300)}, archive, retry, 1000)
	require.NoError(t, err)
	assert.Equal(t, int64(300), got)
	assert.Equal(t, []int64{500}, retry.blockCalls())

	archive.fail(500, errUnavailable)
	retry.fail(500, errUnavailable)
	_, err = backfill.ResolveFloor(context.Background(), backfill.Floor{Date: blockTime(300)}, archive, retry, 1000)
	require.ErrorIs(t, err, errUnavailable)
}
