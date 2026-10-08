-- Removes the (by now empty) explorer schema. pg_trgm stays installed: the
-- instance is shared and other users may rely on it.
DROP SCHEMA IF EXISTS explorer;
