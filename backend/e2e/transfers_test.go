//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// transferHeights are the heights the transfers tests index: every
// transaction fixture, an empty block and a slash (a block without
// transactions whose finalize events burn and move the bonded tokens).
var transferHeights = append([]int64{h317, hSlash}, txHeights...)

// eventRows is every transfers and block_events row as text, with the
// system column xmin, which changes whenever a row is rewritten.
func eventRows(t *testing.T, ix *indexer) []string {
	t.Helper()
	return queryStrings(t, ix.db, `
		SELECT 'x ' || xmin || ' ' || row_to_json(x)::text FROM explorer.transfers x
		UNION ALL SELECT 'e ' || xmin || ' ' || row_to_json(e)::text FROM explorer.block_events e
		ORDER BY 1`)
}

func TestIndexesTransfersAndBlockEvents(t *testing.T) {
	ix := newIndexer(t)
	for _, h := range transferHeights {
		ix.index(t, h)
	}
	feeCollector := chain.ModuleAddress(chain.FeeCollector)

	// MsgSend: the fee row without a message, then the send.
	assert.Equal(t, []string{
		"0 - transfer bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw " + feeCollector + " ubze 2000",
		"1 0 transfer bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw",
	}, queryStrings(t, ix.db, `SELECT concat_ws(' ', seq, coalesce(msg_index::text, '-'), kind, sender,
			CASE WHEN seq = 0 THEN concat_ws(' ', recipient, denom, amount) END)
		FROM explorer.transfers WHERE height = $1 AND tx_index = 0`, hSend))

	// Every transaction pays a fee, and a failed one moves nothing else.
	assert.Equal(t, "0", ix.one(t, `SELECT count(*)::text FROM explorer.transactions t
		WHERE NOT EXISTS (SELECT 1 FROM explorer.transfers x
		  WHERE x.height = t.height AND x.tx_index = t.tx_index AND x.msg_index IS NULL AND x.recipient = $1)`, feeCollector))
	assert.Equal(t, "1 0", ix.one(t, `SELECT concat_ws(' ', count(*), count(x.msg_index)) FROM explorer.transfers x
		JOIN explorer.transactions t USING (height, tx_index) WHERE NOT t.success`))

	// The order block: the settlements of the fill at block level, and the
	// fill itself with its typed values decoded.
	assert.Equal(t, "3", ix.one(t, `SELECT count(*)::text FROM explorer.transfers
		WHERE height = $1 AND tx_index = -1 AND msg_index IS NULL`, hOrders))
	assert.Equal(t, "1 string", ix.one(t, `SELECT concat_ws(' ', count(*), min(jsonb_typeof(attrs->'id')))
		FROM explorer.block_events WHERE height = $1 AND type = 'bze.tradebin.OrderExecutedEvent'`, hOrders))
	assert.Equal(t, "11", ix.one(t, `SELECT count(*)::text FROM explorer.block_events
		WHERE height = $1 AND type = 'bze.tradebin.OrderSavedEvent'`, hOrders))

	// The slash: its burn and the bonded tokens moving pools are rows; the
	// slash event is stored.
	assert.Equal(t, []string{"burn 613806200", "transfer 6137448379694"}, queryStrings(t, ix.db,
		`SELECT concat_ws(' ', kind, amount) FROM explorer.transfers WHERE height = $1`, hSlash))
	assert.Equal(t, []string{"slash"}, queryStrings(t, ix.db, `SELECT type FROM explorer.block_events WHERE height = $1`, hSlash))

	// The empty block has neither, and no height stores a routine move or a
	// routine event.
	assert.Equal(t, "0 0", ix.one(t, `SELECT concat_ws(' ', (SELECT count(*) FROM explorer.transfers WHERE height = $1),
		(SELECT count(*) FROM explorer.block_events WHERE height = $1))`, h317))
	mint, distribution := chain.ModuleAddress(chain.Mint), chain.ModuleAddress(chain.Distribution)
	assert.Equal(t, "0", ix.one(t, `SELECT count(*)::text FROM explorer.transfers WHERE tx_index = -1
		AND (sender = $1 OR recipient = $1 OR (sender = $2 AND recipient = $3))`, mint, feeCollector, distribution))
	assert.Equal(t, "0", ix.one(t, `SELECT count(*)::text FROM explorer.block_events
		WHERE type IN ('mint', 'commission', 'rewards', 'proposer_reward', 'coin_spent', 'coin_received',
		               'coinbase', 'transfer', 'message', 'liveness')`))

	// A second pass writes nothing: every row as it was, row version
	// included.
	first := eventRows(t, ix)
	require.NotEmpty(t, first)
	for _, h := range transferHeights {
		ix.index(t, h)
	}
	assert.Equal(t, first, eventRows(t, ix))
}

// The backfill's batch writer, and a reindex, store the rows the live path
// stores.
func TestTheBatchWriterWritesTransfersLikeTheLivePath(t *testing.T) {
	live := newIndexer(t)
	for _, h := range transferHeights {
		live.index(t, h)
	}

	batch := newIndexer(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, batch.url)
	require.NoError(t, err)
	defer pool.Close()
	var ents []*transform.Entities
	for i := len(transferHeights) - 1; i >= 0; i-- {
		h := transferHeights[i]
		b, _, err := batch.client.Block(ctx, h)
		require.NoError(t, err)
		r, _, err := batch.client.BlockResults(ctx, h)
		require.NoError(t, err)
		c, _, err := batch.client.Commit(ctx, h)
		require.NoError(t, err)
		e, err := batch.tr.Transform(transform.Input{Block: b, Results: r, Commit: c})
		require.NoError(t, err)
		ents = append(ents, e)
	}
	w := writer.NewBatchWriter(pool)
	require.NoError(t, w.Write(ctx, ents, writer.ModeInsert))
	written := eventRows(t, batch)
	require.NoError(t, w.Write(ctx, ents, writer.ModeUpdate), "a reindex of the same heights")

	assert.Equal(t, withoutVersions(eventRows(t, live)), withoutVersions(eventRows(t, batch)))
	assert.Equal(t, written, eventRows(t, batch), "the reindex rewrote no row")
}

func testTransfersOnTheDetailPages(t *testing.T, base string, _ *indexer) {
	feeCollector := chain.ModuleAddress(chain.FeeCollector)

	tx := into[dto.Tx](t, fetch(t, base+"/txs/"+sendHash))
	require.Len(t, tx.Transfers, 2)
	fee := tx.Transfers[0]
	assert.Nil(t, fee.MsgIndex, "the fee belongs to no message")
	assert.Equal(t, feeCollector, *fee.Recipient)
	assert.Equal(t, &dto.Label{Name: "Fee collector", Kind: "module"}, fee.RecipientLabel, "module ends are labelled")
	assert.Nil(t, fee.SenderLabel)
	assert.Equal(t, "2000", fee.Amount)
	require.NotNil(t, tx.Transfers[1].MsgIndex)
	assert.Equal(t, int64(0), *tx.Transfers[1].MsgIndex)

	r := fetch(t, fmt.Sprintf("%s/blocks/%d", base, hOrders))
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	b := into[dto.Block](t, r)
	require.Len(t, b.Transfers, 3, "the settlements of the block's fill")
	for _, x := range b.Transfers {
		assert.Nil(t, x.MsgIndex)
	}
	assert.Len(t, b.Events, 12, "the saved orders and the fill")
	assert.Nil(t, b.EventsNextCursor)
	var executed int
	for i, e := range b.Events {
		if i > 0 {
			assert.Greater(t, e.Seq, b.Events[i-1].Seq, "in event order")
		}
		if e.Type == "bze.tradebin.OrderExecutedEvent" {
			executed++
			assert.JSONEq(t, `"000000000000000006704522"`, string(attr(t, e.Attrs, "id")))
		}
	}
	assert.Equal(t, 1, executed)

	// The events route pages through the same events.
	var seqs []int64
	url := fmt.Sprintf("%s/blocks/%d/events?limit=5", base, hOrders)
	for {
		r := fetch(t, url)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		assert.Equal(t, "public, max-age=31536000, immutable", r.header.Get("Cache-Control"))
		page := into[dto.List[dto.BlockEvent]](t, r)
		for _, e := range page.Items {
			seqs = append(seqs, e.Seq)
		}
		if page.NextCursor == nil {
			break
		}
		url = fmt.Sprintf("%s/blocks/%d/events?limit=5&cursor=%s", base, hOrders, *page.NextCursor)
	}
	want := make([]int64, len(b.Events))
	for i, e := range b.Events {
		want[i] = e.Seq
	}
	assert.Equal(t, want, seqs)

	empty := into[dto.Block](t, fetch(t, fmt.Sprintf("%s/blocks/%d", base, h317)))
	assert.NotNil(t, empty.Transfers)
	assert.Empty(t, empty.Transfers)
	assert.NotNil(t, empty.Events)
	assert.Empty(t, empty.Events)

	assert.Equal(t, http.StatusNotFound, fetch(t, base+"/blocks/1/events").status)
}

// attr returns the raw JSON of key in the attributes object raw.
func attr(t *testing.T, raw json.RawMessage, key string) json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &m))
	v, ok := m[key]
	require.True(t, ok, "no %q in %s", key, raw)
	return v
}
