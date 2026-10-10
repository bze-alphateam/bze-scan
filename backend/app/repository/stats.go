package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// BlockStats are the home tiles read from the indexed blocks and
// transactions, measured back from the latest indexed block (not the wall
// clock, so a lagging indexer shows its last hour and day, never zeros).
type BlockStats struct {
	// LatestHeight and LatestTime are nil before the first block.
	LatestHeight *int64
	LatestTime   *time.Time
	// AvgBlockTimeMs is the average block_time_ms of the blocks of the last
	// hour; nil when none of them has one.
	AvgBlockTimeMs *int64
	// Txs24h counts the transactions of the last 24 hours.
	Txs24h int64
}

// blockStatsSQL anchors both windows on the latest block's time.
const blockStatsSQL = `WITH latest AS (
		SELECT height, time FROM explorer.blocks ORDER BY height DESC LIMIT 1)
	SELECT l.height, l.time,
	       (SELECT round(avg(b.block_time_ms))::bigint FROM explorer.blocks b
	         WHERE b.time > l.time - interval '1 hour' AND b.time <= l.time),
	       (SELECT count(*) FROM explorer.transactions t
	         WHERE t.time > l.time - interval '24 hours' AND t.time <= l.time)
	FROM latest l`

// BlockStats reads the block and transaction tiles.
func (r *Explorer) BlockStats(ctx context.Context) (*BlockStats, error) {
	var s BlockStats
	err := r.db.QueryRow(ctx, blockStatsSQL).Scan(&s.LatestHeight, &s.LatestTime, &s.AvgBlockTimeMs, &s.Txs24h)
	if errors.Is(err, pgx.ErrNoRows) {
		return &s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("block stats: %w", err)
	}
	return &s, nil
}

// ChainState reads every explorer.chain_state row's value by key.
func (r *Explorer) ChainState(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := r.db.Query(ctx, `SELECT key, value FROM explorer.chain_state`)
	if err != nil {
		return nil, fmt.Errorf("chain state: %w", err)
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("chain state: %w", err)
		}
		out[k] = json.RawMessage(v)
	}
	return out, rows.Err()
}

// TopBondedTokens sums the tokens of the n largest bonded validators, as
// decimal text ("0" when none is bonded).
func (r *Explorer) TopBondedTokens(ctx context.Context, n int) (string, error) {
	var sum string
	err := r.db.QueryRow(ctx, `SELECT coalesce(sum(tokens), 0)::text FROM (
			SELECT tokens FROM explorer.validators WHERE status = 'bonded' ORDER BY tokens DESC LIMIT $1) top`,
		n).Scan(&sum)
	if err != nil {
		return "", fmt.Errorf("top bonded tokens: %w", err)
	}
	return sum, nil
}

// ParamChange is a module's latest parameter change.
type ParamChange struct {
	Height     int64
	Time       time.Time
	ProposalID *int64
}

// ParamChanges returns, per module, its latest snapshot that changed a key
// (the first snapshot of a module is no change).
func (r *Explorer) ParamChanges(ctx context.Context) (map[string]ParamChange, error) {
	rows, err := r.db.Query(ctx, `SELECT DISTINCT ON (module) module, height, time, proposal_id
		FROM explorer.param_snapshots WHERE cardinality(changed_keys) > 0
		ORDER BY module, height DESC`)
	if err != nil {
		return nil, fmt.Errorf("param changes: %w", err)
	}
	defer rows.Close()
	out := map[string]ParamChange{}
	for rows.Next() {
		var m string
		var c ParamChange
		if err := rows.Scan(&m, &c.Height, &c.Time, &c.ProposalID); err != nil {
			return nil, fmt.Errorf("param changes: %w", err)
		}
		out[m] = c
	}
	return out, rows.Err()
}
