ALTER TABLE config_revisions ADD COLUMN reason TEXT NOT NULL DEFAULT '';

CREATE TABLE job_leases (
  job_id TEXT PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
  owner_id TEXT NOT NULL,
  acquired_at TEXT NOT NULL,
  heartbeat_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE INDEX job_leases_expiry_idx ON job_leases(expires_at);
