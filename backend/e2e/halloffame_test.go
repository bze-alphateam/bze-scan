//go:build e2e

package e2e_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
)

func getHallOfFame(t *testing.T, base string) dto.HallOfFame {
	t.Helper()
	resp, err := http.Get(base + "/validators/hall-of-fame")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	var fame dto.HallOfFame
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&fame))
	return fame
}

func monikersOf(items []dto.Validator) []string {
	out := make([]string, len(items))
	for i, v := range items {
		out[i] = v.Moniker
	}
	return out
}

// The Hall of Fame: bonded, unjailed validators whose account voted on
// each of the last four proposals whose voting ended, that missed at most
// ten blocks and that came back within ten minutes of the last computed
// upgrade's halt, by voting power ascending. Older proposals and upgrades
// don't count.
func TestHallOfFameAppliesTheRule(t *testing.T) {
	e := newSyncEnv(t)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	require.GreaterOrEqual(t, e.count(t, `SELECT count(*) FROM explorer.proposals WHERE voting_end_time <= now()`), 11,
		"the recorded proposals")

	// Five bonded validators of the recorded set, the largest first.
	type val struct{ op, account, moniker string }
	var vals []val
	rows, err := e.db.Query(`SELECT operator_address, account_address, moniker FROM explorer.validators
		WHERE status = 'bonded' AND NOT jailed ORDER BY tokens DESC, operator_address LIMIT 5`)
	require.NoError(t, err)
	for rows.Next() {
		var v val
		require.NoError(t, rows.Scan(&v.op, &v.account, &v.moniker))
		vals = append(vals, v)
	}
	require.NoError(t, rows.Err())
	require.Len(t, vals, 5)
	large, small, skipped, missing, jailed := vals[0], vals[1], vals[2], vals[3], vals[4]

	// Everybody has missed nothing; missing missed eleven blocks, large
	// exactly the ten allowed.
	_, err = e.db.Exec(`UPDATE explorer.validators SET missed_blocks = 0`)
	require.NoError(t, err)
	_, err = e.db.Exec(`UPDATE explorer.validators SET missed_blocks = 11 WHERE operator_address = $1`, missing.op)
	require.NoError(t, err)
	_, err = e.db.Exec(`UPDATE explorer.validators SET missed_blocks = 10 WHERE operator_address = $1`, large.op)
	require.NoError(t, err)
	_, err = e.db.Exec(`UPDATE explorer.validators SET jailed = true WHERE operator_address = $1`, jailed.op)
	require.NoError(t, err)
	// Stakes of different lengths: as text, "10…" would sort before "9…".
	_, err = e.db.Exec(`UPDATE explorer.validators SET tokens = 10000000000000 WHERE operator_address = $1`, large.op)
	require.NoError(t, err)
	_, err = e.db.Exec(`UPDATE explorer.validators SET tokens = 9000000000000 WHERE operator_address = $1`, small.op)
	require.NoError(t, err)

	// They vote, whatever the option, on the four latest closed proposals
	// only (none of the older ones); skipped leaves out the oldest of the four.
	vote := func(account string, offset int) {
		t.Helper()
		_, err := e.db.Exec(`INSERT INTO explorer.proposal_votes (proposal_id, voter, options, option, height, tx_index, time)
			SELECT id, $1, '[]', CASE WHEN id % 2 = 0 THEN 'yes' ELSE 'no_with_veto' END, 1, 0, now()
			  FROM (SELECT id FROM explorer.proposals WHERE voting_end_time <= now() ORDER BY id DESC LIMIT $2) p
			ON CONFLICT (proposal_id, voter) DO NOTHING`, account, offset)
		require.NoError(t, err)
	}
	for _, v := range []val{large, small, missing, jailed} {
		vote(v.account, 4)
	}
	vote(skipped.account, 3)

	// No upgrade computed: the upgrade condition is dropped.
	_, err = e.db.Exec(`DELETE FROM explorer.upgrade_recovery`)
	require.NoError(t, err)
	_, err = e.db.Exec(`DELETE FROM explorer.upgrades`)
	require.NoError(t, err)

	base := e.serveAPI(t)
	fame := getHallOfFame(t, base)
	assert.Equal(t, []string{small.moniker, large.moniker}, monikersOf(fame.Items), "by voting power ascending")
	assert.Nil(t, fame.NextCursor)
	require.NotNil(t, fame.Rules.SignedBlocksWindow)
	assert.Equal(t, dto.HallOfFameRules{Proposals: 4, MaxMissedBlocks: 10, SignedBlocksWindow: fame.Rules.SignedBlocksWindow}, fame.Rules)
	assert.Equal(t, int64(10000), *fame.Rules.SignedBlocksWindow)
	assert.Equal(t, dto.RecentVotes{Voted: 4, Of: 4}, fame.Items[1].RecentVotes, "counted on the rule's proposals")
	assert.Equal(t, int64(10), *fame.Items[1].MissedBlocks, "ten missed blocks are allowed")
	assert.Nil(t, fame.Rules.Upgrade, "no upgrade computed")
	assert.Equal(t, 600, fame.Rules.MaxSecondsBack)

	// The last computed upgrade: large came back after exactly the ten
	// minutes allowed, small one second later.
	_, err = e.db.Exec(`INSERT INTO explorer.upgrades (height, proposal_id, name, computed_at)
		VALUES (100, 1, 'v-old', now()), (200, 2, 'v-last', now())`)
	require.NoError(t, err)
	back := func(height int64, op string, seconds int) {
		t.Helper()
		_, err := e.db.Exec(`INSERT INTO explorer.upgrade_recovery
			(upgrade_height, consensus_address, operator_address, voting_power, seconds_back)
			VALUES ($1, upper(md5($2)), $2, 1, $3)`, height, op, seconds)
		require.NoError(t, err)
	}
	back(200, large.op, 600)
	back(200, small.op, 601)
	back(100, small.op, 10) // an older upgrade doesn't count
	fame = getHallOfFame(t, base)
	assert.Equal(t, []string{large.moniker}, monikersOf(fame.Items), "small came back too late")
	assert.Equal(t, &dto.HallOfFameUpgrade{Height: 200, Name: "v-last"}, fame.Rules.Upgrade)

	// Outside the set at the upgrade (no row): not in the Hall of Fame.
	_, err = e.db.Exec(`DELETE FROM explorer.upgrade_recovery WHERE upgrade_height = 200 AND operator_address = $1`, small.op)
	require.NoError(t, err)
	_, err = e.db.Exec(`UPDATE explorer.upgrade_recovery SET seconds_back = 60 WHERE upgrade_height = 200 AND operator_address = $1`, large.op)
	require.NoError(t, err)
	fame = getHallOfFame(t, base)
	assert.Equal(t, []string{large.moniker}, monikersOf(fame.Items), "small was not in the set")

	// A newer upgrade still being computed: the last computed one counts.
	_, err = e.db.Exec(`INSERT INTO explorer.upgrades (height, proposal_id, name) VALUES (300, 3, 'v-pending')`)
	require.NoError(t, err)
	fame = getHallOfFame(t, base)
	assert.Equal(t, []string{large.moniker}, monikersOf(fame.Items))
	assert.Equal(t, "v-last", fame.Rules.Upgrade.Name)

	// Back to no computed upgrade for the cases below.
	_, err = e.db.Exec(`DELETE FROM explorer.upgrade_recovery`)
	require.NoError(t, err)
	_, err = e.db.Exec(`DELETE FROM explorer.upgrades`)
	require.NoError(t, err)

	// Fewer than four closed proposals: a vote on each of them is enough,
	// so skipped joins.
	_, err = e.db.Exec(`UPDATE explorer.proposals SET voting_end_time = now() + interval '1 day'
		WHERE id NOT IN (SELECT id FROM explorer.proposals WHERE voting_end_time <= now() ORDER BY id DESC LIMIT 1)`)
	require.NoError(t, err)
	fame = getHallOfFame(t, base)
	assert.ElementsMatch(t, []string{small.moniker, large.moniker, skipped.moniker}, monikersOf(fame.Items))
	for _, v := range fame.Items {
		assert.Equal(t, dto.RecentVotes{Voted: 1, Of: 1}, v.RecentVotes, v.Moniker)
	}

	// No closed proposal: nobody qualifies.
	_, err = e.db.Exec(`UPDATE explorer.proposals SET voting_end_time = now() + interval '1 day'`)
	require.NoError(t, err)
	fame = getHallOfFame(t, base)
	assert.Empty(t, fame.Items)
	assert.Equal(t, 4, fame.Rules.Proposals)
}
