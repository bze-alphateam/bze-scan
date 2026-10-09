package writer_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// signed returns a height whose transactions are signed by signers, one
// transaction per entry.
func signed(h int64, signers ...[]string) *transform.Entities {
	ents := block(h)
	for i, s := range signers {
		ents.Transactions = append(ents.Transactions, transform.Transaction{
			Height: h, TxIndex: i, Hash: fmt.Sprintf("H%d-%d", h, i), Time: time.Unix(h, 0), Signers: s,
		})
	}
	return ents
}

func accountsStatements(tx *mockTx) []statement {
	var out []statement
	for _, st := range tx.stmts {
		if strings.Contains(st.sql, "INSERT INTO explorer.accounts") {
			out = append(out, st)
		}
	}
	return out
}

func TestAccountsCountOnlyTheInsertedTransactions(t *testing.T) {
	db := newMockDB(49)
	// Of the two transactions of height 100, only the second is new.
	db.returning = map[string][][]any{"INSERT INTO explorer.transactions": {{100, 1}}}
	ents := signed(100, []string{"bze1a"}, []string{"bze1a", "bze1b", "bze1a"})

	require.NoError(t, writer.NewLiveWriter(db).WriteBlock(context.Background(), ents))
	stmts := accountsStatements(db.txs[0])
	require.Len(t, stmts, 1)
	assert.Equal(t, []map[string]any{
		{"address": "bze1a", "first_height": float64(100), "first_time": "1970-01-01T00:01:40Z",
			"last_height": float64(100), "tx_count": float64(1)},
		{"address": "bze1b", "first_height": float64(100), "first_time": "1970-01-01T00:01:40Z",
			"last_height": float64(100), "tx_count": float64(1)},
	}, withUTCTimes(payload(t, stmts[0])), "a signer listed twice counts once")
}

func TestAccountsOfAHeightWrittenAgainMoveNoCounter(t *testing.T) {
	db := newMockDB(49) // the insert returns no key: every transaction exists
	require.NoError(t, writer.NewLiveWriter(db).WriteBlock(context.Background(), signed(100, []string{"bze1a"})))
	rows := payload(t, accountsStatements(db.txs[0])[0])
	require.Len(t, rows, 1)
	assert.InDelta(t, 0, rows[0]["tx_count"], 0)
}

func TestAccountsUpsertSQL(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewLiveWriter(db).WriteBlock(context.Background(), signed(100, []string{"bze1a"})))
	sql := accountsStatements(db.txs[0])[0].sql
	for _, part := range []string{
		"ORDER BY r.address",
		"ON CONFLICT (address) DO UPDATE",
		"first_seen_height = LEAST(a.first_seen_height, EXCLUDED.first_seen_height)",
		"last_seen_height = GREATEST(a.last_seen_height, EXCLUDED.last_seen_height)",
		"tx_count = a.tx_count + EXCLUDED.tx_count",
	} {
		assert.Contains(t, sql, part)
	}
}

func TestBlocksWithoutSignersWriteNoAccounts(t *testing.T) {
	db := newMockDB(49)
	require.NoError(t, writer.NewLiveWriter(db).WriteBlock(context.Background(), signed(100, nil)))
	assert.Empty(t, accountsStatements(db.txs[0]))
}

// Two flushes touching the same addresses in different orders apply the
// same deltas, merged per address and in address order.
func TestAccountDeltasAreMergedAndSorted(t *testing.T) {
	flush := func(batch ...*transform.Entities) []map[string]any {
		db := newMockDB(49)
		db.returning = map[string][][]any{"INSERT INTO explorer.transactions": {{10, 0}, {10, 1}, {12, 0}}}
		require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(), batch, writer.ModeInsert))
		stmts := accountsStatements(db.txs[0])
		require.Len(t, stmts, 1)
		return withUTCTimes(payload(t, stmts[0]))
	}
	a := flush(signed(10, []string{"bze1c", "bze1a"}, []string{"bze1b"}), signed(12, []string{"bze1a"}))
	b := flush(signed(12, []string{"bze1a"}), signed(10, []string{"bze1b"}, []string{"bze1a", "bze1c"}))

	assert.Equal(t, a, b)
	assert.Equal(t, []map[string]any{
		{"address": "bze1a", "first_height": float64(10), "first_time": "1970-01-01T00:00:10Z",
			"last_height": float64(12), "tx_count": float64(2)},
		{"address": "bze1b", "first_height": float64(10), "first_time": "1970-01-01T00:00:10Z",
			"last_height": float64(10), "tx_count": float64(1)},
		{"address": "bze1c", "first_height": float64(10), "first_time": "1970-01-01T00:00:10Z",
			"last_height": float64(10), "tx_count": float64(1)},
	}, a)
}

func TestAccountsAreChunked(t *testing.T) {
	signers := make([][]string, writer.ChunkRows+1)
	for i := range signers {
		signers[i] = []string{fmt.Sprintf("bze1%05d", i)}
	}
	db := newMockDB(49)
	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(),
		[]*transform.Entities{signed(100, signers...)}, writer.ModeInsert))
	stmts := accountsStatements(db.txs[0])
	require.Len(t, stmts, 2)
	assert.Len(t, payload(t, stmts[0]), writer.ChunkRows)
	assert.Equal(t, "bze101000", payload(t, stmts[1])[0]["address"], "the last address in the last chunk")
}

var deadlock = &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}

func TestALiveWriteIsRetriedOnceOnADeadlock(t *testing.T) {
	db := newMockDB(49)
	db.execErr, db.execErrOn, db.execErrOnce = deadlock, "explorer.accounts", true

	require.NoError(t, writer.NewLiveWriter(db).WriteBlock(context.Background(), signed(100, []string{"bze1a"})))
	require.Len(t, db.txs, 2)
	assert.True(t, db.txs[0].rolledBack)
	assert.True(t, db.txs[1].committed)
}

func TestAFlushIsRetriedOnceOnADeadlock(t *testing.T) {
	db := newMockDB(49)
	db.execErr, db.execErrOn, db.execErrOnce = deadlock, "explorer.transactions", true

	require.NoError(t, writer.NewBatchWriter(db).Write(context.Background(),
		[]*transform.Entities{signed(100, []string{"bze1a"})}, writer.ModeInsert))
	require.Len(t, db.txs, 2)
	assert.True(t, db.txs[0].rolledBack)
	assert.True(t, db.txs[1].committed)
}

func TestASecondDeadlockFailsTheWrite(t *testing.T) {
	db := newMockDB(49)
	db.execErr, db.execErrOn = deadlock, "explorer.accounts"

	err := writer.NewBatchWriter(db).Write(context.Background(),
		[]*transform.Entities{signed(100, []string{"bze1a"})}, writer.ModeInsert)
	require.ErrorIs(t, err, deadlock)
	assert.Len(t, db.txs, 2, "one retry, no more")
}

func TestOtherErrorsAreNotRetried(t *testing.T) {
	db := newMockDB(49)
	db.execErr, db.execErrOn = errors.New("boom"), "explorer.accounts"

	require.Error(t, writer.NewLiveWriter(db).WriteBlock(context.Background(), signed(100, []string{"bze1a"})))
	assert.Len(t, db.txs, 1)
}

func withUTCTimes(rows []map[string]any) []map[string]any {
	for _, r := range rows {
		ts, err := time.Parse(time.RFC3339Nano, r["first_time"].(string))
		if err == nil {
			r["first_time"] = ts.UTC().Format(time.RFC3339)
		}
	}
	return rows
}
