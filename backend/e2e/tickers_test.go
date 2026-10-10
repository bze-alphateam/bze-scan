//go:build e2e

package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// recordedPrices is the aggregator's /api/prices as getbze.com answered it
// on 2026-10-09: USD prices by CoinGecko id.
const recordedPrices = `[{"denom":"bzedge","price":0.00016806,"price_denom":"usd"},` +
	`{"denom":"osmosis","price":0.03403806,"price_denom":"usd"},{"denom":"cosmos","price":1.96,"price_denom":"usd"}]`

// recordedTickers are the two BZE/USDC.n markets of /api/dex/tickers on
// getbze.com on 2026-10-09: the order book and the liquidity pool, whose
// ticker gives BZE's 24-hour change (USDC.n +2.18 % in ubze: BZE -2.1362 %).
const recordedTickers = `[{"base":"ubze","quote":"ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4",` +
	`"market_id":"ubze/ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4","last_price":0.00018,` +
	`"base_volume":544336.988678,"quote_volume":103.836199,"bid":0.00017,"ask":0.00022,"high":0.00021,"low":0.00018,` +
	`"open_price":0.00018,"change":0},{"base":"ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4",` +
	`"quote":"ubze","market_id":"ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4_ubze",` +
	`"last_price":5747.1387947581325,"base_volume":144.033022,"quote_volume":828644.939685,"bid":0,"ask":0,` +
	`"high":6040.911873237037,"low":5604.113494443305,"open_price":5624.368717574341,"change":2.18}]`

// fakeAggregator answers /api/prices with recordedPrices and
// /api/dex/tickers with recordedTickers until it is told to fail.
type fakeAggregator struct {
	URL  string
	mu   sync.Mutex
	down bool
}

func newFakeAggregator(t *testing.T) *fakeAggregator {
	t.Helper()
	a := &fakeAggregator{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		down := a.down
		a.mu.Unlock()
		body := map[string]string{"/api/prices": recordedPrices, "/api/dex/tickers": recordedTickers}[r.URL.Path]
		switch {
		case body == "":
			http.NotFound(w, r)
		case down:
			http.Error(w, "bad gateway", http.StatusBadGateway)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(srv.Close)
	a.URL = srv.URL
	return a
}

func (a *fakeAggregator) setDown(down bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.down = down
}

// Recorded denoms of the ticker jobs: GGE has five owners, recorded over
// three DenomOwners pages; ATOM and OSMO are priced by BZE's own registry
// asset list.
const (
	ggeDenom  = "factory/bze12gyp30f29zg26nuqrwdhl26ej4q066pt572fhm/GGE"
	atomDenom = "ibc/5FEB332D2B121921C792F1A0DBF7C3163FF205337B4AFE6E14F69E8E49545F49"
	osmoDenom = "ibc/ED07A3391A112B175915CD8FAF43A2DA8E4790EDE12566649D0C2F97716B8518"
	flixDenom = "ibc/FF39851E73089ACBD0B09BDF62FA3C67FBD77A2CD97CD159DBCE9C770561F8AF"
)

// tickerState is everything the three jobs write, without the fetch and
// refresh times.
func (e *syncEnv) tickerState(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, q := range []string{
		`SELECT concat_ws(' ', chain_id, registry_name, pretty_name, network_type, bech32_prefix, logo_url,
			explorer_tx_url, explorer_account_url, registry_fetched_at IS NULL, updated_at) FROM explorer.chains ORDER BY chain_id`,
		`SELECT concat_ws(' ', chain_id, base, symbol, name, display, exponent, logo_url, coingecko_id, traces, updated_at)
			FROM explorer.registry_assets ORDER BY chain_id, base`,
		`SELECT concat_ws(' ', denom, address, balance) FROM explorer.token_holders ORDER BY denom, address`,
		`SELECT concat_ws(' ', denom, symbol, name, exponent, logo_url, origin_chain_id, ibc_base_denom, ibc_path,
			holders_count, price_usd, price_change_24h_pct) FROM explorer.denoms ORDER BY denom`,
	} {
		out = append(out, queryStrings(t, e.db, q)...)
	}
	return out
}

func TestHoldersPricesAndTheChainRegistry(t *testing.T) {
	e := newSyncEnv(t)
	// The IBC story fills the channels; here three of BZE's: Noble's,
	// the Hub's, and one to a chain the registry does not know.
	_, err := e.db.Exec(`INSERT INTO explorer.ibc_channels (channel_id, counterparty_chain_id, state, updated_at) VALUES
		('channel-3', 'noble-1', 'STATE_OPEN', now()), ('channel-11', 'cosmoshub-4', 'STATE_OPEN', now()),
		('channel-5', 'flixnet-4', 'STATE_OPEN', now())`)
	require.NoError(t, err)

	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)

	// The registry cache: BZE and the chains met, the unknown one by its id.
	assert.Equal(t, []string{
		"beezee-1 beezee BeeZee t",
		"cosmoshub-4 cosmoshub Cosmos Hub t",
		"flixnet-4 f",
		"noble-1 noble Noble t",
	}, queryStrings(t, e.db, `SELECT concat_ws(' ', chain_id, registry_name, pretty_name, registry_fetched_at IS NOT NULL)
		FROM explorer.chains ORDER BY chain_id`))
	assert.Positive(t, e.count(t, `SELECT count(*) FROM explorer.registry_assets WHERE chain_id = 'beezee-1'`))
	assert.Zero(t, e.count(t, `SELECT count(*) FROM explorer.registry_assets WHERE chain_id = 'flixnet-4'`))
	assert.Equal(t, 1, e.reg.Requests("/api/"), "one listing per run")

	// The IBC denoms through their channel: the registry's symbol and
	// exponent over ibc-go's placeholder metadata.
	assert.Equal(t, []string{"cosmoshub-4 uatom transfer/channel-11 ATOM 6"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', origin_chain_id, ibc_base_denom, ibc_path, symbol, exponent) FROM explorer.denoms WHERE denom = $1`, atomDenom))
	assert.Equal(t, []string{"noble-1 uusdc USDC.n 6"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', origin_chain_id, ibc_base_denom, symbol, exponent) FROM explorer.denoms WHERE denom = $1`, usdcDenom))
	assert.Equal(t, []string{"flixnet-4 uflix UFLIX 0"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', origin_chain_id, ibc_base_denom, symbol, exponent) FROM explorer.denoms WHERE denom = $1`, flixDenom),
		"an origin the registry does not know keeps the bank's metadata")

	// Holders: five owners over three pages, four of them with 1 GGE or more.
	assert.Equal(t, 5, e.count(t, `SELECT count(*) FROM explorer.token_holders WHERE denom = $1`, ggeDenom))
	assert.Equal(t, recordedDenoms+2, e.grpc.Requests("bank", "DenomOwners"), "a page per denom, GGE's two more")
	assert.Equal(t, []string{"4"}, queryStrings(t, e.db, `SELECT holders_count FROM explorer.denoms WHERE denom = $1`, ggeDenom))
	assert.Equal(t, []string{"0"}, queryStrings(t, e.db, `SELECT holders_count FROM explorer.denoms WHERE denom = 'ubze'`),
		"nobody holds ubze in the fake node")

	// Prices by CoinGecko id: ubze through BZE's own asset list.
	assert.Equal(t, []string{
		atomDenom + " 1.960000000000",
		osmoDenom + " 0.034038060000",
		"ubze 0.000168060000",
	}, queryStrings(t, e.db, `SELECT concat_ws(' ', denom, price_usd) FROM explorer.denoms WHERE price_usd IS NOT NULL ORDER BY denom`))
	assert.Equal(t, []string{"ubze -2.1362"}, queryStrings(t, e.db, `SELECT concat_ws(' ', denom, price_change_24h_pct)
		FROM explorer.denoms WHERE price_change_24h_pct IS NOT NULL`), "BZE's change, from the BZE/USDC.n pool only")

	// The API: prices, holder counts and origins on the list, the holders
	// ranked.
	base := e.serveAPI(t)
	tokens := map[string]dto.TokenSummary{}
	for _, tok := range tokenPages(t, strings.TrimSuffix(base, "/api/v1"), "") {
		tokens[tok.Denom] = tok
	}
	require.NotNil(t, tokens["ubze"].PriceUSD)
	assert.Equal(t, "0.000168060000", *tokens["ubze"].PriceUSD)
	require.NotNil(t, tokens["ubze"].PriceChange24hPct)
	assert.Equal(t, "-2.1362", *tokens["ubze"].PriceChange24hPct)
	assert.Nil(t, tokens[atomDenom].PriceChange24hPct, "a change for BZE only")
	require.NotNil(t, tokens[ggeDenom].HoldersCount)
	assert.Equal(t, int64(4), *tokens[ggeDenom].HoldersCount)
	require.NotNil(t, tokens[atomDenom].OriginChain)
	assert.Equal(t, "cosmoshub-4", tokens[atomDenom].OriginChain.ChainID)
	assert.Equal(t, "Cosmos Hub", *tokens[atomDenom].OriginChain.Name)
	assert.NotNil(t, tokens[atomDenom].OriginChain.LogoURL)
	assert.Equal(t, &dto.OriginChain{ChainID: "flixnet-4"}, tokens[flixDenom].OriginChain)
	assert.Nil(t, tokens[ggeDenom].OriginChain)

	r := fetch(t, base+"/tokens/"+url.PathEscape(ggeDenom)+"/holders?limit=2")
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	page := into[dto.List[dto.TokenHolder]](t, r)
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.NextCursor)
	holders := page.Items
	for page.NextCursor != nil {
		page = into[dto.List[dto.TokenHolder]](t, fetch(t, base+"/token/holders?limit=2&denom="+url.QueryEscape(ggeDenom)+
			"&cursor="+*page.NextCursor))
		holders = append(holders, page.Items...)
	}
	var balances []string
	for i, h := range holders {
		assert.Equal(t, int64(i+1), h.Rank)
		require.NotNil(t, h.SharePct)
		balances = append(balances, h.Balance)
	}
	assert.Equal(t, []string{"50305258722", "49491765665", "198000900", "2800000", "174713"}, balances)
	assert.Equal(t, http.StatusNotFound, fetch(t, base+"/tokens/unothing/holders").status)

	// A second run changes nothing.
	state := e.tickerState(t)
	code, out = e.syncState(t)
	require.Equal(t, 0, code, out)
	assert.Equal(t, state, e.tickerState(t))

	// The aggregator down: the run fails, the prices stay.
	e.agg.setDown(true)
	code, _ = e.syncState(t)
	assert.NotEqual(t, 0, code)
	assert.Equal(t, state, e.tickerState(t))
	assert.Equal(t, []string{"prices"}, queryStrings(t, e.db, `SELECT job FROM explorer.sync_jobs WHERE last_error IS NOT NULL`))

	// The registry down: the cached rows stay.
	e.agg.setDown(false)
	e.reg.SetDown(true)
	code, _ = e.syncState(t)
	assert.NotEqual(t, 0, code)
	assert.Equal(t, state, e.tickerState(t))
}

// The holders job's cursor in sync_jobs is where a restart resumes: a run
// that failed on GGE's second page leaves it there.
func TestTheHoldersCursorResumesAtTheFailedPage(t *testing.T) {
	e := newSyncEnv(t)
	const secondPage = "FFbZ92QcbtUfrx+vF9dZO35EZcTX"
	e.grpc.SetResponse("bank", "DenomOwners", url.PathEscape(ggeDenom)+"."+secondPage,
		[]byte(`{"code":14,"message":"node down","details":[]}`))
	code, _ := e.syncState(t)
	require.NotEqual(t, 0, code)
	assert.Equal(t, []string{ggeDenom}, queryStrings(t, e.db, `SELECT cursor->>'denom' FROM explorer.sync_jobs WHERE job = 'holders'`))
	assert.Equal(t, 2, e.count(t, `SELECT count(*) FROM explorer.token_holders WHERE denom = $1`, ggeDenom), "the first page")

	e.grpc.SetResponse("bank", "DenomOwners", url.PathEscape(ggeDenom)+"."+secondPage,
		fakenode.ReadGRPCFixture(t, "bank", "DenomOwners", url.PathEscape(ggeDenom)+"."+secondPage))
	e.grpc.ResetRequests()
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	assert.Equal(t, 0, e.grpc.RequestsFor("bank", "DenomOwners", url.PathEscape(ggeDenom)), "GGE's first page is not read again")
	assert.Equal(t, 5, e.count(t, `SELECT count(*) FROM explorer.token_holders WHERE denom = $1`, ggeDenom))
	assert.Equal(t, []string{"{}"}, queryStrings(t, e.db, `SELECT cursor::text FROM explorer.sync_jobs WHERE job = 'holders'`))
}
