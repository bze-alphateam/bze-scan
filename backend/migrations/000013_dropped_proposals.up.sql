-- A proposal dropped for lack of deposit (inactive_proposal, proposal_dropped)
-- was stored as rejected. It never reached a vote, so it has no voting start
-- time; a proposal rejected by vote always has one.
UPDATE explorer.proposals SET status = 'dropped'
 WHERE status = 'rejected' AND voting_start_time IS NULL;
