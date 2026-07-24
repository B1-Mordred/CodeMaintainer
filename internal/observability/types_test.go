package observability

import (
	"strings"
	"testing"
	"time"
)

func TestEventRedactsSecretsPromptsBodiesAndReasoningBeforeStorage(t *testing.T) {
	event, err := NewEvent(RecordEventRequest{
		Component: "model-provider", Kind: KindSpan, Name: "remote call", Severity: SeverityInfo,
		Attributes:     []byte(`{"safe":"latency","authorization_header":"Bearer secret","prompt":"sensitive prompt","request_body":{"source_content":"package main","ok":true},"nested":{"hidden_reasoning":"nope"},"long":"` + strings.Repeat("x", 3000) + `"}`),
		DurationMillis: 12, ResourceBytes: 2048,
	}, "operator", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	payload := string(event.Attributes)
	for _, forbidden := range []string{"Bearer secret", "sensitive prompt", "package main", "nope"} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("attribute payload leaked %q: %s", forbidden, payload)
		}
	}
	if event.RedactionCount < 4 || event.ExternalExported {
		t.Fatalf("redaction/export policy not enforced: %#v", event)
	}
}

func TestSupportBundleManifestIsBoundedRedactedAndReviewOnly(t *testing.T) {
	event, err := NewEvent(RecordEventRequest{Component: "scheduler", Kind: KindMetric, Name: "queue", Attributes: []byte(`{"token":"secret","queue_ms":12}`)}, "operator", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	event.ID = "obsevent_fixture"
	bundle, err := NewSupportBundle([]Event{event}, CreateSupportBundleRequest{Reason: "debug slow queue", Sections: []string{"recent_telemetry", "system_status"}}, "operator", time.Unix(2, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(bundle.Manifest)
	if !strings.Contains(manifest, `"external_otlp_enabled":false`) || strings.Contains(manifest, "secret\"") || bundle.ManifestSHA256 == "" || bundle.BundleSHA256 == "" {
		t.Fatalf("support bundle manifest is not safe/reproducible: %#v manifest=%s", bundle, manifest)
	}
}
