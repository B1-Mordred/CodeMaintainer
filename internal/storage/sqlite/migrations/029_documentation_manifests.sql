CREATE TABLE documentation_manifests (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  schema_version INTEGER NOT NULL CHECK(schema_version = 1),
  project_id TEXT NOT NULL,
  contract_sha256 TEXT NOT NULL CHECK(length(contract_sha256) = 64),
  risk_level TEXT NOT NULL CHECK(risk_level IN ('low','medium','high')),
  result_sha TEXT NOT NULL CHECK(length(result_sha) IN (40,64)),
  source_context TEXT NOT NULL,
  policy_version TEXT NOT NULL,
  requirements_json TEXT NOT NULL CHECK(json_valid(requirements_json)),
  changes_json TEXT NOT NULL CHECK(json_valid(changes_json)),
  checks_json TEXT NOT NULL CHECK(json_valid(checks_json)),
  unsupported_claims_json TEXT NOT NULL CHECK(json_valid(unsupported_claims_json)),
  edits_json TEXT NOT NULL CHECK(json_valid(edits_json)),
  status TEXT NOT NULL CHECK(status IN ('passed','changes_applied','no_documentation_required','blocked')),
  policy_summary TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX documentation_manifests_job_created_idx
ON documentation_manifests(job_id, created_at DESC, id);

CREATE TRIGGER documentation_manifests_no_update BEFORE UPDATE ON documentation_manifests BEGIN
  SELECT RAISE(ABORT, 'documentation manifests are append-only');
END;

CREATE TRIGGER documentation_manifests_no_delete BEFORE DELETE ON documentation_manifests BEGIN
  SELECT RAISE(ABORT, 'documentation manifests are append-only');
END;
