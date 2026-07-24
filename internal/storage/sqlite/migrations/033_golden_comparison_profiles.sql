CREATE TABLE golden_comparison_profiles (
  id TEXT PRIMARY KEY,
  report_id TEXT NOT NULL REFERENCES golden_rehearsal_reports(id) ON DELETE CASCADE,
  comparison_id TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  actor_role TEXT NOT NULL CHECK(actor_role IN ('reviewer','administrator')),
  reason TEXT NOT NULL,
  reauthenticated INTEGER NOT NULL CHECK(reauthenticated=1),
  tolerance_json TEXT NOT NULL CHECK(json_valid(tolerance_json)),
  mask_json TEXT NOT NULL CHECK(json_valid(mask_json)),
  created_at TEXT NOT NULL
);

CREATE INDEX golden_comparison_profiles_report_created_idx
ON golden_comparison_profiles(report_id, created_at DESC, id);

CREATE INDEX golden_comparison_profiles_report_comparison_created_idx
ON golden_comparison_profiles(report_id, comparison_id, created_at DESC, id);

CREATE TRIGGER golden_comparison_profiles_no_update BEFORE UPDATE ON golden_comparison_profiles BEGIN
  SELECT RAISE(ABORT, 'golden comparison profiles are append-only');
END;

CREATE TRIGGER golden_comparison_profiles_no_delete BEFORE DELETE ON golden_comparison_profiles BEGIN
  SELECT RAISE(ABORT, 'golden comparison profiles are append-only');
END;
