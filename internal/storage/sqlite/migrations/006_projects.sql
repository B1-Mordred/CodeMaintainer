CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  repository TEXT NOT NULL,
  default_branch TEXT NOT NULL,
  local_remote_name TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX projects_provider_repository_idx
ON projects(provider, repository);
