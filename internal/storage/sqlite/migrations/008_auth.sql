CREATE TABLE users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL COLLATE NOCASE UNIQUE,
  display_name TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL CHECK(role IN ('viewer', 'operator', 'reviewer', 'administrator')),
  disabled INTEGER NOT NULL DEFAULT 0 CHECK(disabled IN (0, 1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE auth_bootstrap (
  singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
  completed_by TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  completed_at TEXT NOT NULL
);

CREATE TABLE auth_sessions (
  id TEXT PRIMARY KEY,
  token_hash BLOB NOT NULL UNIQUE CHECK(length(token_hash) = 32),
  csrf_hash BLOB NOT NULL CHECK(length(csrf_hash) = 32),
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  remote_address TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  reauthenticated_until TEXT,
  revoked_at TEXT
);

CREATE INDEX auth_sessions_user_expiry_idx ON auth_sessions(user_id, expires_at DESC);
CREATE INDEX auth_sessions_expiry_idx ON auth_sessions(expires_at);
