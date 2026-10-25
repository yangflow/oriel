-- Translate the titles of a foreign-language subscription. sources.translate_titles is the switch (default off).
-- The translation lives on the entry: translated_title is the text ('' when there is none), title_tr_status is
-- '' (not queued, or not needed), 'pending', 'ok' or 'failed', title_tr_attempts counts the tries on this one title
-- and title_tr_at is the time of the last try (Unix milliseconds). They are derived content and do not change
-- updated_at. Titles are queued for new entries only; nothing is backfilled. The first partial index holds the rows the
-- AI worker still has to look at, the second the entries that have a translation, which search reads. See docs/product.md "标题翻译".
-- Numbered 0034 because 0033 belongs to the open entry-revisions change (#388); the runner needs contiguous
-- numbers, so this file only applies on top of it.

ALTER TABLE sources ADD COLUMN translate_titles INTEGER NOT NULL DEFAULT 0 CHECK (translate_titles IN (0, 1));
ALTER TABLE entries ADD COLUMN translated_title TEXT NOT NULL DEFAULT '';
ALTER TABLE entries ADD COLUMN title_tr_status TEXT NOT NULL DEFAULT '' CHECK (title_tr_status IN ('', 'pending', 'ok', 'failed'));
ALTER TABLE entries ADD COLUMN title_tr_attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE entries ADD COLUMN title_tr_at INTEGER NOT NULL DEFAULT 0;
CREATE INDEX entries_title_tr ON entries (created_at) WHERE title_tr_status IN ('pending', 'failed');
CREATE INDEX entries_translated_title ON entries (translated_title) WHERE translated_title <> '';
