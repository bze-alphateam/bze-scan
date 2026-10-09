package repository_test

import (
	"context"
	"testing"

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
