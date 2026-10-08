package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

var errDB = errors.New("db down")

// fakeDB records the statements it is given. Query fails with queryErr;
// every QueryRow scan fails with rowErr.
type fakeDB struct {
	queryErr error
	rowErr   error
	sql      []string
	args     [][]any
}

func (f *fakeDB) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.sql, f.args = append(f.sql, sql), append(f.args, args)
	return nil, f.queryErr
}

func (f *fakeDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.sql, f.args = append(f.sql, sql), append(f.args, args)
	return row{f.rowErr}
}

type row struct{ err error }

func (r row) Scan(...any) error { return r.err }

func TestBlocksCursorAddsTheHeightPredicate(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	r := repository.NewExplorer(db)
	ctx := context.Background()

	_, err := r.Blocks(ctx, nil, 26)
	assert.ErrorIs(t, err, errDB)
	assert.NotContains(t, db.sql[0], "WHERE")
	assert.Equal(t, []any{26}, db.args[0])

	before := int64(25000439)
	_, err = r.Blocks(ctx, &before, 3)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[1], "WHERE height < $1 ORDER BY height DESC LIMIT $2")
	assert.Equal(t, []any{before, 3}, db.args[1])
}

func TestTxsBindsCursorAndStatus(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	r := repository.NewExplorer(db)
	failed := false

	_, err := r.Txs(context.Background(), &repository.TxKey{Height: 24999004, TxIndex: 2}, &failed, 11)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[0], "height <= $1 AND (height, tx_index) < ($1, $2) AND success = $3")
	assert.Contains(t, db.sql[0], "ORDER BY height DESC, tx_index DESC LIMIT $4")
	assert.Equal(t, []any{int64(24999004), int64(2), false, 11}, db.args[0])

	_, err = r.Txs(context.Background(), nil, nil, 5)
	assert.ErrorIs(t, err, errDB)
	assert.NotContains(t, db.sql[1], "$2")
	assert.Equal(t, []any{5}, db.args[1])
}

func TestMissingRowsAreNotFound(t *testing.T) {
	r := repository.NewExplorer(&fakeDB{rowErr: pgx.ErrNoRows})
	ctx := context.Background()

	_, err := r.Block(ctx, 1)
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = r.Tx(ctx, "AB")
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, err = r.ValidatorMoniker(ctx, "bzevaloper1x")
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, _, err = r.TxPosition(ctx, "AB")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestDatabaseErrorsAreWrapped(t *testing.T) {
	r := repository.NewExplorer(&fakeDB{rowErr: errDB, queryErr: errDB})
	ctx := context.Background()

	calls := map[string]func() error{
		"block":     func() error { _, err := r.Block(ctx, 1); return err },
		"tx":        func() error { _, err := r.Tx(ctx, "AB"); return err },
		"tx at":     func() error { _, _, err := r.TxPosition(ctx, "AB"); return err },
		"validator": func() error { _, err := r.ValidatorMoniker(ctx, "v"); return err },
		"block?":    func() error { _, err := r.BlockExists(ctx, 1); return err },
		"tx?":       func() error { _, err := r.TxExists(ctx, "AB"); return err },
		"account?":  func() error { _, err := r.AccountIndexed(ctx, "a"); return err },
	}
	for name, call := range calls {
		err := call()
		require.Error(t, err, name)
		assert.ErrorIs(t, err, errDB, name)
		assert.NotErrorIs(t, err, repository.ErrNotFound, name)
	}
}
