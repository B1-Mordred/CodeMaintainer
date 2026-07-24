CREATE TABLE provider_profiles (
  id TEXT PRIMARY KEY,
  schema_version INTEGER NOT NULL CHECK(schema_version = 1),
  interface_family TEXT NOT NULL,
  display_name TEXT NOT NULL,
  trust_tier TEXT NOT NULL,
  remote INTEGER NOT NULL CHECK(remote IN (0,1)),
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  approved_data_classes_json TEXT NOT NULL CHECK(json_valid(approved_data_classes_json)),
  credential_ref TEXT NOT NULL DEFAULT '',
  credential_configured INTEGER NOT NULL CHECK(credential_configured IN (0,1)),
  operator_assertions_json TEXT NOT NULL CHECK(json_valid(operator_assertions_json)),
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE endpoint_profiles (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_profiles(id) ON DELETE RESTRICT,
  base_url TEXT NOT NULL,
  region TEXT NOT NULL DEFAULT '',
  network_zone TEXT NOT NULL,
  allow_private_address INTEGER NOT NULL CHECK(allow_private_address IN (0,1)),
  tls_mode TEXT NOT NULL,
  redirect_policy TEXT NOT NULL,
  dns_policy TEXT NOT NULL,
  timeout_millis INTEGER NOT NULL,
  health_check_path TEXT NOT NULL,
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE model_profiles (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_profiles(id) ON DELETE RESTRICT,
  endpoint_id TEXT NOT NULL REFERENCES endpoint_profiles(id) ON DELETE RESTRICT,
  model_id TEXT NOT NULL,
  display_name TEXT NOT NULL,
  role_eligibility_json TEXT NOT NULL CHECK(json_valid(role_eligibility_json)),
  capabilities_json TEXT NOT NULL CHECK(json_valid(capabilities_json)),
  context_limit INTEGER NOT NULL,
  output_limit INTEGER NOT NULL,
  input_price_per_mtok REAL NOT NULL,
  output_price_per_mtok REAL NOT NULL,
  quality_status TEXT NOT NULL,
  capability_override INTEGER NOT NULL CHECK(capability_override IN (0,1)),
  override_reason TEXT NOT NULL DEFAULT '',
  override_expires_at TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE route_profiles (
  id TEXT PRIMARY KEY,
  role TEXT NOT NULL,
  preference TEXT NOT NULL,
  ordered_model_ids_json TEXT NOT NULL CHECK(json_valid(ordered_model_ids_json)),
  allowed_data_classes_json TEXT NOT NULL CHECK(json_valid(allowed_data_classes_json)),
  max_tokens_per_request INTEGER NOT NULL,
  max_cost_usd REAL NOT NULL,
  retry_budget INTEGER NOT NULL,
  fallback_policy TEXT NOT NULL,
  batch_policy TEXT NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE provider_egress_manifests (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL DEFAULT '',
  project_id TEXT NOT NULL,
  route_id TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  endpoint_id TEXT NOT NULL,
  model_profile_id TEXT NOT NULL,
  purpose TEXT NOT NULL,
  data_classes_json TEXT NOT NULL CHECK(json_valid(data_classes_json)),
  artifact_ids_json TEXT NOT NULL CHECK(json_valid(artifact_ids_json)),
  redactions_json TEXT NOT NULL CHECK(json_valid(redactions_json)),
  estimated_bytes INTEGER NOT NULL,
  estimated_tokens INTEGER NOT NULL,
  retention TEXT NOT NULL,
  policy_decision TEXT NOT NULL CHECK(policy_decision IN ('allowed','denied')),
  decision_reason TEXT NOT NULL,
  manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256) = 64),
  created_at TEXT NOT NULL
);

CREATE INDEX provider_profiles_updated_idx ON provider_profiles(updated_at DESC, id);
CREATE INDEX endpoint_profiles_provider_idx ON endpoint_profiles(provider_id, updated_at DESC, id);
CREATE INDEX model_profiles_provider_idx ON model_profiles(provider_id, updated_at DESC, id);
CREATE INDEX route_profiles_role_idx ON route_profiles(role, updated_at DESC, id);
CREATE INDEX provider_egress_project_idx ON provider_egress_manifests(project_id, created_at DESC, id);
CREATE INDEX provider_egress_job_idx ON provider_egress_manifests(job_id, created_at DESC, id);

CREATE TRIGGER provider_egress_manifests_no_update BEFORE UPDATE ON provider_egress_manifests BEGIN
  SELECT RAISE(ABORT, 'provider egress manifests are append-only');
END;

CREATE TRIGGER provider_egress_manifests_no_delete BEFORE DELETE ON provider_egress_manifests BEGIN
  SELECT RAISE(ABORT, 'provider egress manifests are append-only');
END;
