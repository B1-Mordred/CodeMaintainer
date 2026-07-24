package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/B1-Mordred/CodeMaintainer/internal/observability"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func (s *Store) RecordObservabilityEvent(ctx context.Context, event observability.Event) (observability.Event, error) {
	if event.ID == "" {
		id, err := NewID("obsevent")
		if err != nil {
			return observability.Event{}, err
		}
		event.ID = id
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = s.now()
	}
	if err := event.Validate(); err != nil {
		return observability.Event{}, storage.ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO observability_events(
		id,schema_version,trace_id,span_id,correlation_id,component,kind,name,severity,attributes_json,
		redaction_count,duration_millis,queue_millis,retry_count,resource_bytes,external_exported,external_endpoint,actor_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		event.ID, event.SchemaVersion, event.TraceID, event.SpanID, event.CorrelationID, event.Component,
		event.Kind, event.Name, event.Severity, string(event.Attributes), event.RedactionCount,
		event.DurationMillis, event.QueueMillis, event.RetryCount, event.ResourceBytes, boolInt(event.ExternalExported),
		event.ExternalEndpoint, event.ActorID, event.CreatedAt.Format(timestampFormat))
	if err != nil {
		return observability.Event{}, err
	}
	return event, nil
}

func (s *Store) ListObservabilityEvents(ctx context.Context, component string, limit int) ([]observability.Event, error) {
	query := `SELECT id,schema_version,trace_id,span_id,correlation_id,component,kind,name,severity,attributes_json,
		redaction_count,duration_millis,queue_millis,retry_count,resource_bytes,external_exported,external_endpoint,actor_id,created_at
		FROM observability_events`
	args := []any{}
	if component != "" {
		query += " WHERE component=?"
		args = append(args, component)
	}
	query += " ORDER BY created_at DESC,id DESC LIMIT ?"
	args = append(args, boundedLimit(limit, 100, 500))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []observability.Event{}
	for rows.Next() {
		item, err := scanObservabilityEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RecordSupportBundle(ctx context.Context, bundle observability.SupportBundle) (observability.SupportBundle, error) {
	if bundle.ID == "" {
		id, err := NewID("supportbundle")
		if err != nil {
			return observability.SupportBundle{}, err
		}
		bundle.ID = id
	}
	if bundle.CreatedAt.IsZero() {
		bundle.CreatedAt = s.now()
	}
	if err := bundle.Validate(); err != nil {
		return observability.SupportBundle{}, storage.ErrInvalid
	}
	sections, _ := json.Marshal(bundle.Sections)
	_, err := s.db.ExecContext(ctx, `INSERT INTO support_bundles(
		id,schema_version,status,reason,sections_json,redaction_policy,manifest_json,manifest_sha256,bundle_sha256,bytes,actor_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		bundle.ID, bundle.SchemaVersion, bundle.Status, bundle.Reason, string(sections), bundle.RedactionPolicy,
		string(bundle.Manifest), bundle.ManifestSHA256, bundle.BundleSHA256, bundle.Bytes, bundle.ActorID,
		bundle.CreatedAt.Format(timestampFormat))
	if err != nil {
		return observability.SupportBundle{}, err
	}
	return bundle, nil
}

func (s *Store) ListSupportBundles(ctx context.Context, limit int) ([]observability.SupportBundle, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,schema_version,status,reason,sections_json,redaction_policy,
		manifest_json,manifest_sha256,bundle_sha256,bytes,actor_id,created_at FROM support_bundles
		ORDER BY created_at DESC,id DESC LIMIT ?`, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []observability.SupportBundle{}
	for rows.Next() {
		item, err := scanSupportBundle(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanObservabilityEvent(row scanner) (observability.Event, error) {
	var event observability.Event
	var attributes, created string
	var externalExported int
	err := row.Scan(&event.ID, &event.SchemaVersion, &event.TraceID, &event.SpanID, &event.CorrelationID,
		&event.Component, &event.Kind, &event.Name, &event.Severity, &attributes, &event.RedactionCount,
		&event.DurationMillis, &event.QueueMillis, &event.RetryCount, &event.ResourceBytes, &externalExported,
		&event.ExternalEndpoint, &event.ActorID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return observability.Event{}, storage.ErrNotFound
	}
	if err != nil {
		return observability.Event{}, err
	}
	event.Attributes = json.RawMessage(attributes)
	event.ExternalExported = externalExported == 1
	createdAt, err := parseTime(created)
	if err != nil {
		return observability.Event{}, err
	}
	event.CreatedAt = createdAt
	return event, nil
}

func scanSupportBundle(row scanner) (observability.SupportBundle, error) {
	var bundle observability.SupportBundle
	var sections, manifest, created string
	err := row.Scan(&bundle.ID, &bundle.SchemaVersion, &bundle.Status, &bundle.Reason, &sections, &bundle.RedactionPolicy,
		&manifest, &bundle.ManifestSHA256, &bundle.BundleSHA256, &bundle.Bytes, &bundle.ActorID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return observability.SupportBundle{}, storage.ErrNotFound
	}
	if err != nil {
		return observability.SupportBundle{}, err
	}
	if err := json.Unmarshal([]byte(sections), &bundle.Sections); err != nil {
		return observability.SupportBundle{}, err
	}
	bundle.Manifest = json.RawMessage(manifest)
	createdAt, err := parseTime(created)
	if err != nil {
		return observability.SupportBundle{}, err
	}
	bundle.CreatedAt = createdAt
	return bundle, nil
}
