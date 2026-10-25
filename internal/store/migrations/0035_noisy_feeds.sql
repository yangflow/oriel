-- Noisy subscriptions: a daily cap per subscription and the suggestion to quiet a feed that is read little.
-- sources.daily_cap is how many entries of a local calendar day stay out of the fold (0 is off). sources.noisy_dismissed
-- is the entries per day the user was at when they dismissed the suggestion (0 when they never did); the suggestion
-- comes back when the volume doubles. entries.folded is 1 for an unread, unstarred entry over its source's cap; All and
-- Unread and their counts leave it out, the list endpoint returns it so the timeline can draw one row for the day.
-- The trigger keeps folded and reading state apart: an entry that is read or starred is never folded. The partial index
-- holds the folded rows only. See docs/product.md "每日上限".

ALTER TABLE sources ADD COLUMN daily_cap INTEGER NOT NULL DEFAULT 0 CHECK (daily_cap >= 0);
ALTER TABLE sources ADD COLUMN noisy_dismissed INTEGER NOT NULL DEFAULT 0 CHECK (noisy_dismissed >= 0);
ALTER TABLE entries ADD COLUMN folded INTEGER NOT NULL DEFAULT 0 CHECK (folded IN (0, 1));
CREATE INDEX entries_folded ON entries (source_id) WHERE folded = 1;
CREATE TRIGGER entries_unfold AFTER UPDATE OF is_read, starred ON entries
WHEN new.folded = 1 AND (new.is_read = 1 OR new.starred = 1)
BEGIN
	UPDATE entries SET folded = 0 WHERE id = new.id;
END;
