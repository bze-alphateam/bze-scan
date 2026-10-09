//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/cli"
	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// The reindex range: the recorded heights 24998320..24998330, all present.
// The local node has pruned everything below localEarliest, so the lower
// part of the range comes from the archive.
const (
	reindexFrom   = int64(24998320)
	reindexTo     = int64(24998330)
	localEarliest = int64(24998325)
	holeHeight    = int64(24998326) // carries two transactions
)

type reindexEnv struct {
	*backfillEnv
	local *fakenode.Node
}

// newReindexEnv is a database backfilled over the fixture range, a local
// node that pruned below localEarliest, and the reindex configuration in the
// environment.
func newReindexEnv(t *testing.T) *reindexEnv {
	t.Helper()
	e := &reindexEnv{backfillEnv: newBackfillEnv(t), local: fakenode.New(t)}
	require.NoError(t, serve.RunBackfill(context.Background(), e.config(0), quickRetries))
	e.local.SetEarliestHeight(localEarliest)
	e.archive.ResetRequests()

	t.Chdir(t.TempDir())
	for k, v := range map[string]string{
		"DATABASE_URL": e.url, "NODE_RPC_URL": e.local.URL, "ARCHIVE_RPC_URL": e.archive.URL,
		"LOG_LEVEL": "warn", "BACKFILL_WORKERS": "3", "BACKFILL_BATCH": "4", "BACKFILL_RATE_LIMIT": "1000",
	} {
		t.Setenv(k, v)
	}
	return e
}

type reindexRun struct {
	stdout, stderr string
	code           int
}

func (e *reindexEnv) reindex(t *testing.T, args ...string) reindexRun {
	t.Helper()
	root := cli.NewRootCmd()
	root.SetArgs(append([]string{"reindex"}, args...))
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	err := root.Execute()
	return reindexRun{stdout: out.String(), stderr: errOut.String(), code: cli.ExitCode(err)}
}

// rows is every explorer row a height produces, as text, with the system
// column xmin, which changes whenever a row is rewritten.
func (e *reindexEnv) rows(t *testing.T) []string {
	t.Helper()
	return queryStrings(t, e.db, `
		SELECT 'b ' || xmin || ' ' || row_to_json(b)::text FROM explorer.blocks b
		UNION ALL SELECT 't ' || xmin || ' ' || row_to_json(x)::text FROM explorer.transactions x
		UNION ALL SELECT 'm ' || xmin || ' ' || row_to_json(m)::text FROM explorer.messages m
		UNION ALL SELECT 'x ' || xmin || ' ' || row_to_json(x)::text FROM explorer.transfers x
		UNION ALL SELECT 'e ' || xmin || ' ' || row_to_json(e)::text FROM explorer.block_events e
		ORDER BY 1`)
}

// withoutVersions drops the xmin column of rows, sorted again.
func withoutVersions(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		kind, rest, _ := strings.Cut(r, " ")
		_, data, _ := strings.Cut(rest, " ")
		out[i] = kind + " " + data
	}
	slices.Sort(out)
	return out
}

func (e *reindexEnv) deleteHeight(t *testing.T, h int64) {
	t.Helper()
	for _, table := range []string{"messages", "transactions", "transfers", "block_events", "blocks"} {
		_, err := e.db.Exec(`DELETE FROM explorer.`+table+` WHERE height = $1`, h)
		require.NoError(t, err)
	}
}

func TestReindexFillsAHoleAndChangesNothingElse(t *testing.T) {
	e := newReindexEnv(t)
	before := e.rows(t)
	e.deleteHeight(t, holeHeight)
	holed := e.rows(t)
	require.Less(t, len(holed), len(before))

	from, to := strconv.FormatInt(reindexFrom, 10), strconv.FormatInt(reindexTo, 10)
	run := e.reindex(t, "--from", from, "--to", to)
	require.Equal(t, 0, run.code, run.stderr)
	assert.Regexp(t, `^reindex done heights=11 failed=0 duration=\S+\n$`, run.stdout)

	after := e.rows(t)
	assert.Equal(t, withoutVersions(before), withoutVersions(after), "the hole is filled with the same rows")
	for _, r := range holed {
		assert.Contains(t, after, r, "every other row is untouched, row version included")
	}

	// Read from the local node down to its earliest height, below it from
	// the archive.
	assert.Positive(t, e.local.RequestsAt(fakenode.RouteBlock, reindexTo))
	assert.Positive(t, e.local.RequestsAt(fakenode.RouteBlock, localEarliest))
	assert.Zero(t, e.archive.RequestsAt(fakenode.RouteBlock, localEarliest))
	assert.Positive(t, e.archive.RequestsAt(fakenode.RouteBlock, reindexFrom))
	assert.Zero(t, e.local.RequestsAt(fakenode.RouteBlock, reindexFrom), "pruned: never asked")

	assert.Equal(t, []string{"done " + to + " " + from + " " + from + " 11"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', status, ceiling_height, floor_height, lowest_dispatched, blocks_done)
		   FROM explorer.backfill_checkpoints WHERE job LIKE 'reindex-%'`))

	// Again: identical rows and counters, and not one row rewritten.
	counts := `SELECT concat_ws(' ', (SELECT count(*) FROM explorer.blocks), (SELECT count(*) FROM explorer.transactions),
		(SELECT count(*) FROM explorer.messages), (SELECT sum(tx_count) FROM explorer.blocks))`
	countsBefore := queryStrings(t, e.db, counts)
	run = e.reindex(t, "--from", from, "--to", to, "--workers", "1")
	require.Equal(t, 0, run.code, run.stderr)
	assert.Equal(t, after, e.rows(t))
	assert.Equal(t, countsBefore, queryStrings(t, e.db, counts))
	assert.Empty(t, queryStrings(t, e.db, `SELECT height::text FROM explorer.index_failures WHERE source = 'reindex'`))
}

func TestReindexFailedResolvesTheOpenFailures(t *testing.T) {
	e := newReindexEnv(t)
	const hole, resolved = int64(24998322), int64(24998323)
	e.deleteHeight(t, hole)
	_, err := e.db.Exec(`INSERT INTO explorer.index_failures (height, source, attempts, error, resolved_at)
		VALUES ($1, 'live', 4, 'node unavailable', NULL), ($2, 'backfill', 4, 'archive unavailable', now())`, hole, resolved)
	require.NoError(t, err)

	// The setup's backfill left 24998319 (no fixture) open too.
	run := e.reindex(t, "--failed", "--dry-run")
	require.Equal(t, 0, run.code, run.stderr)
	assert.Equal(t, "reindex selection heights=2 lowest=24998319 highest=24998322\n24998322\n24998319\n", run.stdout)
	assert.Equal(t, "reindex selection heights=1 lowest=24998319 highest=24998319\n24998319\n",
		e.reindex(t, "--failed", "--source", "backfill", "--dry-run").stdout, "a resolved failure is not selected")
	assert.Zero(t, e.archive.Requests(fakenode.RouteBlock), "a dry run fetches nothing")

	run = e.reindex(t, "--failed", "--source", "live")
	require.Equal(t, 0, run.code, run.stderr)
	assert.Contains(t, run.stdout, "reindex done heights=1 failed=0")
	assert.Equal(t, []string{"24998322 0"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', height, tx_count) FROM explorer.blocks WHERE height = $1`, hole))
	assert.Equal(t, []string{"24998319 backfill f", "24998322 live t", "24998323 backfill t"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', height, source, resolved_at IS NOT NULL) FROM explorer.index_failures ORDER BY height`))
	assert.Equal(t, "reindex selection heights=1 lowest=24998319 highest=24998319\n24998319\n",
		e.reindex(t, "--failed", "--dry-run").stdout, "only the failure still open")
}

func TestReindexAboveTheTipExitsTwo(t *testing.T) {
	e := newReindexEnv(t)
	above := reindexTo + 1 // no fixture: both nodes answer above-tip
	e.local.SetStatusHeight(reindexTo)
	e.deleteHeight(t, reindexTo)

	run := e.reindex(t, "--heights", strconv.FormatInt(above, 10)+","+strconv.FormatInt(reindexTo, 10)+",24998321")
	assert.Equal(t, 2, run.code)
	assert.Contains(t, run.stderr, "failed height 24998331\n")
	assert.Contains(t, run.stdout, "reindex done heights=3 failed=1")

	assert.Equal(t, []string{"24998331 reindex 4 f"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', height, source, attempts, resolved_at IS NOT NULL) FROM explorer.index_failures
		  WHERE source = 'reindex'`))
	assert.Equal(t, []string{"24998330"}, queryStrings(t, e.db,
		`SELECT height::text FROM explorer.blocks WHERE height = $1`, reindexTo), "the other heights complete")
	assert.Equal(t, []string{"done"}, queryStrings(t, e.db,
		`SELECT status FROM explorer.backfill_checkpoints WHERE job LIKE 'reindex-%'`))
}

// Rows that differ from what the transformer produces (a transformer fix)
// are overwritten in every table; nothing is counted twice.
func TestReindexOverwritesRowsThatDiffer(t *testing.T) {
	e := newReindexEnv(t)
	before := e.rows(t)
	for _, q := range []string{
		`UPDATE explorer.blocks SET tx_count = 99, hash = 'WRONG', block_time_ms = 1 WHERE height = 24998321`,
		`UPDATE explorer.transactions SET gas_used = 1, signers = '{}' WHERE height = 24998321`,
		`UPDATE explorer.messages SET module = 'wrong', body = NULL, events = '[]' WHERE height = 24998321`,
		`UPDATE explorer.transfers SET amount = 1, recipient = NULL, msg_index = 7 WHERE height = 24998321`,
		`UPDATE explorer.block_events SET type = 'wrong', attrs = '{}' WHERE height = 24998321`,
	} {
		_, err := e.db.Exec(q)
		require.NoError(t, err)
	}
	require.NotEqual(t, withoutVersions(before), withoutVersions(e.rows(t)))

	run := e.reindex(t, "--heights", "24998321")
	require.Equal(t, 0, run.code, run.stderr)
	assert.Equal(t, withoutVersions(before), withoutVersions(e.rows(t)), "every column back to the transformer's output")
}
