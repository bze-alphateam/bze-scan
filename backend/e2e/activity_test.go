//go:build e2e

package e2e_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
)

// activityPages follows the activity list of address from the first page
// to the last, limit items at a time.
func activityPages(t *testing.T, base, address, query string, limit int) [][]dto.Activity {
	t.Helper()
	var pages [][]dto.Activity
	url := fmt.Sprintf("%s/accounts/%s/activity?limit=%d%s", base, address, limit, query)
	for next := url; ; {
		r := fetch(t, next)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
		page := into[dto.List[dto.Activity]](t, r)
		pages = append(pages, page.Items)
		if page.NextCursor == nil {
			return pages
		}
		require.Len(t, page.Items, limit, "only the last page is short")
		next = url + "&cursor=" + *page.NextCursor
	}
}

func positions(pages [][]dto.Activity) []string {
	var out []string
	for _, p := range pages {
		for _, a := range p {
			hash := "-"
			if a.TxHash != nil {
				hash = "tx"
			}
			out = append(out, fmt.Sprintf("%d %s %s %s", a.Height, hash, a.Kind, a.Category))
		}
	}
	return out
}

func TestAccountActivity(t *testing.T) {
	e := newSyncEnv(t)
	code, _ := e.syncState(t)
	require.Equal(t, 0, code)
	e.index(t, txHeights...)

	activity := func() []string {
		return queryStrings(t, e.db, `SELECT concat_ws(' ', address, height, tx_index, kind, category, is_signer,
			success, coalesce(counterparty, '-'), deltas::text, msg_types::text) FROM explorer.account_activity
			ORDER BY address, height, tx_index`)
	}
	rows, accounts := activity(), e.accountRows(t)
	e.index(t, txHeights...)
	assert.Equal(t, rows, activity(), "indexing the heights again writes nothing")
	assert.Equal(t, accounts, e.accountRows(t), "and moves no activity_count")
	assert.Equal(t, []string{"7"}, queryStrings(t, e.db,
		`SELECT activity_count FROM explorer.accounts WHERE address = $1`, trader))
	assert.Equal(t, []string{"0"}, queryStrings(t, e.db, `SELECT count(*) FROM explorer.account_activity a
		JOIN explorer.labels l ON l.address = a.address WHERE l.kind = 'module'`), "module accounts get no rows")
	assert.Equal(t, []string{"bze1xrw00qx3laxg7dkwx0aymxyqtwr9xsuf3k3pa7 25000894 0 1"}, queryStrings(t, e.db,
		`SELECT concat_ws(' ', address, first_seen_height, tx_count, activity_count) FROM explorer.accounts
		  WHERE address = 'bze1xrw00qx3laxg7dkwx0aymxyqtwr9xsuf3k3pa7'`),
		"the recipient of a send never signed: its accounts row comes from its activity")

	base := e.serveAPI(t)

	// The trader: transactions and block-level rows, newest first, the block's
	// own row after its transactions.
	pages := activityPages(t, base, trader, "", 3)
	assert.Len(t, pages, 3)
	assert.Equal(t, []string{
		"24999134 tx dex_order dex", "24999134 tx dex_order dex", "24999134 - dex_order_saved dex",
		"24999004 tx fee_only other", "24999004 tx dex_cancel dex", "24999004 tx dex_cancel dex",
		"24999004 - dex_cancel dex",
	}, positions(pages))
	saved := pages[0][2]
	assert.Nil(t, saved.Fee, "a block-level row has no fee")
	assert.NotEmpty(t, saved.Details)
	failed := pages[1][0]
	assert.False(t, failed.Success)
	require.Len(t, failed.Fee, 1)
	assert.Equal(t, "BZE", *failed.Fee[0].Symbol, "fees carry the denom's display data")
	assert.Equal(t, []dto.Balance{{Denom: "ubze", Amount: "-" + failed.Fee[0].Amount, Symbol: failed.Fee[0].Symbol,
		Exponent: failed.Fee[0].Exponent}}, failed.Deltas, "a failed transaction only moved its fee")

	// One category: the failed transaction only.
	assert.Equal(t, []string{"24999004 tx fee_only other"}, positions(activityPages(t, base, trader, "&category=other", 3)))

	// The relayer's rows lie in two partitions (heights 24 and 25 million).
	assert.Equal(t, []string{"25000894 tx relay other", "24999209 tx relay other"},
		positions(activityPages(t, base, relayer, "", 1)))

	// Counterparties carry the validator's moniker.
	claim := activityPages(t, base, thamarOwner, "&category=staking", 25)
	require.Len(t, claim[0], 1)
	require.NotNil(t, claim[0][0].Counterparty)
	assert.Equal(t, thamar, claim[0][0].Counterparty.Address)
	assert.Equal(t, "Thamar", *claim[0][0].Counterparty.Moniker)
	acc := into[dto.Account](t, fetch(t, base+"/accounts/"+thamarOwner))
	assert.Equal(t, int64(1), acc.ActivityCount)

	assert.Equal(t, [][]dto.Activity{{}}, activityPages(t, base, chainToolsOwner, "", 25), "nothing indexed: an empty list")
	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/accounts/"+trader+"/activity?category=fees").status)
	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/accounts/"+thamar+"/activity").status)
}

// A row the indexer stored as "other" before its message type had an entry
// is reclassified in place by the next `migrate up`.
func TestMigrateReclassifiesIndexedActivity(t *testing.T) {
	e := newSyncEnv(t)
	e.index(t, hSend)
	_, err := e.db.Exec(`UPDATE explorer.account_activity SET kind = 'other', category = 'other' WHERE height = $1 AND tx_index = 0`, hSend)
	require.NoError(t, err)

	migrateUp(t, e.url)
	assert.Equal(t, []string{
		"bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw send sent",
		"bze1xrw00qx3laxg7dkwx0aymxyqtwr9xsuf3k3pa7 send received",
	}, queryStrings(t, e.db, `SELECT concat_ws(' ', address, kind, category) FROM explorer.account_activity
		WHERE height = $1 AND tx_index = 0 ORDER BY address`, hSend), "the signer and the participant categories")
}
