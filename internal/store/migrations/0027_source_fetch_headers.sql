-- The request headers of one source. fetch_user_agent and fetch_referer are empty to send the default User-Agent
-- and no Referer, otherwise the values sent with the requests to the subscription's own host. Never a cookie; the
-- values are not secrets. See docs/product.md "抓取".

ALTER TABLE sources ADD COLUMN fetch_user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN fetch_referer TEXT NOT NULL DEFAULT '';
