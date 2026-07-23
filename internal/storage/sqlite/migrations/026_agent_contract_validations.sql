CREATE TABLE agent_contract_validations (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  phase TEXT NOT NULL,
  contract_kind TEXT NOT NULL CHECK(contract_kind IN (
    'task_packet',
    'implementation_result',
    'qc_report',
    'test_proposal',
    'documentation_manifest',
    'risk_assessment',
    'completion_summary'
  )),
  schema_version INTEGER NOT NULL CHECK(schema_version = 1),
  schema_sha256 TEXT NOT NULL,
  payload_sha256 TEXT NOT NULL,
  attempt INTEGER NOT NULL CHECK(attempt > 0 AND attempt <= 20),
  valid INTEGER NOT NULL CHECK(valid IN (0,1)),
  error TEXT NOT NULL DEFAULT '',
  artifact_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE INDEX agent_contract_validations_job_created_idx
ON agent_contract_validations(job_id, created_at DESC, id);

CREATE TRIGGER agent_contract_validations_no_update BEFORE UPDATE ON agent_contract_validations BEGIN
  SELECT RAISE(ABORT, 'agent contract validations are append-only');
END;
CREATE TRIGGER agent_contract_validations_no_delete BEFORE DELETE ON agent_contract_validations BEGIN
  SELECT RAISE(ABORT, 'agent contract validations are append-only');
END;
