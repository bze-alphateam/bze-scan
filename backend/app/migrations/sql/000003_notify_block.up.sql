-- The one object the explorer attaches to the CometBFT psql sink: a
-- notification at the commit of every block the node writes, carrying the
-- height. It writes nothing; the live indexer LISTENs on explorer_block.
CREATE OR REPLACE FUNCTION explorer.notify_block() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_notify('explorer_block', NEW.height::text);
  RETURN NULL;
END $$;

CREATE TRIGGER trg_notify_block
  AFTER INSERT ON public.blocks
  FOR EACH ROW EXECUTE FUNCTION explorer.notify_block();
