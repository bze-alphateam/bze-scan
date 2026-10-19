//go:build e2e

package e2e_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// Chain v7.0 and v7.1 events name no order owner and no maker or taker:
// the transformer takes the owner from the placing message it matched
// (Order.OwnerFromMessage), and the writer names the maker of the order's
// fills from it, whichever is written first.

const unownedH = int64(15_000_000)

// v7Order is order id resting at h; owner is empty when the transformer
// found no placing message.
func v7Order(h int64, id int, owner string) transform.Order {
	o := savedOrder(h, id, owner, "100", 0, "")
	o.OwnerFromMessage = owner != ""
	return o
}

// v7Fill is a fill of order whose event named no maker.
func v7Fill(h int64, seq, order int, amount string) transform.OrderFill {
	f := takenFill(h, seq, order, amount, 0, "")
	f.Maker, f.TakerMessageID = "", ""
	return f
}

func makers(t *testing.T, e *syncEnv) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', height, seq, coalesce(nullif(maker, ''), '-'))
		FROM explorer.order_fills ORDER BY height, seq`)
}

func owners(t *testing.T, e *syncEnv) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', order_id, coalesce(nullif(owner, ''), '-'), status)
		FROM explorer.orders ORDER BY order_id`)
}

// Regression: the order kept no owner, so the account's open orders from
// the index missed it, and its fills had no maker.
func TestV7OrdersGetTheirOwnerAndTheirFillsAMaker(t *testing.T) {
	e := newSyncEnv(t)
	ctx := context.Background()
	w := attributingBatchWriter(t, e.url)

	saved := dexBlock(unownedH)
	saved.Orders = []transform.Order{v7Order(unownedH, 30, dexOwner)}
	filled := dexBlock(unownedH + 5)
	filled.OrderFills = []transform.OrderFill{v7Fill(unownedH+5, 0, 30, "10")}
	later := dexBlock(unownedH + 9)
	later.OrderFills = []transform.OrderFill{v7Fill(unownedH+9, 1, 30, "20")}

	// The backfill walks down: the fill before the order.
	require.NoError(t, w.Write(ctx, []*transform.Entities{filled}, writer.ModeInsert))
	assert.Equal(t, []string{"15000005 0 -"}, makers(t, e))
	require.NoError(t, w.Write(ctx, []*transform.Entities{saved}, writer.ModeInsert))
	assert.Equal(t, []string{"15000005 0 " + dexOwner}, makers(t, e), "the order names its stored fills' maker")
	// A fill written after its order reads the maker from it.
	require.NoError(t, w.Write(ctx, []*transform.Entities{later}, writer.ModeInsert))
	want := []string{"15000005 0 " + dexOwner, "15000009 1 " + dexOwner}
	assert.Equal(t, want, makers(t, e))
	assert.Equal(t, []string{oid(30) + " " + dexOwner + " open"}, owners(t, e))

	// A reindex rewrites the same values.
	all := []*transform.Entities{saved, filled, later}
	require.NoError(t, w.Write(ctx, all, writer.ModeUpdate))
	assert.Equal(t, want, makers(t, e))
	assert.Equal(t, []string{oid(30) + " " + dexOwner + " open"}, owners(t, e))
}

// The order of an existing database (written before the fix) gets its
// owner from a reindex; a reindex never forgets an owner the state sync
// named when the transformer still finds no placing message.
func TestReindexNamesTheOwnerAndKeepsANamedOne(t *testing.T) {
	e := newSyncEnv(t)
	ctx := context.Background()
	w := attributingBatchWriter(t, e.url)

	old := dexBlock(unownedH)
	old.Orders = []transform.Order{v7Order(unownedH, 30, ""), v7Order(unownedH, 31, "")}
	fill := dexBlock(unownedH + 5)
	fill.OrderFills = []transform.OrderFill{v7Fill(unownedH+5, 0, 30, "10")}
	require.NoError(t, w.Write(ctx, []*transform.Entities{old, fill}, writer.ModeInsert))
	_, err := e.db.Exec(`UPDATE explorer.orders SET owner = $1 WHERE order_id = $2`, dexMaker, oid(31))
	require.NoError(t, err)

	fixed := dexBlock(unownedH)
	fixed.Orders = []transform.Order{v7Order(unownedH, 30, dexOwner), v7Order(unownedH, 31, "")}
	require.NoError(t, w.Write(ctx, []*transform.Entities{fixed, fill}, writer.ModeUpdate))
	assert.Equal(t, []string{oid(30) + " " + dexOwner + " open", oid(31) + " " + dexMaker + " open"}, owners(t, e))
	assert.Equal(t, []string{"15000005 0 " + dexOwner}, makers(t, e))
}

// The hourly reconciliation reads an order without owner from the node by
// market, side and id: it names the owner and its fills' maker, or closes
// the order when the node no longer has it.
func TestReconciliationNamesOrdersWithoutOwner(t *testing.T) {
	e := newSyncEnv(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	live := writer.NewLiveWriter(pool, writer.AttributeOrders)

	at := dexBlock(reconcileH)
	at.Orders = []transform.Order{v7Order(reconcileH, 30, ""), v7Order(reconcileH, 31, "")}
	at.OrderFills = []transform.OrderFill{v7Fill(reconcileH, 0, 30, "10")}
	require.NoError(t, live.WriteBlock(ctx, at))
	e.node.SetStatusHeight(reconcileH)

	key := func(id int) string { return strings.Join([]string{url.PathEscape(dexMarket), "sell", oid(id)}, ".") }
	e.grpc.SetResponse("tradebin", "MarketOrder", key(30), []byte(`{"order":{"id":"`+oid(30)+`","market_id":"`+
		dexMarket+`","order_type":"sell","amount":"90","price":"2","created_at":"1","owner":"`+dexOwner+`"}}`))
	e.grpc.SetResponse("tradebin", "MarketOrder", key(31), []byte(`{"code":5,"message":"not found","details":[]}`))

	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	assert.Equal(t, []string{oid(30) + " " + dexOwner + " open", oid(31) + " - filled"}, owners(t, e))
	assert.Equal(t, []string{"26200000 0 " + dexOwner}, makers(t, e))
	assert.Equal(t, []string{"orders"}, queryStrings(t, e.db,
		`SELECT job FROM explorer.sync_jobs WHERE job = 'orders' AND last_error IS NULL`))
	assert.Equal(t, 0, e.grpc.RequestsFor("tradebin", "UserMarketOrders", ""), "never the empty owner")
}
