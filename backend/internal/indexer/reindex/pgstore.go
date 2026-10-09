package reindex

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/backfill"
)

// PGStore is the Store over the explorer schema: the backfill's checkpoint
// store plus the failure list.
type PGStore struct {
	*backfill.PGStore
	db backfill.DB
}

// NewPGStore returns a store reading and writing through db.
func NewPGStore(db backfill.DB) *PGStore {
	return &PGStore{PGStore: backfill.NewPGStore(db), db: db}
}

// UnresolvedFailures implements Store.
func (s *PGStore) UnresolvedFailures(ctx context.Context, source string) ([]int64, error) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT height FROM explorer.index_failures
		WHERE resolved_at IS NULL AND ($1 = '' OR source = $1)
		ORDER BY height DESC`, source)
	if err != nil {
		return nil, fmt.Errorf("read index_failures: %w", err)
	}
	heights, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("read index_failures: %w", err)
	}
	return heights, nil
}
