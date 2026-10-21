-- Indexes for the timeline filtered by kind or by unread. entries.kind copies sources.kind so that an index can
-- order the entries of one kind by created_at and id; the triggers keep the copy in step with the source.
-- See docs/product.md "条目".

ALTER TABLE entries ADD COLUMN kind TEXT NOT NULL DEFAULT 'article';
UPDATE entries SET kind = (SELECT kind FROM sources WHERE sources.id = entries.source_id);

CREATE TRIGGER entries_kind_insert AFTER INSERT ON entries BEGIN
  UPDATE entries SET kind = (SELECT kind FROM sources WHERE id = new.source_id) WHERE id = new.id;
END;
CREATE TRIGGER sources_kind_update AFTER UPDATE OF kind ON sources BEGIN
  UPDATE entries SET kind = new.kind WHERE source_id = new.id;
END;

CREATE INDEX entries_kind_timeline ON entries (kind, created_at, id);
CREATE INDEX entries_unread_timeline ON entries (is_read, created_at, id);
CREATE INDEX entries_kind_unread_timeline ON entries (kind, is_read, created_at, id);
