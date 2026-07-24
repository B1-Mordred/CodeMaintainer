CREATE TABLE evaluation_datasets (
  id TEXT PRIMARY KEY,
  schema_version INTEGER NOT NULL CHECK(schema_version = 1),
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  source_kind TEXT NOT NULL CHECK(source_kind IN ('curated_fixtures','historical_range')),
  repository TEXT NOT NULL,
  base_revision TEXT NOT NULL,
  target_revision TEXT NOT NULL,
  known_patch_sha256 TEXT NOT NULL CHECK(length(known_patch_sha256)=64),
  hidden_patch_sha256 TEXT NOT NULL CHECK(length(hidden_patch_sha256)=64),
  exclusions_json TEXT NOT NULL CHECK(json_valid(exclusions_json)),
  scoring_profile TEXT NOT NULL,
  retention_days INTEGER NOT NULL CHECK(retention_days BETWEEN 1 AND 3650),
  reproducibility_key TEXT NOT NULL CHECK(length(reproducibility_key)=64),
  metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json)),
  actor_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX evaluation_datasets_reproducibility_idx ON evaluation_datasets(project_id, reproducibility_key);
CREATE INDEX evaluation_datasets_project_created_idx ON evaluation_datasets(project_id, created_at, id);

CREATE TABLE evaluation_runs (
  id TEXT PRIMARY KEY,
  schema_version INTEGER NOT NULL CHECK(schema_version = 1),
  dataset_id TEXT NOT NULL REFERENCES evaluation_datasets(id) ON DELETE RESTRICT,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  status TEXT NOT NULL CHECK(status IN ('queued','completed','rejected')),
  profile_matrix_json TEXT NOT NULL CHECK(json_valid(profile_matrix_json)),
  isolated_memory_namespace TEXT NOT NULL CHECK(isolated_memory_namespace LIKE 'eval://memory/%'),
  isolated_cache_namespace TEXT NOT NULL CHECK(isolated_cache_namespace LIKE 'eval://cache/%'),
  budget_seconds INTEGER NOT NULL CHECK(budget_seconds BETWEEN 60 AND 604800),
  concurrency INTEGER NOT NULL CHECK(concurrency BETWEEN 1 AND 8),
  scoring_profile TEXT NOT NULL,
  results_json TEXT NOT NULL CHECK(json_valid(results_json)),
  report_sha256 TEXT NOT NULL CHECK(length(report_sha256)=64),
  promotion_recommendation TEXT NOT NULL CHECK(promotion_recommendation IN ('review_only','rejected')),
  reason TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX evaluation_runs_dataset_created_idx ON evaluation_runs(dataset_id, created_at, id);
CREATE INDEX evaluation_runs_project_created_idx ON evaluation_runs(project_id, created_at, id);

CREATE TRIGGER evaluation_datasets_no_update
BEFORE UPDATE ON evaluation_datasets BEGIN
  SELECT RAISE(ABORT, 'evaluation datasets are append-only');
END;

CREATE TRIGGER evaluation_datasets_no_delete
BEFORE DELETE ON evaluation_datasets BEGIN
  SELECT RAISE(ABORT, 'evaluation datasets are append-only');
END;

CREATE TRIGGER evaluation_runs_no_update
BEFORE UPDATE ON evaluation_runs BEGIN
  SELECT RAISE(ABORT, 'evaluation runs are append-only');
END;

CREATE TRIGGER evaluation_runs_no_delete
BEFORE DELETE ON evaluation_runs BEGIN
  SELECT RAISE(ABORT, 'evaluation runs are append-only');
END;
