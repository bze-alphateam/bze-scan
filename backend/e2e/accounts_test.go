//go:build e2e

package e2e_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/labels"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// Accounts of the transaction fixtures. thamarOwner owns the validator
// Thamar; its balances, delegations and rewards are recorded in the fake
// gRPC server.
const (
	thamarOwner = "bze19fgph876c3rqxrn6xk5ch6wd73r3g05w690uls" // withdraws its rewards at hRewards
	thamar      = "bzevaloper19fgph876c3rqxrn6xk5ch6wd73r3g05wzpsht0"
	relayer     = "bze17fgrnpg48c6tkhmp72zmjt93g67y6wu96gj787" // signs at hRelay and hSend
	trader      = "bze10kw8lpqd9emyxn94gkm038t4jj90ark4x8ls0d" // 3 transactions at hFailed (one failed), 2 at hOrders
)

// serveAPI runs the real serve wiring over the env's database with the
// indexer off and the live account reads on the fake gRPC server, and
// returns the API's base URL.
func (e *syncEnv) serveAPI(t *testing.T) string {
	t.Helper()
	cfg := &config.Config{
		HTTPAddr: "127.0.0.1:0", LogLevel: "warn", LogFormat: config.LogFormatText,
		DatabaseURL: e.url, NodeRPCURL: e.node.URL, ChainID: "beezee-1", NodeGRPCAddr: e.grpc.Addr,
	}
	ctx, cancel := context.WithCancel(context.Background())
	listening := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- serve.Run(ctx, cfg, serve.Options{OnListen: func(a net.Addr) { listening <- a }}) }()
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-done)
	})
	select {
	case a := <-listening:
		return "http://" + a.String() + "/api/v1"
	case err := <-done:
		t.Fatalf("serve exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not listen")
	}
	return ""
}

func (e *syncEnv) accountRows(t *testing.T) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', address, first_seen_height, first_seen_time, last_seen_height,
		tx_count, activity_count) FROM explorer.accounts ORDER BY address`)
}

func TestAccounts(t *testing.T) {
	e := newSyncEnv(t)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	e.index(t, txHeights...)

	rows := e.accountRows(t)
	e.index(t, txHeights...)
	assert.Equal(t, rows, e.accountRows(t), "indexing the heights again counts nothing twice")
	assert.Equal(t, []string{"5 24999004 24999134"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', tx_count, first_seen_height, last_seen_height) FROM explorer.accounts WHERE address = $1`, trader),
		"every signed transaction counts, failed ones included")
	assert.Equal(t, []string{"2 24999209 25000894"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', tx_count, first_seen_height, last_seen_height) FROM explorer.accounts WHERE address = $1`, relayer))

	base := e.serveAPI(t)
	r := fetch(t, base+"/accounts/"+thamarOwner)
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
	acc := into[dto.Account](t, r)
	assert.Equal(t, &dto.Label{Name: "Thamar", Kind: labels.KindValidatorOwner}, acc.Label, "the sync labels validator owners")
	require.NotNil(t, acc.FirstSeen)
	assert.Equal(t, hRewards, acc.FirstSeen.Height)
	assert.Equal(t, int64(1), acc.TxCount)
	assert.Zero(t, acc.ActivityCount)
	assert.True(t, acc.Live.Available)
	six := 6
	assert.Equal(t, []dto.Balance{
		{Denom: "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uvdl", Amount: "25065076620", Symbol: strp("VDL"), Exponent: &six},
		{Denom: "factory/bze15pqjgk4la0mfphwddce00d05n3th3u66n3ptcv/2MARS", Amount: "17", Symbol: strp("C2M"), Exponent: &six},
		{Denom: "ubze", Amount: "30063263", Symbol: strp("BZE"), Exponent: &six},
	}, acc.Balances, "the denoms the sync wrote give the display data")
	moniker := "Thamar"
	assert.Equal(t, []dto.AccountDelegation{{Validator: thamar, Moniker: &moniker, Amount: "123881169576"}}, acc.Delegations)
	assert.Equal(t, "123881169576", *acc.TotalStaked)
	assert.Empty(t, acc.Unbonding)
	assert.Equal(t, []dto.AccountReward{{Validator: thamar, Coins: []dto.Coin{{Denom: "ubze", Amount: "19220134"}}}}, acc.Rewards)
	assert.Equal(t, []dto.Coin{{Denom: "ubze", Amount: "19220134"}}, acc.TotalRewards)

	// An address nobody indexed: the chain may still know it.
	fresh := fetch(t, base+"/accounts/"+chainToolsOwner)
	require.Equal(t, http.StatusOK, fresh.status)
	assert.Nil(t, into[dto.Account](t, fresh).FirstSeen)

	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/accounts/"+thamar).status)

	// Search by label and by moniker.
	found := into[dto.SearchResponse](t, fetch(t, base+"/search?q=dex"))
	assert.Contains(t, found.Results, dto.SearchResult{Type: dto.ResultAccount,
		ID: "bze18mhtjwczlzqqgvw84uz8lrdv4hqule3jp8allp", Label: "DEX"})
	found = into[dto.SearchResponse](t, fetch(t, base+"/search?q=thama"))
	assert.Equal(t, []dto.SearchResult{
		{Type: dto.ResultValidator, ID: thamar, Label: "Thamar"},
		{Type: dto.ResultAccount, ID: thamarOwner, Label: "Thamar"},
	}, found.Results)

	// With the node down, the indexed part still answers.
	e.grpc.Stop()
	down := fetch(t, e.serveAPI(t)+"/accounts/"+thamarOwner) // a new process: an empty cache
	require.Equal(t, http.StatusOK, down.status)
	acc = into[dto.Account](t, down)
	assert.False(t, acc.Live.Available)
	assert.Equal(t, int64(1), acc.TxCount)
	assert.Empty(t, acc.Balances)
	assert.Nil(t, acc.TotalStaked)
}

// The backfill's batch writer counts exactly what the live path counts,
// once.
func TestTheBatchWriterCountsAccountsLikeTheLivePath(t *testing.T) {
	live := newSyncEnv(t)
	live.index(t, txHeights...)

	batch := newSyncEnv(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, batch.url)
	require.NoError(t, err)
	defer pool.Close()
	client := node.New(batch.node.URL)
	tr := newTransformer(t)
	var ents []*transform.Entities
	for i := len(txHeights) - 1; i >= 0; i-- { // downward, as the backfill walks
		h := txHeights[i]
		b, _, err := client.Block(ctx, h)
		require.NoError(t, err)
		r, _, err := client.BlockResults(ctx, h)
		require.NoError(t, err)
		c, _, err := client.Commit(ctx, h)
		require.NoError(t, err)
		e, err := tr.Transform(transform.Input{Block: b, Results: r, Commit: c})
		require.NoError(t, err)
		ents = append(ents, e)
	}
	w := writer.NewBatchWriter(pool)
	require.NoError(t, w.Write(ctx, ents, writer.ModeInsert))
	require.NoError(t, w.Write(ctx, ents, writer.ModeUpdate), "a reindex of the same heights")

	assert.Equal(t, live.accountRows(t), batch.accountRows(t))
}

func TestMigrateSeedsOneLabelPerModuleAccount(t *testing.T) {
	url := freshDatabase(t, true)
	migrateUp(t, url)
	db := connect(t, url)
	query := `SELECT concat_ws(' ', address, name, module, source, xmin) FROM explorer.labels WHERE kind = 'module' ORDER BY address`
	first := queryStrings(t, db, query)
	assert.Len(t, first, len(labels.Modules()))

	migrateUp(t, url)
	assert.Equal(t, first, queryStrings(t, db, query), "migrating again rewrites no row")
}

func strp(s string) *string { return &s }
