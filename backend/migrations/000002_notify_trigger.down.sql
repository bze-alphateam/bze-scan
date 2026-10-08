-- Drops the trigger and its function and nothing else: the sink's blocks
-- table and its rows stay.
DROP TRIGGER IF EXISTS trg_notify_block ON public.blocks;
DROP FUNCTION IF EXISTS explorer.notify_block();
