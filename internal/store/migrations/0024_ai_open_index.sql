-- Index for the AI queue, which asks for open work every 15 s. It covers only the results that can still be picked up
-- (not `ok`), so the cost no longer grows with the number of finished results. It replaces entry_ai_status, which held
-- every row and with the planner's choice of it kept the queue scanning the finished ones. See docs/product.md "AI 结果".

DROP INDEX entry_ai_status;
CREATE INDEX entry_ai_open ON entry_ai (status, requested) WHERE status <> 'ok';
