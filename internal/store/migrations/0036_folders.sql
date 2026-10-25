-- Folders for subscriptions. A subscription is in at most one folder and folders do not nest: sources.folder_id is
-- NULL for a subscription without one. Deleting a folder keeps its subscriptions, which become unfiled. Names are
-- unique without regard to case (for ASCII letters) and 1 to 60 characters. See docs/product.md "文件夹".

CREATE TABLE folders (
	id         INTEGER PRIMARY KEY,
	name       TEXT NOT NULL COLLATE NOCASE UNIQUE CHECK (length(name) BETWEEN 1 AND 60),
	created_at INTEGER NOT NULL
);

ALTER TABLE sources ADD COLUMN folder_id INTEGER REFERENCES folders (id) ON DELETE SET NULL;
CREATE INDEX sources_folder ON sources (folder_id) WHERE folder_id IS NOT NULL;
