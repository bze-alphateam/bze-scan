//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/cli"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakeregistry"
)

// The recorded validator set: 57 validators, 22 bonded. ChainTools is ranked
// 10th and is the target of the delegations in block 25000440, in which all
// 22 bonded validators signed.
const (
	recordedValidators = 57
	recordedBonded     = 22
	chainTools         = "bzevaloper1prm55vzlp5u6excqdunwlm4tw254cq943m6e6m"
	chainToolsOwner    = "bze1prm55vzlp5u6excqdunwlm4tw254cq94fl9jwy"
	restakeHeight      = int64(25000440)
)

// syncEnv is a migrated database of its own, the fake node and the fake
// gRPC server.
type syncEnv struct {
	url  string
	db   *sql.DB
	node *fakenode.Node
	grpc *fakenode.GRPC
	// reg and agg stand in for the chain registry and the aggregator, the
	// ticker jobs' outbound HTTPS.
	reg *fakeregistry.Registry
	agg *fakeAggregator
}

func newSyncEnv(t *testing.T) *syncEnv {
	t.Helper()
	url := freshDatabase(t, true)
	migrateUp(t, url)
	return &syncEnv{url: url, db: connect(t, url), node: fakenode.New(t), grpc: fakenode.NewGRPC(t),
		reg: fakeregistry.New(t), agg: newFakeAggregator(t)}
}

// tickers points cfg's ticker jobs at the fakes.
func (e *syncEnv) tickers(cfg *config.Config) *config.Config {
	cfg.ChainRegistryAPIURL, cfg.ChainRegistryRawURL, cfg.AggregatorURL = e.reg.APIURL, e.reg.RawURL, e.agg.URL
	cfg.PriceChangeMarket = config.DefaultPriceChangeMarket
	return cfg
}

// syncState runs the sync-state command and returns its exit code and
// standard output.
func (e *syncEnv) syncState(t *testing.T) (int, string) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", e.url)
	t.Setenv("NODE_GRPC_ADDR", e.grpc.Addr)
	t.Setenv("CHAIN_REGISTRY_API_URL", e.reg.APIURL)
	t.Setenv("CHAIN_REGISTRY_RAW_URL", e.reg.RawURL)
	t.Setenv("AGGREGATOR_URL", e.agg.URL)
	t.Setenv("LOG_LEVEL", "warn")
	root := cli.NewRootCmd()
	root.SetArgs([]string{"sync-state"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	return cli.ExitCode(root.Execute()), out.String()
}

// serve runs the real serve wiring with the indexer, the node's tip at tip,
// and returns the API's base URL once it listens.
func (e *syncEnv) serve(t *testing.T, tip int64) string {
	t.Helper()
	e.node.SetStatusHeight(tip)
	cfg := e.tickers(&config.Config{
		HTTPAddr: "127.0.0.1:0", LogLevel: "info", LogFormat: config.LogFormatText,
		DatabaseURL: e.url, NodeRPCURL: e.node.URL, ChainID: "beezee-1", IndexerEnabled: true,
		NodeGRPCAddr: e.grpc.Addr, ArchiveRPCURL: e.node.URL,
		StatusInterval: time.Minute, RawCacheMaxEntries: 10, RawCacheTTL: time.Minute,
	})
	ctx, cancel := context.WithCancel(context.Background())
	listening := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- serve.Run(ctx, cfg, serve.Options{OnListen: func(a net.Addr) { listening <- a }}) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(15 * time.Second):
			t.Error("serve did not stop")
		}
	})
	select {
	case a := <-listening:
		return "http://" + a.String()
	case err := <-done:
		t.Fatalf("serve exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not listen")
	}
	return ""
}

func (e *syncEnv) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, e.db.QueryRow(query, args...).Scan(&n))
	return n
}

func (e *syncEnv) validator(t *testing.T, op string) string {
	t.Helper()
	rows := queryStrings(t, e.db, `SELECT concat_ws(' ', status, coalesce(rank::text, '-'), coalesce(voting_power_pct::text, '-'), moniker)
		FROM explorer.validators WHERE operator_address = $1`, op)
	require.Len(t, rows, 1)
	return rows[0]
}

func getValidators(t *testing.T, base, query string) dto.List[dto.Validator] {
	t.Helper()
	resp, err := http.Get(base + "/api/v1/validators" + query)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var page dto.List[dto.Validator]
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
	return page
}

func TestServeSyncsValidatorsAtStartAndListsThemInRankOrder(t *testing.T) {
	e := newSyncEnv(t)
	base := e.serve(t, h316)

	waitFor(t, 10*time.Second, func() bool {
		return e.count(t, `SELECT count(*) FROM explorer.validators`) == recordedValidators
	}, "the validators")
	assert.Equal(t, recordedBonded, e.count(t, `SELECT count(*) FROM explorer.validators WHERE rank IS NOT NULL`))
	assert.Equal(t, "bonded 10 5.37048 ChainTools", e.validator(t, chainTools))
	assert.Equal(t, []string{"ChainTools validator_owner sync"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', name, kind, source) FROM explorer.labels WHERE address = $1`, chainToolsOwner))
	assert.Equal(t, recordedValidators, e.count(t, `SELECT count(*) FROM explorer.labels WHERE kind = 'validator_owner'`))
	waitFor(t, 5*time.Second, func() bool {
		return len(queryStrings(t, e.db, `SELECT job FROM explorer.sync_jobs WHERE job = 'validators' AND last_success_at IS NOT NULL AND last_error IS NULL`)) == 1
	}, "the sync_jobs row")

	// Every validator, the bonded ones first by rank, page by page.
	var all []dto.Validator
	cursor := ""
	for {
		page := getValidators(t, base, "?limit=20"+cursor)
		all = append(all, page.Items...)
		if page.NextCursor == nil {
			break
		}
		cursor = "&cursor=" + *page.NextCursor
	}
	require.Len(t, all, recordedValidators)
	for i, v := range all[:recordedBonded] {
		require.NotNil(t, v.Rank, v.Moniker)
		assert.Equal(t, int64(i+1), *v.Rank)
		assert.Equal(t, "bonded", v.Status)
	}
	for _, v := range all[recordedBonded:] {
		assert.Nil(t, v.Rank, v.Moniker)
		assert.NotEqual(t, "bonded", v.Status)
	}
	assert.Equal(t, "ChainTools", all[9].Moniker)
	assert.Equal(t, "100.00000", *all[9].Uptime)

	bonded := getValidators(t, base, "?status=bonded&limit=100")
	assert.Len(t, bonded.Items, recordedBonded)
	assert.Equal(t, all[:recordedBonded], bonded.Items)

	// The search resolves a validator's operator address.
	resp, err := http.Get(base + "/api/v1/search?q=" + chainTools)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var found dto.SearchResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&found))
	assert.Equal(t, []dto.SearchResult{{Type: dto.ResultValidator, ID: chainTools, Label: "ChainTools"}}, found.Results)
}

func TestSyncStateFillsAnEmptySchema(t *testing.T) {
	e := newSyncEnv(t)
	require.Zero(t, e.count(t, `SELECT count(*) FROM explorer.validators`))

	code, out := e.syncState(t)
	require.Equal(t, 0, code)
	assert.Equal(t, "sync-state done\n", out)
	assert.Equal(t, recordedValidators, e.count(t, `SELECT count(*) FROM explorer.validators`))
	assert.Equal(t, recordedValidators, e.count(t, `SELECT count(*) FROM explorer.labels WHERE kind = 'validator_owner'`))
	assert.Equal(t, 1, e.grpc.Requests("staking", "Validators"))

	// Every column of one row, as the node describes it.
	assert.Equal(t, []string{strings.Join([]string{
		chainToolsOwner, "090703A2C594C5BA93C0D0E263A9F79AEEE17D10", "ikmP1GM73Y1vVKsLZYjFENPGve7uLWV3Q+8YF60LHMA=",
		"ChainTools", "1857FF1BC2EA6A69", "https://chaintools.tech", "contact@chaintools.tech", "bonded", "f", "f", "-",
		"7895930982372", "7895930982372.000000000000000000", "0.050000000000000000", "0.500000000000000000",
		"0.100000000000000000", "2022-04-29 05:31:29.027929+00", "1", "30081000000", "57", "10", "5.37048", "0", "10000",
	}, "|")}, queryStrings(t, e.db, `SELECT concat_ws('|', account_address, consensus_address, consensus_pubkey, moniker,
			identity, website, security_contact, status, jailed, tombstoned, coalesce(jailed_until::text, '-'), tokens,
			delegator_shares, commission_rate, commission_max_rate, commission_max_change_rate, commission_update_time AT TIME ZONE 'UTC' || '+00',
			min_self_delegation, self_delegation, delegator_count, rank, voting_power_pct, missed_blocks, signed_blocks_window)
		  FROM explorer.validators WHERE operator_address = $1`, chainTools))
	assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM explorer.validators
		WHERE operator_address = $1 AND first_seen_time IS NOT NULL AND updated_at IS NOT NULL AND details IS NOT NULL`, chainTools))

	// Again: the same rows, no duplicate labels.
	code, _ = e.syncState(t)
	require.Equal(t, 0, code)
	assert.Equal(t, recordedValidators, e.count(t, `SELECT count(*) FROM explorer.validators`))
	assert.Equal(t, recordedValidators, e.count(t, `SELECT count(*) FROM explorer.labels WHERE kind = 'validator_owner'`))
}

func TestADirtyValidatorIsResyncedOnceAfterItsBlock(t *testing.T) {
	e := newSyncEnv(t)
	code, _ := e.syncState(t) // the validators exist before the block
	require.Equal(t, 0, code)
	e.grpc.ResetRequests()

	e.serve(t, restakeHeight)
	waitFor(t, 10*time.Second, func() bool {
		return e.count(t, `SELECT count(*) FROM explorer.blocks WHERE height = $1`, restakeHeight) == 1
	}, "block %d", restakeHeight)
	waitFor(t, 10*time.Second, func() bool {
		return e.grpc.RequestsFor("staking", "Validator", chainTools) >= 1
	}, "the resync of %s", chainTools)
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 1, e.grpc.RequestsFor("staking", "Validator", chainTools), "exactly one call for the dirty validator")
	assert.Equal(t, 1, e.grpc.Requests("staking", "Validator"), "and none for the others")
	assert.Equal(t, 1, e.grpc.Requests("staking", "Validators"), "the start resync only")

	// Every bonded validator signed the block.
	assert.Equal(t, []string{"22 100.000"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', signatures_count, signatures_power_pct) FROM explorer.blocks WHERE height = $1`, restakeHeight))
}

func TestSignaturesPowerOfABackfilledBlock(t *testing.T) {
	e := newSyncEnv(t)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	t.Setenv("NODE_RPC_URL", e.node.URL)
	t.Setenv("ARCHIVE_RPC_URL", e.node.URL)
	// A bonded validator that did not sign, holding a third of the bonded
	// tokens: the signers hold two thirds, a share with more decimals than
	// the column keeps.
	_, err := e.db.Exec(`INSERT INTO explorer.validators (operator_address, account_address, moniker, status, tokens,
			delegator_shares, commission_rate, commission_max_rate, commission_max_change_rate, consensus_address, updated_at)
		SELECT 'bzevaloper1absent', 'bze1absent', 'absent', 'bonded', sum(tokens) / 2, 0, 0, 0, 0, 'NOTASIGNER', now()
		  FROM explorer.validators WHERE status = 'bonded'`)
	require.NoError(t, err)

	reindex := func() {
		root := cli.NewRootCmd()
		root.SetArgs([]string{"reindex", "--heights", fmt.Sprint(restakeHeight)})
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		require.Equal(t, 0, cli.ExitCode(root.Execute()))
	}
	version := `SELECT xmin::text || ' ' || signatures_power_pct::text FROM explorer.blocks WHERE height = $1`
	reindex()
	first := queryStrings(t, e.db, version, restakeHeight)
	require.Len(t, first, 1)
	assert.True(t, strings.HasSuffix(first[0], " 66.667"), "against today's validators: approximate for history; got %s", first[0])

	reindex()
	assert.Equal(t, first, queryStrings(t, e.db, version, restakeHeight), "a second reindex rewrites nothing")
}

func TestAValidatorGoneFromTheNodeBecomesUnbonded(t *testing.T) {
	e := newSyncEnv(t)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	require.Equal(t, "bonded 10 5.37048 ChainTools", e.validator(t, chainTools))

	var recorded struct {
		Validators []json.RawMessage `json:"validators"`
	}
	require.NoError(t, json.Unmarshal(fakenode.ReadGRPCFixture(t, "staking", "Validators", ""), &recorded))
	var kept []json.RawMessage
	for _, v := range recorded.Validators {
		if !strings.Contains(string(v), chainTools) {
			kept = append(kept, v)
		}
	}
	require.Len(t, kept, recordedValidators-1)
	body, err := json.Marshal(map[string]any{"validators": kept, "pagination": map[string]any{"next_key": nil, "total": "56"}})
	require.NoError(t, err)
	e.grpc.SetResponse("staking", "Validators", "", body)

	code, _ = e.syncState(t)
	require.Equal(t, 0, code)
	assert.Equal(t, "unbonded - - ChainTools", e.validator(t, chainTools), "kept, unbonded, unranked")
	assert.Equal(t, recordedBonded-1, e.count(t, `SELECT count(*) FROM explorer.validators WHERE rank IS NOT NULL`))
	assert.Equal(t, []string{"ChainTools"}, queryStrings(t, e.db, `SELECT name FROM explorer.labels WHERE address = $1`, chainToolsOwner))
}

func TestANodeDownKeepsTheRows(t *testing.T) {
	e := newSyncEnv(t)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	before := queryStrings(t, e.db, `SELECT row_to_json(v)::text FROM explorer.validators v ORDER BY operator_address`)

	hook := logtest.NewGlobal()
	t.Cleanup(func() { log.StandardLogger().ReplaceHooks(make(log.LevelHooks)) })
	e.grpc.Stop()
	code, _ = e.syncState(t)
	assert.Equal(t, 1, code)

	assert.Equal(t, before, queryStrings(t, e.db, `SELECT row_to_json(v)::text FROM explorer.validators v ORDER BY operator_address`))
	errs := queryStrings(t, e.db, `SELECT last_error FROM explorer.sync_jobs WHERE job = 'validators' AND last_success_at < last_run_at`)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "staking Validators")
	logged := false
	for _, entry := range hook.AllEntries() {
		if entry.Level == log.WarnLevel && strings.Contains(entry.Message, "resync failed") && entry.Data["set"] == "validators" {
			logged = true
		}
	}
	assert.True(t, logged, "the failure is logged")
}
