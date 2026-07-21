CREATE TABLE baseline_supersessions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  baseline_id TEXT NOT NULL REFERENCES verification_baselines(id) ON DELETE RESTRICT,
  differential_id TEXT NOT NULL REFERENCES verification_differentials(id) ON DELETE RESTRICT,
  replacement_baseline_id TEXT NOT NULL REFERENCES verification_baselines(id) ON DELETE RESTRICT,
  actor_id TEXT NOT NULL,
  reason TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(project_id, baseline_id, differential_id)
) STRICT;

CREATE TABLE differential_corrections (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  differential_id TEXT NOT NULL REFERENCES verification_differentials(id) ON DELETE RESTRICT,
  observation_kind TEXT NOT NULL,
  observation_key TEXT NOT NULL,
  before_classification TEXT NOT NULL CHECK(before_classification IN ('pre_existing','resolved','newly_introduced','changed','indeterminate')),
  after_classification TEXT NOT NULL CHECK(after_classification IN ('pre_existing','resolved','newly_introduced','changed','indeterminate')),
  actor_id TEXT NOT NULL,
  reason TEXT NOT NULL,
  created_at TEXT NOT NULL
) STRICT;
CREATE INDEX differential_corrections_history ON differential_corrections(project_id, differential_id, created_at, id);

CREATE TABLE test_impact_overrides (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  impact_id TEXT NOT NULL REFERENCES test_impact_records(id) ON DELETE RESTRICT,
  test_id TEXT NOT NULL,
  selected INTEGER NOT NULL CHECK(selected IN (0,1)),
  actor_id TEXT NOT NULL,
  reason TEXT NOT NULL,
  expires_at TEXT,
  created_at TEXT NOT NULL
) STRICT;
CREATE INDEX test_impact_overrides_history ON test_impact_overrides(project_id, impact_id, created_at, id);
