CREATE TABLE provider_capability_probes (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES provider_profiles(id) ON DELETE RESTRICT,
  endpoint_id TEXT NOT NULL REFERENCES endpoint_profiles(id) ON DELETE RESTRICT,
  model_profile_id TEXT NOT NULL REFERENCES model_profiles(id) ON DELETE RESTRICT,
  interface_family TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('passed','failed')),
  observed_model_id TEXT NOT NULL,
  native_api_shape TEXT NOT NULL,
  capabilities_json TEXT NOT NULL CHECK(json_valid(capabilities_json)),
  request_schema_sha256 TEXT NOT NULL CHECK(length(request_schema_sha256) = 64),
  response_schema_sha256 TEXT NOT NULL CHECK(length(response_schema_sha256) = 64),
  latency_millis INTEGER NOT NULL,
  errors_json TEXT NOT NULL CHECK(json_valid(errors_json)),
  actor_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX provider_capability_probes_model_idx
ON provider_capability_probes(model_profile_id, created_at DESC, id);

CREATE INDEX provider_capability_probes_created_idx
ON provider_capability_probes(created_at DESC, id);

CREATE TRIGGER provider_capability_probes_no_update BEFORE UPDATE ON provider_capability_probes BEGIN
  SELECT RAISE(ABORT, 'provider capability probes are append-only');
END;

CREATE TRIGGER provider_capability_probes_no_delete BEFORE DELETE ON provider_capability_probes BEGIN
  SELECT RAISE(ABORT, 'provider capability probes are append-only');
END;
