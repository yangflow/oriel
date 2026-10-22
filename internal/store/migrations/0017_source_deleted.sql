-- A deleted subscription stays here for the undo window, hidden everywhere; the cleanup removes it for good after
-- that, with its entries. deleted_at is when it was deleted, in Unix milliseconds, NULL for a live subscription.
-- See docs/product.md "删除订阅与撤销".

ALTER TABLE sources ADD COLUMN deleted_at INTEGER;
CREATE INDEX sources_deleted ON sources (deleted_at) WHERE deleted_at IS NOT NULL;
