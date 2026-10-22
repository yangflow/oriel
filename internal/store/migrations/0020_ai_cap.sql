-- Finished chunks of a translation, so a retry repeats only the failed ones, and the automatic AI requests sent
-- per day, for the daily cap. See docs/product.md "AI 结果" and "发送与费用".

CREATE TABLE entry_ai_chunks (
  entry_id INTEGER NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
  task TEXT NOT NULL,
  key TEXT NOT NULL,
  text TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (entry_id, task, key)
) WITHOUT ROWID;

CREATE TABLE ai_usage (
  day TEXT PRIMARY KEY,
  calls INTEGER NOT NULL DEFAULT 0
) WITHOUT ROWID;
