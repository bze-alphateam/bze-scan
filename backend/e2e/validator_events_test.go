//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/cli"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// Recorded heights with a validator event each, and their validators (all
// in the recorded validator set).
const (
	hCommission  int64 = 22933748 // NODESYNC raises its commission to 0.20
	hCreated     int64 = 24113494 // BonyNode is created
	hUnjail      int64 = 24129272 // The Chicken Coop unjails
	hDescription int64 = 24151894 // BonyNode edits its description
	hSlash       int64 = 24160001 // Scafire is slashed for downtime and jailed

	nodeSync    = "bzevaloper1mzqtykn6nq6ukeq0wvr0zvzldt8y2elur3vug3"
	bonyNode    = "bzevaloper13nwzm5dfd26ue74jr6sc39gyn3qze0rjzyrzlz"
	chickenCoop = "bzevaloper1dnhhhhge8qphr5w7mz0cvp5ukxgr6drpw6ludu"
	scafire     = "bzevaloper1gk8k0u3vkt65xjc82l8qryf44pxp68dcsuth5c"
	scafireCons = "EFF58FAD4559319233C282F942C38A0BE8F50120"
)

var validatorEventHeights = []int64{hCommission, hCreated, hUnjail, hDescription, hSlash}

// index writes heights through the live path into the env's database.
func (e *syncEnv) index(t *testing.T, heights ...int64) {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	defer pool.Close()
	w := writer.NewLiveWriter(pool)
	client := node.New(e.node.URL)
	tr := newTransformer(t)
	for _, h := range heights {
		b, _, err := client.Block(ctx, h)
		require.NoError(t, err)
		r, _, err := client.BlockResults(ctx, h)
		require.NoError(t, err)
		c, _, err := client.Commit(ctx, h)
		require.NoError(t, err)
		ents, err := tr.Transform(transform.Input{Block: b, Results: r, Commit: c})
		require.NoError(t, err)
		require.NoError(t, w.WriteBlock(ctx, ents))
	}
}

func (e *syncEnv) events(t *testing.T) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', height, tx_index, seq, kind, operator_address, coalesce(details::text, '-'))
		FROM explorer.validator_events ORDER BY height, tx_index, seq`)
}

func TestValidatorEventsWithTheValidatorsSynced(t *testing.T) {
	e := newSyncEnv(t)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	e.index(t, validatorEventHeights...)

	assert.Equal(t, []string{
		`22933748 0 0 commission_changed ` + nodeSync + ` {"to": "0.200000000000000000", "from": "0.200000000000000000"}`,
		`24113494 0 0 created ` + bonyNode + ` {"moniker": "BonyNode", "self_bond": "3000000000ubze", "commission_rate": "0.100000000000000000"}`,
		`24129272 0 0 unjailed ` + chickenCoop + ` -`,
		`24151894 0 0 description_changed ` + bonyNode + ` {"to": {"details": "💚24/7 management and monitoring!", "moniker": "BonyNode💚", "website": "https://bonynode.online", "identity": "C5C24B65139B46BE", "security_contact": "support@bonynode.online"}}`,
		`24160001 -1 0 slashed ` + scafire + ` {"power": "6138062", "burned": "613806200", "reason": "missing_signature", "consensus_address": "` + scafireCons + `"}`,
	}, e.events(t), `"from" is the stored rate when the row is written: today's for history`)

	// Indexing the heights again changes nothing.
	before := queryStrings(t, e.db, `SELECT xmin::text FROM explorer.validator_events ORDER BY height`)
	e.index(t, validatorEventHeights...)
	assert.Equal(t, before, queryStrings(t, e.db, `SELECT xmin::text FROM explorer.validator_events ORDER BY height`))

	// First sight moves back to the created event on the next sync.
	code, _ = e.syncState(t)
	require.Equal(t, 0, code)
	assert.Equal(t, []string{fmt.Sprint(hCreated)}, queryStrings(t, e.db,
		`SELECT first_seen_height::text FROM explorer.validators WHERE operator_address = $1`, bonyNode))
}

func TestASlashBeforeTheFirstSyncIsResolvedByIt(t *testing.T) {
	e := newSyncEnv(t)
	e.index(t, hSlash, hCommission)
	assert.Equal(t, []string{
		`22933748 0 0 commission_changed ` + nodeSync + ` {"to": "0.200000000000000000", "from": null}`,
		`24160001 -1 0 slashed  {"power": "6138062", "burned": "613806200", "reason": "missing_signature", "consensus_address": "` + scafireCons + `"}`,
	}, e.events(t), "no validator known yet")

	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	assert.Equal(t, []string{scafire}, queryStrings(t, e.db,
		`SELECT operator_address FROM explorer.validator_events WHERE kind = 'slashed'`))
}

// The sync compares a fresh row with the stored one and writes what changed
// at the live cursor's height, after the block's own events.
func TestSyncWritesTransitionsAtTheCursor(t *testing.T) {
	e := newSyncEnv(t)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	e.index(t, hSlash) // the cursor

	_, err := e.db.Exec(`UPDATE explorer.validators SET status = 'unbonded' WHERE operator_address = $1`, chainTools)
	require.NoError(t, err)
	_, err = e.db.Exec(`UPDATE explorer.validators SET status = 'bonded', jailed = false, tombstoned = false
		WHERE operator_address = $1`, scafire)
	require.NoError(t, err)
	code, _ = e.syncState(t)
	require.Equal(t, 0, code)

	// The seqs follow the order the node lists the validators in.
	assert.Equal(t, []string{"10000", "10001", "10002"}, queryStrings(t, e.db,
		`SELECT seq::text FROM explorer.validator_events WHERE seq >= 10000 ORDER BY seq`))
	assert.ElementsMatch(t, []string{
		`24160001 -1 bonded ` + chainTools + ` {"to": "bonded", "from": "unbonded"} true`,
		`24160001 -1 unbonded ` + scafire + ` {"to": "unbonded", "from": "bonded"} true`,
		`24160001 -1 jailed ` + scafire + ` {"to": true, "from": false} true`,
	}, queryStrings(t, e.db, `SELECT concat_ws(' ', height, tx_index, kind, operator_address, details::text,
			(time = (SELECT time FROM explorer.blocks WHERE height = $1))::text)
		FROM explorer.validator_events WHERE seq >= 10000`, hSlash))

	code, _ = e.syncState(t)
	require.Equal(t, 0, code)
	assert.Equal(t, 3, e.count(t, `SELECT count(*) FROM explorer.validator_events WHERE seq >= 10000`),
		"an unchanged validator writes nothing")
}

func TestTheValidatorPageAndProposerNames(t *testing.T) {
	e := newSyncEnv(t)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	e.index(t, validatorEventHeights...)
	e.index(t, restakeHeight)
	base := e.serve(t, restakeHeight) + "/api/v1"

	proposerOf := func(h int64) (string, string) {
		row := queryStrings(t, e.db, `SELECT concat_ws(' ', v.operator_address, v.moniker) FROM explorer.blocks b
			JOIN explorer.validators v ON v.consensus_address = b.proposer_cons_address WHERE b.height = $1`, h)
		require.Len(t, row, 1, "the proposer of %d is a recorded validator", h)
		var op, moniker string
		_, _ = fmt.Sscanf(row[0], "%s", &op)
		moniker = row[0][len(op)+1:]
		return op, moniker
	}

	// The block page names its proposer and stays cacheable.
	op, moniker := proposerOf(hSlash)
	r := fetch(t, fmt.Sprintf("%s/blocks/%d", base, hSlash))
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, "public, max-age=31536000, immutable", r.header.Get("Cache-Control"))
	assert.Equal(t, &dto.Proposer{OperatorAddress: op, Moniker: moniker}, into[dto.Block](t, r).Proposer)
	page := into[dto.List[dto.BlockSummary]](t, fetch(t, base+"/blocks?limit=1"))
	require.Len(t, page.Items, 1)
	op, moniker = proposerOf(page.Items[0].Height)
	assert.Equal(t, &dto.Proposer{OperatorAddress: op, Moniker: moniker}, page.Items[0].Proposer)

	// The slashed validator's page: its event, its uptime.
	r = fetch(t, base+"/validators/"+scafire)
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	v := into[dto.ValidatorDetail](t, r)
	assert.Equal(t, scafire, v.OperatorAddress)
	assert.Equal(t, scafireCons, *v.ConsensusAddress)
	require.NotEmpty(t, v.Events)
	assert.Equal(t, "slashed", v.Events[0].Kind)
	assert.Equal(t, hSlash, v.Events[0].Height)
	assert.Nil(t, v.Events[0].TxHash)
	require.NotNil(t, v.Uptime)
	assert.NotNil(t, v.Votes)

	// BonyNode: two events, newest first, each with its transaction.
	v = into[dto.ValidatorDetail](t, fetch(t, base+"/validators/"+bonyNode))
	require.Len(t, v.Events, 2)
	assert.Equal(t, []string{"description_changed", "created"}, []string{v.Events[0].Kind, v.Events[1].Kind})
	assert.Equal(t, []string{
		ix1Hash(t, e, hDescription), ix1Hash(t, e, hCreated),
	}, []string{*v.Events[0].TxHash, *v.Events[1].TxHash})

	// The proposer of the restake block: its recent blocks, newest first,
	// and the paginated list.
	op, _ = proposerOf(restakeHeight)
	v = into[dto.ValidatorDetail](t, fetch(t, base+"/validators/"+op))
	want := queryStrings(t, e.db, `SELECT height::text FROM explorer.blocks WHERE proposer_cons_address = $1
		ORDER BY height DESC LIMIT 10`, *v.ConsensusAddress)
	got := make([]string, 0, len(v.RecentBlocks))
	for _, b := range v.RecentBlocks {
		got = append(got, fmt.Sprint(b.Height))
	}
	assert.Equal(t, want, got)
	assert.Contains(t, got, fmt.Sprint(restakeHeight))

	var all []string
	cursor := ""
	for {
		blocks := into[dto.List[dto.BlockSummary]](t, fetch(t, base+"/validators/"+op+"/blocks?limit=1"+cursor))
		for _, b := range blocks.Items {
			all = append(all, fmt.Sprint(b.Height))
			assert.Equal(t, op, b.Proposer.OperatorAddress)
		}
		if blocks.NextCursor == nil {
			break
		}
		cursor = "&cursor=" + *blocks.NextCursor
	}
	assert.Equal(t, want, all)

	unknown, err := bech32.ConvertAndEncode("bzevaloper", make([]byte, 20))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, fetch(t, base+"/validators/"+unknown).status)
	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/validators/"+chainToolsOwner).status)
}

func ix1Hash(t *testing.T, e *syncEnv, h int64) string {
	t.Helper()
	rows := queryStrings(t, e.db, `SELECT hash FROM explorer.transactions WHERE height = $1 AND tx_index = 0`, h)
	require.Len(t, rows, 1)
	return rows[0]
}

// The reindex command rewrites validator_events like the other history
// tables, keeping the values only the live path knew: the commission
// "from" and the slash's resolved operator.
func TestReindexRestoresValidatorEvents(t *testing.T) {
	e := newSyncEnv(t)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	e.index(t, validatorEventHeights...)
	want := e.events(t)

	t.Setenv("NODE_RPC_URL", e.node.URL)
	t.Setenv("ARCHIVE_RPC_URL", e.node.URL)
	reindex := func() {
		t.Helper()
		root := cli.NewRootCmd()
		heights := make([]string, len(validatorEventHeights))
		for i, h := range validatorEventHeights {
			heights[i] = fmt.Sprint(h)
		}
		root.SetArgs([]string{"reindex", "--heights", strings.Join(heights, ",")})
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		require.Equal(t, 0, cli.ExitCode(root.Execute()))
	}

	for _, q := range []string{
		`UPDATE explorer.validator_events SET kind = 'wrong', details = '{}' WHERE height = 24113494`,
		`DELETE FROM explorer.validator_events WHERE height = 24129272`,
		// Today's stored rate and owners differ from the ones the rows were
		// written with.
		`UPDATE explorer.validators SET commission_rate = 0.33 WHERE operator_address = '` + nodeSync + `'`,
		`UPDATE explorer.validators SET consensus_address = NULL WHERE operator_address = '` + scafire + `'`,
	} {
		_, err := e.db.Exec(q)
		require.NoError(t, err)
	}
	reindex()
	assert.Equal(t, want, e.events(t))

	versions := `SELECT xmin::text FROM explorer.validator_events ORDER BY height, tx_index, seq`
	before := queryStrings(t, e.db, versions)
	reindex()
	assert.Equal(t, before, queryStrings(t, e.db, versions), "a second reindex rewrites nothing")
}
