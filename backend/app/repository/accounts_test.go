package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

func TestAccountJoinsTheRowAndTheLabelOfAnyAddress(t *testing.T) {
	db := &fakeDB{rowErr: errDB}
	_, err := repository.NewExplorer(db).Account(context.Background(), "bze1a")
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[0], "FROM (SELECT $1::text AS address) q")
	assert.Contains(t, db.sql[0], "LEFT JOIN explorer.accounts a")
	assert.Contains(t, db.sql[0], "LEFT JOIN explorer.labels l")
	assert.Equal(t, []any{"bze1a"}, db.args[0])
}

func TestDenomsAndMonikersOfNothingQueryNothing(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	r := repository.NewExplorer(db)
	d, err := r.Denoms(context.Background(), nil)
	assert.NoError(t, err)
	assert.Empty(t, d)
	m, err := r.Monikers(context.Background(), []string{})
	assert.NoError(t, err)
	assert.Empty(t, m)
	assert.Empty(t, db.sql)
}

func TestDenomsAndMonikersBindTheLists(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	r := repository.NewExplorer(db)
	_, err := r.Denoms(context.Background(), []string{"ubze"})
	assert.ErrorIs(t, err, errDB)
	_, err = r.Monikers(context.Background(), []string{"bzevaloper1a"})
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[0], "FROM explorer.denoms WHERE denom = ANY($1::text[])")
	assert.Equal(t, []any{[]string{"ubze"}}, db.args[0])
	assert.Contains(t, db.sql[1], "WHERE operator_address = ANY($1::text[])")
	assert.Equal(t, []any{[]string{"bzevaloper1a"}}, db.args[1])
}

func TestSearchEscapesTheLikeWildcards(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	r := repository.NewExplorer(db)
	_, err := r.SearchLabels(context.Background(), `50%_off\`, 5)
	assert.ErrorIs(t, err, errDB)
	_, err = r.SearchValidators(context.Background(), "node", 5)
	assert.ErrorIs(t, err, errDB)

	assert.Contains(t, db.sql[0], "WHERE name ILIKE $1")
	assert.Equal(t, []any{`%50\%\_off\\%`, 5}, db.args[0])
	assert.Contains(t, db.sql[1], "WHERE moniker ILIKE $1")
	assert.Equal(t, []any{"%node%", 5}, db.args[1])
}
