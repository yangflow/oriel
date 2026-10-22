-- WebSub push subscriptions. websub_hub and websub_topic are what the feed declares, empty when it declares none;
-- websub_key is the secret part of the callback path and websub_secret signs the pushes, both random and generated
-- at the first subscription request. websub_state is '' (not subscribed), 'pending' (waiting for the hub to verify)
-- or 'active' (leased until websub_expires_at). websub_requested_at is when the last request went out, websub_base
-- the public address its callback used, websub_next_at when to subscribe or renew next. See docs/product.md "WebSub 推送".

ALTER TABLE sources ADD COLUMN websub_hub TEXT NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN websub_topic TEXT NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN websub_key TEXT NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN websub_secret TEXT NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN websub_state TEXT NOT NULL DEFAULT '' CHECK (websub_state IN ('', 'pending', 'active'));
ALTER TABLE sources ADD COLUMN websub_expires_at INTEGER;
ALTER TABLE sources ADD COLUMN websub_requested_at INTEGER;
ALTER TABLE sources ADD COLUMN websub_base TEXT NOT NULL DEFAULT '';
ALTER TABLE sources ADD COLUMN websub_next_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sources ADD COLUMN websub_failures INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sources ADD COLUMN websub_error TEXT NOT NULL DEFAULT '';
