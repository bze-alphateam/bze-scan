-- Order book outcomes read from the transaction's own events (chain v8.3.0,
-- BFE-346). The engine runs inside the message, so a fill names its
-- transaction, message and position directly, and a cancel is a row of its
-- own instead of a block event.
--
-- A fill of a transaction keeps where its event sits: tx_index, msg_index and
-- msg_event_index (the event's 0-based position among the message's stored
-- events, as explorer.messages.events holds them), which number the trades of
-- a block next to the swaps of the same message. Its seq starts at 1,000,000.
-- taker_order_id is the id the taker's placement got. NULL for block-level
-- fills.
ALTER TABLE explorer.order_fills
  ADD COLUMN tx_index         INTEGER,
  ADD COLUMN msg_index        INTEGER,
  ADD COLUMN msg_event_index  INTEGER,
  ADD COLUMN taker_order_id   TEXT;

-- One row per order that a transaction closed without filling it.
CREATE TABLE explorer.order_closures (
  order_id    TEXT        PRIMARY KEY,
  height      BIGINT      NOT NULL,
  time        TIMESTAMPTZ NOT NULL,
  tx_hash     TEXT        NOT NULL,
  reason      TEXT        NOT NULL,            -- cancelled
  merged_into TEXT
);
