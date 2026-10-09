package writer

import (
	"fmt"
	"slices"
	"strings"
)

// Mode is how a write treats rows that already exist.
type Mode int

const (
	// ModeInsert keeps existing rows (ON CONFLICT DO NOTHING): the live
	// indexer, the backfill and the catch-up.
	ModeInsert Mode = iota
	// ModeUpdate overwrites existing rows whose values differ, then inserts
	// the missing ones: the reindex command.
	ModeUpdate
)

func (m Mode) String() string {
	switch m {
	case ModeInsert:
		return "insert"
	case ModeUpdate:
		return "update"
	default:
		return fmt.Sprintf("mode(%d)", int(m))
	}
}

// column is one column of a bulk write: its name, its type in the
// jsonb_to_recordset record, and the expression that reads it from the
// record (the name when empty).
type column struct {
	name, typ, expr string
}

func (c column) value() string {
	if c.expr != "" {
		return c.expr
	}
	return c.name
}

// table describes the bulk writes of one table from a JSON array of rows.
//
// Insert-or-not is decided by the INSERT alone: ON CONFLICT DO NOTHING
// RETURNING the key returns exactly the rows this statement inserted, also
// when another writer inserts the same key concurrently. (RETURNING
// (xmax = 0) would tell inserts from updates in one upsert, but PostgreSQL
// refuses system columns on partitioned tables.) So ModeUpdate runs an UPDATE
// of the rows that differ first, then the same INSERT, and counters are
// driven by the INSERT's rows in both modes.
type table struct {
	name string // e.g. explorer.blocks
	keys []string
	cols []column // every column written, keys included, in insert order
	// inputs are fields of the JSON rows that no column stores but column
	// expressions read.
	inputs []column
}

func (t table) recordset() string {
	defs := make([]string, 0, len(t.cols)+len(t.inputs))
	for _, c := range append(slices.Clone(t.cols), t.inputs...) {
		defs = append(defs, c.name+" "+c.typ)
	}
	return "jsonb_to_recordset($1::jsonb) AS r(" + strings.Join(defs, ", ") + ")"
}

func (t table) names() []string {
	out := make([]string, len(t.cols))
	for i, c := range t.cols {
		out[i] = c.name
	}
	return out
}

// insertSQL inserts the rows whose key is absent and returns their keys.
func (t table) insertSQL() string {
	values := make([]string, len(t.cols))
	for i, c := range t.cols {
		values[i] = c.value()
	}
	return "INSERT INTO " + t.name + " (" + strings.Join(t.names(), ", ") + ")\n" +
		"\t\tSELECT " + strings.Join(values, ", ") + "\n" +
		"\t\t  FROM " + t.recordset() + "\n" +
		"\t\tON CONFLICT (" + strings.Join(t.keys, ", ") + ") DO NOTHING\n" +
		"\t\tRETURNING " + strings.Join(t.keys, ", ")
}

// updateSQL overwrites the existing rows whose values differ from the
// incoming ones; identical rows are left alone, so a repeated reindex writes
// no new row versions.
func (t table) updateSQL() string {
	isKey := map[string]bool{}
	for _, k := range t.keys {
		isKey[k] = true
	}
	var data, incoming, current, match, selected []string
	for _, c := range t.cols {
		selected = append(selected, c.value()+" AS "+c.name)
		if isKey[c.name] {
			match = append(match, "t."+c.name+" = n."+c.name)
			continue
		}
		data = append(data, c.name)
		incoming = append(incoming, "n."+c.name)
		current = append(current, "t."+c.name)
	}
	return "UPDATE " + t.name + " AS t\n" +
		"\t\tSET (" + strings.Join(data, ", ") + ") = ROW(" + strings.Join(incoming, ", ") + ")\n" +
		"\t\tFROM (SELECT " + strings.Join(selected, ", ") + "\n" +
		"\t\t  FROM " + t.recordset() + ") AS n\n" +
		"\t\tWHERE " + strings.Join(match, " AND ") + "\n" +
		"\t\t  AND (" + strings.Join(current, ", ") + ") IS DISTINCT FROM (" + strings.Join(incoming, ", ") + ")"
}

// signaturesPowerPct is the share, in percent, of the bonded validators'
// tokens whose validator signed, signers being an array of the consensus
// addresses in the commit, with the column's three decimals (so a reindex
// finds nothing to rewrite). It reads the current validators table: exact
// for a live block, approximate for history. NULL before the first state
// sync.
func signaturesPowerPct(signers string) string {
	return "(SELECT round(100 * coalesce(sum(v.tokens) FILTER (WHERE v.consensus_address = ANY(" + signers + ")), 0)" +
		" / NULLIF(sum(v.tokens), 0), 3) FROM explorer.validators v WHERE v.status = 'bonded')"
}

// The tables the writers fill. block_time_ms is not a column of the blocks
// write: it is derived from the previous block by blockTimesSQL (and by the
// live writer's insert).
var (
	blocksTable = table{
		name: "explorer.blocks",
		keys: []string{"height"},
		cols: []column{
			{name: "height", typ: "bigint"},
			{name: "time", typ: "timestamptz"},
			{name: "tx_count", typ: "integer"},
			{name: "tx_failed_count", typ: "integer"},
			{name: "hash", typ: "text"},
			{name: "proposer_cons_address", typ: "text"},
			{name: "size_bytes", typ: "integer"},
			{name: "minted", typ: "numeric"},
			{name: "inflation", typ: "numeric"},
			{name: "fees_distributed", typ: "jsonb", expr: "NULLIF(fees_distributed, 'null'::jsonb)"},
			{name: "signatures_count", typ: "integer"},
			{name: "signatures_power_pct", typ: "numeric", expr: signaturesPowerPct("signers")},
		},
		inputs: []column{{name: "signers", typ: "text[]"}},
	}
	transactionsTable = table{
		name: "explorer.transactions",
		keys: []string{"height", "tx_index"},
		cols: []column{
			{name: "height", typ: "bigint"},
			{name: "tx_index", typ: "integer"},
			{name: "hash", typ: "text"},
			{name: "time", typ: "timestamptz"},
			{name: "success", typ: "boolean"},
			{name: "code", typ: "integer"},
			{name: "codespace", typ: "text"},
			{name: "error_log", typ: "text"},
			{name: "gas_wanted", typ: "bigint"},
			{name: "gas_used", typ: "bigint"},
			{name: "fee", typ: "jsonb"},
			{name: "fee_payer", typ: "text"},
			{name: "signers", typ: "text[]"},
			{name: "memo", typ: "text"},
			{name: "msg_count", typ: "integer"},
			{name: "msg_types", typ: "text[]"},
		},
	}
	messagesTable = table{
		name: "explorer.messages",
		keys: []string{"height", "tx_index", "msg_index"},
		cols: []column{
			{name: "height", typ: "bigint"},
			{name: "tx_index", typ: "integer"},
			{name: "msg_index", typ: "integer"},
			{name: "type_url", typ: "text"},
			{name: "sender", typ: "text"},
			{name: "module", typ: "text"},
			{name: "events", typ: "jsonb"},
			{name: "body", typ: "jsonb", expr: "NULLIF(body, 'null'::jsonb)"},
		},
	}
	validatorEventsTable = table{
		name: "explorer.validator_events",
		keys: []string{"height", "tx_index", "seq"},
		cols: []column{
			{name: "height", typ: "bigint"},
			{name: "tx_index", typ: "integer"},
			{name: "seq", typ: "integer"},
			{name: "operator_address", typ: "text", expr: validatorEventOperator},
			{name: "kind", typ: "text"},
			{name: "details", typ: "jsonb", expr: validatorEventDetails},
			{name: "time", typ: "timestamptz"},
		},
		inputs: []column{
			{name: "consensus_address", typ: "text"},
			{name: "commission_from", typ: "boolean"},
		},
	}
)

// validatorEventOperator is the operator of a validator_events row: the
// transformer's, else (a slash) the one the row already holds, else the
// validator with the slash's consensus address, else ” until the state
// sync resolves it.
const validatorEventOperator = `COALESCE(NULLIF(r.operator_address, ''),
		(SELECT e.operator_address FROM explorer.validator_events e
		  WHERE e.height = r.height AND e.tx_index = r.tx_index AND e.seq = r.seq AND e.operator_address <> ''),
		(SELECT v.operator_address FROM explorer.validators v
		  WHERE v.consensus_address = r.consensus_address ORDER BY v.operator_address LIMIT 1),
		'')`

// validatorEventDetails adds "from" to a commission change: the value the
// row already holds (so a reindex keeps the live path's exact one), else the
// validator's stored commission rate.
const validatorEventDetails = `CASE WHEN r.commission_from THEN
		jsonb_build_object('from', COALESCE(
			(SELECT e.details->'from' FROM explorer.validator_events e
			  WHERE e.height = r.height AND e.tx_index = r.tx_index AND e.seq = r.seq),
			to_jsonb((SELECT v.commission_rate::text FROM explorer.validators v
			           WHERE v.operator_address = r.operator_address)))) || r.details
		ELSE NULLIF(r.details, 'null'::jsonb) END`
