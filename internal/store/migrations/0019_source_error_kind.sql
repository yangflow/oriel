-- A stable code for why the last fetch failed, for the interface to explain it in the user's language. '' is an
-- unclassified failure or no failure; last_error keeps the raw text. See docs/product.md "抓取".

ALTER TABLE sources ADD COLUMN last_error_kind TEXT NOT NULL DEFAULT '';
