-- The site address the icon lookup last looked at, so a homepage without a usable icon is not fetched again
-- until the site address changes. See docs/product.md "抓取".

ALTER TABLE sources ADD COLUMN icon_checked_site TEXT NOT NULL DEFAULT '';
