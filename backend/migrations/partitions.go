package migrations

import "fmt"

// Partitioning of the history tables, mirrored from explorer.ensure_partitions
// (000006_functions.up.sql): one partition per PartitionSize heights, named
// <table>_p<NNNNNN> after the millions of its lower bound.
const (
	// PartitionSize is the number of heights per partition (about 70 days of
	// chain at today's block time).
	PartitionSize int64 = 1_000_000
	// PartitionsUpTo is the highest height `migrate up` creates partitions
	// for: 50 partitions per table, heights 0 to 49,999,999 (about four years
	// beyond today's 25 M at the current block time). Empty partitions are
	// cheap; the live indexer tops them up as it climbs.
	PartitionsUpTo int64 = 50*PartitionSize - 1
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

// PartitionNames lists the partitions of table that ensure_partitions(from,
// to) creates, ascending.
func PartitionNames(table string, from, to int64) []string {
	var names []string
	for lo := PartitionLower(from); lo <= to; lo += PartitionSize {
		names = append(names, PartitionName(table, lo))
	}
	return names
}
