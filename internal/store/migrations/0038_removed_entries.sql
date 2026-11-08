-- Entries removed by "delete all read entries". A feed that still carries such an entry must not bring it back as
-- unread, so its GUID is remembered per source until the feed has stopped carrying it for a while: seen_at is the last
-- time, in Unix milliseconds, it was removed or seen in a fetch. See docs/product/data.md "删除所有已读条目".

CREATE TABLE IF NOT EXISTS removed_entries (
	source_id INTEGER NOT NULL REFERENCES sources (id) ON DELETE CASCADE,
	guid      TEXT NOT NULL,
	seen_at   INTEGER NOT NULL,
	PRIMARY KEY (source_id, guid)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS removed_entries_seen ON removed_entries (seen_at);
