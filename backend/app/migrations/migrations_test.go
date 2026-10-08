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

var fileNameRe = regexp.MustCompile(`^\d{6}_[a-z0-9_]+\.up\.sql$`)

func TestMigrationFilesAreUpOnlyAndWellNamed(t *testing.T) {
	entries, err := fs.ReadDir(files, "sql")
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, e := range entries {
		assert.Regexp(t, fileNameRe, e.Name(), "migrations are up only: NNNNNN_name.up.sql")
	}
}

func TestMigrationSetLoadsInOrder(t *testing.T) {
	src, err := Source()
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	v, err := src.First()
	require.NoError(t, err)
	var versions []uint
	for {
		versions = append(versions, v)

		r, _, err := src.ReadUp(v)
		require.NoError(t, err, "version %d has no up migration", v)
		body, err := io.ReadAll(r)
		_ = r.Close()
		require.NoError(t, err)
		assert.NotEmpty(t, strings.TrimSpace(string(body)), "version %d is empty", v)

		_, _, err = src.ReadDown(v)
		assert.True(t, errors.Is(err, os.ErrNotExist), "version %d has a down migration", v)

		next, err := src.Next(v)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		require.NoError(t, err)
		v = next
	}

	for i, got := range versions {
		assert.Equal(t, uint(i+1), got, "versions must be contiguous from 1")
	}
	assert.Len(t, versions, 3)
}

func readMigration(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(files, "sql/"+name)
	require.NoError(t, err)
	return string(b)
}

// TestPartitionedTablesMatchTheSQL keeps the Go table list in step with the
// tables declared PARTITION BY RANGE (height) and with ensure_partitions.
func TestPartitionedTablesMatchTheSQL(t *testing.T) {
	schema := readMigration(t, "000001_explorer_schema.up.sql")
	var partitioned []string
	for _, chunk := range strings.Split(schema, "CREATE TABLE explorer.")[1:] {
		name := chunk[:strings.IndexAny(chunk, " (")]
		// The column list closes on the first line starting with ")"
		// (column comments may contain semicolons).
		closing := chunk[strings.Index(chunk, "\n)")+1:]
		closing = closing[:strings.Index(closing, "\n")]
		if closing == ") PARTITION BY RANGE (height);" {
			partitioned = append(partitioned, name)
		}
	}
	assert.ElementsMatch(t, PartitionedTables, partitioned)

	fn := readMigration(t, "000002_partitions.up.sql")
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

func TestNotifyTriggerUsesTheChannel(t *testing.T) {
	sql := readMigration(t, "000003_notify_block.up.sql")
	assert.Contains(t, sql, "pg_notify('"+NotifyChannel+"', NEW.height::text)")
	assert.Contains(t, sql, "AFTER INSERT ON public.blocks")
}

// TestSinkSchemaIsLeftAlone guards the rule that the only object attached to
// the sink's public schema is the notification trigger.
func TestSinkSchemaIsLeftAlone(t *testing.T) {
	entries, err := fs.ReadDir(files, "sql")
	require.NoError(t, err)
	publicRe := regexp.MustCompile(`(?i)\bpublic\.\w+`)
	for _, e := range entries {
		sql := readMigration(t, e.Name())
		for _, ref := range publicRe.FindAllString(sql, -1) {
			assert.Equal(t, "public.blocks", ref, "%s references %s", e.Name(), ref)
		}
		assert.NotRegexp(t, `(?i)ALTER\s+TABLE\s+(public\.)?(blocks|tx_results|events|attributes)\b`, sql, e.Name())
	}
}
