-- The explorer schema. Everything the explorer owns lives in it; the CometBFT
-- psql sink tables in public stay as vendored. pg_trgm backs the search
-- indexes of migration 4.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE SCHEMA IF NOT EXISTS explorer;
