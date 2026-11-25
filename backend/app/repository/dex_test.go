package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

// An order read carries what a merge adds (chain v8.3.0): the order it
// replaced and the amount it added from its own row, and, for an order with
// the status merged, the order that replaced it from its closure row. Every
// route that reads orders shares the column list.
func TestOrderReadsTheMergeColumns(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	r := repository.NewExplorer(db)
	_, err := r.Order(context.Background(), "000000000000000000000030")
	assert.ErrorIs(t, err, errDB)
	_, err = r.AccountOrders(context.Background(), "bze1a", nil, nil, 10)
	assert.ErrorIs(t, err, errDB)
	for _, sql := range db.sql {
		assert.Contains(t, sql, "o.merged_from, o.added_amount::text")
		assert.Contains(t, sql, "CASE WHEN o.status = 'merged' THEN (SELECT k.merged_into FROM explorer.order_closures k WHERE k.order_id = o.order_id) END")
	}
}

// The first read of a transaction's outcome is the orders it placed, by the
// hash, so a merged order (whose row carries the transaction) is among them.
func TestTxDexReadsThePlacedOrdersFirst(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	_, err := repository.NewExplorer(db).TxDex(context.Background(), 10, 0, "AB")
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[0], "WHERE o.created_tx_hash = $1")
	assert.Contains(t, db.sql[0], "o.merged_from")
	assert.Equal(t, []any{"AB"}, db.args[0])
}
