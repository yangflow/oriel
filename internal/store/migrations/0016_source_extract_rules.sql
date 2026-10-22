-- Extraction rules per source, for feeds whose pages the automatic full-text extraction gets wrong.
-- extract_selector is a CSS selector for the element that holds the article, extract_referer the Referer sent with
-- the request for the entry page, and extract_rewrites the rules that rewrite the entry link before it is fetched,
-- one "from => to" per line. Empty means automatic extraction as before. See docs/product.md "正文抽取".

ALTER TABLE sources ADD COLUMN extract_selector TEXT NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN extract_referer TEXT NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN extract_rewrites TEXT NOT NULL DEFAULT '';
