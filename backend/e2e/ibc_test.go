//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// Recorded heights with cross-chain transfers: 4002550 sends ubze to
// Osmosis over channel-0 and 4002556 acknowledges it (chain v5); 10002143
// receives ubze back; 20235036 sends a transfer nothing settles in the
// fixtures; 20237220 times out two (chain v7); 24999205 sends USDC to
// Noble over channel-3, and 24999209 acknowledges it and receives ubze.
var ibcHeights = []int64{4002550, 4002556, 10002143, 20235036, 20237220, 24999205, 24999209}

const ibcUser = "bze1mjpllwcpca0t2dmnr3wez2ugkr5l07lf62a32y"

// ibcRows are the ibc_transfers rows, every column.
func ibcRows(t *testing.T, e *syncEnv) []string {
	t.Helper()
	return queryStrings(t, e.db, `SELECT concat_ws(' ', channel_id, sequence, direction, status, sender, receiver, denom,
			amount, coalesce(memo, '-'), coalesce(counterparty_chain_id, '-'), created_height, created_tx_index,
			created_tx_hash, created_time, coalesce(timeout_height, '-'), coalesce(timeout_time::text, '-'),
			coalesce(result_height::text, '-'), coalesce(result_tx_hash, '-'), coalesce(result_time::text, '-'),
			coalesce(ack_error, '-'))
		FROM explorer.ibc_transfers ORDER BY channel_id, sequence, direction`)
}

// The backfill walks down, so an acknowledgement or a timeout is written
// before its transfer; the live path walks up. Both end with the same rows,
// whether the channels were synced before the transfers (live) or after
// them (a backfill on a fresh database), and writing the heights again, in
// either mode, changes nothing.
func TestIBCTransfersInEitherOrder(t *testing.T) {
	ctx := context.Background()
	// Both before any sync-state run, which changes the directory.
	down, up := newSyncEnv(t), newSyncEnv(t)
	w := batchWriter(t, down.url)
	for _, h := range slices.Backward(ibcHeights) {
		require.NoError(t, w.Write(ctx, []*transform.Entities{transformed(t, down, h)}, writer.ModeInsert))
	}
	assert.Equal(t, []string{"0"}, queryStrings(t, down.db,
		`SELECT count(*) FROM explorer.ibc_transfers WHERE counterparty_chain_id IS NOT NULL`), "no channel known yet")
	code, out := down.syncState(t)
	require.Equal(t, 0, code, out)
	rows := ibcRows(t, down)
	require.Len(t, rows, 7)

	code, out = up.syncState(t)
	require.Equal(t, 0, code, out)
	pool, err := pgxpool.New(ctx, up.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	live := writer.NewLiveWriter(pool)
	for _, h := range ibcHeights {
		require.NoError(t, live.WriteBlock(ctx, transformed(t, up, h)))
	}
	assert.Equal(t, rows, ibcRows(t, up), "the walking direction does not matter")

	for _, h := range ibcHeights {
		require.NoError(t, live.WriteBlock(ctx, transformed(t, up, h)))
		require.NoError(t, w.Write(ctx, []*transform.Entities{transformed(t, down, h)}, writer.ModeUpdate))
	}
	assert.Equal(t, rows, ibcRows(t, up), "a height written again changes nothing")
	assert.Equal(t, rows, ibcRows(t, down), "a reindex changes nothing")

	statuses := queryStrings(t, down.db, `SELECT concat_ws(' ', channel_id, sequence, direction, status,
			coalesce(counterparty_chain_id, '-'), created_height, coalesce(result_height::text, '-'))
		FROM explorer.ibc_transfers`)
	assert.Equal(t, []string{
		"channel-0 106 out delivered osmosis-1 4002550 4002556",
		"channel-0 10637 out pending osmosis-1 20235036 -",
		"channel-0 10650 out refunded_timeout osmosis-1 20237220 20237220",
		"channel-0 10651 out refunded_timeout osmosis-1 20237220 20237220",
		"channel-0 10973 in received osmosis-1 24999209 24999209",
		"channel-0 863 in received osmosis-1 10002143 10002143",
		"channel-3 5437 out delivered noble-1 24999205 24999209",
	}, statuses, "a settled transfer keeps its sending transaction; a timeout whose transfer predates the fixtures is created by it")
}

// The channels set fills ibc_channels and one escrow label per channel,
// named after the counterparty once the registry knows it; the API serves
// the transfers on the transaction, the account and the channels list.
func TestIBCChannelsAndEndpoints(t *testing.T) {
	e := newSyncEnv(t)
	code, out := e.syncState(t)
	require.Equal(t, 0, code, out)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	live := writer.NewLiveWriter(pool)
	for _, h := range ibcHeights {
		require.NoError(t, live.WriteBlock(ctx, transformed(t, e, h)))
	}

	assert.Equal(t, []string{"14"}, queryStrings(t, e.db, `SELECT count(*) FROM explorer.ibc_channels`))
	assert.Equal(t, []string{"07-tendermint-9 connection-3 noble-1 channel-95 open"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', client_id, connection_id, counterparty_chain_id, counterparty_channel_id, state)
		FROM explorer.ibc_channels WHERE channel_id = 'channel-3'`))
	assert.Equal(t, []string{"14"}, queryStrings(t, e.db,
		`SELECT count(*) FROM explorer.labels WHERE kind = 'ibc_escrow' AND module = 'transfer' AND source = 'sync'`))
	escrow := func(channel string) []string {
		return queryStrings(t, e.db, `SELECT name FROM explorer.labels WHERE address = $1`,
			chain.EscrowAddress(chain.TransferPort, channel))
	}
	assert.Equal(t, []string{"Cross-chain escrow (noble-1)"}, escrow("channel-3"),
		"the start resync runs the channels before the registry knows Noble")
	// The registry asked the channels set for a resync; the sync-state
	// command does not serve that queue, its next run names the escrow.
	code, out = e.syncState(t)
	require.Equal(t, 0, code, out)
	assert.Equal(t, []string{"Cross-chain escrow (Noble)"}, escrow("channel-3"))
	assert.Equal(t, []string{"Cross-chain escrow (osmosis-1)"}, escrow("channel-0"), "a chain the registry fixtures lack")

	base := e.serveAPI(t)
	hashes := queryStrings(t, e.db, `SELECT concat_ws(' ', created_tx_hash, result_tx_hash) FROM explorer.ibc_transfers
		WHERE channel_id = 'channel-3' AND sequence = 5437`)
	require.Len(t, hashes, 1)
	var sendHash, ackHash string
	_, err = fmt.Sscan(hashes[0], &sendHash, &ackHash)
	require.NoError(t, err)

	r := fetch(t, base+"/txs/"+sendHash)
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	assert.Equal(t, "public, max-age=60", r.header.Get("Cache-Control"), "delivered: cached like any transaction")
	sent := into[dto.Tx](t, r)
	require.Len(t, sent.IBCTransfers, 1)
	tr := sent.IBCTransfers[0]
	assert.Equal(t, "delivered", tr.Status)
	assert.Equal(t, "ibc/6490A7EAB61059BFC1CDDEB05917DD70BDF3A611654162A1A47DB930D40D8AF4", tr.Denom)
	require.NotNil(t, tr.Counterparty.PrettyName)
	assert.Equal(t, "Noble", *tr.Counterparty.PrettyName)
	require.NotNil(t, tr.Counterparty.ExplorerAccountURL)
	assert.Contains(t, *tr.Counterparty.ExplorerAccountURL, "${accountAddress}")
	require.NotNil(t, tr.ResultTxHash)
	assert.Equal(t, ackHash, *tr.ResultTxHash)

	ack := into[dto.Tx](t, fetch(t, base+"/txs/"+ackHash))
	require.Len(t, ack.IBCTransfers, 1, "the relayer's acknowledgement settled the transfer")
	assert.Equal(t, int64(5437), ack.IBCTransfers[0].Sequence)

	pending := queryStrings(t, e.db, `SELECT created_tx_hash FROM explorer.ibc_transfers WHERE status = 'pending'`)
	require.Len(t, pending, 1)
	r = fetch(t, base+"/txs/"+pending[0])
	assert.Equal(t, "no-store", r.header.Get("Cache-Control"), "a pending transfer may still settle")
	assert.True(t, into[dto.Tx](t, r).IBCTransfers[0].Expired, "its timeout passed in 2025")

	page := into[dto.List[dto.IBCTransfer]](t, fetch(t, base+"/accounts/"+ibcUser+"/ibc?limit=3"))
	require.Len(t, page.Items, 3)
	keys := func(items []dto.IBCTransfer) []string {
		var out []string
		for _, it := range items {
			out = append(out, it.Direction+" "+it.ChannelID+"/"+strconv.FormatInt(it.Sequence, 10))
		}
		return out
	}
	assert.Equal(t, []string{"in channel-0/10973", "out channel-3/5437", "out channel-0/10651"}, keys(page.Items),
		"newest first, both directions")
	require.NotNil(t, page.NextCursor)
	rest := into[dto.List[dto.IBCTransfer]](t, fetch(t, base+"/accounts/"+ibcUser+"/ibc?limit=3&cursor="+*page.NextCursor))
	assert.Equal(t, []string{"out channel-0/10650", "out channel-0/10637"}, keys(rest.Items))
	assert.Nil(t, rest.NextCursor)
	in := into[dto.List[dto.IBCTransfer]](t, fetch(t, base+"/accounts/"+ibcUser+"/ibc?direction=in"))
	assert.Equal(t, []string{"in channel-0/10973"}, keys(in.Items))

	channels := into[dto.List[dto.IBCChannel]](t, fetch(t, base+"/ibc/channels"))
	require.Len(t, channels.Items, 14)
	assert.Equal(t, "channel-0", channels.Items[0].ChannelID)
	assert.Equal(t, "channel-13", channels.Items[13].ChannelID, "by channel number")
	assert.Equal(t, "bze1a53udazy8ayufvy0s434pfwjcedzqv34tgwwee", channels.Items[0].EscrowAddress)
}
