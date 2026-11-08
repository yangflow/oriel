-- Whether a subscription fetches the full text of its articles: 1 by default, so existing subscriptions go on as they
-- did. With it off new entries stay "skipped", and starring one asks for its full text, since a starred article keeps
-- one. See docs/product/fetching.md "抓取" and docs/product/data.md "收藏的本地副本".

ALTER TABLE sources ADD COLUMN full_text INTEGER NOT NULL DEFAULT 1;

CREATE TRIGGER IF NOT EXISTS entries_star_full_text AFTER UPDATE OF starred ON entries
WHEN old.starred = 0 AND new.starred = 1 AND new.extract_status = 'skipped'
    AND (SELECT kind FROM sources WHERE id = new.source_id) = 'article'
BEGIN
    UPDATE entries SET extract_status = 'pending', extract_attempts = 0, next_extract_at = NULL WHERE id = new.id;
END;
