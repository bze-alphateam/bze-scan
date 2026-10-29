//go:build e2e

package e2e_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// storedBefore are message events as the indexer stored them before
// migration 000018: every type, update_client with its header.
const storedBefore = `[
	{"type":"message","attrs":{"action":"/ibc.core.client.v1.MsgUpdateClient","sender":"bze1relayer","module":"ibc_client"}},
	{"type":"coin_spent","attrs":{"spender":"bze1relayer","amount":"5ubze"}},
	{"type":"coin_received","attrs":{"receiver":"bze1other","amount":"5ubze"}},
	{"type":"transfer","attrs":{"recipient":"bze1other","sender":"bze1relayer","amount":"5ubze"}},
	{"type":"update_client","attrs":{"client_id":"07-tendermint-0","consensus_heights":"1-100","header":"0a0b0c"}},
	{"type":"bze.tradebin.OrderCancelMessageEvent","attrs":{"orderId":"7","message_id":"m1"}}]`

// keptAfter is storedBefore once the dropped events and attribute are gone.
const keptAfter = `[
	{"type":"update_client","attrs":{"client_id":"07-tendermint-0","consensus_heights":"1-100"}},
	{"type":"bze.tradebin.OrderCancelMessageEvent","attrs":{"orderId":"7","message_id":"m1"}}]`

func TestMigrateUpRewritesTheStoredMessageEvents(t *testing.T) {
	dbURL := freshDatabase(t, true)
	db := connect(t, dbURL)
	migrateUp(t, dbURL)
	_, err := openMigrator(t, dbURL).Down(latestVersion - 17) // back to before 000018
	require.NoError(t, err)

	// Rows in two partitions and one above them, as 000017 left them.
	_, err = db.Exec(`SELECT explorer.ensure_partitions(50000000, 50000000)`)
	require.NoError(t, err)
	heights := []int64{3_000_081, 24_998_316, 50_000_001}
	for _, h := range heights {
		_, err := db.Exec(`INSERT INTO explorer.messages (height, tx_index, msg_index, type_url, events)
			VALUES ($1, 0, 0, '/ibc.core.client.v1.MsgUpdateClient', $2), ($1, 0, 1, '/x.MsgKept', $3)`,
			h, storedBefore, `[{"type":"bze.x.Typed","attrs":{"k":"v"}}]`)
		require.NoError(t, err)
	}

	res := migrateUp(t, dbURL)
	assert.Contains(t, res.Steps, "message_events")
	assertEvents := func() {
		t.Helper()
		for _, h := range heights {
			var got, kept string
			require.NoError(t, db.QueryRow(`SELECT events::text FROM explorer.messages WHERE height = $1 AND msg_index = 0`, h).Scan(&got))
			assert.JSONEq(t, keptAfter, got, "height %d", h)
			require.NoError(t, db.QueryRow(`SELECT events::text FROM explorer.messages WHERE height = $1 AND msg_index = 1`, h).Scan(&kept))
			assert.JSONEq(t, `[{"type":"bze.x.Typed","attrs":{"k":"v"}}]`, kept, "height %d: a row without dropped events", h)
		}
	}
	assertEvents()
	assert.Empty(t, queryStrings(t, db, `SELECT name FROM explorer.pending_rewrites WHERE name = 'message_events'`), "the rewrite is done")

	// Once done, a later up leaves new rows alone, whatever they hold.
	_, err = db.Exec(`UPDATE explorer.messages SET events = $1 WHERE height = $2 AND msg_index = 1`, storedBefore, heights[0])
	require.NoError(t, err)
	migrateUp(t, dbURL)
	var untouched string
	require.NoError(t, db.QueryRow(`SELECT events::text FROM explorer.messages WHERE height = $1 AND msg_index = 1`, heights[0]).Scan(&untouched))
	assert.JSONEq(t, storedBefore, untouched)
}

// TestKeptEventsMatchesTheTransformer keeps the SQL filter of migration
// 000018 and the transformer's lists the same.
func TestKeptEventsMatchesTheTransformer(t *testing.T) {
	dbURL := freshDatabase(t, true)
	db := connect(t, dbURL)
	migrateUp(t, dbURL)

	type event struct {
		Type  string         `json:"type"`
		Attrs map[string]any `json:"attrs"`
	}
	in := []event{{Type: "kept", Attrs: map[string]any{"a": "1"}}}
	want := []event{{Type: "kept", Attrs: map[string]any{"a": "1"}}}
	for _, typ := range transform.UnstoredEventTypes {
		in = append(in, event{Type: typ, Attrs: map[string]any{"a": "1"}})
	}
	for typ, attrs := range transform.UnstoredEventAttributes {
		full := map[string]any{"kept": "1"}
		for _, a := range attrs {
			full[a] = "x"
		}
		in = append(in, event{Type: typ, Attrs: full})
		want = append(want, event{Type: typ, Attrs: map[string]any{"kept": "1"}})
	}
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	var got string
	require.NoError(t, db.QueryRow(`SELECT explorer.kept_events($1::jsonb)::text`, string(raw)).Scan(&got))
	wantRaw, err := json.Marshal(want)
	require.NoError(t, err)
	assert.JSONEq(t, string(wantRaw), got)

}
