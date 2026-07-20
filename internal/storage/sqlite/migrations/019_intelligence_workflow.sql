ALTER TABLE verification_differentials ADD COLUMN purpose TEXT NOT NULL DEFAULT 'unspecified';
ALTER TABLE cache_entries ADD COLUMN quota_bytes INTEGER NOT NULL DEFAULT 536870912 CHECK(quota_bytes > 0);
ALTER TABLE cache_entries ADD COLUMN last_result TEXT NOT NULL DEFAULT 'miss' CHECK(last_result IN ('hit', 'miss'));
ALTER TABLE cache_entries ADD COLUMN result_reason TEXT NOT NULL DEFAULT 'verified object registered';

CREATE UNIQUE INDEX verification_baselines_identity
  ON verification_baselines(project_id, revision, config_sha256, toolchain_id, pack_set_sha256);

CREATE UNIQUE INDEX verification_differentials_identity
  ON verification_differentials(baseline_id, candidate_sha, purpose);

CREATE UNIQUE INDEX test_impact_identity
  ON test_impact_records(project_id, revision);
