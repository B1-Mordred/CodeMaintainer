package intelligence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type RemoteAnalyzer struct {
	endpoint string
	client   *http.Client
}

type RemoteAnalysisRequest struct {
	Path          string `json:"path"`
	ParserID      string `json:"parser_id"`
	Content       []byte `json:"content"`
	ContentSHA256 string `json:"content_sha256"`
}

func NewRemoteAnalyzer(endpoint string) (*RemoteAnalyzer, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, errors.New("code-intelligence endpoint must be an exact internal HTTP origin")
	}
	return &RemoteAnalyzer{endpoint: strings.TrimSuffix(parsed.String(), "/"), client: &http.Client{Timeout: 20 * time.Second}}, nil
}

func (analyzer *RemoteAnalyzer) Analyze(ctx context.Context, filePath, parserID string, content []byte) BlobAnalysis {
	digest := sha256.Sum256(content)
	payload, _ := json.Marshal(RemoteAnalysisRequest{Path: filePath, ParserID: parserID, Content: content, ContentSHA256: hex.EncodeToString(digest[:])})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, analyzer.endpoint+"/analyze", bytes.NewReader(payload))
	if err != nil {
		return remoteFailure(filePath, parserID)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := analyzer.client.Do(request)
	if err != nil {
		return remoteFailure(filePath, parserID)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return remoteFailure(filePath, parserID)
	}
	var analysis BlobAnalysis
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&analysis) != nil || analysis.BlobSHA256 != hex.EncodeToString(digest[:]) || analysis.ParserID != parserID || analysis.Language != languageForPath(filePath) || analysis.Classification != classificationForPath(filePath) || analysis.Bytes != int64(len(content)) || !validRemoteFacts(filePath, content, analysis) {
		return remoteFailure(filePath, parserID)
	}
	return analysis
}

func remoteFailure(filePath, parserID string) BlobAnalysis {
	return BlobAnalysis{ParserID: parserID, Language: languageForPath(filePath), Classification: classificationForPath(filePath), Symbols: []Symbol{}, Relations: []Relation{}, Failure: "isolated syntax service was unavailable or returned invalid bounded evidence", Providers: []AnalysisProvider{{ID: "isolated-tree-sitter-service", Capability: "syntax", Status: "unavailable"}}}
}

func validRemoteFacts(filePath string, content []byte, analysis BlobAnalysis) bool {
	if len(analysis.Symbols) > 10_000 || len(analysis.Relations) > 20_000 || len(analysis.Providers) > 32 {
		return false
	}
	lines := bytes.Count(content, []byte("\n")) + 1
	for _, symbol := range analysis.Symbols {
		if symbol.ID == "" || symbol.Name == "" || symbol.Path != filePath || symbol.StartLine < 1 || symbol.EndLine < symbol.StartLine || symbol.EndLine > lines || symbol.Confidence < 0 || symbol.Confidence > 100 {
			return false
		}
	}
	for _, relation := range analysis.Relations {
		if relation.From == "" || relation.To == "" || relation.Kind == "" || strings.ContainsRune(relation.From+relation.To+relation.Kind, 0) || len(relation.From)+len(relation.To)+len(relation.Kind) > 4096 || relation.Confidence < 0 || relation.Confidence > 100 {
			return false
		}
	}
	validStatus := map[string]bool{"complete": true, "partial": true, "rejected": true, "unavailable": true}
	validCapability := map[string]bool{"syntax": true, "scip": true, "read_only_lsp": true}
	for _, provider := range analysis.Providers {
		if ValidateIdentity(provider.ID) != nil || !validCapability[provider.Capability] || !validStatus[provider.Status] {
			return false
		}
	}
	return true
}
