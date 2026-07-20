CREATE TABLE approvals (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL,
  subject_sha TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  actor_role TEXT NOT NULL,
  rationale TEXT NOT NULL,
  reauthenticated INTEGER NOT NULL CHECK(reauthenticated IN (0, 1)),
  created_at TEXT NOT NULL,
  UNIQUE(job_id, kind, subject_sha)
);

CREATE TRIGGER approvals_no_update BEFORE UPDATE ON approvals BEGIN
  SELECT RAISE(ABORT, 'approvals are append-only');
END;
CREATE TRIGGER approvals_no_delete BEFORE DELETE ON approvals BEGIN
  SELECT RAISE(ABORT, 'approvals are append-only');
END;
