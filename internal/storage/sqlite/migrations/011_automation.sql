CREATE TABLE schedules (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  task_type TEXT NOT NULL CHECK(task_type IN ('maintenance', 'sync', 'audit')),
  task TEXT NOT NULL,
  interval_seconds INTEGER NOT NULL,
  window_start_minute INTEGER NOT NULL,
  window_end_minute INTEGER NOT NULL,
  max_wall_seconds INTEGER NOT NULL,
  max_tokens INTEGER NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0, 1)),
  next_run_at TEXT NOT NULL,
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX schedules_due_idx ON schedules(enabled, next_run_at);

CREATE TABLE schedule_runs (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  schedule_id TEXT NOT NULL REFERENCES schedules(id) ON DELETE RESTRICT,
  job_id TEXT REFERENCES jobs(id) ON DELETE RESTRICT,
  status TEXT NOT NULL CHECK(status IN ('enqueued', 'skipped')),
  reason TEXT NOT NULL,
  due_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE skill_proposals (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL,
  content TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('proposed', 'approved_inert', 'rejected')),
  version INTEGER NOT NULL,
  proposed_by TEXT NOT NULL,
  reviewed_by TEXT NOT NULL DEFAULT '',
  review_reason TEXT NOT NULL DEFAULT '',
  activated INTEGER NOT NULL DEFAULT 0 CHECK(activated = 0),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE skill_proposal_events (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  proposal_id TEXT NOT NULL REFERENCES skill_proposals(id) ON DELETE RESTRICT,
  action TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  rationale TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE automation_requests (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL CHECK(kind IN ('review', 'publication_approval')),
  requested_by TEXT NOT NULL,
  rationale TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TRIGGER schedule_runs_no_update BEFORE UPDATE ON schedule_runs BEGIN
  SELECT RAISE(ABORT, 'schedule runs are append-only');
END;
CREATE TRIGGER schedule_runs_no_delete BEFORE DELETE ON schedule_runs BEGIN
  SELECT RAISE(ABORT, 'schedule runs are append-only');
END;
CREATE TRIGGER skill_proposal_events_no_update BEFORE UPDATE ON skill_proposal_events BEGIN
  SELECT RAISE(ABORT, 'skill proposal events are append-only');
END;
CREATE TRIGGER skill_proposal_events_no_delete BEFORE DELETE ON skill_proposal_events BEGIN
  SELECT RAISE(ABORT, 'skill proposal events are append-only');
END;
CREATE TRIGGER automation_requests_no_update BEFORE UPDATE ON automation_requests BEGIN
  SELECT RAISE(ABORT, 'automation requests are append-only');
END;
CREATE TRIGGER automation_requests_no_delete BEFORE DELETE ON automation_requests BEGIN
  SELECT RAISE(ABORT, 'automation requests are append-only');
END;
