CREATE TABLE test_designer_dispositions (
  id TEXT PRIMARY KEY,
  report_id TEXT NOT NULL REFERENCES test_designer_reports(id) ON DELETE RESTRICT,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  proposal_id TEXT NOT NULL,
  disposition TEXT NOT NULL CHECK(disposition IN ('accepted','rejected','not_applicable')),
  reason TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  actor_role TEXT NOT NULL CHECK(actor_role IN ('reviewer','administrator')),
  created_at TEXT NOT NULL
);

CREATE INDEX test_designer_dispositions_job_created_idx
ON test_designer_dispositions(job_id, created_at DESC, id);

CREATE INDEX test_designer_dispositions_report_proposal_created_idx
ON test_designer_dispositions(report_id, proposal_id, created_at DESC, id);

CREATE TRIGGER test_designer_dispositions_no_update BEFORE UPDATE ON test_designer_dispositions BEGIN
  SELECT RAISE(ABORT, 'test designer dispositions are append-only');
END;
CREATE TRIGGER test_designer_dispositions_no_delete BEFORE DELETE ON test_designer_dispositions BEGIN
  SELECT RAISE(ABORT, 'test designer dispositions are append-only');
END;
