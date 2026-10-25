-- The version of an entry from before its latest substantive update (title or text changed, not only white space,
-- timestamps or tracking pictures): one row per entry, replaced by the next update. fetched_at is when that earlier
-- version was fetched, replaced_at when the update that replaced it was fetched (Unix milliseconds). The row goes with
-- its entry. See docs/product.md "条目的修订".

CREATE TABLE entry_revisions (
    entry_id     INTEGER PRIMARY KEY REFERENCES entries (id) ON DELETE CASCADE,
    title        TEXT NOT NULL,
    summary      TEXT NOT NULL,
    content_html TEXT NOT NULL,
    fetched_at   INTEGER NOT NULL,
    replaced_at  INTEGER NOT NULL
);
