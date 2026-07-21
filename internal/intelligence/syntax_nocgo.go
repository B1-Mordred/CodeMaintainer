//go:build !cgo

package intelligence

import "context"

// SyntaxAnalyzer is a non-CGO compatibility fallback. Production controller
// composition uses RemoteAnalyzer and the isolated CGO parser service.
type SyntaxAnalyzer struct {
	Enrichers []ReadOnlyEnricher
}

type ReadOnlyEnrichmentRequest struct {
	Path     string
	Language string
	ParserID string
	Content  []byte
}

type ReadOnlyEnrichment struct {
	Symbols   []Symbol
	Relations []Relation
}

type ReadOnlyEnricher interface {
	ID() string
	Capability() string
	Enrich(context.Context, ReadOnlyEnrichmentRequest) (ReadOnlyEnrichment, error)
}

func (SyntaxAnalyzer) Analyze(ctx context.Context, filePath, parserID string, content []byte) BlobAnalysis {
	analysis := (LexicalAnalyzer{}).Analyze(ctx, filePath, parserID, content)
	if analysis.Language != "go" && analysis.Language != "text" && analysis.Classification == "source" {
		analysis.Providers = append(analysis.Providers, AnalysisProvider{ID: "isolated-tree-sitter-service", Capability: "syntax", Status: "unavailable"})
		if analysis.Failure == "" {
			analysis.Failure = "isolated Tree-sitter parser service is not configured in this non-CGO process"
		}
	}
	return analysis
}
