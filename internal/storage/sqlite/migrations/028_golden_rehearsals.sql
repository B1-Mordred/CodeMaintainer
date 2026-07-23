CREATE TABLE golden_rehearsal_reports (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  schema_version INTEGER NOT NULL CHECK(schema_version=1),
  project_id TEXT NOT NULL,
  contract_sha256 TEXT NOT NULL CHECK(length(contract_sha256)=64),
  risk_level TEXT NOT NULL CHECK(risk_level IN ('low','medium','high')),
  result_sha TEXT NOT NULL CHECK(length(result_sha) IN (40,64)),
  source_context TEXT NOT NULL,
  comparisons_json TEXT NOT NULL CHECK(json_valid(comparisons_json)),
  status TEXT NOT NULL CHECK(status IN ('passed','no_rehearsals','approval_required','failed')),
  policy_summary TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX golden_rehearsal_reports_job ON golden_rehearsal_reports(job_id, created_at DESC, id);

CREATE TRIGGER golden_rehearsal_reports_no_update BEFORE UPDATE ON golden_rehearsal_reports BEGIN
  SELECT RAISE(ABORT, 'golden rehearsal reports are append-only');
END;

CREATE TRIGGER golden_rehearsal_reports_no_delete BEFORE DELETE ON golden_rehearsal_reports BEGIN
  SELECT RAISE(ABORT, 'golden rehearsal reports are append-only');
END;

CREATE TABLE golden_update_approvals (
  id TEXT PRIMARY KEY,
  report_id TEXT NOT NULL REFERENCES golden_rehearsal_reports(id) ON DELETE CASCADE,
  comparison_id TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  actor_role TEXT NOT NULL,
  reason TEXT NOT NULL,
  approved INTEGER NOT NULL CHECK(approved IN (0,1)),
  reauthenticated INTEGER NOT NULL CHECK(reauthenticated=1),
  approved_artifact_sha256 TEXT NOT NULL CHECK(length(approved_artifact_sha256)=64),
  candidate_artifact_sha256 TEXT NOT NULL CHECK(length(candidate_artifact_sha256)=64),
  created_at TEXT NOT NULL
);

CREATE INDEX golden_update_approvals_report ON golden_update_approvals(report_id, created_at DESC, id);

CREATE TRIGGER golden_update_approvals_no_update BEFORE UPDATE ON golden_update_approvals BEGIN
  SELECT RAISE(ABORT, 'golden update approvals are append-only');
END;

CREATE TRIGGER golden_update_approvals_no_delete BEFORE DELETE ON golden_update_approvals BEGIN
  SELECT RAISE(ABORT, 'golden update approvals are append-only');
END;
