package migrations

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPartitionLower(t *testing.T) {
	for _, tc := range []struct{ height, want int64 }{
		{-5, 0},
		{0, 0},
		{1, 0},
		{999_999, 0},
		{1_000_000, 1_000_000},
		{24_998_316, 24_000_000},
		{25_000_000, 25_000_000},
	} {
		assert.Equal(t, tc.want, PartitionLower(tc.height), "height %d", tc.height)
	}
}

func TestPartitionName(t *testing.T) {
	assert.Equal(t, "blocks_p000000", PartitionName("blocks", 0))
	assert.Equal(t, "blocks_p000000", PartitionName("blocks", 999_999))
	assert.Equal(t, "blocks_p000001", PartitionName("blocks", 1_000_000))
	assert.Equal(t, "blocks_p000024", PartitionName("blocks", 24_998_316))
	assert.Equal(t, "account_activity_p000044", PartitionName("account_activity", 44_998_316))
	assert.Equal(t, "order_fills_p001234", PartitionName("order_fills", 1_234_000_000))
}

func TestPartitionRange(t *testing.T) {
	from, to := PartitionRange(0)
	assert.Equal(t, int64(0), from)
	assert.Equal(t, int64(20_000_000), to)

	from, to = PartitionRange(24_998_316)
	assert.Equal(t, int64(0), from)
	assert.Equal(t, int64(44_998_316), to)

	from, to = PartitionRange(-1)
	assert.Equal(t, int64(0), from)
	assert.Equal(t, int64(20_000_000), to)
}

func TestPartitionNamesCoverTheRangeInclusive(t *testing.T) {
	// Like generate_series in ensure_partitions: every partition whose lower
	// bound is <= to, so the partition holding `to` is included.
	assert.Equal(t,
		[]string{"blocks_p000000", "blocks_p000001", "blocks_p000002"},
		PartitionNames("blocks", 0, 2_000_000))
	assert.Equal(t,
		[]string{"blocks_p000000", "blocks_p000001"},
		PartitionNames("blocks", 0, 1_999_999))
	assert.Equal(t,
		[]string{"blocks_p000024", "blocks_p000025"},
		PartitionNames("blocks", 24_998_316, 25_000_001))
	from, to := PartitionRange(24_998_316)
	assert.Len(t, PartitionNames("messages", from, to), 45)
}
