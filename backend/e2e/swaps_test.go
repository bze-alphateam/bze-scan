//go:build e2e

package e2e_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/trades"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// Recorded heights with trades: 22551811 has two transaction swaps and a
// fill, 20230045 (chain v7) and 24999134 fills only. swapH is a synthetic
// height with a two-hop swap and, at block level, a fill, the burner's swap
// and another fill: no recorded height has those.
var swapHeights = []int64{20230045, 22551811, 24999134}

const (
	swapH       = int64(26_000_100)
	swapTrader  = "bze1n4qjms7gdam0k8f06eadgn3ecggvwy042casm5"
	swapBurner  = "bze1v7uw4xhrcv0vk7qp8jf9lu3hm5d8uu5yjp5qun"
	swapAtom    = "ibc/82931180F0962712BFABB189083A94C77EA90E269ABFC6D310AF922B2B14E011"
	swapHoney   = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney"
	swapMaker   = "bze15pqjgk4la0mfphwddce00d05n3th3u66n3ptcv"
	swapMarket  = "factory/bze13gzq40che93tgfm9kzmkpjamah5nj0j73pyhqk/uhoney/ubze"
	recordedTx0 = "FE81352168AC701995487205D41EC6B0DCA758E6DA74CC71DB19561AB56EC0EF"
)

func swapAttr(key, value string) node.Attribute { return node.Attribute{Key: key, Value: value} }

func swapEv(msgIndex, creator, inAmount, inDenom, outAmount, outDenom string) node.Event {
	pool := min(inDenom, outDenom) + "_" + max(inDenom, outDenom)
	ev := node.Event{Type: "bze.tradebin.SwapEvent", Attributes: []node.Attribute{
		swapAttr("creator", `"`+creator+`"`),
		swapAttr("in", fmt.Sprintf(`{"denom":%q,"amount":%q}`, inDenom, inAmount)),
		swapAttr("out", fmt.Sprintf(`{"denom":%q,"amount":%q}`, outDenom, outAmount)),
		swapAttr("pool_id", `"`+pool+`"`),
	}}
	if msgIndex != "" {
		ev.Attributes = append(ev.Attributes, swapAttr("msg_index", msgIndex))
	}
	return ev
}

func swapFill(order string) node.Event {
	q := func(s string) string { return `"` + s + `"` }
	return node.Event{Type: "bze.tradebin.OrderExecutedEvent", Attributes: []node.Attribute{
		swapAttr("id", q(order)), swapAttr("market_id", q(swapMarket)), swapAttr("order_type", q("sell")),
		swapAttr("amount", q("10")), swapAttr("price", q("2")), swapAttr("maker", q(swapMaker)),
		swapAttr("taker", q(swapTrader)),
	}}
}

// swapBlock is swapH's input: one transaction with a two-hop MsgMultiSwap.
func swapBlock(t *testing.T) (*node.Block, *node.BlockResults, *node.Commit) {
	t.Helper()
	msg, err := codectypes.NewAnyWithValue(&tradebintypes.MsgMultiSwap{Creator: swapTrader,
		Routes: []string{swapAtom + "_ubze", swapHoney + "_ubze"}, Input: sdk.NewInt64Coin(swapAtom, 50)})
	require.NoError(t, err)
	body, err := (&sdktx.TxBody{Messages: []*codectypes.Any{msg}}).Marshal()
	require.NoError(t, err)
	auth, err := (&sdktx.AuthInfo{Fee: &sdktx.Fee{Amount: sdk.NewCoins(sdk.NewInt64Coin("ubze", 1))}}).Marshal()
	require.NoError(t, err)
	raw, err := (&sdktx.TxRaw{BodyBytes: body, AuthInfoBytes: auth}).Marshal()
	require.NoError(t, err)
	b := &node.Block{Height: swapH, Time: time.Unix(1_800_000_000, 123_456_789).UTC(), Hash: "C0FFEE",
		Txs: []string{base64.StdEncoding.EncodeToString(raw)}, Raw: json.RawMessage(`{}`)}
	r := &node.BlockResults{Height: swapH,
		TxsResults: []node.TxResult{{Events: []node.Event{
			swapEv("0", swapTrader, "50", swapAtom, "1200", "ubze"),
			swapEv("0", swapTrader, "1200", "ubze", "9", swapHoney),
		}}},
		FinalizeBlockEvents: []node.Event{
			swapFill(oid(900)),
			swapEv("", swapBurner, "300", swapHoney, "5", "ubze"),
			swapFill(oid(901)),
		}}
	return b, r, &node.Commit{Height: swapH}
}

// indexTrades writes the recorded heights and swapH through the live path
// or, with backfill, the batch writer walking down.
func indexTrades(t *testing.T, e *syncEnv, backfill bool) {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	defer pool.Close()
	b, r, c := swapBlock(t)
	synthetic, err := newTransformer(t).Transform(transform.Input{Block: b, Results: r, Commit: c})
	require.NoError(t, err)
	if backfill {
		w := writer.NewBatchWriter(pool)
		require.NoError(t, w.Write(ctx, []*transform.Entities{synthetic}, writer.ModeInsert))
		for _, h := range slices.Backward(swapHeights) {
			require.NoError(t, w.Write(ctx, []*transform.Entities{transformed(t, e, h)}, writer.ModeInsert))
		}
		return
	}
	live := writer.NewLiveWriter(pool)
	for _, h := range swapHeights {
		require.NoError(t, live.WriteBlock(ctx, transformed(t, e, h)))
	}
	require.NoError(t, live.WriteBlock(ctx, synthetic))
}

func swapRows(t *testing.T, e *syncEnv) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', height, trade_id, to_char(time, 'YYYY-MM-DD HH24:MI:SS.US'), pool_id, base, quote, side, base_amount,
			quote_amount, creator, coalesce(tx_hash, '-'), coalesce(tx_index::text, '-'), coalesce(msg_index::text, '-'),
			coalesce(block_seq::text, '-')) FROM explorer.swaps`)
}

func tradeIDs(t *testing.T, e *syncEnv) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', height, seq, coalesce(trade_id::text, '-')) FROM explorer.order_fills`)
}

// asBeforeTheMigration puts the trades back as 000022 left them: no swaps
// rows, no trade ids, the numbering cursor at 0 as the migration sets it.
func asBeforeTheMigration(t *testing.T, e *syncEnv) {
	t.Helper()
	for _, q := range []string{`DELETE FROM explorer.swaps`, `UPDATE explorer.order_fills SET trade_id = NULL`,
		`UPDATE explorer.pending_rewrites SET resume_from = 0 WHERE name = 'swaps'`} {
		_, err := e.db.Exec(q)
		require.NoError(t, err)
	}
}

// The live path, the backfill and the post-migration step over the stored
// blocks give the same rows and the same ids.
func TestSwapsOfTheIndexerAndTheMigrationStepAgree(t *testing.T) {
	var written [][]string
	for _, backfill := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "backfill"}[backfill], func(t *testing.T) {
			e := newSyncEnv(t)
			indexTrades(t, e, backfill)
			swaps, fills := swapRows(t, e), tradeIDs(t, e)
			id := func(h int64, n int) string { return fmt.Sprintf("%d %d", h, h*1_000_000+int64(n)) }
			require.Len(t, swaps, 5)
			assert.Equal(t, id(22551811, 1)+" "+swapAtom+"_ubze "+swapAtom+" ubze sell 16665987 1136977755 "+swapTrader+
				" "+recordedTx0+" 29 0 -", noTime(swaps[0]))
			assert.Equal(t, id(22551811, 2)+" ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4_ubze"+
				" ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4 ubze buy 2691817 3942911039"+
				" bze1hywehmyk26d0hcw9ekdkggthjuedmpkj3fgauh C39BF923864A722C232950D7BE5354A69813AB2B919427C2093DD3AC6757E935 30 0 -",
				noTime(swaps[1]))
			// The hops in order, then the burner's swap between the two fills.
			for i, want := range []string{id(swapH, 1) + " ", id(swapH, 2) + " ", id(swapH, 4) + " "} {
				assert.Contains(t, swaps[2+i], want)
			}
			assert.Contains(t, swaps[4], swapBurner+" - - - 1")
			assert.Equal(t, []string{"20230045 83 20230045000001", "20230045 92 20230045000002", "22551811 124 22551811000003",
				"24999134 74 24999134000001", fmt.Sprintf("%d 0 %d", swapH, swapH*1_000_000+3),
				fmt.Sprintf("%d 2 %d", swapH, swapH*1_000_000+5)}, fills, "fills after the transactions' swaps")
			assert.Equal(t, queryStrings(t, e.db, `SELECT concat_ws(' ', s.trade_id, to_char(b.time, 'YYYY-MM-DD HH24:MI:SS.US'))
				FROM explorer.swaps s JOIN explorer.blocks b USING (height)`),
				queryStrings(t, e.db, `SELECT concat_ws(' ', trade_id, to_char(time, 'YYYY-MM-DD HH24:MI:SS.US')) FROM explorer.swaps`),
				"a swap's time is its stored block's")
			var plain []string
			for _, s := range swaps {
				plain = append(plain, noTime(s))
			}
			written = append(written, append(plain, fills...))

			asBeforeTheMigration(t, e)
			res := migrateUp(t, e.url)
			assert.Contains(t, res.Steps, "swaps")
			assert.Equal(t, swaps, swapRows(t, e))
			assert.Equal(t, fills, tradeIDs(t, e))
			assert.Equal(t, []string{fmt.Sprintf("swaps %d", swapH+1)}, queryStrings(t, e.db,
				`SELECT concat_ws(' ', name, resume_from) FROM explorer.pending_rewrites WHERE name = 'swaps'`),
				"the cursor above the highest block")

			// Done, a later up rewrites nothing.
			_, err := e.db.Exec(`DELETE FROM explorer.swaps WHERE height = $1`, swapH)
			require.NoError(t, err)
			migrateUp(t, e.url)
			assert.Len(t, swapRows(t, e), 2)
		})
	}
	require.Len(t, written, 2)
	assert.Equal(t, written[0], written[1], "both writers give the same rows and ids")
}

// noTime drops the time of a swapRows row.
func noTime(row string) string {
	f := strings.Fields(row)
	return strings.Join(append(f[:2:2], f[4:]...), " ")
}

// An interrupted rewrite resumes at the first partition it had not done.
func TestSwapsMigrationStepResumes(t *testing.T) {
	e := newSyncEnv(t)
	indexTrades(t, e, false)
	swaps, fills := swapRows(t, e), tradeIDs(t, e)

	asBeforeTheMigration(t, e)
	// The run stopped after partition 22: everything below 23,000,000 done.
	pool, err := pgxpool.New(context.Background(), e.url)
	require.NoError(t, err)
	defer pool.Close()
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	_, err = trades.Range(context.Background(), tx, 0, 23_000_000, candles.Default)
	require.NoError(t, err)
	_, err = tx.Exec(context.Background(), `UPDATE explorer.pending_rewrites SET resume_from = 23000000 WHERE name = 'swaps'`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(context.Background()))
	// A row of a done partition taken away shows the resumed run skips it.
	_, err = e.db.Exec(`DELETE FROM explorer.swaps WHERE height = 22551811 AND trade_id = 22551811000001`)
	require.NoError(t, err)

	migrateUp(t, e.url)
	assert.Equal(t, swaps[1:], swapRows(t, e), "the partitions above are done, partition 22 is not redone")
	assert.Equal(t, fills, tradeIDs(t, e))
	assert.Equal(t, []string{fmt.Sprintf("%d", swapH+1)}, queryStrings(t, e.db,
		`SELECT resume_from::text FROM explorer.pending_rewrites WHERE name = 'swaps'`))
}

// During a deploy both releases index: the previous one stores blocks
// without swaps rows or trade ids, above the cursor the migration left and,
// when it wins a height the new one writes too, below it. The new release's
// serve numbers them from the stored rows.
func TestTradesOfThePreviousReleaseAreNumberedOnStart(t *testing.T) {
	e := newSyncEnv(t)
	indexTrades(t, e, false)
	swaps, fills := swapRows(t, e), tradeIDs(t, e)

	// `migrate up` ran when 20230045 was the highest block; the previous
	// release then wrote everything above it, and 20230045 again.
	for _, q := range []string{
		`UPDATE explorer.pending_rewrites SET resume_from = 20230046 WHERE name = 'swaps'`,
		`DELETE FROM explorer.swaps`,
		`UPDATE explorer.order_fills SET trade_id = NULL`,
	} {
		_, err := e.db.Exec(q)
		require.NoError(t, err)
	}
	e.serve(t, 24999134)
	waitFor(t, 20*time.Second, func() bool {
		return len(swapRows(t, e)) == len(swaps) && !slices.ContainsFunc(tradeIDs(t, e), func(s string) bool {
			return strings.HasSuffix(s, " -")
		})
	}, "the trades numbered")
	assert.Equal(t, swaps, swapRows(t, e))
	assert.Equal(t, fills, tradeIDs(t, e))
	assert.Equal(t, []string{fmt.Sprintf("%d", swapH+1)}, queryStrings(t, e.db,
		`SELECT resume_from::text FROM explorer.pending_rewrites WHERE name = 'swaps'`))
}

// GET /aggregator/api/dex/swaps over the stored swaps: newest first, the
// last 100, display units from explorer.denoms.
func TestCompatSwapsFromTheStoredSwaps(t *testing.T) {
	e := newSyncEnv(t)
	indexTrades(t, e, false)
	_, err := e.db.Exec(`INSERT INTO explorer.denoms (denom, exponent, kind, updated_at)
		VALUES ('ubze', 6, 'native', now()), ($1, 6, 'ibc', now()), ($2, 6, 'factory', now()) ON CONFLICT (denom) DO UPDATE SET exponent = 6`, swapAtom, swapHoney)
	require.NoError(t, err)
	base := e.serveCompat(t, "https://explorer.getbze.com")
	get := func(address string) response {
		r := fetch(t, base+"/aggregator/api/dex/swaps?address="+address, "Origin", "https://dex.getbze.com")
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		assert.Equal(t, "*", r.header.Get("Access-Control-Allow-Origin"))
		return r
	}

	assert.Equal(t, `[{"order_id":26000100000002,"price":"133.333333333333333333","base_volume":"0.000009","quote_volume":"0.0012",`+
		`"executed_at":"1800000000123","order_type":"buy","maker":"","taker":"`+swapTrader+`","pool_id":"`+swapHoney+`_ubze",`+
		`"base":"`+swapHoney+`","quote":"ubze"},`+
		`{"order_id":26000100000001,"price":"24","base_volume":"0.00005","quote_volume":"0.0012","executed_at":"1800000000123",`+
		`"order_type":"sell","maker":"","taker":"`+swapTrader+`","pool_id":"`+swapAtom+`_ubze","base":"`+swapAtom+`","quote":"ubze"},`+
		`{"order_id":22551811000001,"price":"68.221447370623774038","base_volume":"16.665987","quote_volume":"1136.977755",`+
		`"executed_at":"1777391551683","order_type":"sell","maker":"","taker":"`+swapTrader+`","pool_id":"`+swapAtom+`_ubze",`+
		`"base":"`+swapAtom+`","quote":"ubze"}]`+"\n", string(get(swapTrader).body))
	assert.Equal(t, "[]\n", string(get("bze1nobody").body))

	// 150 more swaps of the burner: the last 100, newest first.
	_, err = e.db.Exec(`INSERT INTO explorer.swaps (height, trade_id, time, pool_id, base, quote, side, base_amount,
			quote_amount, creator, block_seq)
		SELECT $1::bigint, $1::bigint * 1000000 + 100 + n, now(), $2::text || '_ubze', $2::text, 'ubze', 'sell', 1, 1, $3::text, 100 + n
		  FROM generate_series(1, 150) n`, swapH, swapHoney, swapBurner)
	require.NoError(t, err)
	var listed []map[string]any
	require.NoError(t, json.Unmarshal(get(swapBurner).body, &listed))
	require.Len(t, listed, 100)
	assert.Equal(t, float64(swapH*1_000_000+250), listed[0]["order_id"])
	assert.Equal(t, float64(swapH*1_000_000+151), listed[99]["order_id"])

	// executed_at truncates to the millisecond, as the aggregator does (on
	// mainnet, block 25294781 at 22:47:00.563878310 answers …563, not …564).
	_, err = e.db.Exec(`INSERT INTO explorer.swaps (height, trade_id, time, pool_id, base, quote, side, base_amount,
			quote_amount, creator, block_seq)
		VALUES ($1::bigint, $1::bigint * 1000000 + 900, '2026-10-28 22:47:00.5635+00', $2::text || '_ubze', $2::text,
		        'ubze', 'sell', 1, 1, 'bze1ms', 900),
		       ($1::bigint, $1::bigint * 1000000 + 901, '2026-10-28 22:47:00.5634+00', $2::text || '_ubze', $2::text,
		        'ubze', 'sell', 1, 1, 'bze1ms', 901)`, swapH, swapHoney)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(get("bze1ms").body, &listed))
	require.Len(t, listed, 2)
	assert.Equal(t, []any{"1793227620563", "1793227620563"}, []any{listed[0]["executed_at"], listed[1]["executed_at"]},
		"the sub-millisecond digits 5 and 4 alike")

	r := fetch(t, base+"/aggregator/api/dex/swaps")
	assert.Equal(t, http.StatusBadRequest, r.status)
	assert.Equal(t, `{"message":"invalid request"}`+"\n", string(r.body))
}

// A reindex (the batch writer in ModeUpdate) puts back the swaps and trade
// ids that differ from the transformer's.
func TestReindexRestoresSwapsAndTradeIDs(t *testing.T) {
	e := newSyncEnv(t)
	indexTrades(t, e, false)
	swaps, fills := swapRows(t, e), tradeIDs(t, e)
	for _, q := range []string{
		`UPDATE explorer.swaps SET side = 'wrong', creator = 'bze1wrong', base_amount = 1, time = now() WHERE height = 22551811`,
		`UPDATE explorer.order_fills SET trade_id = 1 WHERE height = 22551811`,
	} {
		_, err := e.db.Exec(q)
		require.NoError(t, err)
	}
	require.NotEqual(t, swaps, swapRows(t, e))
	require.NoError(t, batchWriter(t, e.url).Write(context.Background(),
		[]*transform.Entities{transformed(t, e, 22551811)}, writer.ModeUpdate))
	// The batch writer rewrites the block's time too (its JSON time is
	// rounded to the microsecond, the live insert's truncated): the swaps
	// follow it.
	var plain, got []string
	for _, r := range swaps {
		plain = append(plain, noTime(r))
	}
	for _, r := range swapRows(t, e) {
		got = append(got, noTime(r))
	}
	assert.Equal(t, plain, got)
	assert.Empty(t, queryStrings(t, e.db, `SELECT s.trade_id::text FROM explorer.swaps s JOIN explorer.blocks b USING (height)
		WHERE s.time <> b.time`), "a swap's time is its block's")
	assert.Equal(t, fills, tradeIDs(t, e))
}

// A block with pool swaps and no fill that the previous release stores below
// the cursor after a pass (its backfill filling a hole during a deploy's
// overlap) gets no swaps rows from it, and no fill gives it away: the
// cursor recorded the hole, and serve numbers what appeared in it.
func TestPoolSwapsOnlyBlockInAHoleIsNumbered(t *testing.T) {
	const holeH = int64(24_000_500)
	e := newSyncEnv(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	defer pool.Close()
	live := writer.NewLiveWriter(pool)
	for _, h := range []int64{20230045, 24999134} {
		require.NoError(t, live.WriteBlock(ctx, transformed(t, e, h)))
	}
	migrateUp(t, e.url) // the cursor passes holeH, which has no block yet
	assert.Equal(t, []string{"24999135"}, queryStrings(t, e.db,
		`SELECT resume_from::text FROM explorer.pending_rewrites WHERE name = 'swaps'`))

	// The previous release stores holeH: its swaps, no swaps rows.
	b, r, c := swapBlock(t)
	b.Height, r.Height, c.Height = holeH, holeH, holeH
	r.FinalizeBlockEvents = r.FinalizeBlockEvents[1:2] // the burner's swap only
	ents, err := newTransformer(t).Transform(transform.Input{Block: b, Results: r, Commit: c})
	require.NoError(t, err)
	require.Empty(t, ents.OrderFills)
	require.NoError(t, live.WriteBlock(ctx, ents))
	want := queryStrings(t, e.db, `SELECT concat_ws(' ', trade_id, side, creator, coalesce(block_seq::text, '-'))
		FROM explorer.swaps WHERE height = $1`, holeH)
	require.Len(t, want, 3, "two hops and the burner's swap")
	_, err = e.db.Exec(`DELETE FROM explorer.swaps WHERE height = $1`, holeH)
	require.NoError(t, err)

	e.serve(t, 24999134)
	waitFor(t, 20*time.Second, func() bool {
		return len(queryStrings(t, e.db, `SELECT trade_id::text FROM explorer.swaps WHERE height = $1`, holeH)) == 3
	}, "the swaps of the hole's block")
	assert.Equal(t, want, queryStrings(t, e.db, `SELECT concat_ws(' ', trade_id, side, creator, coalesce(block_seq::text, '-'))
		FROM explorer.swaps WHERE height = $1`, holeH))
	assert.Equal(t, []string{fmt.Sprintf(`[[0,20230044],[20230046,%d],[%d,24999133]]`, holeH-1, holeH+1)},
		queryStrings(t, e.db, `SELECT value FROM explorer.indexer_state WHERE key = 'trades_holes'`),
		"the height is cut out of the holes, the others stay")
}

// A database the cursor walked before holes were tracked (testnet ran
// 000023 first): no holes row, the cursor at the tip, and a block with only
// pool swaps below it that the previous release stored without swaps rows.
// The first pass of this release checks every stored height below the
// cursor once and numbers it.
func TestFirstRunNumbersPoolSwapsBelowAnOldCursor(t *testing.T) {
	const oldH = int64(24_000_700)
	e := newSyncEnv(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	defer pool.Close()
	live := writer.NewLiveWriter(pool)
	require.NoError(t, live.WriteBlock(ctx, transformed(t, e, 24999134)))
	b, r, c := swapBlock(t)
	b.Height, r.Height, c.Height = oldH, oldH, oldH
	r.FinalizeBlockEvents = r.FinalizeBlockEvents[1:2] // the burner's swap only
	ents, err := newTransformer(t).Transform(transform.Input{Block: b, Results: r, Commit: c})
	require.NoError(t, err)
	require.NoError(t, live.WriteBlock(ctx, ents))
	want := queryStrings(t, e.db, `SELECT concat_ws(' ', trade_id, side, creator) FROM explorer.swaps WHERE height = $1`, oldH)
	require.Len(t, want, 3)

	// As the release before this one leaves it.
	for _, q := range []string{
		`DELETE FROM explorer.swaps WHERE height = 24000700`,
		`DELETE FROM explorer.indexer_state WHERE key = 'trades_holes'`,
		`UPDATE explorer.pending_rewrites SET resume_from = 24999135 WHERE name = 'swaps'`,
	} {
		_, err := e.db.Exec(q)
		require.NoError(t, err)
	}

	migrateUp(t, e.url)
	assert.Equal(t, want, queryStrings(t, e.db,
		`SELECT concat_ws(' ', trade_id, side, creator) FROM explorer.swaps WHERE height = $1`, oldH))
	assert.Equal(t, []string{fmt.Sprintf(`[[0,%d],[%d,24999133]]`, oldH-1, oldH+1)},
		queryStrings(t, e.db, `SELECT value FROM explorer.indexer_state WHERE key = 'trades_holes'`),
		"only the heights without a block stay holes")
}
