-- The result of each fetch, a few per source, for the activity view. The store keeps the newest attempts of each source
-- and trims the rest when it adds one, so the table stays bounded. See docs/product.md "抓取记录".

CREATE TABLE fetch_log (
    id          INTEGER PRIMARY KEY,
    source_id   INTEGER NOT NULL REFERENCES sources (id) ON DELETE CASCADE,
    at          INTEGER NOT NULL,                -- when the fetch ended, in milliseconds
    duration_ms INTEGER NOT NULL,
    ok          INTEGER NOT NULL,
    status      INTEGER NOT NULL DEFAULT 0,      -- the last HTTP status received, 0 when there was none
    new_entries INTEGER NOT NULL DEFAULT 0,
    error       TEXT NOT NULL DEFAULT ''
);

CREATE INDEX fetch_log_source ON fetch_log (source_id, id DESC);
