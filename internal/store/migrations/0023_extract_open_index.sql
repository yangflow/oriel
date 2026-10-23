-- Index for the full-text extraction round, which asks for open work every 5 s. It covers only the entries that can
-- still be picked up, so the cost no longer grows with the library. See docs/product.md "正文抽取".

CREATE INDEX entries_extract_open ON entries (extract_status, created_at, id) WHERE extract_status IN ('pending', 'failed');
