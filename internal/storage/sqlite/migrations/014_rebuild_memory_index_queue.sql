ALTER TABLE memory_index_operations RENAME TO memory_index_operations_pre_sequence;
DROP INDEX IF EXISTS memory_index_operations_ready_idx;

CREATE TABLE memory_index_operations (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  record_id TEXT NOT NULL REFERENCES memory_records(id) ON DELETE RESTRICT,
  owner TEXT NOT NULL,
  repository TEXT NOT NULL,
  action TEXT NOT NULL CHECK(action IN ('upsert', 'forget')),
  record_version INTEGER NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('pending', 'running', 'completed', 'failed')),
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  next_attempt_at TEXT NOT NULL,
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_expires_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(record_id, record_version, action)
) STRICT;

INSERT INTO memory_index_operations(
  id, record_id, owner, repository, action, record_version, state, attempts,
  last_error, next_attempt_at, lease_owner, lease_expires_at, created_at, updated_at
)
SELECT id, record_id, owner, repository, action, record_version, state, attempts,
  last_error, next_attempt_at, lease_owner, lease_expires_at, created_at, updated_at
FROM memory_index_operations_pre_sequence
ORDER BY created_at, id;

DROP TABLE memory_index_operations_pre_sequence;

CREATE INDEX memory_index_operations_ready_idx
ON memory_index_operations(state, next_attempt_at, sequence);
