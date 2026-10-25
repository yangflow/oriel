-- The tracking parameters dropped from a link when entries are compared now include mkt_tok and a feed-reader
-- "ref" (docs/product.md "链接里的跟踪参数"). Work the stored fingerprints out again so older entries match too.
UPDATE entries SET link_key = group_key(url, guid);
