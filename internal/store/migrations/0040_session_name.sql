-- The name a reader gave a signed-in device; empty means the name comes from its User-Agent. See docs/product/data.md.

ALTER TABLE sessions ADD COLUMN name TEXT NOT NULL DEFAULT '';
