ALTER TABLE job_artifacts ADD COLUMN idempotency_key TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX job_artifacts_job_idempotency_idx
ON job_artifacts(job_id, idempotency_key)
WHERE idempotency_key <> '';

CREATE TABLE workflow_phase_records (
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  phase_state TEXT NOT NULL,
  phase_version INTEGER NOT NULL CHECK(phase_version > 0),
  outcome TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(job_id, phase_version)
);

CREATE INDEX workflow_phase_job_state_idx
ON workflow_phase_records(job_id, phase_state, phase_version);

CREATE TRIGGER workflow_phase_records_no_update
BEFORE UPDATE ON workflow_phase_records BEGIN
  SELECT RAISE(ABORT, 'workflow phase records are append-only');
END;

CREATE TRIGGER workflow_phase_records_no_delete
BEFORE DELETE ON workflow_phase_records BEGIN
  SELECT RAISE(ABORT, 'workflow phase records are append-only');
END;
