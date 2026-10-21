-- Unread counts per subscription. A partial index over the unread entries lets the count of one source read only its
-- own unread rows instead of every entry. See docs/product.md "条目".

CREATE INDEX entries_source_unread ON entries (source_id) WHERE is_read = 0;
