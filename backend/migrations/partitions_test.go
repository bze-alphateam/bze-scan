package migrations_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bze-alphateam/bze-scan/backend/migrations"
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
		assert.Equal(t, tc.want, migrations.PartitionLower(tc.height), "height %d", tc.height)
	}
}

func TestPartitionName(t *testing.T) {
	assert.Equal(t, "blocks_p000000", migrations.PartitionName("blocks", 0))
	assert.Equal(t, "blocks_p000000", migrations.PartitionName("blocks", 999_999))
	assert.Equal(t, "blocks_p000001", migrations.PartitionName("blocks", 1_000_000))
	assert.Equal(t, "blocks_p000024", migrations.PartitionName("blocks", 24_998_316))
	assert.Equal(t, "account_activity_p000044", migrations.PartitionName("account_activity", 44_998_316))
	assert.Equal(t, "order_fills_p001234", migrations.PartitionName("order_fills", 1_234_000_000))
}

func TestPartitionsUpTo(t *testing.T) {
	names := migrations.PartitionNames("blocks", 0, migrations.PartitionsUpTo)
	assert.Len(t, names, 50)
	assert.Equal(t, "blocks_p000000", names[0])
	assert.Equal(t, "blocks_p000049", names[49])
	assert.Equal(t, migrations.PartitionName("blocks", 25_000_000), names[25])
	assert.Equal(t, migrations.PartitionName("blocks", 49_999_999), names[49])
}

func TestPartitionNamesCoverTheRangeInclusive(t *testing.T) {
	// Like generate_series in ensure_partitions: every partition whose lower
	// bound is <= to, so the partition holding `to` is included.
	assert.Equal(t,
		[]string{"blocks_p000000", "blocks_p000001", "blocks_p000002"}, migrations.PartitionNames("blocks", 0, 2_000_000))
	assert.Equal(t,
		[]string{"blocks_p000000", "blocks_p000001"}, migrations.PartitionNames("blocks", 0, 1_999_999))
	assert.Equal(t,
		[]string{"blocks_p000024", "blocks_p000025"}, migrations.PartitionNames("blocks", 24_998_316, 25_000_001))
}
