CREATE TABLE memory_records (
  id TEXT PRIMARY KEY,
  owner TEXT NOT NULL,
  repository TEXT NOT NULL,
  namespace TEXT NOT NULL,
  kind TEXT NOT NULL,
  content TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  source_uri TEXT NOT NULL DEFAULT '',
  base_commit TEXT NOT NULL DEFAULT '',
  merged_commit TEXT NOT NULL DEFAULT '',
  affected_paths TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL CHECK(status IN ('quarantine', 'canonical', 'stale', 'deleted')),
  verified INTEGER NOT NULL DEFAULT 0 CHECK(verified IN (0, 1)),
  secret_scan_pass INTEGER NOT NULL CHECK(secret_scan_pass IN (0, 1)),
  invalidation_rule TEXT NOT NULL DEFAULT '',
  expires_at TEXT,
  deleted_at TEXT,
  version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX memory_records_scope_status_idx ON memory_records(owner, repository, status, updated_at DESC);
CREATE INDEX memory_records_namespace_idx ON memory_records(namespace, status);

CREATE TABLE memory_events (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  record_id TEXT NOT NULL REFERENCES memory_records(id) ON DELETE RESTRICT,
  owner TEXT NOT NULL,
  repository TEXT NOT NULL,
  action TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  rationale TEXT NOT NULL,
  details TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE INDEX memory_events_record_idx ON memory_events(record_id, sequence);

CREATE TABLE memory_retrieval_traces (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL DEFAULT '',
  owner TEXT NOT NULL,
  repository TEXT NOT NULL,
  namespace TEXT NOT NULL,
  query_text TEXT NOT NULL,
  query_hash TEXT NOT NULL,
  candidate_ids TEXT NOT NULL,
  selected_ids TEXT NOT NULL,
  budget_tokens INTEGER NOT NULL,
  allocated_tokens INTEGER NOT NULL,
  trajectory TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX memory_retrieval_scope_idx ON memory_retrieval_traces(owner, repository, created_at DESC);

CREATE TRIGGER memory_events_no_update BEFORE UPDATE ON memory_events BEGIN
  SELECT RAISE(ABORT, 'memory events are append-only');
END;
CREATE TRIGGER memory_events_no_delete BEFORE DELETE ON memory_events BEGIN
  SELECT RAISE(ABORT, 'memory events are append-only');
END;
CREATE TRIGGER memory_retrieval_no_update BEFORE UPDATE ON memory_retrieval_traces BEGIN
  SELECT RAISE(ABORT, 'memory retrieval traces are append-only');
END;
CREATE TRIGGER memory_retrieval_no_delete BEFORE DELETE ON memory_retrieval_traces BEGIN
  SELECT RAISE(ABORT, 'memory retrieval traces are append-only');
END;
