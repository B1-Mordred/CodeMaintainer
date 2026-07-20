CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS jobs (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  repository TEXT NOT NULL,
  task TEXT NOT NULL,
  issue_number INTEGER,
  state TEXT NOT NULL,
  base_sha TEXT NOT NULL DEFAULT '',
  result_sha TEXT NOT NULL DEFAULT '',
  acceptance_criteria TEXT NOT NULL DEFAULT '[]',
  acceptance_criteria_hash TEXT NOT NULL DEFAULT '',
  review_cycle INTEGER NOT NULL DEFAULT 0,
  version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS jobs_state_updated_idx ON jobs(state, updated_at DESC);
CREATE INDEX IF NOT EXISTS jobs_project_updated_idx ON jobs(project_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS job_transitions (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  from_state TEXT,
  to_state TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  reason TEXT NOT NULL,
  details TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS job_transitions_job_seq_idx ON job_transitions(job_id, sequence);

CREATE TABLE IF NOT EXISTS audit_events (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  actor_id TEXT NOT NULL,
  actor_role TEXT NOT NULL,
  action TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  correlation_id TEXT NOT NULL DEFAULT '',
  remote_address TEXT NOT NULL DEFAULT '',
  details TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS audit_target_idx ON audit_events(target_type, target_id, sequence);

CREATE TABLE IF NOT EXISTS config_revisions (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  actor_id TEXT NOT NULL,
  schema_version INTEGER NOT NULL,
  before_document TEXT NOT NULL,
  after_document TEXT NOT NULL,
  document_diff TEXT NOT NULL,
  validation_result TEXT NOT NULL,
  rollback_of TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS idempotency_records (
  scope TEXT NOT NULL,
  key TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  status TEXT NOT NULL,
  response TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (scope, key)
);

CREATE TRIGGER IF NOT EXISTS audit_events_no_update
BEFORE UPDATE ON audit_events BEGIN
  SELECT RAISE(ABORT, 'audit events are append-only');
END;

CREATE TRIGGER IF NOT EXISTS audit_events_no_delete
BEFORE DELETE ON audit_events BEGIN
  SELECT RAISE(ABORT, 'audit events are append-only');
END;

CREATE TRIGGER IF NOT EXISTS job_transitions_no_update
BEFORE UPDATE ON job_transitions BEGIN
  SELECT RAISE(ABORT, 'job transitions are append-only');
END;

CREATE TRIGGER IF NOT EXISTS job_transitions_no_delete
BEFORE DELETE ON job_transitions BEGIN
  SELECT RAISE(ABORT, 'job transitions are append-only');
END;

CREATE TRIGGER IF NOT EXISTS config_revisions_no_update
BEFORE UPDATE ON config_revisions BEGIN
  SELECT RAISE(ABORT, 'configuration revisions are append-only');
END;

CREATE TRIGGER IF NOT EXISTS config_revisions_no_delete
BEFORE DELETE ON config_revisions BEGIN
  SELECT RAISE(ABORT, 'configuration revisions are append-only');
END;
