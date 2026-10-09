//go:build e2e

package e2e_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/classify"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// Fixture heights with transactions (see the fake node's testdata).
const (
	hFailed   int64 = 24999004 // two MsgCancelOrder transactions and one out-of-gas MsgCreateOrder
	hOrders   int64 = 24999134 // multi-message MsgCreateOrder transactions
	hTransfer int64 = 24999205 // IBC MsgTransfer
	hRelay    int64 = 24999209 // relayer MsgUpdateClient + MsgRecvPacket / MsgAcknowledgement
	hRewards  int64 = 25000439 // MsgWithdrawDelegatorReward + MsgWithdrawValidatorCommission
	hExec     int64 = 25000440 // authz MsgExec
	hSend     int64 = 25000894 // MsgSend
)

var txHeights = []int64{hFailed, hOrders, hTransfer, hRelay, hRewards, hExec, hSend}

func newTransformer(t *testing.T) *transform.Transformer {
	t.Helper()
	codec, err := chain.NewCodec()
	require.NoError(t, err)
	return transform.New(codec, log.StandardLogger())
}

// indexer runs the live indexer's pass for given heights: the fake node, the
// real transformer and the live writer over a migrated database of its own.
type indexer struct {
	url    string
	db     *sql.DB
	client *node.Client
	tr     *transform.Transformer
	w      *writer.LiveWriter
}

func newIndexer(t *testing.T) *indexer {
	t.Helper()
	url := freshDatabase(t, true)
	migrateUp(t, url)
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return &indexer{
		url:    url,
		db:     connect(t, url),
		client: node.New(fakenode.New(t).URL),
		tr:     newTransformer(t),
		w:      writer.NewLiveWriter(pool),
	}
}

func (ix *indexer) index(t *testing.T, h int64) {
	t.Helper()
	ctx := context.Background()
	b, _, err := ix.client.Block(ctx, h)
	require.NoError(t, err)
	r, _, err := ix.client.BlockResults(ctx, h)
	require.NoError(t, err)
	c, _, err := ix.client.Commit(ctx, h)
	require.NoError(t, err)
	ents, err := ix.tr.Transform(transform.Input{Block: b, Results: r, Commit: c})
	require.NoError(t, err)
	require.NoError(t, ix.w.WriteBlock(ctx, ents))
}

func (ix *indexer) one(t *testing.T, query string, args ...any) string {
	t.Helper()
	rows := queryStrings(t, ix.db, query, args...)
	require.Len(t, rows, 1, query)
	return rows[0]
}

func TestIndexesTransactionsAndMessages(t *testing.T) {
	ix := newIndexer(t)
	for _, h := range txHeights {
		ix.index(t, h)
	}

	// Every height: one transactions row per transaction of the block, one
	// messages row per message, and blocks.tx_count agrees.
	for _, h := range txHeights {
		assert.Equal(t, "true", ix.one(t, `SELECT (b.tx_count = (SELECT count(*) FROM explorer.transactions WHERE height = b.height)
			AND b.tx_failed_count = (SELECT count(*) FROM explorer.transactions WHERE height = b.height AND NOT success))::text
			FROM explorer.blocks b WHERE b.height = $1`, h), "height %d", h)
		assert.Equal(t, "true", ix.one(t, `SELECT ((SELECT coalesce(sum(msg_count), 0) FROM explorer.transactions WHERE height = $1)
			= (SELECT count(*) FROM explorer.messages WHERE height = $1))::text`, h), "height %d", h)
	}
	assert.Equal(t, "0", ix.one(t, `SELECT count(*)::text FROM explorer.messages WHERE body IS NULL`), "every message decoded")

	// MsgSend: the sink's hash, the fee, the signer and the message.
	assert.Equal(t, "t|0|[{\"denom\": \"ubze\", \"amount\": \"2000\"}]|bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw|{bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw}|{/cosmos.bank.v1beta1.MsgSend}|",
		ix.one(t, `SELECT concat_ws('|', success, code, fee, fee_payer, signers, msg_types, coalesce(memo, ''))
			FROM explorer.transactions WHERE hash = 'E580BFA56DE28886E51DDB9BE50DD610C1C832C579CA08FCF9177B04D2F7B7B9'`))
	assert.Equal(t, "bank|bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw|message|t", ix.one(t, `SELECT concat_ws('|',
			module, sender, events->0->>'type', body ? 'to_address')
			FROM explorer.messages WHERE height = $1 AND tx_index = 0`, hSend))

	// The failed transaction: code, log, fee and signer from the ante
	// handler, the message decoded from the raw bytes, no events.
	assert.Equal(t, "f|11|sdk|t|t|1", ix.one(t, `SELECT concat_ws('|', success, code, codespace,
			error_log LIKE 'out of gas%', jsonb_array_length(fee) = 1, msg_count)
			FROM explorer.transactions WHERE height = $1 AND NOT success`, hFailed))
	assert.Equal(t, "/bze.tradebin.MsgCreateOrder|[]|t", ix.one(t, `SELECT concat_ws('|', m.type_url, m.events, m.body ? 'creator')
			FROM explorer.messages m JOIN explorer.transactions t USING (height, tx_index)
			WHERE m.height = $1 AND NOT t.success`, hFailed))
	assert.Equal(t, "0", ix.one(t, `SELECT count(*)::text FROM explorer.transactions WHERE success AND error_log IS NOT NULL`))

	// Typed events are stored decoded: the order event's values are JSON
	// strings, not quoted strings.
	assert.Equal(t, "string", ix.one(t, `SELECT DISTINCT jsonb_typeof(e->'attrs'->'creator')
			FROM explorer.messages, jsonb_array_elements(events) e
			WHERE height = $1 AND e->>'type' = 'bze.tradebin.OrderCreateMessageEvent'`, hOrders))
	assert.Equal(t, "0", ix.one(t, `SELECT count(*)::text FROM explorer.messages, jsonb_array_elements(events) e
			WHERE e->'attrs' ? 'msg_index'`), "msg_index lives in the row, not in the events")

	// authz MsgExec is one row carrying the nested messages.
	assert.Equal(t, "/cosmos.authz.v1beta1.MsgExec|t", ix.one(t, `SELECT concat_ws('|', type_url,
			jsonb_array_length(body->'msgs') > 0) FROM explorer.messages WHERE height = $1`, hExec))

	// Relay and IBC transactions decode, multi-message transactions keep
	// their order.
	assert.Equal(t, []string{"/ibc.applications.transfer.v1.MsgTransfer"},
		queryStrings(t, ix.db, `SELECT DISTINCT type_url FROM explorer.messages WHERE height = $1`, hTransfer))
	assert.Contains(t, queryStrings(t, ix.db, `SELECT DISTINCT type_url FROM explorer.messages WHERE height = $1`, hRelay),
		"/ibc.core.channel.v1.MsgRecvPacket")
	assert.Equal(t, []string{
		"/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward",
		"/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission",
	}, queryStrings(t, ix.db, `SELECT type_url FROM explorer.messages WHERE height = $1 ORDER BY tx_index, msg_index`, hRewards))
}

func TestIndexingAHeightTwiceChangesNothing(t *testing.T) {
	ix := newIndexer(t)
	snapshot := func() []string {
		return queryStrings(t, ix.db, `
			SELECT 't' || to_jsonb(t)::text FROM explorer.transactions t
			UNION ALL SELECT 'm' || to_jsonb(m)::text FROM explorer.messages m
			UNION ALL SELECT 'b' || to_jsonb(b)::text FROM explorer.blocks b
			ORDER BY 1`)
	}
	ix.index(t, hOrders)
	first := snapshot()
	require.NotEmpty(t, first)

	ix.index(t, hOrders)
	assert.Equal(t, first, snapshot())
}

// The real wiring: the live indexer started on a tip with transactions
// writes them with the block.
func TestLiveIndexerWritesTransactions(t *testing.T) {
	env := newLiveEnv(t, hSend)
	var n int
	require.NoError(t, env.db.QueryRow(`SELECT count(*) FROM explorer.transactions WHERE height = $1`, hSend).Scan(&n))
	assert.Equal(t, 2, n)
	require.NoError(t, env.db.QueryRow(`SELECT count(*) FROM explorer.messages WHERE height = $1`, hSend).Scan(&n))
	assert.Equal(t, 2, n, "MsgSend and a relayer's MsgUpdateClient")
}

func TestMigrateMirrorsTheClassification(t *testing.T) {
	url := freshDatabase(t, true)
	db := connect(t, url)
	migrateUp(t, url)

	assert.Equal(t, len(classify.Messages()), countRows(t, db, "explorer.message_kinds"))
	assert.Equal(t, len(classify.BlockEvents()), countRows(t, db, "explorer.block_event_kinds"))
	assert.Equal(t, []string{"send sent received transfer.recipient"}, queryStrings(t, db,
		`SELECT concat_ws(' ', kind, signer_category, participant_category, counterparty_attr)
		FROM explorer.message_kinds WHERE type_url = '/cosmos.bank.v1beta1.MsgSend'`))
	assert.Equal(t, []string{"dex_fill dex {maker,taker}"}, queryStrings(t, db,
		`SELECT concat_ws(' ', kind, category, address_attrs)
		FROM explorer.block_event_kinds WHERE event_type = 'bze.tradebin.OrderExecutedEvent'`))
	assert.Equal(t, []string{"{}"}, queryStrings(t, db,
		`SELECT address_attrs::text FROM explorer.block_event_kinds WHERE event_type = 'slash'`))

	// A later `migrate up` rewrites the mirror (an entry dropped from the Go
	// table disappears, an edited one is restored) and reclassifies the
	// activity stored as "other" whose type is now known.
	_, err := db.Exec(`
		INSERT INTO explorer.message_kinds (type_url, kind, signer_category) VALUES ('/bze.gone.MsgOld', 'old', 'other');
		UPDATE explorer.message_kinds SET kind = 'edited' WHERE type_url = '/cosmos.bank.v1beta1.MsgSend';
		INSERT INTO explorer.account_activity (address, height, tx_index, time, category, kind, is_signer, msg_types)
		VALUES ('bze1delegator', 100, 0, now(), 'other', 'other', true, '{/cosmos.staking.v1beta1.MsgDelegate}')`)
	require.NoError(t, err)

	res := migrateUp(t, url)
	assert.False(t, res.Applied, "no SQL migration pending")
	assert.Equal(t, []string{"partitions", "classification", "labels"}, res.Steps, "the steps run on every up")
	assert.Equal(t, len(classify.Messages()), countRows(t, db, "explorer.message_kinds"))
	assert.Equal(t, []string{"send"}, queryStrings(t, db,
		`SELECT kind FROM explorer.message_kinds WHERE type_url = '/cosmos.bank.v1beta1.MsgSend'`))
	assert.Equal(t, []string{"staking delegate"}, queryStrings(t, db,
		`SELECT category || ' ' || kind FROM explorer.account_activity WHERE address = 'bze1delegator'`))
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM `+table).Scan(&n))
	return n
}
