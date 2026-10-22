-- A starred entry keeps a local copy of its pictures, so it survives its original page going away. The copies live in
-- the database, which makes a backup of the database include them. copy_status is none for an entry that is not
-- starred, then pending, ok or failed; next_copy_at is when a failed copy may be tried again (Unix milliseconds),
-- NULL when it is given up. See docs/product.md "收藏的本地副本".

ALTER TABLE entries ADD COLUMN copy_status TEXT NOT NULL DEFAULT 'none';
ALTER TABLE entries ADD COLUMN copy_attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE entries ADD COLUMN next_copy_at INTEGER;

CREATE TABLE entry_image_copies (
    id           INTEGER PRIMARY KEY,
    entry_id     INTEGER NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
    url          TEXT NOT NULL,
    content_type TEXT NOT NULL,
    body         BLOB NOT NULL,
    created_at   INTEGER NOT NULL,
    UNIQUE (entry_id, url)
);
CREATE INDEX entry_image_copies_url ON entry_image_copies (url);
CREATE INDEX entries_copy_due ON entries (copy_status) WHERE copy_status IN ('pending', 'failed');

-- Starring (also at import) queues the copy, and a failed full-text extraction starts over so the text is kept too;
-- unstarring removes the copies.
CREATE TRIGGER entries_star_insert AFTER INSERT ON entries WHEN new.starred = 1 BEGIN
    UPDATE entries SET copy_status = 'pending' WHERE id = new.id;
END;
CREATE TRIGGER entries_star_on AFTER UPDATE OF starred ON entries WHEN new.starred = 1 AND old.starred = 0 BEGIN
    UPDATE entries SET copy_status = 'pending', copy_attempts = 0, next_copy_at = NULL WHERE id = new.id;
    UPDATE entries SET extract_status = 'pending', extract_attempts = 0, next_extract_at = NULL
        WHERE id = new.id AND extract_status = 'failed';
END;
CREATE TRIGGER entries_star_off AFTER UPDATE OF starred ON entries WHEN new.starred = 0 AND old.starred = 1 BEGIN
    DELETE FROM entry_image_copies WHERE entry_id = new.id;
    UPDATE entries SET copy_status = 'none', copy_attempts = 0, next_copy_at = NULL WHERE id = new.id;
END;

-- Entries starred before this migration get their copy too.
UPDATE entries SET copy_status = 'pending' WHERE starred = 1;
UPDATE entries SET extract_status = 'pending', extract_attempts = 0, next_extract_at = NULL
    WHERE starred = 1 AND extract_status = 'failed';
