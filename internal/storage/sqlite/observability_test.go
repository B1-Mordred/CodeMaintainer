package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/observability"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func TestObservabilityEventsAndSupportBundlesPersistAppendOnlyRedacted(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	event, err := observability.NewEvent(observability.RecordEventRequest{
		Component: "policy", Kind: observability.KindSpan, Name: "decision", Severity: observability.SeverityInfo,
		Attributes: []byte(`{"bundle":"default","secret_token":"do-not-store"}`), DurationMillis: 5,
	}, "operator", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.RecordObservabilityEvent(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored.Attributes), "do-not-store") || stored.RedactionCount == 0 {
		t.Fatalf("event was not redacted before storage: %#v", stored)
	}
	events, err := store.ListObservabilityEvents(ctx, "policy", 10)
	if err != nil || len(events) != 1 || events[0].ID != stored.ID {
		t.Fatalf("events = %#v, %v", events, err)
	}
	bundle, err := observability.NewSupportBundle(events, observability.CreateSupportBundleRequest{Reason: "debug policy", Sections: []string{"recent_telemetry"}}, "operator", time.Unix(2, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	storedBundle, err := store.RecordSupportBundle(ctx, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if storedBundle.BundleSHA256 == "" || strings.Contains(string(storedBundle.Manifest), "do-not-store") {
		t.Fatalf("support bundle is not redacted/reproducible: %#v", storedBundle)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE observability_events SET name='changed' WHERE id=?", stored.ID); err == nil {
		t.Fatal("append-only observability event update unexpectedly succeeded")
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM support_bundles WHERE id=?", storedBundle.ID); err == nil {
		t.Fatal("append-only support bundle delete unexpectedly succeeded")
	}
}

func TestObservabilityStorageRejectsExternalExportByDefault(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.RecordObservabilityEvent(ctx, observability.Event{
		ID: "obsevent_bad", SchemaVersion: observability.SchemaVersion, TraceID: "trace", SpanID: "span",
		CorrelationID: "trace", Component: "controller", Kind: observability.KindSpan, Name: "bad",
		Severity: observability.SeverityInfo, Attributes: []byte(`{}`), ExternalExported: true,
		ExternalEndpoint: "https://collector.example", ActorID: "operator",
	})
	if !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("expected invalid external export, got %v", err)
	}
}
