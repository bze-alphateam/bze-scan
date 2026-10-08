package migrations

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fileNameRe = regexp.MustCompile(`^\d{6}_[a-z0-9_]+\.(up|down)\.sql$`)

func TestMigrationFilesAreWellNamed(t *testing.T) {
	names, err := fs.Glob(files, "*")
	require.NoError(t, err)
	require.NotEmpty(t, names)
	for _, n := range names {
		assert.Regexp(t, fileNameRe, n)
	}
}

func readAll(t *testing.T, r io.ReadCloser) string {
	t.Helper()
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(b)
}

// TestMigrationSetLoadsInOrder walks the embedded source as golang-migrate
// does: versions contiguous from 1, each with a non-empty up and down file.
func TestMigrationSetLoadsInOrder(t *testing.T) {
	src, err := Source()
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	var idents []string
	v, err := src.First()
	require.NoError(t, err)
	for {
		up, ident, err := src.ReadUp(v)
		require.NoError(t, err, "version %d has no up migration", v)
		assert.NotEmpty(t, strings.TrimSpace(readAll(t, up)), "version %d up is empty", v)
		down, _, err := src.ReadDown(v)
		require.NoError(t, err, "version %d has no down migration", v)
		assert.NotEmpty(t, strings.TrimSpace(readAll(t, down)), "version %d down is empty", v)
		assert.Equal(t, uint(len(idents)+1), v, "versions must be contiguous from 1")
		idents = append(idents, ident)

		next, err := src.Next(v)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		require.NoError(t, err)
		v = next
	}

	assert.Equal(t, []string{
		"schema", "notify_trigger", "history_tables", "state_tables", "operational", "functions",
	}, idents)
}

func readMigration(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(files, name)
	require.NoError(t, err)
	return string(b)
}

// createdTables returns the tables a migration creates and, among them, the
// ones declared PARTITION BY RANGE (height).
func createdTables(t *testing.T, name string) (all, partitioned []string) {
	t.Helper()
	for _, chunk := range strings.Split(readMigration(t, name), "CREATE TABLE explorer.")[1:] {
		table := chunk[:strings.IndexAny(chunk, " (")]
		all = append(all, table)
		// The column list closes on the first line starting with ")"
		// (column comments may contain semicolons).
		closing := chunk[strings.Index(chunk, "\n)")+1:]
		closing = closing[:strings.Index(closing, "\n")]
		if closing == ") PARTITION BY RANGE (height);" {
			partitioned = append(partitioned, table)
		}
	}
	return all, partitioned
}

// TestPartitionedTablesMatchTheSQL keeps the Go table list in step with the
// history tables migration and with ensure_partitions.
func TestPartitionedTablesMatchTheSQL(t *testing.T) {
	all, partitioned := createdTables(t, "000003_history_tables.up.sql")
	assert.Equal(t, PartitionedTables, all)
	assert.Equal(t, PartitionedTables, partitioned)
	for _, name := range []string{"000004_state_tables.up.sql", "000005_operational.up.sql"} {
		_, partitioned := createdTables(t, name)
		assert.Empty(t, partitioned, name)
	}

	fn := readMigration(t, "000006_functions.up.sql")
	m := regexp.MustCompile(`ARRAY\[([^\]]+)\]`).FindStringSubmatch(fn)
	require.NotNil(t, m)
	var listed []string
	for _, q := range strings.Split(m[1], ",") {
		listed = append(listed, strings.Trim(strings.TrimSpace(q), "'"))
	}
	assert.Equal(t, PartitionedTables, listed)
	assert.Contains(t, fn, "generate_series((p_from / 1000000) * 1000000, p_to, 1000000)")
	assert.Contains(t, fn, "lpad((lo / 1000000)::text, 6, '0')")
}

// TestDownMigrationsDropWhatUpCreates checks that every table an up file
// creates is dropped by its down file.
func TestDownMigrationsDropWhatUpCreates(t *testing.T) {
	for _, base := range []string{"000003_history_tables", "000004_state_tables", "000005_operational"} {
		all, _ := createdTables(t, base+".up.sql")
		down := readMigration(t, base+".down.sql")
		for _, table := range all {
			assert.Contains(t, down, "DROP TABLE IF EXISTS explorer."+table+";", base)
		}
	}
}

func TestNotifyTriggerUsesTheChannel(t *testing.T) {
	up := readMigration(t, "000002_notify_trigger.up.sql")
	assert.Contains(t, up, "pg_notify('"+NotifyChannel+"', NEW.height::text)")
	assert.Contains(t, up, "AFTER INSERT ON public.blocks")
	down := readMigration(t, "000002_notify_trigger.down.sql")
	assert.Contains(t, down, "DROP TRIGGER IF EXISTS trg_notify_block ON public.blocks;")
}

// TestSinkSchemaIsLeftAlone guards the rule that the only object attached to
// the sink's public schema is the notification trigger, and that no down
// migration touches the sink's tables.
func TestSinkSchemaIsLeftAlone(t *testing.T) {
	names, err := fs.Glob(files, "*.sql")
	require.NoError(t, err)
	publicRe := regexp.MustCompile(`(?i)\bpublic\.\w+`)
	for _, name := range names {
		sql := readMigration(t, name)
		for _, ref := range publicRe.FindAllString(sql, -1) {
			assert.Equal(t, "public.blocks", ref, "%s references %s", name, ref)
		}
		assert.NotRegexp(t, `(?i)(ALTER|DROP|TRUNCATE)\s+TABLE\s+(IF\s+EXISTS\s+)?(public\.)?(blocks|tx_results|events|attributes)\b`, sql, name)
		assert.NotRegexp(t, `(?i)DELETE\s+FROM\s+(public\.)?(blocks|tx_results|events|attributes)\b`, sql, name)
	}
}

func TestPartitionsAreTheFirstPostMigrationStep(t *testing.T) {
	require.NotEmpty(t, postMigrate)
	assert.Equal(t, "partitions", postMigrate[0].Name)
}
