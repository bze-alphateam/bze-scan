package repository_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// Regression: the joins ran before OFFSET, so a deep page of a busy account
// looked up the transaction, label and validator of every skipped row (0.4 to
// 0.8 s at 75k rows on mainnet). The page is cut from account_activity alone
// now, and only its rows are joined.
func TestActivityPageIsCutBeforeTheJoins(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	_, err := repository.NewExplorer(db).ActivityPage(context.Background(), "bze1a", nil, 75_000, 26)
	assert.ErrorIs(t, err, errDB)
	sql := db.sql[0]
	page := strings.Index(sql, "LIMIT $5 OFFSET $6) a")
	require.Positive(t, page, "OFFSET applies inside the account_activity subquery")
	assert.Less(t, strings.Index(sql, "FROM (SELECT * FROM explorer.account_activity"), page)
	for _, join := range []string{"JOIN explorer.transactions", "JOIN explorer.labels", "JOIN explorer.validators"} {
		assert.Greater(t, strings.Index(sql, join), page, join+" comes after the page is cut")
	}
	assert.Equal(t, []any{"bze1a", (*string)(nil), (*int64)(nil), (*int64)(nil), 26, int64(75_000)}, db.args[0])
}

func TestActivityCursorPageKeepsTheKeyset(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	cat := "dex"
	_, err := repository.NewExplorer(db).Activity(context.Background(), "bze1a", &cat,
		&repository.ActivityKey{Height: 9, TxIndex: 2}, 26)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[0], "($3::bigint IS NULL OR (height, tx_index) < ($3::bigint, $4::integer))")
	assert.Contains(t, db.sql[0], "ORDER BY a.height DESC, a.tx_index DESC")
}
