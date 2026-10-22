-- One article shown once when several sources carry it. link_key is the fingerprint of an entry's link (see groupKey in
-- identity.go and docs/product.md "同一篇文章只显示一次"); entries that share a non-empty one are the same article. Nothing is
-- merged in storage: the timeline groups them when it reads. The index serves finding the other copies of an entry.

ALTER TABLE entries ADD COLUMN link_key TEXT NOT NULL DEFAULT '';

UPDATE entries SET link_key = group_key(url, guid);

CREATE INDEX entries_link_key ON entries (link_key, created_at, id) WHERE link_key <> '';
