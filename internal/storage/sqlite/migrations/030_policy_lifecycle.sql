CREATE TABLE policy_bundles (
  id TEXT PRIMARY KEY,
  schema_version INTEGER NOT NULL CHECK(schema_version = 1),
  version TEXT NOT NULL,
  source_kind TEXT NOT NULL CHECK(source_kind IN ('structured_template','advanced_rego')),
  source_sha256 TEXT NOT NULL CHECK(length(source_sha256) = 64),
  compiled_sha256 TEXT NOT NULL CHECK(length(compiled_sha256) = 64),
  structured_rules_json TEXT NOT NULL CHECK(json_valid(structured_rules_json)),
  rego_source TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL,
  reason TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX policy_bundles_created_idx
ON policy_bundles(created_at DESC, id);

CREATE TABLE policy_activations (
  id TEXT PRIMARY KEY,
  bundle_id TEXT NOT NULL REFERENCES policy_bundles(id) ON DELETE RESTRICT,
  bundle_version TEXT NOT NULL,
  action TEXT NOT NULL CHECK(action IN ('activate','rollback')),
  previous_bundle_id TEXT NOT NULL DEFAULT '',
  actor_id TEXT NOT NULL,
  actor_role TEXT NOT NULL,
  reason TEXT NOT NULL,
  staged_rollout_percent INTEGER NOT NULL CHECK(staged_rollout_percent BETWEEN 0 AND 100),
  reauthenticated INTEGER NOT NULL CHECK(reauthenticated IN (0,1)),
  created_at TEXT NOT NULL
);

CREATE INDEX policy_activations_created_idx
ON policy_activations(created_at DESC, id);

CREATE TABLE policy_simulations (
  id TEXT PRIMARY KEY,
  bundle_id TEXT NOT NULL REFERENCES policy_bundles(id) ON DELETE RESTRICT,
  bundle_version TEXT NOT NULL,
  decision_point TEXT NOT NULL,
  input_sha256 TEXT NOT NULL CHECK(length(input_sha256) = 64),
  redacted_input_json TEXT NOT NULL CHECK(json_valid(redacted_input_json)),
  decision_json TEXT NOT NULL CHECK(json_valid(decision_json)),
  status TEXT NOT NULL CHECK(status IN ('passed','failed')),
  errors_json TEXT NOT NULL CHECK(json_valid(errors_json)),
  actor_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX policy_simulations_created_idx
ON policy_simulations(created_at DESC, id);

CREATE TABLE policy_decisions (
  id TEXT PRIMARY KEY,
  bundle_id TEXT NOT NULL REFERENCES policy_bundles(id) ON DELETE RESTRICT,
  bundle_version TEXT NOT NULL,
  job_id TEXT REFERENCES jobs(id) ON DELETE RESTRICT,
  decision_point TEXT NOT NULL,
  input_sha256 TEXT NOT NULL CHECK(length(input_sha256) = 64),
  redacted_input_json TEXT NOT NULL CHECK(json_valid(redacted_input_json)),
  outcome TEXT NOT NULL CHECK(outcome IN ('allow','deny')),
  allowed INTEGER NOT NULL CHECK(allowed IN (0,1)),
  required_stages_json TEXT NOT NULL CHECK(json_valid(required_stages_json)),
  explanation TEXT NOT NULL,
  fail_closed INTEGER NOT NULL CHECK(fail_closed IN (0,1)),
  created_at TEXT NOT NULL
);

CREATE INDEX policy_decisions_job_created_idx
ON policy_decisions(job_id, created_at DESC, id);

CREATE INDEX policy_decisions_created_idx
ON policy_decisions(created_at DESC, id);

CREATE TRIGGER policy_bundles_no_update BEFORE UPDATE ON policy_bundles BEGIN
  SELECT RAISE(ABORT, 'policy bundles are append-only');
END;

CREATE TRIGGER policy_bundles_no_delete BEFORE DELETE ON policy_bundles BEGIN
  SELECT RAISE(ABORT, 'policy bundles are append-only');
END;

CREATE TRIGGER policy_activations_no_update BEFORE UPDATE ON policy_activations BEGIN
  SELECT RAISE(ABORT, 'policy activations are append-only');
END;

CREATE TRIGGER policy_activations_no_delete BEFORE DELETE ON policy_activations BEGIN
  SELECT RAISE(ABORT, 'policy activations are append-only');
END;

CREATE TRIGGER policy_simulations_no_update BEFORE UPDATE ON policy_simulations BEGIN
  SELECT RAISE(ABORT, 'policy simulations are append-only');
END;

CREATE TRIGGER policy_simulations_no_delete BEFORE DELETE ON policy_simulations BEGIN
  SELECT RAISE(ABORT, 'policy simulations are append-only');
END;

CREATE TRIGGER policy_decisions_no_update BEFORE UPDATE ON policy_decisions BEGIN
  SELECT RAISE(ABORT, 'policy decisions are append-only');
END;

CREATE TRIGGER policy_decisions_no_delete BEFORE DELETE ON policy_decisions BEGIN
  SELECT RAISE(ABORT, 'policy decisions are append-only');
END;
