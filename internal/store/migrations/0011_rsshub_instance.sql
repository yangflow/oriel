-- The RSSHub instance that served the last successful fetch of a source, as the base address from the settings; empty
-- for a source that is not an RSSHub route or has not been fetched yet. See docs/product.md "抓取".

ALTER TABLE sources ADD COLUMN rsshub_instance TEXT NOT NULL DEFAULT '';
