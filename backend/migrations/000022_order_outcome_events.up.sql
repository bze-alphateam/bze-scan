-- The order book outcome events of chain v8.3.0 (BFE-280): a refund, a
-- skipped cancel or a failure of a queue message. The transaction page and
-- its pending flag look them up by message id at or above the message's
-- height; they stay block_events rows, so this is their only lookup.
CREATE INDEX idx_block_events_order_outcome ON explorer.block_events ((attrs->>'message_id'), height)
  WHERE type IN ('bze.tradebin.OrderMessageRefundedEvent', 'bze.tradebin.OrderCancelSkippedEvent',
                 'bze.tradebin.OrderMessageFailedEvent');
