package migrations

import "fmt"

// Partitioning of the history tables, mirrored from explorer.ensure_partitions
// (sql/000002_partitions.up.sql): one partition per PartitionSize heights,
// named <table>_p<NNNNNN> after the millions of its lower bound.
const (
	// PartitionSize is the number of heights per partition (about 70 days of
	// chain at today's block time).
	PartitionSize int64 = 1_000_000
	// PartitionLookAhead is how far above the highest known height migrate
	// creates partitions (several years at today's block time).
	PartitionLookAhead int64 = 20_000_000
)

// PartitionedTables are the explorer tables partitioned by height, in the
// order ensure_partitions creates them.
var PartitionedTables = []string{
	"blocks", "transactions", "messages", "transfers",
	"account_activity", "block_events", "order_fills",
}

// PartitionLower is the lower bound (inclusive) of the partition holding
// height; the upper bound (exclusive) is PartitionLower + PartitionSize.
func PartitionLower(height int64) int64 {
	if height < 0 {
		return 0
	}
	return height / PartitionSize * PartitionSize
}

// PartitionName is the name (without schema) of table's partition holding
// height: PartitionName("blocks", 24998316) is "blocks_p000024".
func PartitionName(table string, height int64) string {
	return fmt.Sprintf("%s_p%06d", table, PartitionLower(height)/PartitionSize)
}

// PartitionRange is the height range migrate passes to ensure_partitions
// given the highest height already known (in the sink or the explorer):
// from genesis, since the backfill floor may be genesis, to head plus the
// look-ahead. A negative head counts as 0.
func PartitionRange(head int64) (from, to int64) {
	if head < 0 {
		head = 0
	}
	return 0, head + PartitionLookAhead
}

// PartitionNames lists the partitions of table that ensure_partitions(from,
// to) creates, ascending.
func PartitionNames(table string, from, to int64) []string {
	var names []string
	for lo := PartitionLower(from); lo <= to; lo += PartitionSize {
		names = append(names, PartitionName(table, lo))
	}
	return names
}
