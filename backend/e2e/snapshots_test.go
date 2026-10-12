//go:build e2e

package e2e_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/cli"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
)

// Proposal 43 resolved at 17131437, before any indexed height: its
// resolution height is searched in the archive's blocks.
const (
	hResolve43      int64 = 17131437
	bonded43              = "147775542451704"
	bonded47              = "131537493190125"
	votesBefore43         = 27
	votesBefore47         = 24
	archiveHeight47       = hResolve - 1
)

// snapshots runs the snapshots command with args against the fake archive
// and returns its exit code and standard error.
func (e *syncEnv) snapshots(t *testing.T, args ...string) (int, string) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", e.url)
	t.Setenv("ARCHIVE_RPC_URL", e.node.URL)
	t.Setenv("BACKFILL_RATE_LIMIT", "1000")
	t.Setenv("LOG_LEVEL", "warn")
	root := cli.NewRootCmd()
	root.SetArgs(append([]string{"snapshots"}, args...))
	var stderr bytes.Buffer
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&stderr)
	return cli.ExitCode(root.Execute()), stderr.String()
}

// govRows is every governance value the snapshots write, for comparing runs.
func (e *syncEnv) govRows(t *testing.T) []string {
	t.Helper()
	return append(append(
		queryStrings(t, e.db, `SELECT concat_ws(' ', id, tally_bonded_tokens, tally_source, resolved_height)
			FROM explorer.proposals WHERE id IN (43, 47)`),
		queryStrings(t, e.db, `SELECT concat_ws(' ', proposal_id, voter, options::text, option, height, tx_index, time, source)
			FROM explorer.proposal_votes`)...),
		queryStrings(t, e.db, `SELECT concat_ws(' ', proposal_id, depositor, amount::text, height, tx_index, time, source)
			FROM explorer.proposal_deposits`)...)
}

func TestSnapshotsFillTheHistoryOfResolvedProposals(t *testing.T) {
	e := newSyncEnv(t)
	// Proposal 47 indexed live (submission, two votes, its tally); the
	// sync lists every proposal, 43 among them without a resolution height.
	e.index(t, hSubmit, hVoteVal, hVote, hResolve)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	assert.Equal(t, []string{"43 ", "47  23821225"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', id, coalesce(tally_bonded_tokens::text, ''), resolved_height) FROM explorer.proposals WHERE id IN (43, 47) ORDER BY id`),
		"no bonded tokens before the snapshot")

	code, stderr := e.snapshots(t, "--kind", "proposals", "--id", "43", "--id", "47")
	require.Equal(t, 0, code, stderr)

	assert.Equal(t, []string{
		"43 " + bonded43 + " archive 17131437",
		"47 " + bonded47 + " archive 23821225",
	}, queryStrings(t, e.db, `SELECT concat_ws(' ', id, tally_bonded_tokens, tally_source, resolved_height)
		FROM explorer.proposals WHERE id IN (43, 47)`), "43's resolution found by the search")
	assert.Equal(t, []string{"snapshot:proposals t"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', job, last_error, last_success_at IS NOT NULL) FROM explorer.sync_jobs WHERE job LIKE 'snapshot:%'`))

	assert.Equal(t, votesBefore43, e.count(t, `SELECT count(*) FROM explorer.proposal_votes WHERE proposal_id = 43 AND source = 'archive'
		AND height = $1 AND tx_index = -1`, hResolve43-1))
	// 43's votes predate v0.47: state holds their weights in the legacy Dec's
	// wire form (10^18 for a whole vote), read as plain decimals, so a vote
	// for one option is that option, not a split.
	assert.Zero(t, e.count(t, `SELECT count(*) FROM explorer.proposal_votes WHERE proposal_id = 43
		AND (option IS NULL OR EXISTS (SELECT 1 FROM jsonb_array_elements(options) o WHERE o->>'weight' NOT LIKE '%.%'))`))
	assert.Equal(t, votesBefore47, e.count(t, `SELECT count(*) FROM explorer.proposal_votes WHERE proposal_id = 47`))
	assert.Equal(t, []string{
		"23745061 0 event yes " + alphaOwner,
	}, queryStrings(t, e.db, `SELECT concat_ws(' ', height, tx_index, source, option, voter) FROM explorer.proposal_votes
		WHERE proposal_id = 47 AND voter = $1`, alphaOwner), "an indexed vote keeps its transaction")
	assert.Equal(t, 2, e.count(t, `SELECT count(*) FROM explorer.proposal_votes WHERE proposal_id = 47 AND source = 'event'`))
	assert.Equal(t, votesBefore47-2, e.count(t, `SELECT count(*) FROM explorer.proposal_votes WHERE proposal_id = 47 AND source = 'archive'
		AND height = $1 AND tx_index = -1`, archiveHeight47))
	assert.Equal(t, []string{"43 archive -1", "47 event 0"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', proposal_id, source, tx_index) FROM explorer.proposal_deposits`),
		"47's depositor has its transaction's row, 43's gets its total")

	// The API marks the historical values.
	base := e.serveAPI(t)
	p := into[dto.Proposal](t, fetch(t, base+"/proposals/43"))
	require.NotNil(t, p.TallySource)
	assert.Equal(t, "archive", *p.TallySource)
	require.NotNil(t, p.TurnoutPct)
	assert.Equal(t, "67.30967", *p.TurnoutPct)
	assert.Equal(t, hResolve43, *p.ResolvedHeight)
	assert.Equal(t, votesBefore43, p.VotesCount)
	p47 := into[dto.Proposal](t, fetch(t, base+"/proposals/47"))
	assert.Equal(t, "81.02998", *p47.TurnoutPct, "against the bonded tokens at the tally, not today's")

	votes := into[dto.List[dto.ProposalVote]](t, fetch(t, base+"/proposals/43/votes?limit=10"))
	require.Len(t, votes.Items, 10)
	assert.Equal(t, "archive", votes.Items[0].Source)
	assert.Nil(t, votes.Items[0].TxHash)
	assert.Equal(t, int64(-1), votes.Items[0].TxIndex)
	require.NotNil(t, votes.NextCursor)
	seen := len(votes.Items)
	for votes.NextCursor != nil {
		votes = into[dto.List[dto.ProposalVote]](t, fetch(t, base+"/proposals/43/votes?limit=10&cursor="+*votes.NextCursor))
		seen += len(votes.Items)
	}
	assert.Equal(t, votesBefore43, seen, "the cursor walks the archive rows")
	deposits := into[dto.List[dto.ProposalDeposit]](t, fetch(t, base+"/proposals/43/deposits"))
	require.Len(t, deposits.Items, 1)
	assert.Equal(t, "archive", deposits.Items[0].Source)
	assert.Nil(t, deposits.Items[0].TxHash)
	deposits = into[dto.List[dto.ProposalDeposit]](t, fetch(t, base+"/proposals/47/deposits"))
	require.Len(t, deposits.Items, 1)
	assert.Equal(t, "event", deposits.Items[0].Source)

	// A second run changes nothing.
	before := e.govRows(t)
	code, stderr = e.snapshots(t, "--id", "43", "--id", "47")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, before, e.govRows(t))

	// A run repairs what went wrong, keeping the indexed vote's transaction.
	_, err := e.db.Exec(`UPDATE explorer.proposal_votes SET options = '[{"option":3,"weight":"1.000000000000000000"}]', option = 'no'
		WHERE proposal_id = 47 AND voter = $1`, alphaOwner)
	require.NoError(t, err)
	_, err = e.db.Exec(`UPDATE explorer.proposal_votes SET option = 'abstain' WHERE proposal_id = 43`)
	require.NoError(t, err)
	_, err = e.db.Exec(`UPDATE explorer.proposals SET tally_bonded_tokens = 1, tally_source = 'sync' WHERE id = 47`)
	require.NoError(t, err)
	require.NotEqual(t, before, e.govRows(t))
	code, stderr = e.snapshots(t, "--id", "43", "--id", "47")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, before, e.govRows(t))
}

func TestSnapshotsReportTheTargetsThatFailed(t *testing.T) {
	e := newSyncEnv(t)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)

	// No archive state is recorded for proposal 46.
	code, stderr := e.snapshots(t, "--id", "46")
	assert.Equal(t, cli.ExitSomeFailed, code)
	assert.Contains(t, stderr, "failed proposals 46")
	assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM explorer.sync_jobs WHERE job = 'snapshot:proposals' AND last_error LIKE '1 of 1 targets failed: 46'`))
	assert.Equal(t, 0, e.count(t, `SELECT count(*) FROM explorer.proposals WHERE tally_source = 'archive'`))

	code, stderr = e.snapshots(t, "--kind", "votes")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, `unknown kind "votes"`)
}

func TestIndexedTransactionsReplaceArchiveRows(t *testing.T) {
	e := newSyncEnv(t)
	// Only 47's tally is indexed: the snapshot reads every vote and the
	// deposit from the archive.
	e.index(t, hResolve)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	code, stderr := e.snapshots(t, "--id", "47")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, votesBefore47, e.count(t, `SELECT count(*) FROM explorer.proposal_votes WHERE proposal_id = 47 AND source = 'archive'`))

	// The backfill then reaches the vote and the deposit transactions.
	e.index(t, hSubmit, hVoteVal)
	assert.Equal(t, []string{"23745061 0 event yes"}, queryStrings(t, e.db, `SELECT concat_ws(' ', height, tx_index, source, option)
		FROM explorer.proposal_votes WHERE proposal_id = 47 AND voter = $1`, alphaOwner), "the vote's transaction replaces the archive row")
	assert.Equal(t, votesBefore47, e.count(t, `SELECT count(*) FROM explorer.proposal_votes WHERE proposal_id = 47`))

	base := e.serveAPI(t)
	deposits := into[dto.List[dto.ProposalDeposit]](t, fetch(t, base+"/proposals/47/deposits"))
	require.Len(t, deposits.Items, 1, "the archive total is hidden behind its transaction's row, never deleted")
	assert.Equal(t, "event", deposits.Items[0].Source)
	assert.Equal(t, 2, e.count(t, `SELECT count(*) FROM explorer.proposal_deposits WHERE proposal_id = 47`))

	// Another snapshot keeps the event rows.
	code, stderr = e.snapshots(t, "--id", "47")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, []string{"23745061 0 event yes"}, queryStrings(t, e.db, `SELECT concat_ws(' ', height, tx_index, source, option)
		FROM explorer.proposal_votes WHERE proposal_id = 47 AND voter = $1`, alphaOwner))
}
