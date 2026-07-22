CREATE TABLE windows_worker_profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  mode TEXT NOT NULL CHECK(mode IN ('simulator','remote')),
  endpoint TEXT NOT NULL,
  endpoint_allowlist_json TEXT NOT NULL CHECK(json_valid(endpoint_allowlist_json)),
  credential_reference TEXT NOT NULL DEFAULT '',
  credential_status TEXT NOT NULL,
  health TEXT NOT NULL,
  capacity INTEGER NOT NULL CHECK(capacity BETWEEN 1 AND 64),
  vm_template_id TEXT NOT NULL,
  toolchains_json TEXT NOT NULL CHECK(json_valid(toolchains_json)),
  allowed_job_types_json TEXT NOT NULL CHECK(json_valid(allowed_job_types_json)),
  timeout_seconds INTEGER NOT NULL CHECK(timeout_seconds BETWEEN 30 AND 86400),
  simulator_profile_ids_json TEXT NOT NULL CHECK(json_valid(simulator_profile_ids_json)),
  artifact_retention_days INTEGER NOT NULL CHECK(artifact_retention_days BETWEEN 1 AND 3650),
  signing_policy_reference TEXT NOT NULL DEFAULT '',
  manual_gates_json TEXT NOT NULL CHECK(json_valid(manual_gates_json)),
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  revision INTEGER NOT NULL CHECK(revision > 0),
  updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE windows_worker_runs (
  run_id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES windows_worker_profiles(id) ON DELETE RESTRICT,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  job_id TEXT NOT NULL,
  job_type TEXT NOT NULL,
  input_sha256 TEXT NOT NULL,
  request_json TEXT NOT NULL CHECK(json_valid(request_json)),
  result_json TEXT NOT NULL CHECK(json_valid(result_json)),
  state TEXT NOT NULL CHECK(state IN ('completed','failed')),
  idempotency_key TEXT NOT NULL UNIQUE,
  started_at TEXT NOT NULL,
  completed_at TEXT NOT NULL
) STRICT;

CREATE INDEX windows_worker_runs_profile ON windows_worker_runs(profile_id,started_at DESC);
CREATE INDEX windows_worker_runs_project ON windows_worker_runs(project_id,started_at DESC);
