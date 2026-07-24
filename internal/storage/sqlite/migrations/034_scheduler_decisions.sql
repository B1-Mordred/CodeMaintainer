CREATE TABLE scheduler_decisions (
  id TEXT PRIMARY KEY,
  mode TEXT NOT NULL CHECK(mode IN ('quality_latency','throughput_batching')),
  selected_job_id TEXT,
  selected_project_id TEXT,
  selected_profile_id TEXT,
  status TEXT NOT NULL CHECK(status IN ('scheduled','deferred','rejected')),
  reason TEXT NOT NULL,
  rejected_job_ids_json TEXT NOT NULL CHECK(json_valid(rejected_job_ids_json)),
  deferred_job_ids_json TEXT NOT NULL CHECK(json_valid(deferred_job_ids_json)),
  co_residence_safe INTEGER NOT NULL CHECK(co_residence_safe IN (0,1)),
  fairness_applied INTEGER NOT NULL CHECK(fairness_applied IN (0,1)),
  model_batch_group TEXT NOT NULL DEFAULT '',
  resource_summary TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX scheduler_decisions_created_idx
ON scheduler_decisions(created_at DESC, id);

CREATE TRIGGER scheduler_decisions_no_update BEFORE UPDATE ON scheduler_decisions BEGIN
  SELECT RAISE(ABORT, 'scheduler decisions are append-only');
END;

CREATE TRIGGER scheduler_decisions_no_delete BEFORE DELETE ON scheduler_decisions BEGIN
  SELECT RAISE(ABORT, 'scheduler decisions are append-only');
END;
