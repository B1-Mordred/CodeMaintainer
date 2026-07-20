CREATE TABLE code_intel_blobs (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  repository TEXT NOT NULL,
  blob_sha256 TEXT NOT NULL CHECK(length(blob_sha256) = 64),
  parser_id TEXT NOT NULL,
  language TEXT NOT NULL,
  classification TEXT NOT NULL CHECK(classification IN ('source', 'generated', 'vendor', 'binary')),
  bytes INTEGER NOT NULL CHECK(bytes >= 0),
  analysis_json TEXT NOT NULL CHECK(json_valid(analysis_json)),
  created_at TEXT NOT NULL,
  PRIMARY KEY(project_id, repository, blob_sha256, parser_id)
) STRICT;

CREATE TABLE code_intel_runs (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  repository TEXT NOT NULL,
  revision TEXT NOT NULL,
  parser_id TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('complete', 'partial', 'cancelled')),
  files INTEGER NOT NULL CHECK(files >= 0),
  parsed INTEGER NOT NULL CHECK(parsed >= 0),
  reused INTEGER NOT NULL CHECK(reused >= 0),
  failures INTEGER NOT NULL CHECK(failures >= 0),
  bytes INTEGER NOT NULL CHECK(bytes >= 0),
  started_at TEXT NOT NULL,
  completed_at TEXT NOT NULL
) STRICT;
CREATE INDEX code_intel_runs_project_revision ON code_intel_runs(project_id, revision, completed_at DESC);

CREATE TABLE code_intel_files (
  run_id TEXT NOT NULL REFERENCES code_intel_runs(id) ON DELETE RESTRICT,
  path TEXT NOT NULL,
  blob_sha256 TEXT NOT NULL CHECK(length(blob_sha256) = 64),
  language TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('indexed', 'failed')),
  failure TEXT NOT NULL DEFAULT '',
  reused INTEGER NOT NULL CHECK(reused IN (0, 1)),
  PRIMARY KEY(run_id, path)
) STRICT;

CREATE TABLE context_manifests (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  job_id TEXT REFERENCES jobs(id) ON DELETE RESTRICT,
  stage TEXT NOT NULL,
  schema_version INTEGER NOT NULL,
  budget_tokens INTEGER NOT NULL CHECK(budget_tokens > 0),
  reserved_output_tokens INTEGER NOT NULL CHECK(reserved_output_tokens > 0),
  used_tokens INTEGER NOT NULL CHECK(used_tokens >= 0),
  truncated INTEGER NOT NULL CHECK(truncated IN (0, 1)),
  selections_json TEXT NOT NULL CHECK(json_valid(selections_json)),
  created_at TEXT NOT NULL
) STRICT;
CREATE INDEX context_manifests_project_job ON context_manifests(project_id, job_id, created_at DESC);

CREATE TABLE verification_baselines (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  revision TEXT NOT NULL,
  config_sha256 TEXT NOT NULL CHECK(length(config_sha256) = 64),
  toolchain_id TEXT NOT NULL,
  pack_set_sha256 TEXT NOT NULL CHECK(length(pack_set_sha256) = 64),
  actor_id TEXT NOT NULL,
  reason TEXT NOT NULL,
  observations_json TEXT NOT NULL CHECK(json_valid(observations_json)),
  created_at TEXT NOT NULL
) STRICT;

CREATE TABLE verification_differentials (
  id TEXT PRIMARY KEY,
  baseline_id TEXT NOT NULL REFERENCES verification_baselines(id) ON DELETE RESTRICT,
  candidate_sha TEXT NOT NULL CHECK(length(candidate_sha) = 64),
  items_json TEXT NOT NULL CHECK(json_valid(items_json)),
  created_at TEXT NOT NULL
) STRICT;

CREATE TABLE test_impact_records (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  revision TEXT NOT NULL,
  changed_symbols_json TEXT NOT NULL CHECK(json_valid(changed_symbols_json)),
  selections_json TEXT NOT NULL CHECK(json_valid(selections_json)),
  full_suite_required INTEGER NOT NULL CHECK(full_suite_required IN (0, 1)),
  policy_explanation TEXT NOT NULL,
  created_at TEXT NOT NULL
) STRICT;

CREATE TABLE cache_entries (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  cache_key TEXT NOT NULL CHECK(length(cache_key) = 64),
  trust_domain TEXT NOT NULL,
  kind TEXT NOT NULL,
  input_sha256 TEXT NOT NULL CHECK(length(input_sha256) = 64),
  object_sha256 TEXT NOT NULL CHECK(length(object_sha256) = 64),
  bytes INTEGER NOT NULL CHECK(bytes >= 0),
  verified INTEGER NOT NULL CHECK(verified IN (0, 1)),
  created_at TEXT NOT NULL,
  last_hit_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  PRIMARY KEY(project_id, cache_key)
) STRICT;
CREATE INDEX cache_entries_retention ON cache_entries(project_id, expires_at);
