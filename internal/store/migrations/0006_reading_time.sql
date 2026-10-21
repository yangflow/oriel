-- The time an entry takes, in seconds, worked out by the server; NULL when there is nothing to estimate or it is not
-- computed yet. Existing entries are filled in the background (Store.BackfillReadingTime).
-- See docs/product.md "阅读时间".

ALTER TABLE entries ADD COLUMN reading_sec INTEGER;

CREATE INDEX entries_reading_timeline ON entries (reading_sec, created_at, id) WHERE reading_sec IS NOT NULL;
