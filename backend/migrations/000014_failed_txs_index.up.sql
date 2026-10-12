-- GET /txs?status=failed: without it, a page walks the primary key down from
-- the top until it finds enough failed transactions, which on mainnet (few
-- failures) is a scan of a large part of the table. Failed transactions are a
-- small share of the rows, so the index stays small.
CREATE INDEX idx_transactions_failed ON explorer.transactions (height DESC, tx_index DESC) WHERE NOT success;
