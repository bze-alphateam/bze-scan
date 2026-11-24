-- Reverts 000030_order_book_tx_events.
DROP TABLE IF EXISTS explorer.order_closures;
ALTER TABLE explorer.order_fills
  DROP COLUMN IF EXISTS taker_order_id,
  DROP COLUMN IF EXISTS msg_event_index,
  DROP COLUMN IF EXISTS msg_index,
  DROP COLUMN IF EXISTS tx_index;
