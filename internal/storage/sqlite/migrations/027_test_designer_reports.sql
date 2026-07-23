CREATE TABLE test_designer_reports (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  schema_version INTEGER NOT NULL CHECK(schema_version = 1),
  contract_sha256 TEXT NOT NULL,
  risk_level TEXT NOT NULL CHECK(risk_level IN ('low','medium','high')),
  result_sha TEXT NOT NULL,
  source_context TEXT NOT NULL,
  proposals TEXT NOT NULL,
  dispositions_required INTEGER NOT NULL CHECK(dispositions_required IN (0,1)),
  status TEXT NOT NULL CHECK(status IN ('proposed','skipped')),
  created_at TEXT NOT NULL
);

CREATE INDEX test_designer_reports_job_created_idx
ON test_designer_reports(job_id, created_at DESC, id);

CREATE TRIGGER test_designer_reports_no_update BEFORE UPDATE ON test_designer_reports BEGIN
  SELECT RAISE(ABORT, 'test designer reports are append-only');
END;
CREATE TRIGGER test_designer_reports_no_delete BEFORE DELETE ON test_designer_reports BEGIN
  SELECT RAISE(ABORT, 'test designer reports are append-only');
END;
