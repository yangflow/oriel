-- A rule can name several subscriptions and words an entry must not contain. rule_sources holds the subscriptions
-- of a rule; a rule without a row there is for every source. The old rules.source_id moves into it, so the rules
-- table is rebuilt without that column (SQLite cannot drop a column that has a foreign key). exclude_keywords are
-- one per line like keywords: an entry that contains any of them does not match. A rule used to go with its one
-- source when that source was purged; the trigger keeps that for a rule whose every source goes, so it does not
-- turn into a rule for all sources. See docs/product.md "规则".

CREATE TEMPORARY TABLE rule_source_seed AS SELECT id AS rule_id, source_id FROM rules WHERE source_id IS NOT NULL;

CREATE TABLE rules_new (
    id               INTEGER PRIMARY KEY,
    kind             TEXT NOT NULL DEFAULT '',
    keywords         TEXT NOT NULL DEFAULT '',
    exclude_keywords TEXT NOT NULL DEFAULT '',
    author           TEXT NOT NULL DEFAULT '',
    star             INTEGER NOT NULL DEFAULT 0,
    mark_read        INTEGER NOT NULL DEFAULT 0,
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       INTEGER NOT NULL
);

INSERT INTO rules_new (id, kind, keywords, author, star, mark_read, enabled, created_at)
SELECT id, kind, keywords, author, star, mark_read, enabled, created_at FROM rules;

DROP TABLE rules;
ALTER TABLE rules_new RENAME TO rules;

CREATE TABLE rule_sources (
    rule_id   INTEGER NOT NULL REFERENCES rules (id) ON DELETE CASCADE,
    source_id INTEGER NOT NULL REFERENCES sources (id) ON DELETE CASCADE,
    PRIMARY KEY (rule_id, source_id)
) WITHOUT ROWID;

CREATE INDEX rule_sources_source ON rule_sources (source_id);

INSERT INTO rule_sources (rule_id, source_id) SELECT rule_id, source_id FROM rule_source_seed;
DROP TABLE rule_source_seed;

CREATE TRIGGER rules_lose_last_source BEFORE DELETE ON sources
BEGIN
    DELETE FROM rules WHERE id IN (
        SELECT rule_id FROM rule_sources WHERE source_id = OLD.id
    ) AND NOT EXISTS (
        SELECT 1 FROM rule_sources WHERE rule_id = rules.id AND source_id <> OLD.id
    );
END;
