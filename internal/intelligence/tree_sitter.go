//go:build cgo

package intelligence

import (
	"context"
	"path"
	"sort"
	"strconv"

	treesitterr "github.com/r-lib/tree-sitter-r/bindings/go"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	treesittercsharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	treesitterc "github.com/tree-sitter/tree-sitter-c/bindings/go"
	treesittercpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	treesitterjavascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	treesitterphp "github.com/tree-sitter/tree-sitter-php/bindings/go"
	treesitterpython "github.com/tree-sitter/tree-sitter-python/bindings/go"
	treesitterrust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	treesittertypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// SyntaxAnalyzer uses an exact native Go AST and pinned Tree-sitter grammars
// for the remaining supported languages. Lexical facts supplement, but never
// substitute for, Tree-sitter syntax validity.
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

// ReadOnlyEnricher is the only SCIP/LSP extension seam. It receives one
// bounded file, not a worktree or command channel, and returns facts that the
// controller validates before persistence.
type ReadOnlyEnricher interface {
	ID() string
	Capability() string
	Enrich(context.Context, ReadOnlyEnrichmentRequest) (ReadOnlyEnrichment, error)
}

func (analyzer SyntaxAnalyzer) Analyze(ctx context.Context, filePath, parserID string, content []byte) BlobAnalysis {
	base := (LexicalAnalyzer{}).Analyze(ctx, filePath, parserID, content)
	if base.Language == "go" || base.Classification != "source" || base.Language == "text" || base.Classification == "binary" {
		return analyzer.enrich(ctx, filePath, parserID, content, base)
	}
	language, grammarID := syntaxLanguage(base.Language, path.Ext(filePath))
	if language == nil {
		base.Failure = "no pinned Tree-sitter grammar is registered for this source language"
		return analyzer.enrich(ctx, filePath, parserID, content, base)
	}
	parser := treesitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(language); err != nil {
		base.Failure = "pinned Tree-sitter grammar is incompatible with the parser runtime"
		return analyzer.enrich(ctx, filePath, parserID, content, base)
	}
	if ctx.Err() != nil {
		base.Failure = "syntax analysis cancelled"
		return analyzer.enrich(ctx, filePath, parserID, content, base)
	}
	tree := parser.Parse(content, nil)
	if tree == nil {
		base.Failure = "Tree-sitter did not return a syntax tree"
		return analyzer.enrich(ctx, filePath, parserID, content, base)
	}
	defer tree.Close()
	base.Providers = append(base.Providers, AnalysisProvider{ID: grammarID, Capability: "syntax", Status: "complete"})
	root := tree.RootNode()
	if root.HasError() {
		base.Failure = "Tree-sitter syntax contains error or missing nodes"
		base.Providers[len(base.Providers)-1].Status = "partial"
	}
	extractTreeSymbols(filePath, content, root, &base)
	return analyzer.enrich(ctx, filePath, parserID, content, base)
}

func (analyzer SyntaxAnalyzer) enrich(ctx context.Context, filePath, parserID string, content []byte, analysis BlobAnalysis) BlobAnalysis {
	for _, enricher := range analyzer.Enrichers {
		provider := AnalysisProvider{ID: enricher.ID(), Capability: enricher.Capability(), Status: "complete"}
		if ValidateIdentity(provider.ID) != nil || (provider.Capability != "scip" && provider.Capability != "read_only_lsp") {
			provider.Status = "rejected"
			analysis.Providers = append(analysis.Providers, provider)
			if analysis.Failure == "" {
				analysis.Failure = "read-only enrichment provider identity or capability was rejected"
			}
			continue
		}
		facts, err := enricher.Enrich(ctx, ReadOnlyEnrichmentRequest{Path: filePath, Language: analysis.Language, ParserID: parserID, Content: append([]byte(nil), content...)})
		if err != nil || !validEnrichment(filePath, content, facts) {
			provider.Status = "rejected"
			analysis.Providers = append(analysis.Providers, provider)
			if analysis.Failure == "" {
				analysis.Failure = "read-only SCIP/LSP enrichment evidence failed bounded validation"
			}
			continue
		}
		analysis.Symbols = append(analysis.Symbols, facts.Symbols...)
		analysis.Relations = append(analysis.Relations, facts.Relations...)
		analysis.Providers = append(analysis.Providers, provider)
	}
	return analysis
}

func validEnrichment(filePath string, content []byte, facts ReadOnlyEnrichment) bool {
	if len(facts.Symbols) > 10_000 || len(facts.Relations) > 20_000 {
		return false
	}
	lineCount := 1
	for _, value := range content {
		if value == '\n' {
			lineCount++
		}
	}
	for _, symbol := range facts.Symbols {
		if symbol.ID == "" || symbol.Name == "" || len(symbol.Name) > 512 || symbol.Path != filePath || symbol.StartLine < 1 || symbol.EndLine < symbol.StartLine || symbol.EndLine > lineCount || symbol.Confidence < 0 || symbol.Confidence > 100 {
			return false
		}
	}
	for _, relation := range facts.Relations {
		if relation.From == "" || relation.To == "" || relation.Kind == "" || len(relation.From)+len(relation.To)+len(relation.Kind) > 4096 || relation.Confidence < 0 || relation.Confidence > 100 {
			return false
		}
	}
	return true
}

func syntaxLanguage(language, extension string) (*treesitter.Language, string) {
	switch language {
	case "python":
		return treesitter.NewLanguage(treesitterpython.Language()), "tree-sitter-python@0.25.0"
	case "javascript":
		return treesitter.NewLanguage(treesitterjavascript.Language()), "tree-sitter-javascript@0.25.0"
	case "typescript":
		if extension == ".tsx" {
			return treesitter.NewLanguage(treesittertypescript.LanguageTSX()), "tree-sitter-tsx@0.23.2"
		}
		return treesitter.NewLanguage(treesittertypescript.LanguageTypescript()), "tree-sitter-typescript@0.23.2"
	case "php":
		return treesitter.NewLanguage(treesitterphp.LanguagePHP()), "tree-sitter-php@0.24.2"
	case "rust":
		return treesitter.NewLanguage(treesitterrust.Language()), "tree-sitter-rust@0.24.2"
	case "c":
		return treesitter.NewLanguage(treesitterc.Language()), "tree-sitter-c@0.24.2"
	case "cpp":
		return treesitter.NewLanguage(treesittercpp.Language()), "tree-sitter-cpp@0.23.4"
	case "csharp":
		return treesitter.NewLanguage(treesittercsharp.Language()), "tree-sitter-c-sharp@0.23.5"
	case "r":
		return treesitter.NewLanguage(treesitterr.Language()), "tree-sitter-r@1.3.0"
	default:
		return nil, ""
	}
}

var treeDeclarationKinds = map[string]string{
	"function_definition": "function", "function_declaration": "function", "method_declaration": "method",
	"class_declaration": "class", "class_definition": "class", "interface_declaration": "interface",
	"type_alias_declaration": "type", "type_definition": "type", "struct_item": "struct", "struct_specifier": "struct",
	"enum_item": "enum", "enum_specifier": "enum", "trait_item": "trait",
}

func extractTreeSymbols(filePath string, content []byte, root *treesitter.Node, analysis *BlobAnalysis) {
	seen := make(map[string]bool, len(analysis.Symbols))
	for _, symbol := range analysis.Symbols {
		seen[symbol.Name+"\x00"+symbol.Kind+"\x00"+strconv.Itoa(symbol.StartLine)] = true
	}
	remaining := 200_000
	var walk func(*treesitter.Node)
	walk = func(node *treesitter.Node) {
		if node == nil || remaining <= 0 {
			return
		}
		remaining--
		if kind, ok := treeDeclarationKinds[node.Kind()]; ok {
			nameNode := node.ChildByFieldName("name")
			if nameNode == nil {
				nameNode = findIdentifier(node, 0)
			}
			if nameNode != nil && nameNode.EndByte() <= uint(len(content)) {
				name := string(content[nameNode.StartByte():nameNode.EndByte()])
				start, end := int(node.StartPosition().Row)+1, int(node.EndPosition().Row)+1
				key := name + "\x00" + kind + "\x00" + strconv.Itoa(start)
				if name != "" && !seen[key] {
					analysis.Symbols = append(analysis.Symbols, Symbol{ID: symbolID(filePath, name, start), Name: name, Kind: kind, Path: filePath, StartLine: start, EndLine: end, Confidence: 95})
					seen[key] = true
				}
			}
		}
		for index := uint(0); index < node.NamedChildCount(); index++ {
			walk(node.NamedChild(index))
		}
	}
	walk(root)
	if remaining <= 0 && analysis.Failure == "" {
		analysis.Failure = "Tree-sitter node traversal exceeded the bounded node count"
	}
	sort.SliceStable(analysis.Symbols, func(i, j int) bool {
		if analysis.Symbols[i].Path == analysis.Symbols[j].Path && analysis.Symbols[i].StartLine == analysis.Symbols[j].StartLine {
			return analysis.Symbols[i].Name < analysis.Symbols[j].Name
		}
		return analysis.Symbols[i].StartLine < analysis.Symbols[j].StartLine
	})
}

func findIdentifier(node *treesitter.Node, depth int) *treesitter.Node {
	if node == nil || depth > 8 {
		return nil
	}
	switch node.Kind() {
	case "identifier", "type_identifier", "name", "constant":
		return node
	}
	for index := uint(0); index < node.NamedChildCount(); index++ {
		if found := findIdentifier(node.NamedChild(index), depth+1); found != nil {
			return found
		}
	}
	return nil
}
