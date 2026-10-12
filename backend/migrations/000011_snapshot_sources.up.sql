-- Where the snapshots command's values come from (BFE-121): it reads state
-- of the archive node at a past height, and the API marks those values
-- "historical".
--
-- The source of tally_bonded_tokens: 'sync' (the proposals set, while the
-- proposal votes) or 'archive' (the snapshots command, at the voting end).
ALTER TABLE explorer.proposals ADD COLUMN tally_source TEXT;
UPDATE explorer.proposals SET tally_source = 'sync' WHERE tally_bonded_tokens IS NOT NULL;
-- 'event' rows come from transactions; 'archive' rows from the state at
-- resolved_height - 1, with tx_index -1 and no transaction.
ALTER TABLE explorer.proposal_votes ADD COLUMN source TEXT NOT NULL DEFAULT 'event';
ALTER TABLE explorer.proposal_deposits ADD COLUMN source TEXT NOT NULL DEFAULT 'event';
