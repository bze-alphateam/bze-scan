package repository_test

import (
	"context"
	"strings"
	"testing"
	"time"

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
	_, err := repository.NewExplorer(db).ActivityPage(context.Background(), "bze1a", nil, nil, false, 75_000, 26)
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
	_, err := repository.NewExplorer(db).Activity(context.Background(), "bze1a", &cat, nil, false,
		&repository.ActivityKey{Height: 9, TxIndex: 2}, 26)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[0], "($3::bigint IS NULL OR (height, tx_index) < ($3::bigint, $4::integer))")
	assert.Contains(t, db.sql[0], "ORDER BY a.height DESC, a.tx_index DESC")
}

// The incoming filter is added to the text only when asked for, and with a
// denom it binds one more parameter for the denom that has to rise.
func TestActivityIncomingFilterIsOnlyAddedWhenAskedFor(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	ex := repository.NewExplorer(db)
	_, err := ex.Activity(context.Background(), "bze1a", nil, nil, false, nil, 26)
	assert.ErrorIs(t, err, errDB)
	assert.NotContains(t, db.sql[0], "explorer.messages m")

	_, err = ex.Activity(context.Background(), "bze1a", nil, nil, true, nil, 26)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[1], "explorer.messages m")
	assert.Len(t, db.args[1], 6)

	denom := "ubze"
	_, err = ex.Activity(context.Background(), "bze1a", nil, &denom, true, nil, 26)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[2], "jsonb_build_object('d', $8::text)")
	assert.Len(t, db.args[2], 8)
	assert.Equal(t, "ubze", db.args[2][7])
}

// The denom filter is added to the text only when a denom is given: the
// unfiltered statement keeps its six parameters.
func TestActivityDenomFilterIsOnlyAddedWithADenom(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	ex := repository.NewExplorer(db)
	_, err := ex.Activity(context.Background(), "bze1a", nil, nil, false, nil, 26)
	assert.ErrorIs(t, err, errDB)
	assert.NotContains(t, db.sql[0], "deltas @>")

	denom := "factory/bze1x/upage"
	_, err = ex.Activity(context.Background(), "bze1a", nil, &denom, false, nil, 26)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[1], "AND deltas @> $7::jsonb\n")
	assert.Equal(t, []any{"bze1a", (*string)(nil), (*int64)(nil), (*int64)(nil), 26, int64(0),
		`[{"denom":"factory/bze1x/upage"}]`}, db.args[1])
}

// The oldest-first list has its own statement: the keyset is written into the
// text (never behind an IS NULL OR), both orders are ascending, and the desc
// statement is left as it was.
func TestActivityAscKeepsTheKeysetInTheText(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	ex := repository.NewExplorer(db)
	_, err := ex.ActivityAsc(context.Background(), "bze1a", nil, nil, true,
		repository.ActivityStart{After: &repository.ActivityKey{Height: 9, TxIndex: 2}}, 26)
	assert.ErrorIs(t, err, errDB)
	sql := db.sql[0]
	assert.Contains(t, sql, "AND (height, tx_index) > ($4::bigint, $5::integer)")
	assert.Contains(t, sql, "AND height >= $4::bigint\n\t\t\t  AND (height", "a plain bound the partitions are pruned by")
	assert.NotContains(t, sql, "IS NULL OR (height")
	assert.NotContains(t, sql, "$4::bigint IS NULL")
	assert.NotContains(t, sql, "time >=")
	assert.Contains(t, sql, "ORDER BY height ASC, tx_index ASC\n\t\t\tLIMIT $3) a")
	assert.Contains(t, sql, "ORDER BY a.height ASC, a.tx_index ASC")
	assert.NotContains(t, sql, "DESC")
	assert.NotContains(t, sql, "OFFSET")
	assert.Contains(t, sql, "explorer.messages m", "the incoming filter")
	assert.Equal(t, []any{"bze1a", (*string)(nil), 26, int64(9), int64(2)}, db.args[0])
}

func TestActivityAscFromATimeHasNoKeyset(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	ex := repository.NewExplorer(db)
	since := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	denom, cat := "ubze", "received"
	_, err := ex.ActivityAsc(context.Background(), "bze1a", &cat, &denom, true, repository.ActivityStart{Since: &since}, 26)
	assert.ErrorIs(t, err, errDB)
	sql := db.sql[0]
	assert.Contains(t, sql, "AND time >= $4::timestamptz")
	assert.NotContains(t, sql, "(height, tx_index) >")
	assert.Contains(t, sql, "AND deltas @> $5::jsonb", "the filters number on from the start's parameters")
	assert.Contains(t, sql, "jsonb_build_object('d', $6::text)")
	assert.Equal(t, []any{"bze1a", &cat, 26, since, `[{"denom":"ubze"}]`, "ubze"}, db.args[0])
}

func TestActivityAscNeedsExactlyOneStart(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	ex := repository.NewExplorer(db)
	since := time.Now()
	for _, s := range []repository.ActivityStart{{}, {After: &repository.ActivityKey{}, Since: &since}} {
		_, err := ex.ActivityAsc(context.Background(), "bze1a", nil, nil, false, s, 26)
		assert.Error(t, err)
		assert.NotErrorIs(t, err, errDB)
	}
	assert.Empty(t, db.sql, "nothing is queried")
}
