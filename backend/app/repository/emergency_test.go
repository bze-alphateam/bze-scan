package repository_test

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

func TestEmergencyActionsReadBothIndexesAndBindTheKeyset(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	_, err := repository.NewExplorer(db).EmergencyActions(context.Background(), &repository.EmergencyKey{Height: 20, Order: 7}, 51)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[0], "m.type_url = ANY($1) AND t.success")
	assert.Contains(t, db.sql[0], "e.type = ANY($5)")
	assert.Contains(t, db.sql[0], "UNION ALL")
	assert.Contains(t, db.sql[0], "p.status = 'passed'")
	assert.Equal(t, int64(20), db.args[0][1])
	assert.Equal(t, int64(7), db.args[0][2])
	assert.Equal(t, 51, db.args[0][3])
}

func TestEmergencyActionsWithoutACursorBindSentinelsNotNulls(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	_, err := repository.NewExplorer(db).EmergencyActions(context.Background(), nil, 10)
	assert.ErrorIs(t, err, errDB)
	assert.Equal(t, int64(math.MaxInt64), db.args[0][1])
	assert.Equal(t, int64(math.MaxInt64), db.args[0][2])
	assert.NotContains(t, db.sql[0], "IS NULL", "a plain height bound keeps the partitions prunable")
}

func TestEmergencyLookupsOfNothingQueryNothing(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	r := repository.NewExplorer(db)
	d, err := r.EmergencyDenoms(context.Background(), nil)
	assert.NoError(t, err)
	assert.Empty(t, d)
	c, err := r.EmergencyChannels(context.Background(), []string{})
	assert.NoError(t, err)
	assert.Empty(t, c)
	assert.Empty(t, db.sql)
}
