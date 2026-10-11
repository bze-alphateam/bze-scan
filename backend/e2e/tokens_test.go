//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// The recorded mainnet denoms (bank supply and metadata of chain v8.1.1).
const (
	recordedDenoms = 29 // 28 with a supply, one with metadata only
	vdlDenom       = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uvdl"
	usdcDenom      = "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4"
)

// tokenPages reads every page of the tokens list.
func tokenPages(t *testing.T, base, query string) []dto.TokenSummary {
	t.Helper()
	var all []dto.TokenSummary
	cursor := ""
	for {
		r := fetch(t, base+"/api/v1/tokens?limit=7"+query+cursor)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		page := into[dto.List[dto.TokenSummary]](t, r)
		all = append(all, page.Items...)
		if page.NextCursor == nil {
			return all
		}
		cursor = "&cursor=" + *page.NextCursor
	}
}

func TestServeSyncsTheDenomsAndListsTheTokens(t *testing.T) {
	e := newSyncEnv(t)
	base := e.serve(t, h316)
	waitFor(t, 10*time.Second, func() bool {
		return e.count(t, `SELECT count(*) FROM explorer.denoms`) == recordedDenoms
	}, "the denoms")
	waitFor(t, 5*time.Second, func() bool {
		return len(queryStrings(t, e.db, `SELECT job FROM explorer.sync_jobs WHERE job = 'denoms' AND last_error IS NULL`)) == 1
	}, "the sync_jobs row")

	// Every denom by kind then symbol, page by page.
	all := tokenPages(t, base, "")
	require.Len(t, all, recordedDenoms)
	kinds := map[string]int{}
	for _, tok := range all {
		kinds[tok.Kind]++
	}
	assert.Equal(t, map[string]int{"native": 1, "factory": 6, "ibc": 19, "lp": 3}, kinds)
	assert.Equal(t, "ubze", all[0].Denom)
	assert.Equal(t, "BZE", *all[0].Symbol)
	assert.Equal(t, 6, all[0].Exponent)
	assert.Equal(t, "factory", all[1].Kind)
	for i := 1; i < len(all); i++ {
		assert.LessOrEqual(t, rank(all[i-1].Kind), rank(all[i].Kind), "kinds in order")
	}
	factories := tokenPages(t, base, "&kind=factory")
	assert.Len(t, factories, 6)
	// Every item carries the base denom, the exponent, the logo and the
	// origin chain, null or not.
	var raw struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	require.NoError(t, json.Unmarshal(fetch(t, base+"/api/v1/tokens?limit=100").body, &raw))
	require.Len(t, raw.Items, recordedDenoms)
	for _, item := range raw.Items {
		for _, key := range []string{"denom", "exponent", "logo_url", "origin_chain"} {
			assert.Contains(t, item, key)
		}
		assert.NotEqual(t, `""`, string(item["denom"]))
	}

	// A factory denom, URL-encoded on the path or as ?denom=.
	for _, path := range []string{"/api/v1/tokens/" + url.PathEscape(vdlDenom), "/api/v1/token?denom=" + url.QueryEscape(vdlDenom)} {
		r := fetch(t, base+path)
		require.Equal(t, http.StatusOK, r.status, path+": "+string(r.body))
		tok := into[dto.Token](t, r)
		assert.Equal(t, "VDL", *tok.Symbol)
		assert.Equal(t, 6, tok.Exponent)
		assert.Equal(t, "20983185271838", *tok.Supply)
		assert.Equal(t, "bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk", *tok.Creator)
		assert.Nil(t, tok.Admin, "renounced: the supply is fixed")
		assert.Contains(t, tok.Markets, vdlDenom+"/ubze")
		assert.False(t, tok.Halted, "chain v8.1.1 has no halt")
		assert.NotEmpty(t, tok.Metadata)
		assert.Empty(t, tok.Events)
	}
	ibc := into[dto.Token](t, fetch(t, base+"/api/v1/tokens/"+url.PathEscape(usdcDenom)))
	assert.Equal(t, "ibc", ibc.Kind)
	require.NotNil(t, ibc.OriginChainID, "the IBC origin through its channel")
	assert.Equal(t, "noble-1", *ibc.OriginChainID)
	assert.Equal(t, http.StatusNotFound, fetch(t, base+"/api/v1/tokens/"+url.PathEscape("factory/bze1nobody/uzz")).status)
}

func rank(kind string) int {
	return map[string]int{"native": 0, "factory": 1, "ibc": 2, "lp": 3}[kind]
}

// withoutDenom is a recorded list answer (TotalSupply's supply,
// DenomsMetadata's metadatas) with denom left out.
func withoutDenom(t *testing.T, method, list, key, denom string) []byte {
	t.Helper()
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(fakenode.ReadGRPCFixture(t, "bank", method, ""), &body))
	var items []map[string]any
	require.NoError(t, json.Unmarshal(body[list], &items))
	kept := items[:0]
	for _, it := range items {
		if it[key] != denom {
			kept = append(kept, it)
		}
	}
	require.Len(t, kept, len(items)-1)
	raw, err := json.Marshal(kept)
	require.NoError(t, err)
	body[list] = raw
	out, err := json.Marshal(body)
	require.NoError(t, err)
	return out
}

// A denom the explorer does not hold yet appears within the block that
// moves it: the transformer marks it seen, the denoms set resyncs it alone.
// Denoms it already holds are not resynced.
func TestADenomFirstSeenInABlockIsSyncedAfterIt(t *testing.T) {
	e := newSyncEnv(t)
	e.grpc.SetResponse("bank", "TotalSupply", "", withoutDenom(t, "TotalSupply", "supply", "denom", usdcDenom))
	e.grpc.SetResponse("bank", "DenomsMetadata", "", withoutDenom(t, "DenomsMetadata", "metadatas", "base", usdcDenom))

	e.serve(t, hOrders)
	waitFor(t, 10*time.Second, func() bool {
		return e.count(t, `SELECT count(*) FROM explorer.blocks WHERE height = $1`, hOrders) == 1
	}, "block %d", hOrders)
	waitFor(t, 10*time.Second, func() bool {
		return e.count(t, `SELECT count(*) FROM explorer.denoms WHERE denom = $1`, usdcDenom) == 1
	}, "the denom first seen in block %d", hOrders)

	var supply struct {
		Amount struct{ Amount string } `json:"amount"`
	}
	require.NoError(t, json.Unmarshal(fakenode.ReadGRPCFixture(t, "bank", "SupplyOf", url.PathEscape(usdcDenom)), &supply))
	assert.Equal(t, []string{"ibc USDC.n " + supply.Amount.Amount}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', kind, symbol, supply) FROM explorer.denoms WHERE denom = $1`, usdcDenom))
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 1, e.grpc.Requests("bank", "SupplyOf")-e.grpc.RequestsFor("bank", "SupplyOf", "ubze"),
		"the denoms set asks for the unknown denom only (ubze's supply is the chain state's)")
	assert.Equal(t, 1, e.grpc.RequestsFor("bank", "SupplyOf", url.PathEscape(usdcDenom)))
	assert.Equal(t, recordedDenoms, e.count(t, `SELECT count(*) FROM explorer.denoms`))
}

// The token page's first sight and history come from token_events, and its
// transfers from the indexed transfers.
func TestTokenHistoryAndTransfers(t *testing.T) {
	e := newSyncEnv(t)
	base := e.serve(t, hOrders)
	waitFor(t, 10*time.Second, func() bool {
		return e.count(t, `SELECT count(*) FROM explorer.blocks WHERE height = $1`, hOrders) == 1 &&
			e.count(t, `SELECT count(*) FROM explorer.denoms`) == recordedDenoms
	}, "block %d and the denoms", hOrders)

	// No recorded height creates a denom: a created row as the indexer
	// writes it, in a transaction of the block.
	hash := queryStrings(t, e.db, `SELECT hash FROM explorer.transactions WHERE height = $1 AND tx_index = 0`, hOrders)[0]
	_, err := e.db.Exec(`INSERT INTO explorer.token_events (height, tx_index, seq, denom, kind, actor, details, time)
		SELECT $1, 0, 0, $2, 'created', 'bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk', '{"subdenom":"uvdl"}', time
		FROM explorer.blocks WHERE height = $1`, hOrders, vdlDenom)
	require.NoError(t, err)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)

	tok := into[dto.Token](t, fetch(t, base+"/api/v1/tokens/"+url.PathEscape(vdlDenom)))
	require.NotNil(t, tok.CreatedHeight)
	assert.Equal(t, hOrders, *tok.CreatedHeight, "first sight from the created event")
	assert.Equal(t, hash, *tok.CreatedTxHash)
	require.Len(t, tok.Events, 1)
	assert.Equal(t, "created", tok.Events[0].Kind)
	assert.Equal(t, hash, *tok.Events[0].TxHash)
	assert.JSONEq(t, `{"subdenom":"uvdl"}`, string(tok.Events[0].Details))
	events := into[dto.List[dto.TokenEvent]](t, fetch(t, base+"/api/v1/tokens/"+url.PathEscape(vdlDenom)+"/events"))
	assert.Len(t, events.Items, 1)

	// The denom's moves, newest first, page by page.
	want := e.count(t, `SELECT count(*) FROM explorer.transfers WHERE denom = $1`, usdcDenom)
	require.Positive(t, want)
	var got []dto.TokenTransfer
	next := base + "/api/v1/tokens/" + url.PathEscape(usdcDenom) + "/transfers?limit=2"
	for {
		r := fetch(t, next)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		page := into[dto.List[dto.TokenTransfer]](t, r)
		got = append(got, page.Items...)
		if page.NextCursor == nil {
			break
		}
		next = fmt.Sprintf("%s/api/v1/token/transfers?denom=%s&limit=2&cursor=%s", base, url.QueryEscape(usdcDenom), *page.NextCursor)
	}
	require.Len(t, got, want)
	for i, x := range got {
		assert.Equal(t, usdcDenom, x.Denom)
		assert.Equal(t, hOrders, x.Height)
		if i > 0 {
			prev := got[i-1]
			assert.True(t, prev.TxIndex > x.TxIndex || (prev.TxIndex == x.TxIndex && prev.Seq > x.Seq), "newest first")
		}
		if x.TxIndex == -1 {
			assert.Nil(t, x.TxHash, "a block-level settlement")
		}
	}

	// transfers_24h: the only block is the latest, so all of its moves.
	volume := func() dto.TransferVolume {
		t.Helper()
		r := fetch(t, base+"/api/v1/tokens/"+url.PathEscape(usdcDenom))
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		return into[dto.Token](t, r).Transfers24h
	}
	sum := queryStrings(t, e.db, `SELECT sum(amount)::text FROM explorer.transfers WHERE denom = $1`, usdcDenom)[0]
	assert.Equal(t, dto.TransferVolume{Count: int64(want), Amount: sum}, volume())

	// A newer latest block 25 hours later moves the window past the
	// recorded block. A move 2 hours after it is inside the window, one
	// exactly 24 hours before the latest block is not.
	inside, edge, latest := hOrders+500, hOrders+300, hOrders+1000
	_, err = e.db.Exec(`INSERT INTO explorer.blocks (height, time, hash)
		SELECT v.height, b.time + v.later, 'SEEDED' FROM explorer.blocks b,
		       (VALUES ($2::bigint, interval '2 hours'), ($3::bigint, interval '1 hour'), ($4::bigint, interval '25 hours')) v(height, later)
		 WHERE b.height = $1`, hOrders, inside, edge, latest)
	require.NoError(t, err)
	_, err = e.db.Exec(`INSERT INTO explorer.transfers (height, tx_index, seq, kind, sender, recipient, denom, amount)
		VALUES ($1, -1, 0, 'transfer', 'bze1a', 'bze1b', $3, 7), ($2, -1, 0, 'transfer', 'bze1a', 'bze1b', $3, 100)`,
		inside, edge, usdcDenom)
	require.NoError(t, err)
	assert.Equal(t, dto.TransferVolume{Count: 1, Amount: "7"}, volume())

	// A denom that never moved counts nothing.
	still := queryStrings(t, e.db, `SELECT denom FROM explorer.denoms d
		WHERE NOT EXISTS (SELECT 1 FROM explorer.transfers x WHERE x.denom = d.denom)`)
	require.NotEmpty(t, still)
	assert.Equal(t, dto.TransferVolume{Count: 0, Amount: "0"}, func() dto.TransferVolume {
		r := fetch(t, base+"/api/v1/tokens/"+url.PathEscape(still[0]))
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		return into[dto.Token](t, r).Transfers24h
	}())
}
