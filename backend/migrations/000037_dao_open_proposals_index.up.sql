-- The open-proposals feed of GET /api/v1/dao-proposals?status=voting (BFE-368):
-- voting proposals in voting_end order, the chain's cleaned-up ones left out.
CREATE INDEX idx_dao_proposals_open ON explorer.dao_proposals (voting_end, id)
  WHERE status = 'voting' AND cleaned_height IS NULL;
