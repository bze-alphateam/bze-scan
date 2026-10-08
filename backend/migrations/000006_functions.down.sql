-- Reverts 000006_functions.
DROP FUNCTION IF EXISTS explorer.reclassify_unknown();
DROP FUNCTION IF EXISTS explorer.ensure_partitions(BIGINT, BIGINT);
