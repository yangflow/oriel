-- A fifth kind, forum (社区). sources.kind has CHECK (kind IN ('article', 'video', 'social', 'image')) from
-- 0001_init.sql, and SQLite cannot alter a constraint. Rebuilding the table is not safe here: the connection has
-- foreign keys on, and dropping sources would cascade into entries, rule_sources and everything else that points at it.
-- A CHECK that only gets looser leaves every stored row valid, which is the case SQLite documents for editing the
-- stored table definition in place, so the text of the constraint is rewritten and the schema is read again.
-- entries.kind (0005_timeline_indexes.sql) has no CHECK, and its triggers copy sources.kind whatever the value, so a
-- forum source needs nothing more there. See docs/product.md "数据模型".

PRAGMA writable_schema = ON;
UPDATE sqlite_schema
SET sql = replace(sql, 'CHECK (kind IN (''article'', ''video'', ''social'', ''image''))',
                       'CHECK (kind IN (''article'', ''video'', ''social'', ''image'', ''forum''))')
WHERE type = 'table' AND name = 'sources';
PRAGMA writable_schema = RESET;
