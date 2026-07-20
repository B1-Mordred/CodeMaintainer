CREATE TABLE config_scope_heads (
  scope_kind TEXT NOT NULL CHECK(scope_kind IN ('system', 'capability_pack', 'project', 'environment', 'job_template', 'job_override')),
  scope_id TEXT NOT NULL,
  version INTEGER NOT NULL CHECK(version >= 1),
  revision_id TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(scope_kind, scope_id),
  CHECK((scope_kind = 'system' AND scope_id = '') OR (scope_kind <> 'system' AND length(scope_id) BETWEEN 1 AND 256))
) STRICT;

CREATE TABLE config_registry_revisions (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  scope_kind TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  scope_version INTEGER NOT NULL CHECK(scope_version >= 1),
  actor_id TEXT NOT NULL,
  actor_role TEXT NOT NULL,
  operation TEXT NOT NULL CHECK(operation IN ('apply', 'rollback', 'import', 'reset')),
  reason TEXT NOT NULL CHECK(length(reason) BETWEEN 1 AND 1000),
  rollback_of TEXT NOT NULL DEFAULT '',
  draft_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(scope_kind, scope_id, scope_version)
) STRICT;

CREATE INDEX config_registry_revisions_scope_idx
  ON config_registry_revisions(scope_kind, scope_id, sequence DESC);

CREATE TABLE config_registry_revision_entries (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  revision_id TEXT NOT NULL REFERENCES config_registry_revisions(id) ON DELETE RESTRICT,
  setting_key TEXT NOT NULL,
  before_value TEXT,
  after_value TEXT,
  before_configured INTEGER NOT NULL CHECK(before_configured IN (0, 1)),
  after_configured INTEGER NOT NULL CHECK(after_configured IN (0, 1)),
  secret INTEGER NOT NULL CHECK(secret IN (0, 1)),
  UNIQUE(revision_id, setting_key),
  CHECK(secret = 0 OR (before_value IS NULL AND after_value IS NULL))
) STRICT;

CREATE TABLE config_scope_values (
  setting_key TEXT NOT NULL,
  scope_kind TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  value_json TEXT,
  configured INTEGER NOT NULL CHECK(configured IN (0, 1)),
  secret INTEGER NOT NULL CHECK(secret IN (0, 1)),
  version INTEGER NOT NULL CHECK(version >= 1),
  revision_id TEXT NOT NULL REFERENCES config_registry_revisions(id) ON DELETE RESTRICT,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(setting_key, scope_kind, scope_id),
  CHECK(secret = 0 OR value_json IS NULL),
  CHECK((configured = 0 AND value_json IS NULL) OR configured = 1)
) STRICT;

CREATE INDEX config_scope_values_scope_idx
  ON config_scope_values(scope_kind, scope_id, setting_key);

CREATE TABLE config_drafts (
  id TEXT PRIMARY KEY,
  scope_kind TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('draft', 'reviewed', 'applied', 'discarded')),
  base_scope_version INTEGER NOT NULL CHECK(base_scope_version >= 0),
  version INTEGER NOT NULL CHECK(version >= 1),
  author_id TEXT NOT NULL,
  reviewer_id TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  applied_revision_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  CHECK((scope_kind = 'system' AND scope_id = '') OR (scope_kind <> 'system' AND length(scope_id) BETWEEN 1 AND 256))
) STRICT;

CREATE INDEX config_drafts_scope_idx ON config_drafts(scope_kind, scope_id, updated_at DESC);

CREATE TABLE config_draft_entries (
  draft_id TEXT NOT NULL REFERENCES config_drafts(id) ON DELETE RESTRICT,
  setting_key TEXT NOT NULL,
  value_json TEXT,
  reset_value INTEGER NOT NULL CHECK(reset_value IN (0, 1)),
  secret INTEGER NOT NULL CHECK(secret IN (0, 1)),
  configured INTEGER NOT NULL CHECK(configured IN (0, 1)),
  PRIMARY KEY(draft_id, setting_key),
  CHECK(secret = 0 OR value_json IS NULL),
  CHECK(reset_value = 0 OR (value_json IS NULL AND configured = 0))
) STRICT;

CREATE TABLE config_checks (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  draft_id TEXT NOT NULL REFERENCES config_drafts(id) ON DELETE RESTRICT,
  draft_version INTEGER NOT NULL CHECK(draft_version >= 1),
  kind TEXT NOT NULL CHECK(kind IN ('validation', 'dry_run', 'prerequisite')),
  handler TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('passed', 'failed', 'unavailable')),
  result TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT
) STRICT;

CREATE INDEX config_checks_draft_idx ON config_checks(draft_id, draft_version, sequence DESC);

CREATE TABLE job_config_snapshots (
  job_id TEXT PRIMARY KEY REFERENCES jobs(id) ON DELETE RESTRICT,
  schema_version INTEGER NOT NULL,
  registry_hash TEXT NOT NULL CHECK(length(registry_hash) = 64),
  snapshot_sha256 TEXT NOT NULL CHECK(length(snapshot_sha256) = 64),
  document TEXT NOT NULL,
  created_at TEXT NOT NULL
) STRICT;

INSERT INTO config_registry_revisions(id, scope_kind, scope_id, scope_version, actor_id,
  actor_role, operation, reason, created_at)
SELECT 'config_registry_system_v16', 'system', '', 1, 'system-migration', 'administrator',
  'import', 'Import the active Increment 1 configuration into the scoped registry.',
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE EXISTS(SELECT 1 FROM config_revisions);

INSERT INTO config_scope_heads(scope_kind, scope_id, version, revision_id, updated_at)
SELECT 'system', '', 1, 'config_registry_system_v16', strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE EXISTS(SELECT 1 FROM config_revisions);

INSERT INTO config_registry_revision_entries(revision_id, setting_key, before_value,
  after_value, before_configured, after_configured, secret)
SELECT 'config_registry_system_v16', setting_key, NULL, value_json, 0, 1, 0
FROM (
  SELECT 'workflow.max_review_cycles' AS setting_key, CAST(json_extract(after_document, '$.workflow.max_review_cycles') AS TEXT) AS value_json FROM config_revisions ORDER BY sequence DESC LIMIT 1
) WHERE value_json IS NOT NULL;
INSERT INTO config_registry_revision_entries(revision_id, setting_key, before_value, after_value, before_configured, after_configured, secret)
SELECT 'config_registry_system_v16', 'workflow.max_wall_seconds', NULL, CAST(json_extract(after_document, '$.workflow.max_wall_seconds') AS TEXT), 0, 1, 0 FROM config_revisions ORDER BY sequence DESC LIMIT 1;
INSERT INTO config_registry_revision_entries(revision_id, setting_key, before_value, after_value, before_configured, after_configured, secret)
SELECT 'config_registry_system_v16', 'workflow.max_log_bytes', NULL, CAST(json_extract(after_document, '$.workflow.max_log_bytes') AS TEXT), 0, 1, 0 FROM config_revisions ORDER BY sequence DESC LIMIT 1;
INSERT INTO config_registry_revision_entries(revision_id, setting_key, before_value, after_value, before_configured, after_configured, secret)
SELECT 'config_registry_system_v16', 'qc.block_on', NULL, json_extract(after_document, '$.qc.block_on'), 0, 1, 0 FROM config_revisions ORDER BY sequence DESC LIMIT 1;
INSERT INTO config_registry_revision_entries(revision_id, setting_key, before_value, after_value, before_configured, after_configured, secret)
SELECT 'config_registry_system_v16', 'qc.require_evidence_for_blocking', NULL, CASE json_extract(after_document, '$.qc.require_evidence_for_blocking') WHEN 1 THEN 'true' ELSE 'false' END, 0, 1, 0 FROM config_revisions ORDER BY sequence DESC LIMIT 1;
INSERT INTO config_registry_revision_entries(revision_id, setting_key, before_value, after_value, before_configured, after_configured, secret)
SELECT 'config_registry_system_v16', 'qc.require_verification_method', NULL, CASE json_extract(after_document, '$.qc.require_verification_method') WHEN 1 THEN 'true' ELSE 'false' END, 0, 1, 0 FROM config_revisions ORDER BY sequence DESC LIMIT 1;
INSERT INTO config_registry_revision_entries(revision_id, setting_key, before_value, after_value, before_configured, after_configured, secret)
SELECT 'config_registry_system_v16', 'qc.human_waiver_enabled', NULL, CASE json_extract(after_document, '$.qc.human_waiver_enabled') WHEN 1 THEN 'true' ELSE 'false' END, 0, 1, 0 FROM config_revisions ORDER BY sequence DESC LIMIT 1;
INSERT INTO config_registry_revision_entries(revision_id, setting_key, before_value, after_value, before_configured, after_configured, secret)
SELECT 'config_registry_system_v16', 'qc.waiver_rationale_required', NULL, CASE json_extract(after_document, '$.qc.waiver_rationale_required') WHEN 1 THEN 'true' ELSE 'false' END, 0, 1, 0 FROM config_revisions ORDER BY sequence DESC LIMIT 1;
INSERT INTO config_registry_revision_entries(revision_id, setting_key, before_value, after_value, before_configured, after_configured, secret)
SELECT 'config_registry_system_v16', 'protected_paths.patterns', NULL, json_extract(after_document, '$.protected_paths.patterns'), 0, 1, 0 FROM config_revisions ORDER BY sequence DESC LIMIT 1;
INSERT INTO config_registry_revision_entries(revision_id, setting_key, before_value, after_value, before_configured, after_configured, secret)
SELECT 'config_registry_system_v16', 'notifications.local_inbox_enabled', NULL, CASE json_extract(after_document, '$.notifications.local_inbox_enabled') WHEN 1 THEN 'true' ELSE 'false' END, 0, 1, 0 FROM config_revisions ORDER BY sequence DESC LIMIT 1;

INSERT INTO config_scope_values(setting_key, scope_kind, scope_id, value_json, configured,
  secret, version, revision_id, updated_at)
SELECT setting_key, 'system', '', after_value, 1, 0, 1, revision_id,
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM config_registry_revision_entries WHERE revision_id = 'config_registry_system_v16';

CREATE TRIGGER config_registry_revisions_no_update BEFORE UPDATE ON config_registry_revisions BEGIN
  SELECT RAISE(ABORT, 'configuration registry revisions are append-only');
END;
CREATE TRIGGER config_registry_revisions_no_delete BEFORE DELETE ON config_registry_revisions BEGIN
  SELECT RAISE(ABORT, 'configuration registry revisions are append-only');
END;
CREATE TRIGGER config_registry_revision_entries_no_update BEFORE UPDATE ON config_registry_revision_entries BEGIN
  SELECT RAISE(ABORT, 'configuration registry revision entries are append-only');
END;
CREATE TRIGGER config_registry_revision_entries_no_delete BEFORE DELETE ON config_registry_revision_entries BEGIN
  SELECT RAISE(ABORT, 'configuration registry revision entries are append-only');
END;
CREATE TRIGGER config_checks_no_update BEFORE UPDATE ON config_checks BEGIN
  SELECT RAISE(ABORT, 'configuration checks are append-only');
END;
CREATE TRIGGER config_checks_no_delete BEFORE DELETE ON config_checks BEGIN
  SELECT RAISE(ABORT, 'configuration checks are append-only');
END;
CREATE TRIGGER job_config_snapshots_no_update BEFORE UPDATE ON job_config_snapshots BEGIN
  SELECT RAISE(ABORT, 'job configuration snapshots are immutable');
END;
CREATE TRIGGER job_config_snapshots_no_delete BEFORE DELETE ON job_config_snapshots BEGIN
  SELECT RAISE(ABORT, 'job configuration snapshots are immutable');
END;
