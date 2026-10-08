-- Reverts 000004_state_tables.

DROP TABLE IF EXISTS explorer.daily_stats;
DROP TABLE IF EXISTS explorer.chain_state;
DROP TABLE IF EXISTS explorer.param_snapshots;
DROP TABLE IF EXISTS explorer.labels;
DROP TABLE IF EXISTS explorer.ibc_transfers;
DROP TABLE IF EXISTS explorer.registry_assets;
DROP TABLE IF EXISTS explorer.chains;
DROP TABLE IF EXISTS explorer.ibc_channels;
DROP TABLE IF EXISTS explorer.token_events;
DROP TABLE IF EXISTS explorer.token_holders;
DROP TABLE IF EXISTS explorer.denoms;
DROP TABLE IF EXISTS explorer.proposal_deposits;
DROP TABLE IF EXISTS explorer.proposal_votes;
DROP TABLE IF EXISTS explorer.proposals;
DROP TABLE IF EXISTS explorer.validator_events;
DROP TABLE IF EXISTS explorer.validators;
DROP TABLE IF EXISTS explorer.order_messages;
DROP TABLE IF EXISTS explorer.orders;
DROP TABLE IF EXISTS explorer.block_event_kinds;
DROP TABLE IF EXISTS explorer.message_kinds;
DROP TABLE IF EXISTS explorer.accounts;
