-- Hide a source from All. hide_from_all = 1 keeps the source's entries out of All and the kind timelines and out of
-- the unread totals of those two; they stay in the source's own view, in search and in Starred, and the source's own
-- unread count does not change. Nothing is marked read or deleted. See docs/product.md "不在全部里出现".

ALTER TABLE sources ADD COLUMN hide_from_all INTEGER NOT NULL DEFAULT 0 CHECK (hide_from_all IN (0, 1));
