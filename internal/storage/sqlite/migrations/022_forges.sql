CREATE TABLE forge_profiles (
  project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE RESTRICT,
  provider TEXT NOT NULL CHECK(provider IN ('github','gitlab','local')),
  endpoint TEXT NOT NULL,
  endpoint_allowlist_json TEXT NOT NULL CHECK(json_valid(endpoint_allowlist_json)),
  repository TEXT NOT NULL,
  credential_reference TEXT NOT NULL DEFAULT '',
  credential_status TEXT NOT NULL,
  webhook_status TEXT NOT NULL,
  sync_direction TEXT NOT NULL CHECK(sync_direction IN ('pull','bidirectional_draft')),
  polling_minutes INTEGER NOT NULL CHECK(polling_minutes BETWEEN 1 AND 10080),
  branch_convention TEXT NOT NULL,
  change_request_convention TEXT NOT NULL,
  label_mapping_json TEXT NOT NULL CHECK(json_valid(label_mapping_json)),
  ci_artifact_policy TEXT NOT NULL,
  release_policy TEXT NOT NULL,
  submodules_enabled INTEGER NOT NULL CHECK(submodules_enabled IN (0,1)),
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  revision INTEGER NOT NULL CHECK(revision > 0),
  updated_at TEXT NOT NULL
) STRICT;
CREATE TABLE forge_sync_runs (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  provider TEXT NOT NULL,
  input_cursor TEXT NOT NULL DEFAULT '',
  output_cursor TEXT NOT NULL DEFAULT '',
  idempotency_key TEXT NOT NULL UNIQUE,
  state TEXT NOT NULL CHECK(state IN ('complete','partial')),
  objects INTEGER NOT NULL CHECK(objects >= 0),
  partial INTEGER NOT NULL CHECK(partial IN (0,1)),
  unsupported_json TEXT NOT NULL CHECK(json_valid(unsupported_json)),
  created_at TEXT NOT NULL
) STRICT;
CREATE INDEX forge_sync_runs_project ON forge_sync_runs(project_id,created_at DESC);
CREATE TABLE forge_objects (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  provider TEXT NOT NULL,
  kind TEXT NOT NULL,
  external_id TEXT NOT NULL,
  object_json TEXT NOT NULL CHECK(json_valid(object_json)),
  observed_at TEXT NOT NULL,
  PRIMARY KEY(project_id,provider,kind,external_id)
) STRICT;
