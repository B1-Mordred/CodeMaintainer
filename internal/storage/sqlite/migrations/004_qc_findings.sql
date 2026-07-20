CREATE TABLE qc_findings (
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  id TEXT NOT NULL,
  severity TEXT NOT NULL,
  category TEXT NOT NULL,
  claim TEXT NOT NULL,
  location TEXT NOT NULL,
  required_resolution TEXT NOT NULL,
  verification_method TEXT NOT NULL,
  status TEXT NOT NULL,
  first_seen_cycle INTEGER NOT NULL CHECK(first_seen_cycle >= 0),
  last_seen_cycle INTEGER NOT NULL CHECK(last_seen_cycle >= first_seen_cycle),
  version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(job_id, id)
);

CREATE TABLE qc_finding_observations (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id TEXT NOT NULL,
  finding_id TEXT NOT NULL,
  review_cycle INTEGER NOT NULL CHECK(review_cycle >= 0),
  report TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(job_id, finding_id) REFERENCES qc_findings(job_id, id) ON DELETE RESTRICT
);

CREATE TABLE qc_finding_transitions (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id TEXT NOT NULL,
  finding_id TEXT NOT NULL,
  from_status TEXT NOT NULL,
  to_status TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  actor_role TEXT NOT NULL,
  rationale TEXT NOT NULL,
  reauthenticated INTEGER NOT NULL CHECK(reauthenticated IN (0, 1)),
  created_at TEXT NOT NULL,
  FOREIGN KEY(job_id, finding_id) REFERENCES qc_findings(job_id, id) ON DELETE RESTRICT
);

CREATE INDEX qc_findings_job_status_idx ON qc_findings(job_id, status, id);
CREATE INDEX qc_observations_finding_idx ON qc_finding_observations(job_id, finding_id, sequence);

CREATE TRIGGER qc_finding_observations_no_update BEFORE UPDATE ON qc_finding_observations BEGIN
  SELECT RAISE(ABORT, 'QC finding observations are append-only');
END;
CREATE TRIGGER qc_finding_observations_no_delete BEFORE DELETE ON qc_finding_observations BEGIN
  SELECT RAISE(ABORT, 'QC finding observations are append-only');
END;
CREATE TRIGGER qc_finding_transitions_no_update BEFORE UPDATE ON qc_finding_transitions BEGIN
  SELECT RAISE(ABORT, 'QC finding transitions are append-only');
END;
CREATE TRIGGER qc_finding_transitions_no_delete BEFORE DELETE ON qc_finding_transitions BEGIN
  SELECT RAISE(ABORT, 'QC finding transitions are append-only');
END;
