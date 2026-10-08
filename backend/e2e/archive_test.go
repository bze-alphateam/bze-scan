//go:build e2e

package e2e_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// A v7.2.0 height (legacy event format) with one transaction of two
// tradebin orders, sent under the pre-v8 type URL and the legacy router's
// create_order action.
const legacyHeight = int64(20230045)

// The backfill pipeline indexes an old-generation height end to end: the
// archive adapter normalises it and the rows read as a current height's.
func TestBackfillOfALegacyHeight(t *testing.T) {
	url := freshDatabase(t, true)
	migrateUp(t, url)
	db := connect(t, url)
	_, err := db.Exec(`INSERT INTO explorer.indexer_state (key, value, updated_at) VALUES ('live_floor', $1, now())`,
		strconv.FormatInt(legacyHeight+1, 10))
	require.NoError(t, err)

	cfg := &config.Config{
		LogLevel: "warn", LogFormat: config.LogFormatText, DatabaseURL: url,
		ArchiveRPCURL: fakenode.New(t).URL, BackfillFloorHeight: legacyHeight,
		BackfillWorkers: 1, BackfillBatch: 1, BackfillQuiet: 0, BackfillRateLimit: 0,
	}
	require.NoError(t, serve.RunBackfill(context.Background(), cfg, quickRetries))

	assert.Empty(t, queryStrings(t, db, `SELECT height::text FROM explorer.index_failures`))
	assert.Equal(t, []string{"20230045 1 0"}, queryStrings(t, db,
		`SELECT concat_ws(' ', height, tx_count, tx_failed_count) FROM explorer.blocks`))
	assert.Equal(t, []string{
		"49B1C592FA7862FBD94D00B0B5FCCD02F4D0DE6306889028B57DEBAB9968D63B t 2 " +
			"{/bze.tradebin.MsgCreateOrder,/bze.tradebin.MsgCreateOrder} " +
			"{bze10kw8lpqd9emyxn94gkm038t4jj90ark4x8ls0d} bze10kw8lpqd9emyxn94gkm038t4jj90ark4x8ls0d",
	}, queryStrings(t, db, `SELECT concat_ws(' ', hash, success, msg_count, msg_types, signers, fee_payer)
		FROM explorer.transactions`))

	assert.Equal(t, []string{
		"0 /bze.tradebin.MsgCreateOrder bze10kw8lpqd9emyxn94gkm038t4jj90ark4x8ls0d tradebin sell 4019206 sell",
		"1 /bze.tradebin.MsgCreateOrder bze10kw8lpqd9emyxn94gkm038t4jj90ark4x8ls0d tradebin buy 4513295 buy",
	}, queryStrings(t, db, `SELECT concat_ws(' ', msg_index, type_url, sender, module, body->>'order_type', body->>'amount',
			(SELECT e->'attrs'->>'order_type' FROM jsonb_array_elements(events) e
			  WHERE e->>'type' = 'bze.tradebin.OrderCreateMessageEvent'))
		FROM explorer.messages ORDER BY msg_index`),
		"current type URL and module, the decoded body, and the renamed typed event with its JSON values decoded")
	assert.Equal(t, []string{"10", "10"}, queryStrings(t, db,
		`SELECT jsonb_array_length(events)::text FROM explorer.messages ORDER BY msg_index`),
		"each message holds exactly its own events, split by the reconstructed msg_index")
}
