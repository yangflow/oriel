-- Automatic fetch interval. fetch_interval_auto = 1 makes the fetcher recompute fetch_interval_sec after every fetch
-- from how often the feed publishes; fetch_interval_sec then holds the value in use. suggested_kind is the kind the
-- first fetch found likely, empty when there is none; it is only shown and never changes kind.
-- Sources that used the old default of 900 seconds, the ones never changed, become automatic.
-- See docs/product.md "抓取".

ALTER TABLE sources ADD COLUMN fetch_interval_auto INTEGER NOT NULL DEFAULT 0 CHECK (fetch_interval_auto IN (0, 1));
ALTER TABLE sources ADD COLUMN suggested_kind TEXT NOT NULL DEFAULT '';

UPDATE sources SET fetch_interval_auto = 1 WHERE fetch_interval_sec = 900;
