//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/migrations"
)

// ascPages follows the oldest-first activity list of address: the first
// page from start, the next ones from the cursor alone, filters on every
// page.
func ascPages(t *testing.T, base, address, start, filters string, limit int) [][]dto.Activity {
	t.Helper()
	list := fmt.Sprintf("%s/accounts/%s/activity?order=asc&limit=%d%s", base, address, limit, filters)
	var pages [][]dto.Activity
	var last *dto.Activity
	for next := list + start; ; {
		r := fetch(t, next)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
		page := into[dto.List[dto.Activity]](t, r)
		pages = append(pages, page.Items)
		for i := range page.Items {
			a := &page.Items[i]
			if last != nil {
				require.True(t, a.Height > last.Height || a.Height == last.Height && a.TxIndex > last.TxIndex,
					"(height, tx_index) strictly ascending: %d/%d after %d/%d", a.Height, a.TxIndex, last.Height, last.TxIndex)
			}
			last = a
		}
		if page.NextCursor == nil {
			return pages
		}
		require.Len(t, page.Items, limit, "only the last page is short")
		next = list + "&cursor=" + url.QueryEscape(*page.NextCursor)
	}
}

// ascRow is one synthetic activity row of the oldest-first test with the
// single message of its transaction.
type ascRow struct {
	height  int64
	txIndex int
	time    time.Time
	deltas  string
	types   string
	body    string
}

// order=asc lists the rows oldest first from a height (strictly above it),
// from a time (at or after it) or from a cursor, without a gap or a repeat
// across a block holding several transactions, with the filters applied, and
// the newest-first list is left as it was.
func TestAccountActivityOldestFirst(t *testing.T) {
	e := newSyncEnv(t)
	const h0 int64 = 30_000_000
	address := chainToolsOwner
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	at := func(i int) time.Time { return t0.Add(time.Duration(i) * time.Minute) }
	const plus, minus = `[{"denom":"ubze","amount":"5"}]`, `[{"denom":"ubze","amount":"-5"}]`
	send := fmt.Sprintf(`{"to_address":%q}`, address)
	rows := []ascRow{
		{h0 + 100, 0, at(1), plus, msgSend, send},
		{h0 + 200, 0, at(2), plus, msgSend, send},
		{h0 + 200, 1, at(2).Add(time.Second), plus, msgSend, send},
		{h0 + 200, 2, at(2).Add(2 * time.Second), plus, msgSend, send},
		{h0 + 200, 3, at(2), minus, msgSend, `{"to_address":"bze1xrw00qx3laxg7dkwx0aymxyqtwr9xsuf3k3pa7"}`},
		{h0 + 300, 0, at(3), `[{"denom":"ufoo","amount":"7"}]`, msgSend, send},
		{h0 + 400, 0, at(4), plus, msgSend, send},
	}
	// Rows the incoming filter leaves out, so the partial index is the
	// cheaper walk for the planner, as on mainnet.
	for i := range 300 {
		rows = append(rows, ascRow{h0 + 1000 + int64(i), 0, at(10 + i), minus, msgDelegate, `{}`})
	}
	for _, r := range rows {
		_, err := e.db.Exec(`INSERT INTO explorer.account_activity
			(address, height, tx_index, time, category, kind, is_signer, success, deltas, msg_types)
			VALUES ($1, $2, $3, $4, 'received', 'send', false, true, $5::jsonb, ARRAY[$6]::text[])`,
			address, r.height, r.txIndex, r.time, r.deltas, r.types)
		require.NoError(t, err)
		_, err = e.db.Exec(`INSERT INTO explorer.messages (height, tx_index, msg_index, type_url, events, body)
			VALUES ($1, $2, 0, $3, '[]'::jsonb, $4::jsonb)`, r.height, r.txIndex, r.types, r.body)
		require.NoError(t, err)
	}
	_, err := e.db.Exec(`ANALYZE explorer.account_activity; ANALYZE explorer.messages`)
	require.NoError(t, err)

	base := e.serveAPI(t)
	key := func(pages [][]dto.Activity) []string {
		var out []string
		for _, p := range pages {
			for _, a := range p {
				out = append(out, fmt.Sprintf("%d/%s", a.Height-h0, a.Time.UTC().Format("15:04:05")))
			}
		}
		return out
	}
	// The three transactions of height 200 carry distinct times so that a
	// repeat or a gap at a page cut inside the block shows.
	incoming := []string{"200/00:02:00", "200/00:02:01", "200/00:02:02", "300/00:03:00", "400/00:04:00"}
	from100 := fmt.Sprintf("&after_height=%d", h0+100)

	for _, limit := range []int{1, 2, 4, 25} {
		pages := ascPages(t, base, address, from100, "&incoming=true", limit)
		assert.Equal(t, incoming, key(pages), "%d at a time: strictly above the height, oldest first", limit)
	}
	below := key(ascPages(t, base, address, fmt.Sprintf("&after_height=%d", h0+99), "&incoming=true", 25))
	assert.Equal(t, append([]string{"100/00:01:00"}, incoming...), below, "the height below includes the row at 100")
	assert.Equal(t, [][]dto.Activity{{}}, ascPages(t, base, address, fmt.Sprintf("&after_height=%d", h0+400),
		"&incoming=true", 25), "nothing above the last row")

	// From a time: rows at or after it.
	assert.Equal(t, incoming, key(ascPages(t, base, address, "&after_time="+url.QueryEscape(at(2).Format(time.RFC3339)),
		"&incoming=true", 2)))
	assert.Equal(t, incoming[3:], key(ascPages(t, base, address,
		"&after_time="+url.QueryEscape(at(2).Add(time.Second).Format(time.RFC3339)), "&incoming=true", 2)))

	// Filters keep working.
	assert.Equal(t, []string{"300/00:03:00"}, key(ascPages(t, base, address, from100, "&incoming=true&denom=ufoo", 1)))
	assert.Equal(t, [][]dto.Activity{{}}, ascPages(t, base, address, from100, "&incoming=true&category=dex", 25))
	all := key(ascPages(t, base, address, from100, "", 50))
	assert.Len(t, all, len(rows)-1, "without incoming every row above the height")
	assert.Equal(t, incoming[:3], all[:3])
	assert.Equal(t, "200/00:02:00", all[3], "the outgoing tx 3 of height 200 after its incoming ones")

	// The newest-first list is the same rows the other way round.
	var desc []string
	for _, p := range activityPages(t, base, address, "&incoming=true", 2) {
		for _, a := range p {
			desc = append([]string{fmt.Sprintf("%d/%s", a.Height-h0, a.Time.UTC().Format("15:04:05"))}, desc...)
		}
	}
	assert.Equal(t, append([]string{"100/00:01:00"}, incoming...), desc)

	for _, q := range []string{"?order=asc", "?order=asc&after_height=1&page=1", "?order=sideways",
		"?after_height=1", "?order=asc&after_time=yesterday"} {
		assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/accounts/"+address+"/activity"+q).status, q)
	}

	testActivityAscPlan(t, e, address, h0+100)
}

// ascPlanNode is the part of an EXPLAIN (FORMAT JSON) node the plan check
// reads.
type ascPlanNode struct {
	NodeType  string        `json:"Node Type"`
	Relation  string        `json:"Relation Name"`
	Index     string        `json:"Index Name"`
	Direction string        `json:"Scan Direction"`
	IndexCond string        `json:"Index Cond"`
	Plans     []ascPlanNode `json:"Plans"`
}

// The oldest-first incoming list walks idx_account_activity_incoming
// backward from the keyset, which is an index condition, and never sorts
// account_activity rows: the cost of a page does not grow with the rows
// below the start.
func testActivityAscPlan(t *testing.T, e *syncEnv, address string, after int64) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	defer pool.Close()
	rec := &recordingDB{Pool: pool}
	_, err = repository.NewExplorer(rec).ActivityAsc(ctx, address, nil, nil, true,
		repository.ActivityStart{After: &repository.ActivityKey{Height: after, TxIndex: math.MaxInt32}}, 3)
	require.NoError(t, err)

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(ctx, `SET enable_seqscan = off`)
	require.NoError(t, err)
	var raw json.RawMessage
	require.NoError(t, conn.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+rec.sql, rec.args...).Scan(&raw))
	var plan []struct {
		Plan ascPlanNode `json:"Plan"`
	}
	require.NoError(t, json.Unmarshal(raw, &plan))

	incoming := queryStrings(t, e.db, `SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = 'explorer.idx_account_activity_incoming'::regclass`)
	require.NotEmpty(t, incoming, "an index on every account_activity partition")
	partition := migrations.PartitionName("account_activity", after)

	var scans []ascPlanNode
	var sortsActivity bool
	var walk func(n ascPlanNode, underSort bool)
	walk = func(n ascPlanNode, underSort bool) {
		underSort = underSort || n.NodeType == "Sort" || n.NodeType == "Incremental Sort"
		if strings.HasPrefix(n.Relation, "account_activity") {
			sortsActivity = sortsActivity || underSort
			if n.Relation == partition {
				scans = append(scans, n)
			}
		}
		for _, c := range n.Plans {
			walk(c, underSort)
		}
	}
	walk(plan[0].Plan, false)
	assert.False(t, sortsActivity, "no Sort above an account_activity scan: %s", raw)
	require.Len(t, scans, 1, "the partition holding the rows is read once: %s", raw)
	s := scans[0]
	assert.Contains(t, incoming, s.Index, "the incoming index: %s", raw)
	assert.Equal(t, "Backward", s.Direction, "walked oldest first: %s", raw)
	assert.Contains(t, s.IndexCond, "height", "the start bounds the walk: %s", raw)
}
