package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

func TestTransfersJoinTheDenomAndBothLabels(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	_, err := repository.NewExplorer(db).Transfers(context.Background(), 9, repository.BlockTxIndex)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[0], "LEFT JOIN explorer.denoms d ON d.denom = t.denom")
	assert.Contains(t, db.sql[0], "LEFT JOIN explorer.labels ls ON ls.address = t.sender")
	assert.Contains(t, db.sql[0], "LEFT JOIN explorer.labels lr ON lr.address = t.recipient")
	assert.Contains(t, db.sql[0], "ORDER BY t.seq")
	assert.Equal(t, []any{int64(9), int64(-1)}, db.args[0])
}

func TestBlockEventsPageBySeq(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	_, err := repository.NewExplorer(db).BlockEvents(context.Background(), 9, 4, 26)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[0], "WHERE height = $1 AND seq > $2 ORDER BY seq LIMIT $3")
	assert.Equal(t, []any{int64(9), int64(4), 26}, db.args[0])
}

func TestTokensOrderByKindThenSymbolWithATextKeyset(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	kind := "factory"
	_, err := repository.NewExplorer(db).Tokens(context.Background(), &kind,
		&repository.TokenKey{KindRank: 1, SortKey: "honey", Denom: "factory/x/uhoney"}, 26)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[0], "($1::text IS NULL OR d.kind = $1)")
	assert.Contains(t, db.sql[0], "lower(coalesce(d.symbol, d.denom)), d.denom) > ($3, $4, $5)")
	assert.Contains(t, db.sql[0], "ORDER BY CASE d.kind WHEN 'native' THEN 0")
	assert.Equal(t, []any{&kind, 26, 1, "honey", "factory/x/uhoney"}, db.args[0])
}

func TestDenomHistoryPagesNewestFirst(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	r := repository.NewExplorer(db)
	before := &repository.EventKey{Height: 9, TxIndex: -1, Seq: 2}
	_, err := r.TokenEvents(context.Background(), "ubze", before, 21)
	assert.ErrorIs(t, err, errDB)
	_, err = r.DenomTransfers(context.Background(), "ubze", nil, 26)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[0], "AND (e.height, e.tx_index, e.seq) < ($3, $4, $5)")
	assert.Contains(t, db.sql[0], "ORDER BY e.height DESC, e.tx_index DESC, e.seq DESC")
	assert.Equal(t, []any{"ubze", 21, int64(9), int64(-1), int64(2)}, db.args[0])
	assert.NotContains(t, db.sql[1], "$3", "the first page has no keyset")
	assert.Contains(t, db.sql[1], "WHERE x.denom = $1")
	assert.Contains(t, db.sql[1], "ORDER BY x.height DESC, x.tx_index DESC, x.seq DESC")
}

func TestBlockTransfersPageByIndexThenSeq(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	r := repository.NewExplorer(db)
	ctx := context.Background()

	_, err := r.BlockTransfers(ctx, 9, nil, 26)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[0], "WHERE x.height = $1\n")
	assert.NotContains(t, db.sql[0], "$3", "the first page has no keyset")
	assert.Contains(t, db.sql[0], "ORDER BY x.tx_index, x.seq LIMIT $2")
	assert.Contains(t, db.sql[0], "LEFT JOIN explorer.transactions tx ON tx.height = $1 AND tx.tx_index = x.tx_index")
	assert.Equal(t, []any{int64(9), 26}, db.args[0])

	_, err = r.BlockTransfers(ctx, 9, &repository.TransferKey{TxIndex: repository.BlockTxIndex, Seq: 4}, 26)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[1], "AND (x.tx_index, x.seq) > ($3, $4)")
	assert.Equal(t, []any{int64(9), 26, int64(-1), int64(4)}, db.args[1])
}

func TestDenomVolumeWindowEndsAtTheLatestBlock(t *testing.T) {
	db := &fakeDB{rowErr: errDB}
	_, err := repository.NewExplorer(db).DenomVolume(context.Background(), "ubze", 24*time.Hour)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[0], "x.denom = $1 AND x.height >= (")
	assert.Contains(t, db.sql[0], "b.time > (SELECT max(time) FROM explorer.blocks) - $2 * interval '1 second'")
	assert.Equal(t, []any{"ubze", float64(86400)}, db.args[0])
}
