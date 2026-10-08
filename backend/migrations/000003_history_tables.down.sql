-- Reverts 000003_history_tables.

DROP TABLE IF EXISTS explorer.order_fills;
DROP TABLE IF EXISTS explorer.block_events;
DROP TABLE IF EXISTS explorer.account_activity;
DROP TABLE IF EXISTS explorer.transfers;
DROP TABLE IF EXISTS explorer.messages;
DROP TABLE IF EXISTS explorer.transactions;
DROP TABLE IF EXISTS explorer.blocks;
