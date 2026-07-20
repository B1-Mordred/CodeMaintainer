ALTER TABLE jobs ADD COLUMN max_wall_seconds INTEGER NOT NULL DEFAULT 14400 CHECK(max_wall_seconds BETWEEN 60 AND 604800);
ALTER TABLE jobs ADD COLUMN deadline_at TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN max_tokens INTEGER NOT NULL DEFAULT 262144 CHECK(max_tokens BETWEEN 1 AND 10000000);
ALTER TABLE jobs ADD COLUMN reserved_tokens INTEGER NOT NULL DEFAULT 0 CHECK(reserved_tokens >= 0 AND reserved_tokens <= max_tokens);

CREATE TABLE job_token_reservations (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  phase_version INTEGER NOT NULL,
  phase_state TEXT NOT NULL,
  reserved_tokens INTEGER NOT NULL CHECK(reserved_tokens > 0),
  created_at TEXT NOT NULL,
  UNIQUE(job_id, phase_version)
);

CREATE TRIGGER job_token_reservations_no_update BEFORE UPDATE ON job_token_reservations BEGIN
  SELECT RAISE(ABORT, 'job token reservations are append-only');
END;
CREATE TRIGGER job_token_reservations_no_delete BEFORE DELETE ON job_token_reservations BEGIN
  SELECT RAISE(ABORT, 'job token reservations are append-only');
END;
