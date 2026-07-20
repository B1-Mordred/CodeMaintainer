package main

import (
	"strings"
	"testing"
)

func TestReadJSONRejectsBytesBeyondLimit(t *testing.T) {
	payload := `"` + strings.Repeat("a", maxConfigDocumentBytes-2) + `" `
	if _, err := readJSON(strings.NewReader(payload)); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized valid-prefix JSON returned %v", err)
	}
}

func TestReadJSONAcceptsBoundedDocument(t *testing.T) {
	payload, err := readJSON(strings.NewReader(`{"schema_version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"schema_version":1}` {
		t.Fatalf("unexpected payload %s", payload)
	}
}
