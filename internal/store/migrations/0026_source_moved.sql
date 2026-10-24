-- A subscription address that the site moved for good. moved_url is the address that 301 or 308 redirects led to and
-- moved_seen counts the fetches in a row that reached it; the address is suggested once moved_seen reaches 3.
-- moved_dismissed is the address the user turned down, which is not counted again. feed_title is the title the feed gave
-- last, so that a title the user did not change follows the feed and one the user changed does not; it is empty for
-- sources from before, which take on the feed's title at the next fetch only when theirs equals it.
-- See docs/product.md "抓取" and "永久重定向".

ALTER TABLE sources ADD COLUMN moved_url TEXT NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN moved_seen INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sources ADD COLUMN moved_dismissed TEXT NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN feed_title TEXT NOT NULL DEFAULT '';
