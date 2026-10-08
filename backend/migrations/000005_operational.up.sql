-- The operational tables of the indexer, the backfill and the state sync
-- (section 15 of the schema design).

-- 15. Operational tables
CREATE TABLE explorer.indexer_state (
  key         TEXT         PRIMARY KEY,   -- live_floor | last_indexed_height
  value       TEXT         NOT NULL,
  updated_at  TIMESTAMPTZ  NOT NULL
);
-- live_floor is written once, at the live indexer's first write, and never changed:
-- the live side never indexes below it, the backfill never above it.

CREATE TABLE explorer.index_failures (
  height       BIGINT       NOT NULL,
  source       TEXT         NOT NULL,     -- live | backfill | reindex
  attempts     INTEGER      NOT NULL,
  error        TEXT         NOT NULL,
  failed_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
  resolved_at  TIMESTAMPTZ,              -- set by a successful reindex of the height
  PRIMARY KEY (height, source)
);
CREATE INDEX idx_index_failures_open ON explorer.index_failures (failed_at) WHERE resolved_at IS NULL;

CREATE TABLE explorer.backfill_checkpoints (
  job                TEXT         PRIMARY KEY,   -- 'main', or a named reindex run
  ceiling_height     BIGINT       NOT NULL,      -- live_floor - 1 for the main job; the top of a range otherwise
  floor_height       BIGINT       NOT NULL,      -- stop here (inclusive)
  lowest_dispatched  BIGINT       NOT NULL,      -- the checkpoint: dispatch order is monotonic downward
  blocks_done        BIGINT       NOT NULL DEFAULT 0,
  status             TEXT         NOT NULL,      -- running | paused | done | error
  last_error         TEXT,
  started_at         TIMESTAMPTZ  NOT NULL,
  updated_at         TIMESTAMPTZ  NOT NULL
);
-- on restart the dispatcher resumes X + M heights above lowest_dispatched and skips the heights already in explorer.blocks

CREATE TABLE explorer.sync_jobs (
  job              TEXT         PRIMARY KEY,   -- validators | proposals | denoms | holders | ibc_channels | chain_registry | labels | params | prices | chain_state | orders | daily_stats
  last_run_at      TIMESTAMPTZ,
  last_success_at  TIMESTAMPTZ,
  cursor           JSONB,                      -- e.g. the denom being paginated, or the chain ids queued for a registry refetch
  last_error       TEXT
);
