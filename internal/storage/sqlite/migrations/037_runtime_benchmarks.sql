CREATE TABLE runtime_benchmarks (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL,
  role TEXT NOT NULL,
  model_family TEXT NOT NULL,
  model_sha256 TEXT NOT NULL DEFAULT '',
  quantization TEXT NOT NULL,
  context_limit INTEGER NOT NULL,
  threads INTEGER NOT NULL,
  batch INTEGER NOT NULL,
  ubatch INTEGER NOT NULL,
  numa TEXT NOT NULL,
  runtime_identity_sha256 TEXT NOT NULL,
  prompt_tokens_second REAL NOT NULL,
  decode_tokens_second REAL NOT NULL,
  duration_millis INTEGER NOT NULL,
  memory_bytes INTEGER NOT NULL,
  healthy INTEGER NOT NULL,
  quality_fixtures_json TEXT NOT NULL,
  quality_status TEXT NOT NULL,
  determinism_status TEXT NOT NULL,
  cache_mode TEXT NOT NULL,
  cache_identity_sha256 TEXT NOT NULL DEFAULT '',
  experimental_features_json TEXT NOT NULL,
  recommendation TEXT NOT NULL,
  reason TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX runtime_benchmarks_profile_created_idx ON runtime_benchmarks(profile_id, created_at DESC);
CREATE INDEX runtime_benchmarks_created_idx ON runtime_benchmarks(created_at DESC);

CREATE TRIGGER runtime_benchmarks_no_update
BEFORE UPDATE ON runtime_benchmarks
BEGIN
  SELECT RAISE(ABORT, 'runtime_benchmarks are append-only');
END;

CREATE TRIGGER runtime_benchmarks_no_delete
BEFORE DELETE ON runtime_benchmarks
BEGIN
  SELECT RAISE(ABORT, 'runtime_benchmarks are append-only');
END;
