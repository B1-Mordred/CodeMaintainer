CREATE TABLE task_contracts (
  job_id TEXT PRIMARY KEY REFERENCES jobs(id) ON DELETE RESTRICT,
  schema_version INTEGER NOT NULL CHECK(schema_version = 1),
  version INTEGER NOT NULL CHECK(version > 0),
  status TEXT NOT NULL CHECK(status IN ('draft','approved')),
  source_kind TEXT NOT NULL CHECK(source_kind IN ('free_form','issue','forge_object')),
  source_ref TEXT NOT NULL DEFAULT '',
  requested_behavior TEXT NOT NULL,
  explicit_non_goals TEXT NOT NULL,
  affected_users TEXT NOT NULL,
  acceptance_criteria TEXT NOT NULL,
  constraints_json TEXT NOT NULL,
  likely_components TEXT NOT NULL,
  likely_risks TEXT NOT NULL,
  required_evidence TEXT NOT NULL,
  required_documentation TEXT NOT NULL,
  assumptions TEXT NOT NULL,
  questions TEXT NOT NULL,
  completion_checklist TEXT NOT NULL,
  contract_sha256 TEXT NOT NULL,
  approved_by TEXT NOT NULL DEFAULT '',
  approved_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE task_contract_events (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  version INTEGER NOT NULL CHECK(version > 0),
  action TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  actor_role TEXT NOT NULL,
  reason TEXT NOT NULL,
  details TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE INDEX task_contract_events_job_idx
ON task_contract_events(job_id, created_at DESC, id);

CREATE TRIGGER task_contract_events_no_update BEFORE UPDATE ON task_contract_events BEGIN
  SELECT RAISE(ABORT, 'task contract events are append-only');
END;
CREATE TRIGGER task_contract_events_no_delete BEFORE DELETE ON task_contract_events BEGIN
  SELECT RAISE(ABORT, 'task contract events are append-only');
END;

CREATE TABLE risk_assessments (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  schema_version INTEGER NOT NULL CHECK(schema_version = 1),
  contract_version INTEGER NOT NULL CHECK(contract_version > 0),
  contract_sha256 TEXT NOT NULL,
  level TEXT NOT NULL CHECK(level IN ('low','medium','high')),
  score INTEGER NOT NULL CHECK(score >= 0),
  signals TEXT NOT NULL,
  routing TEXT NOT NULL,
  policy_decision TEXT NOT NULL,
  explanation TEXT NOT NULL,
  requested_by_operator INTEGER NOT NULL CHECK(requested_by_operator IN (0,1)),
  superseded_by TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE INDEX risk_assessments_job_created_idx
ON risk_assessments(job_id, created_at DESC, id);

CREATE TRIGGER risk_assessments_no_delete BEFORE DELETE ON risk_assessments BEGIN
  SELECT RAISE(ABORT, 'risk assessments are append-only');
END;

CREATE TABLE risk_waivers (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  assessment_id TEXT NOT NULL REFERENCES risk_assessments(id) ON DELETE RESTRICT,
  from_level TEXT NOT NULL CHECK(from_level IN ('low','medium','high')),
  to_level TEXT NOT NULL CHECK(to_level IN ('low','medium','high')),
  reason TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  actor_role TEXT NOT NULL,
  reauthenticated INTEGER NOT NULL CHECK(reauthenticated IN (0,1)),
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX risk_waivers_job_created_idx
ON risk_waivers(job_id, created_at DESC, id);

CREATE TRIGGER risk_waivers_no_update BEFORE UPDATE ON risk_waivers BEGIN
  SELECT RAISE(ABORT, 'risk waivers are append-only');
END;
CREATE TRIGGER risk_waivers_no_delete BEFORE DELETE ON risk_waivers BEGIN
  SELECT RAISE(ABORT, 'risk waivers are append-only');
END;
