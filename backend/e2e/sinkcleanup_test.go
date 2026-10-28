//go:build e2e

package e2e_test

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/cli"
)

// cascadingSinkSchema is the stock sink schema as production runs it: every
// foreign key ON DELETE CASCADE, plus the aggregator's events.status column.
func cascadingSinkSchema(t *testing.T, stock string) string {
	t.Helper()
	for old, n := range map[string]int{
		"REFERENCES blocks(rowid)":     2,
		"REFERENCES tx_results(rowid)": 1,
		"REFERENCES events(rowid)":     1,
	} {
		require.Equal(t, n, strings.Count(stock, old), old)
		stock = strings.ReplaceAll(stock, old, old+" ON DELETE CASCADE")
	}
	const eventsEnd = "  type VARCHAR NOT NULL\n);"
	require.Equal(t, 1, strings.Count(stock, eventsEnd))
	return strings.Replace(stock, eventsEnd, "  type VARCHAR NOT NULL,\n  status SMALLINT NOT NULL DEFAULT 0\n);", 1)
}

// sinkDatabase is a fresh database with schema as its sink and the explorer
// migrated on top. Heights lo..hi are logged three days ago, hi+1..hi+10
// now; each has a transaction, a block event and a transaction event, and
// two attributes per event.
func sinkDatabase(t *testing.T, schema string, lo, hi int64) (string, *sql.DB) {
	t.Helper()
	url := freshDatabase(t, false)
	db := connect(t, url)
	_, err := db.Exec(schema)
	require.NoError(t, err)
	migrateUp(t, url)
	_, err = db.Exec(`
		WITH b AS (
		  INSERT INTO public.blocks (height, chain_id, created_at)
		  SELECT h, 'beezee-1', CASE WHEN h <= $2 THEN now() - interval '72 hours' ELSE now() END
		  FROM generate_series($1::bigint, $2::bigint + 10) h
		  RETURNING rowid
		), tx AS (
		  INSERT INTO public.tx_results (block_id, index, created_at, tx_hash, tx_result)
		  SELECT rowid, 0, now(), md5(rowid::text), '\x00'::bytea FROM b
		  RETURNING rowid, block_id
		), ev AS (
		  INSERT INTO public.events (block_id, tx_id, type)
		  SELECT rowid, NULL, 'burn' FROM b
		  UNION ALL SELECT block_id, rowid, 'transfer' FROM tx
		  RETURNING rowid, type
		)
		INSERT INTO public.attributes (event_id, key, composite_key, value)
		SELECT rowid, k, type || '.' || k, 'v' FROM ev, unnest(ARRAY['amount', 'sender']) k`, lo, hi)
	require.NoError(t, err)
	return url, db
}

func setLiveCursor(t *testing.T, db *sql.DB, h int64) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO explorer.indexer_state (key, value, updated_at) VALUES ('last_indexed_height', $1::bigint::text, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, h)
	require.NoError(t, err)
}

// sinkCleanup runs the sink-cleanup command and returns its exit code and
// output.
func sinkCleanup(t *testing.T, url string, args ...string) (int, string) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", url)
	t.Setenv("SINK_RETENTION", "")
	t.Setenv("SINK_CLEANUP_BATCH", "4")
	t.Setenv("LOG_LEVEL", "warn")
	root := cli.NewRootCmd()
	root.SetArgs(append([]string{"sink-cleanup"}, args...))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	return cli.ExitCode(root.Execute()), out.String()
}

// sinkRows is the stored heights and the rows per child table, plus the
// orphans: child rows whose parent is gone.
type sinkRows struct {
	Heights                        []string
	TxResults, Events, Attributes  int
	OrphanTxs, OrphanEvents        int
	OrphanTxEvents, OrphanAttrRows int
}

func readSink(t *testing.T, db *sql.DB) sinkRows {
	t.Helper()
	var r sinkRows
	r.Heights = queryStrings(t, db, `SELECT lpad(height::text, 6, '0') FROM public.blocks`)
	require.NoError(t, db.QueryRow(`SELECT
		(SELECT count(*) FROM public.tx_results),
		(SELECT count(*) FROM public.events),
		(SELECT count(*) FROM public.attributes),
		(SELECT count(*) FROM public.tx_results t WHERE NOT EXISTS (SELECT 1 FROM public.blocks b WHERE b.rowid = t.block_id)),
		(SELECT count(*) FROM public.events e WHERE NOT EXISTS (SELECT 1 FROM public.blocks b WHERE b.rowid = e.block_id)),
		(SELECT count(*) FROM public.events e WHERE e.tx_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM public.tx_results t WHERE t.rowid = e.tx_id)),
		(SELECT count(*) FROM public.attributes a WHERE NOT EXISTS (SELECT 1 FROM public.events e WHERE e.rowid = a.event_id))`).
		Scan(&r.TxResults, &r.Events, &r.Attributes, &r.OrphanTxs, &r.OrphanEvents, &r.OrphanTxEvents, &r.OrphanAttrRows))
	return r
}

// heightsFrom lists lo..hi as readSink prints them.
func heightsFrom(lo, hi int64) []string {
	var out []string
	for h := lo; h <= hi; h++ {
		out = append(out, fmt.Sprintf("%06d", h))
	}
	return out
}

// kept is what readSink returns when heights lo..hi are left.
func kept(lo, hi int64) sinkRows {
	n := int(hi - lo + 1)
	return sinkRows{Heights: heightsFrom(lo, hi), TxResults: n, Events: 2 * n, Attributes: 4 * n}
}

func TestSinkCleanupDeletesTheHeightsBelowTheCutOff(t *testing.T) {
	stockBytes, err := os.ReadFile(sinkSchemaFile)
	require.NoError(t, err)
	stock := string(stockBytes)

	for name, schema := range map[string]string{
		"stock schema":     stock,
		"cascading schema": cascadingSinkSchema(t, stock),
	} {
		t.Run(name, func(t *testing.T) {
			// Heights 100..119 are three days old, 120..129 new.
			url, db := sinkDatabase(t, schema, 100, 119)
			require.Equal(t, kept(100, 129), readSink(t, db))

			// Before the live indexer has written, nothing goes.
			code, out := sinkCleanup(t, url, "--retention", "48h")
			require.Equal(t, 0, code, out)
			assert.Contains(t, out, "nothing to delete")
			require.Equal(t, kept(100, 129), readSink(t, db))

			// The cursor limits the cut-off: 1110 - 1000 = 110.
			setLiveCursor(t, db, 1110)
			code, out = sinkCleanup(t, url, "--retention", "48h", "--dry-run")
			require.Equal(t, 0, code, out)
			assert.Equal(t, "cut-off 110: heights 100..109 (by age 119, by cursor 110)\n"+
				"would delete attributes=40 events=20 tx_results=10 blocks=10\n", out)
			require.Equal(t, kept(100, 129), readSink(t, db), "a dry run deletes nothing")

			code, out = sinkCleanup(t, url, "--retention", "48h")
			require.Equal(t, 0, code, out)
			assert.Contains(t, out, "deleted attributes=40 events=20 tx_results=10 blocks=10")
			require.Equal(t, kept(110, 129), readSink(t, db))

			// Then the age: 119 is the newest block older than 48 hours
			// and is kept, in batches of 4 heights.
			setLiveCursor(t, db, 5000)
			code, out = sinkCleanup(t, url, "--retention", "48h")
			require.Equal(t, 0, code, out)
			assert.Contains(t, out, "cut-off 119")
			assert.Contains(t, out, "deleted attributes=36 events=18 tx_results=9 blocks=9")
			require.Equal(t, kept(119, 129), readSink(t, db))

			// Once the cut-off is the oldest height, a run is a no-op.
			code, out = sinkCleanup(t, url, "--retention", "48h")
			require.Equal(t, 0, code, out)
			assert.Contains(t, out, "nothing to delete")
			require.Equal(t, kept(119, 129), readSink(t, db))
		})
	}
}

func TestSinkCleanupRefusesWithoutARetention(t *testing.T) {
	stockBytes, err := os.ReadFile(sinkSchemaFile)
	require.NoError(t, err)
	url, db := sinkDatabase(t, string(stockBytes), 100, 119)
	setLiveCursor(t, db, 5000)

	code, out := sinkCleanup(t, url)
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "SINK_RETENTION")
	require.Equal(t, kept(100, 129), readSink(t, db))
}
