//go:build e2e

package e2e_test

import (
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
)

// flowValidator received the five authz (REStake) delegations of hExec;
// unknownValidator is a valid operator address the explorer does not know.
const (
	flowValidator    = "bzevaloper1prm55vzlp5u6excqdunwlm4tw254cq943m6e6m"
	unknownValidator = "bzevaloper10dcm7hqv9a5ajdjr9ctcu5pgg2969ag7qx0nyg"
)

func stakeChanges(t *testing.T, base string) dto.StakeChanges {
	t.Helper()
	r := fetch(t, base+"/validators/stake-changes")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
	return into[dto.StakeChanges](t, r)
}

// waitStakeChanges waits until the set has seen height.
func waitStakeChanges(t *testing.T, base string, height int64) dto.StakeChanges {
	t.Helper()
	var got dto.StakeChanges
	waitFor(t, 10*time.Second, func() bool {
		got = stakeChanges(t, base)
		return got.AsOfHeight == height
	}, "stake changes as of %d", height)
	return got
}

// delegatedAt sums the delegate events of validator at height, read from
// the stored events.
func delegatedAt(t *testing.T, db *sql.DB, height int64, validator string) string {
	t.Helper()
	var sum string
	require.NoError(t, db.QueryRow(`SELECT sum(replace(e->'attrs'->>'amount', 'ubze', '')::numeric)::text
		FROM explorer.messages, jsonb_array_elements(events) e
		WHERE height = $1 AND e->>'type' = 'delegate' AND e->'attrs'->>'validator' = $2`, height, validator).Scan(&sum))
	return sum
}

func testStakeMovementsFromTheStoredEvents(t *testing.T, base string, ix *indexer) {
	_, err := ix.db.Exec(`INSERT INTO explorer.validators (operator_address, account_address, moniker, status, tokens,
		delegator_shares, commission_rate, commission_max_rate, commission_max_change_rate, updated_at)
		VALUES ($1, 'bze1flow', 'Flow', 'bonded', 1, 1, 0.1, 0.2, 0.01, now()) ON CONFLICT DO NOTHING`, flowValidator)
	require.NoError(t, err)

	// The fixtures span a few hours: the window is not fully indexed, but
	// what is indexed is counted.
	ch := waitStakeChanges(t, base, hSend)
	assert.False(t, ch.Complete)
	want := delegatedAt(t, ix.db, hExec, flowValidator)
	assert.Equal(t, dto.StakeChange{Day: want, Week: want}, ch.Items[flowValidator])
	assert.Len(t, ch.Items, 1, "hExec holds the only staking events")

	r := fetch(t, base+"/validators/"+flowValidator+"/movements?limit=2")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
	page := into[dto.StakeMovements](t, r)
	assert.False(t, page.Complete)
	assert.Equal(t, 5, page.Total)
	assert.EqualValues(t, 1, page.Page)
	require.NotNil(t, page.WindowFrom)
	require.Len(t, page.Items, 2)
	hash := ix.one(t, `SELECT hash FROM explorer.transactions WHERE height = $1`, hExec)
	for _, m := range page.Items {
		assert.Equal(t, hExec, m.Height)
		assert.Equal(t, hash, m.TxHash)
		assert.Equal(t, "delegate", m.Kind)
		assert.Nil(t, m.CounterpartyValidator)
		assert.Contains(t, m.Delegator, "bze1")
	}
	// Newest first: the last authz message comes first.
	last := ix.one(t, `SELECT e->'attrs'->>'amount' FROM explorer.messages, jsonb_array_elements(events) WITH ORDINALITY x(e, n)
		WHERE height = $1 AND e->>'type' = 'delegate' ORDER BY tx_index DESC, msg_index DESC, n DESC LIMIT 1`, hExec)
	assert.Equal(t, last, page.Items[0].Amount+"ubze")

	third := into[dto.StakeMovements](t, fetch(t, base+"/validators/"+flowValidator+"/movements?limit=2&page=3"))
	assert.Len(t, third.Items, 1)
	assert.EqualValues(t, 3, third.Page)

	assert.Equal(t, http.StatusNotFound, fetch(t, base+"/validators/"+unknownValidator+"/movements").status)
	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/validators/"+flowValidator+"/movements?limit=51").status)
}

func TestStakeMovementsFollowTheLiveIndexer(t *testing.T) {
	env := newSyncEnv(t)
	// The first start indexes the head only; the set loads it.
	base := env.serve(t, hRewards) + "/api/v1"
	ch := waitStakeChanges(t, base, hRewards)
	assert.Empty(t, ch.Items)

	// The next block arrives live: its delegations reach the totals through
	// the writer's hook, without a reload.
	insertSinkBlock(t, env.db, hExec)
	ch = waitStakeChanges(t, base, hExec)
	assert.False(t, ch.Complete)
	want := delegatedAt(t, env.db, hExec, flowValidator)
	assert.Equal(t, dto.StakeChange{Day: want, Week: want}, ch.Items[flowValidator])
}
