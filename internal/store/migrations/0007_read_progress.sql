-- How far the user has read an entry, as a fraction from 0 to 1 of the article's scrollable height; NULL when it was
-- never scrolled. Set through the API from the reader. See docs/product.md "阅读器".

ALTER TABLE entries ADD COLUMN read_progress REAL;
