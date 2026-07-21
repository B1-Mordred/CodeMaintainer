CREATE TABLE capability_installations (
  pack_id TEXT PRIMARY KEY,
  pack_version TEXT NOT NULL,
  checksum_sha256 TEXT NOT NULL CHECK(length(checksum_sha256) = 64),
  state TEXT NOT NULL CHECK(state IN ('enabled', 'disabled')),
  pinned INTEGER NOT NULL CHECK(pinned IN (0, 1)),
  revision INTEGER NOT NULL CHECK(revision > 0),
  previous_version TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE capability_lifecycle_events (
  id TEXT PRIMARY KEY,
  pack_id TEXT NOT NULL,
  from_version TEXT NOT NULL DEFAULT '',
  to_version TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL CHECK(action IN ('install', 'enable', 'disable', 'upgrade', 'rollback', 'pin', 'unpin')),
  actor_id TEXT NOT NULL,
  reason TEXT NOT NULL,
  created_at TEXT NOT NULL
) STRICT;
CREATE INDEX capability_lifecycle_events_pack ON capability_lifecycle_events(pack_id, created_at DESC);

CREATE TABLE capability_assignments (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  pack_id TEXT NOT NULL,
  pack_version TEXT NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0, 1)),
  config_json TEXT NOT NULL CHECK(json_valid(config_json)),
  revision INTEGER NOT NULL CHECK(revision > 0),
  updated_at TEXT NOT NULL,
  PRIMARY KEY(project_id, pack_id)
) STRICT;

CREATE TABLE repo_doctor_scans (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  repository TEXT NOT NULL,
  revision TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state = 'complete'),
  findings_json TEXT NOT NULL CHECK(json_valid(findings_json)),
  drift_json TEXT NOT NULL CHECK(json_valid(drift_json)),
  files_observed INTEGER NOT NULL CHECK(files_observed >= 0),
  excluded_files INTEGER NOT NULL CHECK(excluded_files >= 0),
  created_at TEXT NOT NULL
) STRICT;
CREATE INDEX repo_doctor_scans_project ON repo_doctor_scans(project_id, created_at DESC);

CREATE TABLE repo_doctor_proposals (
  id TEXT PRIMARY KEY,
  scan_id TEXT NOT NULL REFERENCES repo_doctor_scans(id) ON DELETE RESTRICT,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL CHECK(kind IN ('capability_pack', 'command_allowlist', 'protected_path', 'project_profile')),
  proposal_key TEXT NOT NULL,
  value_json TEXT NOT NULL CHECK(json_valid(value_json)),
  confidence INTEGER NOT NULL CHECK(confidence BETWEEN 1 AND 100),
  evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)),
  state TEXT NOT NULL CHECK(state IN ('pending', 'accepted', 'rejected')),
  version INTEGER NOT NULL CHECK(version > 0),
  reason TEXT NOT NULL DEFAULT '',
  reviewed_by TEXT NOT NULL DEFAULT '',
  reviewed_at TEXT
) STRICT;
CREATE INDEX repo_doctor_proposals_scan ON repo_doctor_proposals(scan_id, id);
