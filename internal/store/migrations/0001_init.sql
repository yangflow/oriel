-- Times are Unix milliseconds. Booleans are 0 or 1. See docs/product.md "数据模型".

CREATE TABLE sources (
  id INTEGER PRIMARY KEY,
  url TEXT NOT NULL DEFAULT '',
  rsshub_path TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  site_url TEXT NOT NULL DEFAULT '',
  icon_url TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL CHECK (kind IN ('article', 'video', 'social', 'image')),
  etag TEXT NOT NULL DEFAULT '',
  last_modified TEXT NOT NULL DEFAULT '',
  last_fetched_at INTEGER,
  last_success_at INTEGER,
  last_error TEXT NOT NULL DEFAULT '',
  consecutive_failures INTEGER NOT NULL DEFAULT 0,
  fetch_interval_sec INTEGER NOT NULL DEFAULT 900 CHECK (fetch_interval_sec > 0),
  next_fetch_at INTEGER NOT NULL DEFAULT 0,
  prompt_summary TEXT NOT NULL DEFAULT '',
  prompt_translate TEXT NOT NULL DEFAULT '',
  auto_summary INTEGER NOT NULL DEFAULT 0,
  auto_translate INTEGER NOT NULL DEFAULT 0,
  mute_keywords TEXT NOT NULL DEFAULT '',
  paused INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  CHECK ((url = '') <> (rsshub_path = ''))
);
CREATE UNIQUE INDEX sources_feed ON sources (url, rsshub_path);
CREATE INDEX sources_due ON sources (paused, next_fetch_at);

CREATE TABLE entries (
  id INTEGER PRIMARY KEY,
  source_id INTEGER NOT NULL REFERENCES sources (id) ON DELETE CASCADE,
  guid TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL DEFAULT '',
  author TEXT NOT NULL DEFAULT '',
  published_at INTEGER,
  rss_summary TEXT NOT NULL DEFAULT '',
  rss_content_html TEXT NOT NULL DEFAULT '',
  extracted_html TEXT NOT NULL DEFAULT '',
  extract_status TEXT NOT NULL DEFAULT 'pending' CHECK (extract_status IN ('pending', 'ok', 'failed', 'skipped')),
  extract_attempts INTEGER NOT NULL DEFAULT 0,
  thumbnail_url TEXT NOT NULL DEFAULT '',
  duration_sec INTEGER,
  platform TEXT NOT NULL DEFAULT '',
  images TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(images) AND json_type(images) = 'array'),
  is_read INTEGER NOT NULL DEFAULT 0,
  read_at INTEGER,
  starred INTEGER NOT NULL DEFAULT 0,
  search_text TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE (source_id, guid)
);
CREATE INDEX entries_timeline ON entries (created_at, id);
CREATE INDEX entries_source_timeline ON entries (source_id, created_at, id);

CREATE VIRTUAL TABLE entries_fts USING fts5 (
  title, rss_summary, search_text,
  content = 'entries', content_rowid = 'id', tokenize = 'trigram'
);
CREATE TRIGGER entries_fts_insert AFTER INSERT ON entries BEGIN
  INSERT INTO entries_fts (rowid, title, rss_summary, search_text)
  VALUES (new.id, new.title, new.rss_summary, new.search_text);
END;
CREATE TRIGGER entries_fts_delete AFTER DELETE ON entries BEGIN
  INSERT INTO entries_fts (entries_fts, rowid, title, rss_summary, search_text)
  VALUES ('delete', old.id, old.title, old.rss_summary, old.search_text);
END;
CREATE TRIGGER entries_fts_update AFTER UPDATE OF title, rss_summary, search_text ON entries BEGIN
  INSERT INTO entries_fts (entries_fts, rowid, title, rss_summary, search_text)
  VALUES ('delete', old.id, old.title, old.rss_summary, old.search_text);
  INSERT INTO entries_fts (rowid, title, rss_summary, search_text)
  VALUES (new.id, new.title, new.rss_summary, new.search_text);
END;

CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE sessions (
  id INTEGER PRIMARY KEY,
  token_hash BLOB NOT NULL UNIQUE,
  user_agent TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL
);
