-- A rule stars or marks as read the entries that arrive after it exists, once, when SaveFeed writes them. source_id
-- NULL is every source, kind '' is any kind; keywords hold one per line, author is a part of a name. At least one
-- condition and one action are required, which the API checks. The global muted keywords are the setting
-- mute_keywords, not a rule. See docs/product.md "规则".

CREATE TABLE rules (
    id         INTEGER PRIMARY KEY,
    source_id  INTEGER REFERENCES sources (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL DEFAULT '',
    keywords   TEXT NOT NULL DEFAULT '',
    author     TEXT NOT NULL DEFAULT '',
    star       INTEGER NOT NULL DEFAULT 0,
    mark_read  INTEGER NOT NULL DEFAULT 0,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);

CREATE INDEX rules_source ON rules (source_id);
