ALTER TABLE config_drafts ADD COLUMN operation TEXT NOT NULL DEFAULT 'apply'
  CHECK(operation IN ('apply', 'import'));

CREATE TABLE config_import_unknown_entries (
  draft_id TEXT NOT NULL REFERENCES config_drafts(id) ON DELETE RESTRICT,
  setting_key TEXT NOT NULL,
  value_json TEXT,
  configured INTEGER NOT NULL CHECK(configured IN (0, 1)),
  secret INTEGER NOT NULL CHECK(secret IN (0, 1)),
  redacted INTEGER NOT NULL CHECK(redacted IN (0, 1)),
  PRIMARY KEY(draft_id, setting_key)
) STRICT;

CREATE TRIGGER config_import_unknown_entries_no_update BEFORE UPDATE ON config_import_unknown_entries BEGIN
  SELECT RAISE(ABORT, 'preserved unknown configuration entries are immutable');
END;
CREATE TRIGGER config_import_unknown_entries_no_delete BEFORE DELETE ON config_import_unknown_entries BEGIN
  SELECT RAISE(ABORT, 'preserved unknown configuration entries are immutable');
END;
