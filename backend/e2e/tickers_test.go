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

// recordedPool is mainnet's BZE/USDC.n liquidity pool (USDC.n priced in
// ubze). The tests point PRICE_CHANGE_MARKET at it, since the default moved
// to the USDC.inj pool.
const recordedPool = "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4_ubze"

// geckoIDs are mainnet's COINGECKO_PRICE_IDS.
var geckoIDs = []string{"bzedge", "osmosis", "cosmos"}

// recordedGecko is CoinGecko's simple price answer for geckoIDs, the prices
// of recordedPrices, in an order of its own.
const recordedGecko = `{"cosmos":{"usd":1.96},"bzedge":{"usd":0.00016806},"osmosis":{"usd":0.03403806}}`

// fakeGecko answers CoinGecko's /api/v3/simple/price with recordedGecko
// until it is told to fail, and counts the calls.
type fakeGecko struct {
	URL   string
	mu    sync.Mutex
	down  bool
	calls int
	query string
}

func newFakeGecko(t *testing.T) *fakeGecko {
	t.Helper()
	g := &fakeGecko{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		switch {
		case r.URL.Path != "/api/v3/simple/price":
			http.NotFound(w, r)
		case g.down:
			http.Error(w, "too many requests", http.StatusTooManyRequests)
		default:
			g.calls++
			g.query = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(recordedGecko))
		}
	}))
	t.Cleanup(srv.Close)
	g.URL = srv.URL
	return g
}

func (g *fakeGecko) setDown(down bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.down = down
}

// answered is how many price reads succeeded, and the last one's query.
func (g *fakeGecko) answered() (int, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls, g.query
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
	// The BZE/USDC.n pool's ticker gives BZE's change: the pool's recorded
	// open and last price of 2026-10-09 (USDC.n +2.18 % in ubze).
	seedChangeSwaps(t, e)
	// The channels set runs first and fills mainnet's fourteen channels:
	// Noble's (channel-3) and the Hub's (channel-11) are in the registry
	// fixtures, OmniFlix's (channel-5) and the others are not.
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)

	// The registry cache: BZE and the chains met, the unknown ones by their
	// id.
	assert.Equal(t, []string{
		"archway-1 f",
		"atomone-1 f",
		"beezee-1 beezee BeeZee t",
		"cosmoshub-4 cosmoshub Cosmos Hub t",
		"dungeon-1 f",
		"injective-1 f",
		"jackal-1 f",
		"lumen f",
		"noble-1 noble Noble t",
		"omniflixhub-1 f",
		"osmosis-1 f",
		"vidulum-1 f",
	}, queryStrings(t, e.db, `SELECT concat_ws(' ', chain_id, registry_name, pretty_name, registry_fetched_at IS NOT NULL)
		FROM explorer.chains ORDER BY chain_id`))
	assert.Positive(t, e.count(t, `SELECT count(*) FROM explorer.registry_assets WHERE chain_id = 'beezee-1'`))
	assert.Zero(t, e.count(t, `SELECT count(*) FROM explorer.registry_assets WHERE chain_id = 'omniflixhub-1'`))
	assert.Equal(t, 1, e.reg.Requests("/api/"), "one listing per run")

	// The IBC denoms through their channel: the registry's symbol and
	// exponent over ibc-go's placeholder metadata.
	assert.Equal(t, []string{"cosmoshub-4 uatom transfer/channel-11 ATOM 6"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', origin_chain_id, ibc_base_denom, ibc_path, symbol, exponent) FROM explorer.denoms WHERE denom = $1`, atomDenom))
	assert.Equal(t, []string{"noble-1 uusdc USDC.n 6"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', origin_chain_id, ibc_base_denom, symbol, exponent) FROM explorer.denoms WHERE denom = $1`, usdcDenom))
	assert.Equal(t, []string{"omniflixhub-1 uflix UFLIX 0"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', origin_chain_id, ibc_base_denom, symbol, exponent) FROM explorer.denoms WHERE denom = $1`, flixDenom),
		"an origin the registry does not know keeps the bank's metadata")

	// Holders: five owners over three pages, four of them with 1 GGE or more.
	assert.Equal(t, 5, e.count(t, `SELECT count(*) FROM explorer.token_holders WHERE denom = $1`, ggeDenom))
	assert.Equal(t, recordedDenoms+2, e.grpc.Requests("bank", "DenomOwners"), "a page per denom, GGE's two more")
	assert.Equal(t, []string{"4"}, queryStrings(t, e.db, `SELECT holders_count FROM explorer.denoms WHERE denom = $1`, ggeDenom))
	assert.Equal(t, []string{"0"}, queryStrings(t, e.db, `SELECT holders_count FROM explorer.denoms WHERE denom = 'ubze'`),
		"nobody holds ubze in the fake node")

	// Prices by CoinGecko id, read from CoinGecko itself: ubze through
	// BZE's own asset list.
	calls, query := e.gecko.answered()
	assert.Equal(t, 1, calls)
	assert.Equal(t, "ids=bzedge,osmosis,cosmos&vs_currencies=usd", query)
	assert.Equal(t, []string{
		atomDenom + " 1.960000000000",
		osmoDenom + " 0.034038060000",
		"ubze 0.000168060000",
	}, queryStrings(t, e.db, `SELECT concat_ws(' ', denom, price_usd) FROM explorer.denoms WHERE price_usd IS NOT NULL ORDER BY denom`))
	assert.Equal(t, []string{"ubze -2.1362"}, queryStrings(t, e.db, `SELECT concat_ws(' ', denom, price_change_24h_pct)
		FROM explorer.denoms WHERE price_change_24h_pct IS NOT NULL`), "BZE's change, from the BZE/USDC.n pool's ticker")

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
	assert.Equal(t, &dto.OriginChain{ChainID: "omniflixhub-1"}, tokens[flixDenom].OriginChain)
	// FLIX came over channel-5, whose light client has expired: stranded.
	require.NotNil(t, tokens[flixDenom].IBCChannel)
	assert.Equal(t, "channel-5", tokens[flixDenom].IBCChannel.ChannelID)
	assert.Equal(t, "expired", *tokens[flixDenom].IBCChannel.ClientStatus)
	assert.True(t, tokens[flixDenom].Stranded)
	assert.False(t, tokens[ggeDenom].Stranded, "not an IBC denom")
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

	// The candles unreadable: the run fails on the change, the prices and
	// the change stay.
	_, err := e.db.Exec(`ALTER TABLE explorer.candles RENAME TO candles_away`)
	require.NoError(t, err)
	code, _ = e.syncState(t)
	assert.NotEqual(t, 0, code)
	assert.Equal(t, state, e.tickerState(t))
	assert.Equal(t, []string{"prices"}, queryStrings(t, e.db, `SELECT job FROM explorer.sync_jobs WHERE last_error IS NOT NULL`))
	_, err = e.db.Exec(`ALTER TABLE explorer.candles_away RENAME TO candles`)
	require.NoError(t, err)

	// CoinGecko down (a new process: no backup to serve): the run fails, the
	// prices stay.
	e.gecko.setDown(true)
	code, _ = e.syncState(t)
	assert.NotEqual(t, 0, code)
	assert.Equal(t, state, e.tickerState(t))
	assert.Equal(t, []string{"prices"}, queryStrings(t, e.db, `SELECT job FROM explorer.sync_jobs WHERE last_error IS NOT NULL`))
	e.gecko.setDown(false)

	// The registry down: the cached rows stay.
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
