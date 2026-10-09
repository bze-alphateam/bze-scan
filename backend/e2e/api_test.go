//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/migrations"
)

const sendHash = "E580BFA56DE28886E51DDB9BE50DD610C1C832C579CA08FCF9177B04D2F7B7B9"

// apiHeights are every fixture height that indexes, the block-only ones and
// the ones with transactions.
var apiHeights = append([]int64{h316, h317, h318, h320}, txHeights...)

// startAPI indexes every fixture height into a database of its own and
// serves the real wiring over it with the indexer off and CORS open. It
// returns the API's base URL and the indexer (for SQL).
func startAPI(t *testing.T) (string, *indexer) {
	t.Helper()
	ix := newIndexer(t)
	for _, h := range apiHeights {
		ix.index(t, h)
	}
	cfg := &config.Config{
		HTTPAddr: "127.0.0.1:0", LogLevel: "warn", LogFormat: config.LogFormatText,
		DatabaseURL: ix.url, NodeRPCURL: "http://127.0.0.1:1", ChainID: "beezee-1",
		CORSAllowedOrigins: []string{"*"},
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
		return "http://" + a.String() + "/api/v1", ix
	case err := <-done:
		t.Fatalf("serve exited early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not listen")
	}
	return "", nil
}

// TestAPI runs the read API's acceptance tests against one indexed
// database.
func TestAPI(t *testing.T) {
	base, ix := startAPI(t)
	for name, test := range map[string]func(*testing.T, string, *indexer){
		"block list paginates":          testBlockListPaginates,
		"tx list paginates and filters": testTxListPaginatesAndFilters,
		"block detail":                  testBlockDetailMatchesTheIndex,
		"tx detail":                     testTxDetailMatchesTheIndex,
		"errors":                        testErrors,
		"search":                        testSearchResolvesEachKind,
		"cors":                          testCORSHeadersWhenConfigured,
		"partition pruning":             testListQueriesPrunePartitions,
	} {
		t.Run(name, func(t *testing.T) { test(t, base, ix) })
	}
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func fetch(t *testing.T, url string, header ...string) response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return response{status: resp.StatusCode, header: resp.Header, body: body}
}

func into[T any](t *testing.T, r response) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(r.body, &v), string(r.body))
	return v
}

func testBlockListPaginates(t *testing.T, base string, _ *indexer) {
	var heights []int64
	pages := 0
	url := base + "/blocks?limit=2"
	for {
		r := fetch(t, url)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
		page := into[dto.List[dto.BlockSummary]](t, r)
		pages++
		require.LessOrEqual(t, len(page.Items), 2)
		for _, b := range page.Items {
			heights = append(heights, b.Height)
		}
		if page.NextCursor == nil {
			break
		}
		url = base + "/blocks?limit=2&cursor=" + *page.NextCursor
	}

	want := slices.Clone(apiHeights)
	slices.Sort(want)
	slices.Reverse(want)
	assert.Equal(t, want, heights, "every indexed block once, height descending")
	assert.Equal(t, (len(want)+1)/2, pages)
}

func testTxListPaginatesAndFilters(t *testing.T, base string, ix *indexer) {
	var all []string
	url := base + "/txs?limit=3"
	for {
		page := into[dto.List[dto.TxSummary]](t, fetch(t, url))
		for _, tx := range page.Items {
			all = append(all, fmt.Sprintf("%d/%d", tx.Height, tx.TxIndex))
		}
		if page.NextCursor == nil {
			break
		}
		url = base + "/txs?limit=3&cursor=" + *page.NextCursor
	}
	// queryStrings sorts its rows; with one-digit indexes that is (height,
	// index) ascending.
	want := queryStrings(t, ix.db, `SELECT height || '/' || tx_index FROM explorer.transactions`)
	slices.Reverse(want)
	assert.Equal(t, want, all, "every transaction once, height and index descending")

	var failed []string
	for _, tx := range into[dto.List[dto.TxSummary]](t, fetch(t, base+"/txs?status=failed&limit=100")).Items {
		assert.False(t, tx.Success)
		failed = append(failed, tx.Hash)
	}
	assert.ElementsMatch(t, queryStrings(t, ix.db, `SELECT hash FROM explorer.transactions WHERE NOT success`), failed)
	assert.Contains(t, failed, ix.one(t, `SELECT hash FROM explorer.transactions WHERE height = $1 AND NOT success`, hFailed))
	for _, tx := range into[dto.List[dto.TxSummary]](t, fetch(t, base+"/txs?status=success&limit=100")).Items {
		assert.True(t, tx.Success)
	}
}

func testBlockDetailMatchesTheIndex(t *testing.T, base string, ix *indexer) {
	r := fetch(t, fmt.Sprintf("%s/blocks/%d", base, hSend))
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	// No validator is synced here, so the proposer is not named yet and the
	// block is not cacheable until it is.
	assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
	b := into[dto.Block](t, r)
	assert.Nil(t, b.Proposer)

	assert.Equal(t, hSend, b.Height)
	assert.Equal(t, ix.one(t, `SELECT hash FROM explorer.blocks WHERE height = $1`, hSend), b.Hash)
	assert.Equal(t, "true", ix.one(t, `SELECT (time = $2::timestamptz)::text FROM explorer.blocks WHERE height = $1`,
		hSend, b.Time.Format(time.RFC3339Nano)))
	require.Len(t, b.Transactions, 2)
	assert.Equal(t, int64(2), b.TxCount)
	tx := b.Transactions[0]
	assert.Equal(t, sendHash, tx.Hash)
	assert.Equal(t, hSend, tx.Height)
	assert.Equal(t, []string{"/cosmos.bank.v1beta1.MsgSend"}, tx.MsgTypes)
	assert.JSONEq(t, `[{"denom":"ubze","amount":"2000"}]`, string(tx.Fee))
	require.NotNil(t, tx.Signer)
	assert.Equal(t, "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw", *tx.Signer)

	// A block without transactions answers an empty list, with the summary
	// columns the indexer wrote.
	empty := into[dto.Block](t, fetch(t, fmt.Sprintf("%s/blocks/%d", base, h317)))
	assert.Empty(t, empty.Transactions)
	assert.NotNil(t, empty.Transactions)
	require.NotNil(t, empty.BlockTimeMs)
	assert.Equal(t, ix.one(t, `SELECT block_time_ms::text FROM explorer.blocks WHERE height = $1`, h317),
		fmt.Sprint(*empty.BlockTimeMs))
}

func testTxDetailMatchesTheIndex(t *testing.T, base string, ix *indexer) {
	r := fetch(t, base+"/txs/"+strings.ToLower(sendHash))
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, "public, max-age=31536000, immutable", r.header.Get("Cache-Control"))
	tx := into[dto.Tx](t, r)
	assert.Equal(t, sendHash, tx.Hash)
	assert.Equal(t, hSend, tx.Height)
	assert.Equal(t, int64(0), tx.TxIndex)
	assert.True(t, tx.Success)
	assert.Equal(t, []string{"bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw"}, tx.Signers)
	require.Len(t, tx.Messages, 1)
	m := tx.Messages[0]
	assert.Equal(t, "/cosmos.bank.v1beta1.MsgSend", m.TypeURL)
	assert.Equal(t, "bank", *m.Module)
	assert.JSONEq(t, ix.one(t, `SELECT body::text FROM explorer.messages WHERE height = $1 AND tx_index = 0`, hSend), string(m.Body))
	assert.JSONEq(t, ix.one(t, `SELECT events::text FROM explorer.messages WHERE height = $1 AND tx_index = 0`, hSend), string(m.Events))

	// The failed transaction: error fields, the attempted message, no events.
	hash := ix.one(t, `SELECT hash FROM explorer.transactions WHERE height = $1 AND NOT success`, hFailed)
	failed := into[dto.Tx](t, fetch(t, base+"/txs/"+hash))
	assert.False(t, failed.Success)
	assert.Equal(t, int64(11), failed.Code)
	assert.Equal(t, "sdk", *failed.Codespace)
	assert.True(t, strings.HasPrefix(*failed.ErrorLog, "out of gas"))
	require.Len(t, failed.Messages, 1)
	assert.Equal(t, "/bze.tradebin.MsgCreateOrder", failed.Messages[0].TypeURL)
	assert.JSONEq(t, `[]`, string(failed.Messages[0].Events))
}

func testErrors(t *testing.T, base string, _ *indexer) {
	for path, want := range map[string]int{
		"/blocks/1":                        http.StatusNotFound,
		"/txs/" + strings.Repeat("ab", 32): http.StatusNotFound,
		"/blocks/0":                        http.StatusBadRequest,
		"/blocks/latest":                   http.StatusBadRequest,
		"/txs/xyz":                         http.StatusBadRequest,
		"/blocks?limit=101":                http.StatusBadRequest,
		"/blocks?cursor=bogus":             http.StatusBadRequest,
		"/txs?status=maybe":                http.StatusBadRequest,
		"/search":                          http.StatusBadRequest,
	} {
		r := fetch(t, base+path)
		assert.Equal(t, want, r.status, path)
		code := map[int]string{http.StatusNotFound: "not_found", http.StatusBadRequest: "bad_request"}[want]
		assert.Equal(t, code, into[struct {
			Error struct{ Code string } `json:"error"`
		}](t, r).Error.Code, path)
		assert.Equal(t, "no-store", r.header.Get("Cache-Control"), path)
	}
}

func testSearchResolvesEachKind(t *testing.T, base string, ix *indexer) {
	const indexed = "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw"
	_, key, err := bech32.DecodeAndConvert(indexed)
	require.NoError(t, err)
	operator, err := bech32.ConvertAndEncode("bzevaloper", key)
	require.NoError(t, err)
	unknown, err := bech32.ConvertAndEncode("bze", make([]byte, 20))
	require.NoError(t, err)
	// Accounts and validators are written by later stories; seed one of each.
	_, err = ix.db.Exec(`INSERT INTO explorer.accounts (address, first_seen_height, first_seen_time, last_seen_height)
		VALUES ($1, 1, now(), 1) ON CONFLICT DO NOTHING`, indexed)
	require.NoError(t, err)
	_, err = ix.db.Exec(`INSERT INTO explorer.validators (operator_address, account_address, moniker, status, tokens,
		delegator_shares, commission_rate, commission_max_rate, commission_max_change_rate, updated_at)
		VALUES ($1, $2, 'Vidulum', 'bonded', 1, 1, 0.1, 0.2, 0.01, now()) ON CONFLICT DO NOTHING`, operator, indexed)
	require.NoError(t, err)

	search := func(q string) []dto.SearchResult {
		t.Helper()
		r := fetch(t, base+"/search?q="+q)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		return into[dto.SearchResponse](t, r).Results
	}
	yes, no := true, false

	assert.Equal(t, []dto.SearchResult{{Type: "block", ID: "25000894", Label: "Block 25000894"}}, search("25000894"))
	assert.Empty(t, search("25000895"), "a height that is not indexed")
	assert.Equal(t, "transaction", search(strings.ToLower(sendHash))[0].Type)
	assert.Equal(t, sendHash, search(sendHash)[0].ID)
	assert.Equal(t, []dto.SearchResult{{Type: "account", ID: indexed, Label: indexed, Indexed: &yes}}, search(indexed))
	assert.Equal(t, []dto.SearchResult{{Type: "account", ID: unknown, Label: unknown, Indexed: &no}}, search(unknown))
	assert.Equal(t, []dto.SearchResult{{Type: "validator", ID: operator, Label: "Vidulum"}}, search(operator))
	assert.Empty(t, search("nothing"))
}

func testCORSHeadersWhenConfigured(t *testing.T, base string, _ *indexer) {
	r := fetch(t, base+"/blocks?limit=1", "Origin", "https://example.org")
	assert.Equal(t, "*", r.header.Get("Access-Control-Allow-Origin"))
}

// recordingDB remembers the last statement the repository ran.
type recordingDB struct {
	*pgxpool.Pool
	sql  string
	args []any
}

func (r *recordingDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	r.sql, r.args = sql, args
	return r.Pool.Query(ctx, sql, args...)
}

// scannedRelations returns the relations an EXPLAIN of the statement reads.
func scannedRelations(t *testing.T, pool *pgxpool.Pool, sql string, args []any) []string {
	t.Helper()
	var plan []struct {
		Plan planNode `json:"Plan"`
	}
	require.NoError(t, pool.QueryRow(context.Background(), "EXPLAIN (FORMAT JSON) "+sql, args...).Scan(&plan))
	var rels []string
	var walk func(n planNode)
	walk = func(n planNode) {
		if n.Relation != "" {
			rels = append(rels, n.Relation)
		}
		for _, c := range n.Plans {
			walk(c)
		}
	}
	walk(plan[0].Plan)
	return rels
}

type planNode struct {
	Relation string     `json:"Relation Name"`
	Plans    []planNode `json:"Plans"`
}

func testListQueriesPrunePartitions(t *testing.T, _ string, ix *indexer) {
	pool, err := pgxpool.New(context.Background(), ix.url)
	require.NoError(t, err)
	defer pool.Close()
	rec := &recordingDB{Pool: pool}
	repo := repository.NewExplorer(rec)
	ctx := context.Background()

	cursor := hTransfer // partition 24
	// The proposer's name is a lookup in the (unpartitioned) validators.
	allowed := append(migrations.PartitionNames("blocks", 0, cursor), "validators")

	_, err = repo.Blocks(ctx, &cursor, 2)
	require.NoError(t, err)
	rels := scannedRelations(t, pool, rec.sql, rec.args)
	require.NotEmpty(t, rels)
	for _, r := range rels {
		assert.Contains(t, allowed, r, "partitions above the cursor are pruned")
	}
	assert.NotContains(t, rels, migrations.PartitionName("blocks", hSend), "the next partition up is not read")

	_, err = repo.Txs(ctx, &repository.TxKey{Height: cursor, TxIndex: 0}, nil, 2)
	require.NoError(t, err)
	allowed = migrations.PartitionNames("transactions", 0, cursor)
	for _, r := range scannedRelations(t, pool, rec.sql, rec.args) {
		assert.Contains(t, allowed, r)
	}
}
