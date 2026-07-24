CREATE TABLE observability_events (
  id TEXT PRIMARY KEY,
  schema_version INTEGER NOT NULL CHECK(schema_version = 1),
  trace_id TEXT NOT NULL,
  span_id TEXT NOT NULL,
  correlation_id TEXT NOT NULL,
  component TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('span','metric','log')),
  name TEXT NOT NULL,
  severity TEXT NOT NULL CHECK(severity IN ('info','warn','error')),
  attributes_json TEXT NOT NULL CHECK(json_valid(attributes_json)),
  redaction_count INTEGER NOT NULL CHECK(redaction_count >= 0),
  duration_millis INTEGER NOT NULL CHECK(duration_millis >= 0),
  queue_millis INTEGER NOT NULL CHECK(queue_millis >= 0),
  retry_count INTEGER NOT NULL CHECK(retry_count >= 0),
  resource_bytes INTEGER NOT NULL CHECK(resource_bytes >= 0),
  external_exported INTEGER NOT NULL CHECK(external_exported = 0),
  external_endpoint TEXT NOT NULL CHECK(external_endpoint = ''),
  actor_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX observability_events_component_created_idx ON observability_events(component, created_at, id);
CREATE INDEX observability_events_trace_idx ON observability_events(trace_id, span_id);

CREATE TABLE support_bundles (
  id TEXT PRIMARY KEY,
  schema_version INTEGER NOT NULL CHECK(schema_version = 1),
  status TEXT NOT NULL CHECK(status = 'ready'),
  reason TEXT NOT NULL,
  sections_json TEXT NOT NULL CHECK(json_valid(sections_json)),
  redaction_policy TEXT NOT NULL,
  manifest_json TEXT NOT NULL CHECK(json_valid(manifest_json)),
  manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64),
  bundle_sha256 TEXT NOT NULL CHECK(length(bundle_sha256)=64),
  bytes INTEGER NOT NULL CHECK(bytes BETWEEN 1 AND 65536),
  actor_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX support_bundles_created_idx ON support_bundles(created_at, id);

CREATE TRIGGER observability_events_no_update
BEFORE UPDATE ON observability_events BEGIN
  SELECT RAISE(ABORT, 'observability events are append-only');
END;

CREATE TRIGGER observability_events_no_delete
BEFORE DELETE ON observability_events BEGIN
  SELECT RAISE(ABORT, 'observability events are append-only');
END;

CREATE TRIGGER support_bundles_no_update
BEFORE UPDATE ON support_bundles BEGIN
  SELECT RAISE(ABORT, 'support bundles are append-only');
END;

CREATE TRIGGER support_bundles_no_delete
BEFORE DELETE ON support_bundles BEGIN
  SELECT RAISE(ABORT, 'support bundles are append-only');
END;
