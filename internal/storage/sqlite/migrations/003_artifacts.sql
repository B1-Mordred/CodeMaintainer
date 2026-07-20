CREATE TABLE artifact_objects (
  sha256 TEXT PRIMARY KEY,
  bytes INTEGER NOT NULL CHECK(bytes >= 0),
  relative_path TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL
);

CREATE TABLE job_artifacts (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  project_id TEXT NOT NULL,
  object_sha256 TEXT NOT NULL REFERENCES artifact_objects(sha256) ON DELETE RESTRICT,
  kind TEXT NOT NULL,
  media_type TEXT NOT NULL,
  producer TEXT NOT NULL,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE INDEX job_artifacts_job_created_idx ON job_artifacts(job_id, created_at, id);
CREATE INDEX job_artifacts_project_created_idx ON job_artifacts(project_id, created_at, id);

CREATE TRIGGER artifact_objects_no_update
BEFORE UPDATE ON artifact_objects BEGIN
  SELECT RAISE(ABORT, 'artifact objects are immutable');
END;

CREATE TRIGGER job_artifacts_no_update
BEFORE UPDATE ON job_artifacts BEGIN
  SELECT RAISE(ABORT, 'job artifacts are immutable');
END;
