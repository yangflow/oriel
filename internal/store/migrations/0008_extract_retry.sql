-- When a failed full-text extraction may be tried again, in Unix milliseconds; NULL when it is not tried again
-- automatically (it succeeded, it failed for good, or its attempts ran out). Entries that already failed with attempts
-- left are due at once. See docs/product.md "正文抽取".

ALTER TABLE entries ADD COLUMN next_extract_at INTEGER;

UPDATE entries SET next_extract_at = 0 WHERE extract_status = 'failed' AND extract_attempts < 3;
