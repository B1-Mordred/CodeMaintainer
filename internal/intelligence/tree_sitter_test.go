//go:build cgo

package intelligence_test

import (
	"context"
	"errors"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/intelligence"
)

func TestPinnedTreeSitterSyntaxAndProvenance(t *testing.T) {
	analyzer := intelligence.SyntaxAnalyzer{}
	for _, fixture := range []struct {
		path, source, language, provider string
	}{
		{"service.py", "class Service:\n    def run(self):\n        return 1\n", "python", "tree-sitter-python@0.25.0"},
		{"service.php", "<?php\nclass Service { public function run(): int { return 1; } }\n", "php", "tree-sitter-php@0.24.2"},
		{"service.ts", "export class Service { run(): number { return 1 } }\n", "typescript", "tree-sitter-typescript@0.23.2"},
		{"service.R", "run <- function(x) { x + 1 }\n", "r", "tree-sitter-r@1.3.0"},
	} {
		analysis := analyzer.Analyze(context.Background(), fixture.path, "controller-syntax-v2", []byte(fixture.source))
		if analysis.Language != fixture.language || analysis.Failure != "" || len(analysis.Providers) != 1 || analysis.Providers[0].ID != fixture.provider || analysis.Providers[0].Status != "complete" {
			t.Errorf("%s analysis = %#v", fixture.path, analysis)
		}
	}
	malformed := analyzer.Analyze(context.Background(), "broken.py", "controller-syntax-v2", []byte("def broken(:\n"))
	if malformed.Failure == "" || len(malformed.Providers) != 1 || malformed.Providers[0].Status != "partial" {
		t.Fatalf("malformed syntax was not explicit partial evidence: %#v", malformed)
	}
}

type fixtureEnricher struct {
	id, capability string
	facts          intelligence.ReadOnlyEnrichment
	err            error
}

func (fixture fixtureEnricher) ID() string         { return fixture.id }
func (fixture fixtureEnricher) Capability() string { return fixture.capability }
func (fixture fixtureEnricher) Enrich(context.Context, intelligence.ReadOnlyEnrichmentRequest) (intelligence.ReadOnlyEnrichment, error) {
	return fixture.facts, fixture.err
}

func TestReadOnlySCIPAndLSPEnrichmentIsBounded(t *testing.T) {
	valid := fixtureEnricher{id: "scip-go-v1", capability: "scip", facts: intelligence.ReadOnlyEnrichment{Relations: []intelligence.Relation{{From: "service.go", To: "pkg.Target", Kind: "reference", Confidence: 90}}}}
	analysis := (intelligence.SyntaxAnalyzer{Enrichers: []intelligence.ReadOnlyEnricher{valid}}).Analyze(context.Background(), "service.go", "controller-syntax-v2", []byte("package service\n"))
	if analysis.Failure != "" || len(analysis.Relations) != 1 || analysis.Providers[len(analysis.Providers)-1].Status != "complete" {
		t.Fatalf("valid SCIP evidence = %#v", analysis)
	}
	unsafe := fixtureEnricher{id: "lsp-go-v1", capability: "read_only_lsp", facts: intelligence.ReadOnlyEnrichment{Symbols: []intelligence.Symbol{{ID: "escaped", Name: "Escaped", Kind: "type", Path: "../outside.go", StartLine: 1, EndLine: 1, Confidence: 100}}}, err: nil}
	analysis = (intelligence.SyntaxAnalyzer{Enrichers: []intelligence.ReadOnlyEnricher{unsafe}}).Analyze(context.Background(), "service.go", "controller-syntax-v2", []byte("package service\n"))
	if analysis.Failure == "" || analysis.Providers[len(analysis.Providers)-1].Status != "rejected" {
		t.Fatalf("unsafe LSP evidence was accepted: %#v", analysis)
	}
	failing := fixtureEnricher{id: "scip-failed", capability: "scip", err: errors.New("indexer detail must not escape")}
	analysis = (intelligence.SyntaxAnalyzer{Enrichers: []intelligence.ReadOnlyEnricher{failing}}).Analyze(context.Background(), "service.go", "controller-syntax-v2", []byte("package service\n"))
	if analysis.Failure == "" || analysis.Failure == failing.err.Error() {
		t.Fatalf("enricher error was absent or leaked: %#v", analysis)
	}
}
