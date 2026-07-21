package intelligence_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/local-code-maintainer/appliance/internal/intelligence"
)

func TestRemoteAnalyzerValidatesHashBoundFacts(t *testing.T) {
	content := []byte("package fixture\n")
	digest := sha256.Sum256(content)
	unsafe := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request intelligence.RemoteAnalysisRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		path := request.Path
		if unsafe {
			path = "../outside.go"
		}
		_ = json.NewEncoder(w).Encode(intelligence.BlobAnalysis{BlobSHA256: hex.EncodeToString(digest[:]), ParserID: request.ParserID, Language: "go", Classification: "source", Bytes: int64(len(content)), Symbols: []intelligence.Symbol{{ID: "symbol_fixture", Name: "Fixture", Kind: "type", Path: path, StartLine: 1, EndLine: 1, Confidence: 100}}, Relations: []intelligence.Relation{}, Providers: []intelligence.AnalysisProvider{{ID: "go-ast@1.25.12", Capability: "syntax", Status: "complete"}}})
	}))
	defer server.Close()
	analyzer, err := intelligence.NewRemoteAnalyzer(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	analysis := analyzer.Analyze(t.Context(), "fixture.go", "controller-syntax-v2", content)
	if analysis.Failure != "" || len(analysis.Symbols) != 1 {
		t.Fatalf("valid remote facts = %#v", analysis)
	}
	unsafe = true
	analysis = analyzer.Analyze(t.Context(), "fixture.go", "controller-syntax-v2", content)
	if analysis.Failure == "" || len(analysis.Symbols) != 0 || analysis.Providers[0].Status != "unavailable" {
		t.Fatalf("unsafe remote facts were accepted: %#v", analysis)
	}
}
