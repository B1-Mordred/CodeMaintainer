CREATE TABLE policy_test_runs (
  id TEXT PRIMARY KEY,
  bundle_id TEXT NOT NULL REFERENCES policy_bundles(id) ON DELETE RESTRICT,
  bundle_version TEXT NOT NULL,
  source_sha256 TEXT NOT NULL CHECK(length(source_sha256) = 64),
  formatted_source_sha256 TEXT NOT NULL CHECK(length(formatted_source_sha256) = 64),
  status TEXT NOT NULL CHECK(status IN ('passed','failed')),
  tests_json TEXT NOT NULL CHECK(json_valid(tests_json)),
  results_json TEXT NOT NULL CHECK(json_valid(results_json)),
  coverage_json TEXT NOT NULL CHECK(json_valid(coverage_json)),
  errors_json TEXT NOT NULL CHECK(json_valid(errors_json)),
  actor_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX policy_test_runs_bundle_created_idx
ON policy_test_runs(bundle_id, created_at DESC, id);

CREATE TRIGGER policy_test_runs_no_update BEFORE UPDATE ON policy_test_runs BEGIN
  SELECT RAISE(ABORT, 'policy test runs are append-only');
END;

CREATE TRIGGER policy_test_runs_no_delete BEFORE DELETE ON policy_test_runs BEGIN
  SELECT RAISE(ABORT, 'policy test runs are append-only');
END;
