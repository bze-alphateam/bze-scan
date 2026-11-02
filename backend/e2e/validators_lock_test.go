//go:build e2e

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/validators"
)

// A validators save running while the live writer marks the signers never
// deadlocks with it: the writer locks the rows in operator order, and so
// does the save, whatever order the node lists the validators in. Here the
// writer holds the lowest row when the save starts, with the node listing
// the highest first, and then asks for the highest row.
func TestValidatorsSaveAndLastSignedNeverDeadlock(t *testing.T) {
	url := freshDatabase(t, true)
	migrateUp(t, url)
	db := connect(t, url)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := validators.NewPGStore(pool)

	low, high := lockValidator("bzevaloper1aaa", "bze1aaa"), lockValidator("bzevaloper1zzz", "bze1zzz")
	require.NoError(t, store.Save(ctx, validators.Snapshot{Validators: []validators.Validator{low, high}}))

	writer, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback() }()
	lock := `SELECT 1 FROM explorer.validators WHERE operator_address = $1 FOR UPDATE`
	_, err = writer.Exec(lock, low.OperatorAddress)
	require.NoError(t, err)

	saved := make(chan error, 1)
	go func() {
		saved <- store.Save(ctx, validators.Snapshot{Validators: []validators.Validator{high, low}})
	}()
	waitFor(t, 10*time.Second, func() bool {
		var n int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&n))
		return n == 1
	}, "the save to wait on the writer's row")

	// Before the fix the save held the highest row by now, and Postgres
	// failed one of the two with "deadlock detected" after deadlock_timeout.
	_, err = writer.Exec(lock, high.OperatorAddress)
	require.NoError(t, err, "the writer gets the highest row")
	require.NoError(t, writer.Commit())

	select {
	case err := <-saved:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the save never finished")
	}
}

func lockValidator(operator, account string) validators.Validator {
	return validators.Validator{
		OperatorAddress: operator, AccountAddress: account, Moniker: operator, Status: "bonded",
		Tokens: "1", DelegatorShares: "1",
		CommissionRate: "0.1", CommissionMaxRate: "0.2", CommissionMaxChangeRate: "0.01",
	}
}
