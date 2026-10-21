-- The language of each entry as a BCP 47 code, detected from its title and text when it is saved. NULL means
-- unknown: too short, not sure, or saved before this column. See docs/product.md "条目".

ALTER TABLE entries ADD COLUMN language TEXT;
