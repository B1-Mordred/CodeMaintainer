CREATE TABLE evidence_nodes (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  subject_id TEXT NOT NULL,
  subject_sha256 TEXT NOT NULL CHECK(length(subject_sha256)=64),
  label TEXT NOT NULL,
  producer TEXT NOT NULL,
  metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json)),
  metadata_sha256 TEXT NOT NULL CHECK(length(metadata_sha256)=64),
  created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX evidence_nodes_subject_idx ON evidence_nodes(job_id, kind, subject_id);
CREATE INDEX evidence_nodes_job_created_idx ON evidence_nodes(job_id, created_at, id);
CREATE INDEX evidence_nodes_project_created_idx ON evidence_nodes(project_id, created_at, id);

CREATE TABLE evidence_edges (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  project_id TEXT NOT NULL,
  from_node_id TEXT NOT NULL REFERENCES evidence_nodes(id) ON DELETE RESTRICT,
  to_node_id TEXT NOT NULL REFERENCES evidence_nodes(id) ON DELETE RESTRICT,
  relationship TEXT NOT NULL,
  reason TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json)),
  created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX evidence_edges_relation_idx ON evidence_edges(job_id, from_node_id, to_node_id, relationship);
CREATE INDEX evidence_edges_job_created_idx ON evidence_edges(job_id, created_at, id);
CREATE INDEX evidence_edges_project_created_idx ON evidence_edges(project_id, created_at, id);

CREATE TRIGGER evidence_nodes_no_update
BEFORE UPDATE ON evidence_nodes BEGIN
  SELECT RAISE(ABORT, 'evidence nodes are append-only');
END;

CREATE TRIGGER evidence_nodes_no_delete
BEFORE DELETE ON evidence_nodes BEGIN
  SELECT RAISE(ABORT, 'evidence nodes are append-only');
END;

CREATE TRIGGER evidence_edges_no_update
BEFORE UPDATE ON evidence_edges BEGIN
  SELECT RAISE(ABORT, 'evidence edges are append-only');
END;

CREATE TRIGGER evidence_edges_no_delete
BEFORE DELETE ON evidence_edges BEGIN
  SELECT RAISE(ABORT, 'evidence edges are append-only');
END;
