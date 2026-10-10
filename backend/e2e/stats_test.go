//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/internal/params"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// recordedStatusHeight is the height of the fake node's recorded /status.
const recordedStatusHeight = int64(24998377)

// The recorded numbers: Pool's bonded tokens over SupplyOf ubze, 5%
// inflation and a 5% community tax, and the five largest bonded validators'
// share of the bonded tokens.
const (
	recordedBondedTokens = "147027371221650"
	recordedStakedShare  = "52.16983"
	recordedRewardRate   = "9.10488"
	recordedTop5Share    = "40.27702"
)

func TestChainStateParamsAndStats(t *testing.T) {
	e := newSyncEnv(t)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)

	// One chain_state row per key, read at the node's height.
	assert.Equal(t, []string{
		"chain_params 24998377", "community_pool 24998377", "mint 24998377", "node_status 24998377",
		"price_ubze 24998377", "staking_pool 24998377", "supply 24998377", "validator_counts 24998377",
	}, queryStrings(t, e.db, `SELECT concat_ws(' ', key, height) FROM explorer.chain_state ORDER BY key`))
	assert.Equal(t, []string{`{"total": 57, "bonded": 22, "jailed": 35}`},
		queryStrings(t, e.db, `SELECT value::text FROM explorer.chain_state WHERE key = 'validator_counts'`))
	assert.Equal(t, []string{"a_team_pub_xl false"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', value->>'moniker', value->>'catching_up') FROM explorer.chain_state WHERE key = 'node_status'`))

	// The first parameters run snapshots every module, changing no key.
	assert.Equal(t, len(params.Modules), e.count(t, `SELECT count(*) FROM explorer.param_snapshots
		WHERE height = $1 AND changed_keys = '{}' AND proposal_id IS NULL`, recordedStatusHeight))
	assert.Equal(t, []string{"40"}, queryStrings(t, e.db,
		`SELECT params->>'max_validators' FROM explorer.param_snapshots WHERE module = 'staking'`))

	// The API: the tiles, the parameters and the validators header.
	base := e.serve(t, h316)
	waitFor(t, 10*time.Second, func() bool {
		return e.count(t, `SELECT count(*) FROM explorer.blocks WHERE height = $1`, h316) == 1
	}, "block %d", h316)
	price := queryStrings(t, e.db, `SELECT concat_ws(' ', price_usd, price_change_24h_pct) FROM explorer.denoms WHERE denom = 'ubze'`)
	require.Len(t, price, 1)
	priceUSD, change, _ := strings.Cut(price[0], " ")

	txs := queryStrings(t, e.db, `SELECT count(*) FROM explorer.transactions`)

	r := fetch(t, base+"/api/v1/stats")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	stats := into[map[string]json.RawMessage](t, r)
	for k, want := range map[string]string{
		"latest_height": "24998316", "txs_24h": txs[0], "bonded_tokens": `"` + recordedBondedTokens + `"`,
		"staked_share": `"` + recordedStakedShare + `"`, "inflation": `"5.00000"`, "reward_rate": `"` + recordedRewardRate + `"`,
		"validators": `{"active":22,"total":57}`, "max_validators": "40", "unbonding_period": "1814400",
		"price_usd": `"` + priceUSD + `"`, "price_change_24h_pct": `"` + change + `"`,
		"community_pool": `"34939146569152"`, "supply": `"281824500438386"`, "daily": "[]",
	} {
		assert.JSONEq(t, want, string(stats[k]), k)
	}

	r = fetch(t, base+"/api/v1/params")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	mods := into[map[string]struct {
		Params     map[string]json.RawMessage `json:"params"`
		LastChange json.RawMessage            `json:"last_change"`
	}](t, r)
	assert.Len(t, mods, len(params.Modules))
	for _, m := range params.Modules {
		assert.NotEmpty(t, mods[m].Params, m)
		assert.JSONEq(t, "null", string(mods[m].LastChange), "a first snapshot is no change: %s", m)
	}
	assert.JSONEq(t, `"1814400s"`, string(mods["staking"].Params["unbonding_time"]))
	assert.JSONEq(t, `{"denom":"ubze","amount":"100000"}`, string(mods["tradebin"].Params["marketTakerFee"]))
	assert.JSONEq(t, `"0.334000000000000000"`, string(mods["gov"].Params["quorum"]))

	r = fetch(t, base+"/api/v1/validators?limit=1")
	require.Equal(t, http.StatusOK, r.status)
	assert.JSONEq(t, `{"bonded_tokens":"`+recordedBondedTokens+`","staked_share":"`+recordedStakedShare+`",
		"reward_rate":"`+recordedRewardRate+`","unbonding_period":1814400,"top5_share":"`+recordedTop5Share+`",
		"max_validators":40}`, string(into[map[string]json.RawMessage](t, r)["summary"]))
}

func TestParamsSnapshotOnlyWhatChanged(t *testing.T) {
	e := newSyncEnv(t)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	first := e.count(t, `SELECT count(*) FROM explorer.param_snapshots`)
	require.Equal(t, len(params.Modules), first)

	// Unchanged: no row.
	code, _ = e.syncState(t)
	require.Equal(t, 0, code)
	assert.Equal(t, first, e.count(t, `SELECT count(*) FROM explorer.param_snapshots`))

	// One staking value changes at a later height: one row, that key.
	var staking map[string]map[string]any
	require.NoError(t, json.Unmarshal(fakenode.ReadGRPCFixture(t, "staking", "Params", ""), &staking))
	staking["params"]["max_validators"] = 45
	changed, err := json.Marshal(staking)
	require.NoError(t, err)
	e.grpc.SetResponse("staking", "Params", "", changed)
	e.node.SetStatusHeight(recordedStatusHeight + 100)
	code, _ = e.syncState(t)
	require.Equal(t, 0, code)
	assert.Equal(t, first+1, e.count(t, `SELECT count(*) FROM explorer.param_snapshots`))
	assert.Equal(t, []string{"staking 24998477 {max_validators} -"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', module, height, changed_keys::text, coalesce(proposal_id::text, '-'))
		   FROM explorer.param_snapshots WHERE cardinality(changed_keys) > 0`))
	assert.Equal(t, []string{"45"}, queryStrings(t, e.db,
		`SELECT value->>'max_validators' FROM explorer.chain_state WHERE key = 'chain_params'`))

	// A change seen after a passed upgrade's plan height is attributed to
	// it: proposal 47 upgraded the chain at 23855000.
	_, err = e.db.Exec(`UPDATE explorer.param_snapshots SET height = 23000000, params = '{"periodic_burning_weeks":"2"}'
		WHERE module = 'burner'`)
	require.NoError(t, err)
	code, _ = e.syncState(t)
	require.Equal(t, 0, code)
	assert.Equal(t, []string{"burner 24998477 {periodic_burning_weeks} 47"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', module, height, changed_keys::text, coalesce(proposal_id::text, '-'))
		   FROM explorer.param_snapshots WHERE module = 'burner' AND cardinality(changed_keys) > 0`))
}

func TestStatsWindowsAnchorOnTheLatestBlock(t *testing.T) {
	url := freshDatabase(t, true)
	migrateUp(t, url)
	db := connect(t, url)
	// Blocks every 5 s up to 00:30 UTC, plus an older block 2 h before the
	// latest; transactions on both sides of midnight and of the 24 h line.
	latest := time.Date(2026, 10, 10, 0, 30, 0, 0, time.UTC)
	for i := range 6 {
		_, err := db.Exec(`INSERT INTO explorer.blocks (height, time, block_time_ms, hash) VALUES ($1, $2, $3, 'H')`,
			int64(1000+i), latest.Add(time.Duration(i-5)*5*time.Second), 5000+i*100)
		require.NoError(t, err)
	}
	_, err := db.Exec(`INSERT INTO explorer.blocks (height, time, block_time_ms, hash) VALUES (900, $1, 60000, 'H')`,
		latest.Add(-2*time.Hour))
	require.NoError(t, err)
	for i, at := range []time.Time{
		latest.Add(-24*time.Hour - time.Second), // outside
		latest.Add(-24 * time.Hour),             // the line itself is outside
		latest.Add(-23 * time.Hour),             // the day before, inside
		latest.Add(-31 * time.Minute),           // just before midnight
		latest,
	} {
		_, err := db.Exec(`INSERT INTO explorer.transactions (height, tx_index, hash, time, success, code)
			VALUES (900, $1, $2, $3, true, 0)`, i, strings.Repeat("A", 63)+string(rune('0'+i)), at)
		require.NoError(t, err)
	}

	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	s, err := repository.NewExplorer(pool).BlockStats(context.Background())
	require.NoError(t, err)
	require.NotNil(t, s.LatestHeight)
	assert.Equal(t, int64(1005), *s.LatestHeight)
	assert.True(t, latest.Equal(*s.LatestTime))
	require.NotNil(t, s.AvgBlockTimeMs)
	assert.Equal(t, int64(5250), *s.AvgBlockTimeMs, "the six blocks of the last hour, not the one 2 h back")
	assert.Equal(t, int64(3), s.Txs24h, "across midnight, the 24 h line excluded")

	empty := freshDatabase(t, true)
	migrateUp(t, empty)
	emptyPool, err := pgxpool.New(context.Background(), empty)
	require.NoError(t, err)
	t.Cleanup(emptyPool.Close)
	s, err = repository.NewExplorer(emptyPool).BlockStats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, &repository.BlockStats{}, s)
}
