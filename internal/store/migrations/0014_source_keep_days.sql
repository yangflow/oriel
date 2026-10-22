-- Keep period per source. keep_days overrides the global retention_days for this source's entries: 0 follows the
-- global setting (the default), a positive number is a day count, -1 keeps them forever. Starred entries are always
-- kept. See docs/product.md "保留期限".

ALTER TABLE sources ADD COLUMN keep_days INTEGER NOT NULL DEFAULT 0 CHECK (keep_days >= -1);
