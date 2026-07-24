package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	storesqlite "github.com/B1-Mordred/CodeMaintainer/internal/storage/sqlite"
)

func TestObservabilityAPIRedactsEventsAndCreatesSupportBundle(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server := httptest.NewServer(NewServer(store, slog.New(slog.NewTextHandler(io.Discard, nil)), "mock"))
	defer server.Close()
	body := `{"component":"model-provider","kind":"span","name":"remote request","severity":"info","attributes":{"safe":"ok","authorization_header":"Bearer no","prompt":"do not store"},"duration_millis":10}`
	response, err := http.Post(server.URL+"/api/v1/observability/events", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("event status = %d", response.StatusCode)
	}
	var eventResponse struct {
		Event struct {
			Attributes     map[string]any `json:"attributes"`
			RedactionCount int            `json:"redaction_count"`
		} `json:"event"`
	}
	if err := json.NewDecoder(response.Body).Decode(&eventResponse); err != nil {
		t.Fatal(err)
	}
	if eventResponse.Event.RedactionCount < 2 || eventResponse.Event.Attributes["authorization_header"] != "[REDACTED]" {
		t.Fatalf("event was not redacted: %#v", eventResponse.Event)
	}
	bundleResponse, err := http.Post(server.URL+"/api/v1/observability/support-bundles", "application/json", bytes.NewBufferString(`{"reason":"debug model latency","sections":["recent_telemetry","system_status"]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer bundleResponse.Body.Close()
	if bundleResponse.StatusCode != http.StatusCreated {
		t.Fatalf("support bundle status = %d", bundleResponse.StatusCode)
	}
	var bundle struct {
		Bundle struct {
			Manifest       map[string]any `json:"manifest"`
			BundleSHA256   string         `json:"bundle_sha256"`
			ManifestSHA256 string         `json:"manifest_sha256"`
		} `json:"bundle"`
	}
	if err := json.NewDecoder(bundleResponse.Body).Decode(&bundle); err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(bundle.Bundle.Manifest)
	if bundle.Bundle.BundleSHA256 == "" || bundle.Bundle.ManifestSHA256 == "" || strings.Contains(string(manifest), "Bearer no") {
		t.Fatalf("unsafe support bundle response: %#v", bundle.Bundle)
	}
}
