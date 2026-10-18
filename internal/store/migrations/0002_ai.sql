-- AI summaries and translations of entries, one row per entry and task. See docs/product.md "AI 结果".

CREATE TABLE entry_ai (
  entry_id INTEGER NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
  task TEXT NOT NULL CHECK (task IN ('summary', 'translation')),
  status TEXT NOT NULL CHECK (status IN ('pending', 'ok', 'failed')),
  text TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  attempts INTEGER NOT NULL DEFAULT 0,
  requested INTEGER NOT NULL DEFAULT 0,
  fingerprint TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (entry_id, task)
) WITHOUT ROWID;
CREATE INDEX entry_ai_status ON entry_ai (status);
