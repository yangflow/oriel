-- The fetch proxy of one source. proxy is empty to follow the global proxy (the settings fetch_proxy,
-- fetch_proxy_user and fetch_proxy_password), "direct" to connect without one, or the address of the proxy to use
-- instead. proxy_user and proxy_password are its credentials, secrets that stay in the database like the AI API key.
-- See docs/product.md "抓取".

ALTER TABLE sources ADD COLUMN proxy TEXT NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN proxy_user TEXT NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN proxy_password TEXT NOT NULL DEFAULT '';
