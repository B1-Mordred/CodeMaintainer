CREATE TABLE github_deliveries (
  delivery_id TEXT PRIMARY KEY,
  event TEXT NOT NULL CHECK(event = 'pull_request'),
  action TEXT NOT NULL CHECK(action = 'closed'),
  outcome TEXT NOT NULL CHECK(outcome IN ('merged', 'rejected')),
  repository TEXT NOT NULL,
  pr_number INTEGER NOT NULL CHECK(pr_number > 0),
  job_id TEXT NOT NULL REFERENCES jobs(id),
  head_sha TEXT NOT NULL CHECK(length(head_sha) = 40),
  merged_commit TEXT NOT NULL DEFAULT '',
  payload_sha256 TEXT NOT NULL CHECK(length(payload_sha256) = 64),
  affected_records INTEGER NOT NULL CHECK(affected_records >= 0),
  created_at TEXT NOT NULL
) STRICT;

CREATE TRIGGER github_deliveries_no_update
BEFORE UPDATE ON github_deliveries BEGIN
  SELECT RAISE(ABORT, 'github deliveries are append-only');
END;

CREATE TRIGGER github_deliveries_no_delete
BEFORE DELETE ON github_deliveries BEGIN
  SELECT RAISE(ABORT, 'github deliveries are append-only');
END;
