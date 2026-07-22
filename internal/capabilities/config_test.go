package capabilities

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeConfigurationFillsTypedDefaultsAndRejectsUnknownOrDuplicateFields(t *testing.T) {
	catalog, err := BuiltInCatalog()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := catalog.Get("r-statistical-validation", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := NormalizeConfiguration(manifest, json.RawMessage(`{"tolerance":{"absolute":0.01},"random":{"seed":41},"golden":{"dataset_reference":"tests/golden"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(normalized), `"absolute":0.01`) || !strings.Contains(string(normalized), `"relative":0.000001`) || !strings.Contains(string(normalized), `"seed":41`) {
		t.Fatalf("normalized configuration did not fill safe defaults: %s", normalized)
	}
	for _, raw := range []string{
		`{"tolerance":{"absolute":2}}`,
		`{"tolerance":{"absolute":0.1,"absolute":0.2}}`,
		`{"unknown":true}`,
		`{"golden":{"dataset_reference":"../secret"}}`,
		`[]`,
	} {
		if _, err := NormalizeConfiguration(manifest, json.RawMessage(raw)); err == nil {
			t.Fatalf("unsafe configuration accepted: %s", raw)
		}
	}
}

func TestNormalizeSecurityConfigurationRequiresCompleteExpiringSuppression(t *testing.T) {
	catalog, err := BuiltInCatalog()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := catalog.Get("sbom-fmea-security", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeConfiguration(manifest, json.RawMessage(`{"suppression":{"reference":"reviewed-one"}}`)); err == nil {
		t.Fatal("suppression without reason and expiry accepted")
	}
	valid := json.RawMessage(`{"suppression":{"reference":"reviewed-one","reason":"accepted upstream-only false positive","expires_at":"2026-08-01T00:00:00Z"}}`)
	if _, err := NormalizeConfiguration(manifest, valid); err != nil {
		t.Fatal(err)
	}
}
