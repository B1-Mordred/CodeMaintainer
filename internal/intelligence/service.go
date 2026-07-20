package intelligence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxIndexFiles = 10_000
	maxIndexBytes = 64 << 20
	maxFileBytes  = 2 << 20
)

type Analyzer interface {
	Analyze(context.Context, string, string, []byte) BlobAnalysis
}

type Service struct {
	store    Store
	analyzer Analyzer
}

func NewService(store Store, analyzer Analyzer) (*Service, error) {
	if store == nil {
		return nil, errors.New("intelligence store is required")
	}
	if analyzer == nil {
		analyzer = LexicalAnalyzer{}
	}
	return &Service{store: store, analyzer: analyzer}, nil
}

func (service *Service) Index(ctx context.Context, request IndexRequest) (IndexRun, error) {
	if request.CacheRetentionDays == 0 {
		request.CacheRetentionDays = 30
	}
	if request.CacheQuotaBytes == 0 {
		request.CacheQuotaBytes = 512 << 20
	}
	if err := validateIndexRequest(request); err != nil {
		return IndexRun{}, err
	}
	analyses := make([]BlobAnalysis, 0, len(request.Files))
	allAnalyses := make([]BlobAnalysis, 0, len(request.Files))
	files := make([]IndexedFile, 0, len(request.Files))
	for _, file := range request.Files {
		if ctx.Err() != nil {
			if len(files) == 0 {
				return IndexRun{}, ctx.Err()
			}
			request.Files = request.Files[:len(files)]
			request.TerminalState = "cancelled"
			break
		}
		analysis, found, err := service.store.FindBlobAnalysis(ctx, request.ProjectID, request.Repository, file.BlobSHA256, request.ParserID)
		if err != nil {
			return IndexRun{}, err
		}
		if !found {
			analysis = service.analyzer.Analyze(ctx, file.Path, request.ParserID, file.Content)
			analysis.BlobSHA256 = file.BlobSHA256
			analysis.ParserID = request.ParserID
			analysis.Bytes = int64(len(file.Content))
			analyses = append(analyses, analysis)
		}
		allAnalyses = append(allAnalyses, analysis)
		status := "indexed"
		if analysis.Failure != "" {
			status = "failed"
		}
		files = append(files, IndexedFile{Path: file.Path, BlobSHA256: file.BlobSHA256, Language: analysis.Language, Status: status, Failure: analysis.Failure, Reused: found})
	}
	commitContext := ctx
	cancelCommit := func() {}
	if request.TerminalState == "cancelled" {
		commitContext, cancelCommit = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	}
	defer cancelCommit()
	run, err := service.store.CommitIndex(commitContext, request, analyses, files)
	if err != nil {
		return IndexRun{}, err
	}
	for index, file := range request.Files {
		if err := service.registerParseCache(commitContext, request, file, allAnalyses[index]); err != nil {
			return IndexRun{}, fmt.Errorf("verify parsed-blob cache identity for %q: %w", file.Path, err)
		}
	}
	if run.State != "cancelled" {
		if _, err := service.store.PruneIntelligence(commitContext, request.ProjectID, time.Now().UTC().Add(-time.Duration(request.CacheRetentionDays)*24*time.Hour), time.Now().UTC()); err != nil {
			return IndexRun{}, fmt.Errorf("apply index retention: %w", err)
		}
	}
	return run, nil
}

func (service *Service) registerParseCache(ctx context.Context, request IndexRequest, file SourceFile, analysis BlobAnalysis) error {
	const trustDomain = "trusted-source"
	const kind = "source-parse"
	key, err := NewCacheKey(request.ProjectID, trustDomain, kind, request.Repository, file.BlobSHA256, request.ParserID, fmt.Sprintf("schema-%d", SchemaVersion))
	if err != nil {
		return err
	}
	inputDigest := sha256.Sum256([]byte(strings.Join([]string{request.Repository, file.BlobSHA256, request.ParserID, fmt.Sprintf("schema-%d", SchemaVersion)}, "\x00")))
	payload, err := json.Marshal(analysis)
	if err != nil {
		return err
	}
	objectDigest := sha256.Sum256(payload)
	_, err = service.RegisterCacheEntry(ctx, CacheEntry{
		Key: key, ProjectID: request.ProjectID, TrustDomain: trustDomain, Kind: kind,
		InputSHA256: hex.EncodeToString(inputDigest[:]), ObjectSHA256: hex.EncodeToString(objectDigest[:]),
		Bytes: int64(len(payload)), Verified: true, ExpiresAt: time.Now().UTC().Add(time.Duration(request.CacheRetentionDays) * 24 * time.Hour),
		QuotaBytes: request.CacheQuotaBytes,
	})
	return err
}

func validateIndexRequest(request IndexRequest) error {
	if ValidateIdentity(request.ProjectID) != nil || ValidateIdentity(request.Repository) != nil || ValidateIdentity(request.Revision) != nil || ValidateIdentity(request.ParserID) != nil || request.CacheRetentionDays < 1 || request.CacheRetentionDays > 3650 || request.CacheQuotaBytes < 1<<20 || request.CacheQuotaBytes > 1<<40 || (request.TerminalState != "" && request.TerminalState != "cancelled") {
		return errors.New("project, repository, revision, and parser identities are required and bounded")
	}
	if len(request.Files) == 0 || len(request.Files) > maxIndexFiles {
		return fmt.Errorf("index must contain between 1 and %d files", maxIndexFiles)
	}
	seen := make(map[string]bool, len(request.Files))
	total := 0
	for _, file := range request.Files {
		clean := path.Clean(file.Path)
		if clean != file.Path || clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.ContainsRune(clean, 0) || len(clean) > 1024 || seen[clean] {
			return fmt.Errorf("unsafe or duplicate repository path %q", file.Path)
		}
		seen[clean] = true
		if len(file.Content) > maxFileBytes {
			return fmt.Errorf("file %q exceeds the bounded parser input", file.Path)
		}
		total += len(file.Content)
		if total > maxIndexBytes {
			return errors.New("index input exceeds the bounded revision size")
		}
		digest := sha256.Sum256(file.Content)
		if file.BlobSHA256 != hex.EncodeToString(digest[:]) {
			return fmt.Errorf("file %q does not match its trusted blob hash", file.Path)
		}
	}
	return nil
}

type LexicalAnalyzer struct{}

var genericDeclaration = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:async\s+)?(?:class|interface|type|function|def|fn|struct|enum)\s+([A-Za-z_][A-Za-z0-9_]*)`)
var genericImport = regexp.MustCompile(`(?m)^\s*(?:import|from|require\s*\()\s*["']?([^"'\s;)]+)`)

func (LexicalAnalyzer) Analyze(_ context.Context, filePath, parserID string, content []byte) BlobAnalysis {
	analysis := BlobAnalysis{ParserID: parserID, Language: languageForPath(filePath), Classification: classificationForPath(filePath), Symbols: []Symbol{}, Relations: []Relation{}}
	if bytes.IndexByte(content, 0) >= 0 {
		analysis.Classification = "binary"
		return analysis
	}
	if analysis.Classification == "vendor" || analysis.Classification == "generated" {
		return analysis
	}
	if analysis.Language == "go" {
		return analyzeGo(filePath, parserID, content, analysis)
	}
	lines := bytes.Split(content, []byte("\n"))
	for _, match := range genericDeclaration.FindAllSubmatchIndex(content, -1) {
		name := string(content[match[2]:match[3]])
		line := 1 + bytes.Count(content[:match[0]], []byte("\n"))
		analysis.Symbols = append(analysis.Symbols, Symbol{ID: symbolID(filePath, name, line), Name: name, Kind: "declaration", Path: filePath, StartLine: line, EndLine: line, Confidence: 70})
	}
	for _, match := range genericImport.FindAllSubmatchIndex(content, -1) {
		analysis.Relations = append(analysis.Relations, Relation{From: filePath, To: string(content[match[2]:match[3]]), Kind: "import", Confidence: 60})
	}
	if len(lines) > 100_000 {
		analysis.Failure = "file exceeds bounded line count"
	}
	return analysis
}

func analyzeGo(filePath, parserID string, content []byte, analysis BlobAnalysis) BlobAnalysis {
	set := token.NewFileSet()
	parsed, err := parser.ParseFile(set, filePath, content, parser.SkipObjectResolution)
	if err != nil {
		analysis.Failure = "Go syntax could not be parsed"
		return analysis
	}
	for _, declaration := range parsed.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			start, end := set.Position(typed.Pos()).Line, set.Position(typed.End()).Line
			kind := "function"
			if typed.Recv != nil {
				kind = "method"
			}
			analysis.Symbols = append(analysis.Symbols, Symbol{ID: symbolID(filePath, typed.Name.Name, start), Name: typed.Name.Name, Kind: kind, Path: filePath, StartLine: start, EndLine: end, Confidence: 100})
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				if typeSpec, ok := spec.(*ast.TypeSpec); ok {
					start, end := set.Position(typeSpec.Pos()).Line, set.Position(typeSpec.End()).Line
					analysis.Symbols = append(analysis.Symbols, Symbol{ID: symbolID(filePath, typeSpec.Name.Name, start), Name: typeSpec.Name.Name, Kind: "type", Path: filePath, StartLine: start, EndLine: end, Confidence: 100})
				}
			}
		}
	}
	for _, imported := range parsed.Imports {
		analysis.Relations = append(analysis.Relations, Relation{From: filePath, To: strings.Trim(imported.Path.Value, `"`), Kind: "import", Confidence: 100})
	}
	return analysis
}

func languageForPath(filePath string) string {
	switch strings.ToLower(path.Ext(filePath)) {
	case ".go":
		return "go"
	case ".php":
		return "php"
	case ".cs":
		return "csharp"
	case ".r":
		return "r"
	case ".js", ".jsx":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".hpp":
		return "cpp"
	default:
		return "text"
	}
}

func classificationForPath(filePath string) string {
	lower := strings.ToLower(filePath)
	if strings.Contains(lower, "/vendor/") || strings.Contains(lower, "/node_modules/") || strings.HasPrefix(lower, "vendor/") || strings.HasPrefix(lower, "node_modules/") {
		return "vendor"
	}
	if strings.Contains(lower, ".generated.") || strings.HasSuffix(lower, ".gen.go") || strings.Contains(lower, "/generated/") {
		return "generated"
	}
	return "source"
}

func symbolID(filePath, name string, line int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", filePath, name, line)))
	return "symbol_" + hex.EncodeToString(digest[:16])
}

func (service *Service) Status(ctx context.Context, projectID string) (Status, error) {
	if ValidateIdentity(projectID) != nil {
		return Status{}, errors.New("valid project identity is required")
	}
	return service.store.IntelligenceStatus(ctx, projectID)
}

func (service *Service) Query(ctx context.Context, query Query) (QueryResult, error) {
	query.Term = strings.TrimSpace(query.Term)
	if ValidateIdentity(query.ProjectID) != nil || query.Term == "" || len(query.Term) > 256 {
		return QueryResult{}, errors.New("valid project and bounded query term are required")
	}
	if query.Limit <= 0 || query.Limit > 200 {
		query.Limit = 50
	}
	return service.store.QueryIntelligence(ctx, query)
}

func (service *Service) Rebuild(ctx context.Context, projectID, actorID, reason string) error {
	reason = strings.TrimSpace(reason)
	if ValidateIdentity(projectID) != nil || ValidateIdentity(actorID) != nil || reason == "" || len(reason) > 1000 {
		return errors.New("valid project and actor identities and a bounded audited reason are required")
	}
	return service.store.RebuildIntelligence(ctx, projectID, actorID, reason)
}

func (service *Service) CompileContext(ctx context.Context, projectID, jobID, stage string, budgetTokens, reserveOutput int, candidates []ContextCandidate) (ContextPacket, error) {
	if ValidateIdentity(projectID) != nil || (jobID != "" && ValidateIdentity(jobID) != nil) || ValidateIdentity(stage) != nil || budgetTokens < 256 || reserveOutput < 64 || reserveOutput >= budgetTokens || len(candidates) > 1000 {
		return ContextPacket{}, errors.New("invalid context identity, budget, or candidate count")
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Priority == candidates[j].Priority {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].Priority > candidates[j].Priority
	})
	manifest := ContextManifest{ProjectID: projectID, JobID: jobID, Stage: stage, SchemaVersion: SchemaVersion, BudgetTokens: budgetTokens, ReservedOutputTokens: reserveOutput, Selections: []ContextSelection{}}
	packet := ContextPacket{Content: [][]byte{}}
	seen := map[string]bool{}
	available := budgetTokens - reserveOutput
	for _, candidate := range candidates {
		digest := sha256.Sum256(candidate.Content)
		hash := hex.EncodeToString(digest[:])
		cost := (len(candidate.Content) + 3) / 4
		selection := ContextSelection{ID: candidate.ID, Source: candidate.Source, Version: candidate.Version, Reason: candidate.Reason, Trust: candidate.Trust, SHA256: hash, Bytes: len(candidate.Content), EstimatedTokens: cost, Stale: candidate.Stale}
		switch {
		case candidate.ID == "" || candidate.Source == "" || candidate.Version == "" || candidate.Reason == "" || (candidate.Trust != "trusted" && candidate.Trust != "untrusted" && candidate.Trust != "verified_memory"):
			selection.Exclusion = "invalid metadata"
		case seen[hash]:
			selection.Exclusion = "duplicate content"
		case manifest.UsedTokens+cost > available:
			selection.Exclusion = "budget exhausted"
			manifest.Truncated = true
		default:
			selection.Included = true
			manifest.UsedTokens += cost
			seen[hash] = true
			packet.Content = append(packet.Content, append([]byte(nil), candidate.Content...))
		}
		manifest.Selections = append(manifest.Selections, selection)
	}
	saved, err := service.store.SaveContextManifest(ctx, manifest)
	if err != nil {
		return ContextPacket{}, err
	}
	packet.Manifest = saved
	return packet, nil
}

func CompareObservations(baseline, candidate []Observation) []DifferentialObservation {
	base := make(map[string]Observation, len(baseline))
	current := make(map[string]Observation, len(candidate))
	keys := map[string]bool{}
	for _, item := range baseline {
		base[item.Kind+"\x00"+item.Key] = item
		keys[item.Kind+"\x00"+item.Key] = true
	}
	for _, item := range candidate {
		current[item.Kind+"\x00"+item.Key] = item
		keys[item.Kind+"\x00"+item.Key] = true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	result := make([]DifferentialObservation, 0, len(ordered))
	for _, composite := range ordered {
		before, hadBefore := base[composite]
		after, hasAfter := current[composite]
		parts := strings.SplitN(composite, "\x00", 2)
		item := DifferentialObservation{Kind: parts[0], Key: parts[1]}
		switch {
		case hadBefore && !hasAfter:
			item.Classification, item.Explanation = "resolved", "The baseline observation is absent from the candidate."
		case !hadBefore && hasAfter:
			item.Classification, item.Explanation = "newly_introduced", "The candidate introduced an observation not present in the baseline."
		case before.Status == after.Status && bytes.Equal(before.Value, after.Value):
			item.Classification, item.Explanation = "pre_existing", "Baseline and candidate evidence are equivalent."
		case hadBefore && hasAfter:
			item.Classification, item.Explanation = "changed", "The observation exists in both runs with different status or evidence."
		default:
			item.Classification, item.Explanation = "indeterminate", "The evidence is insufficient for a deterministic classification."
		}
		if hadBefore {
			item.Baseline, _ = json.Marshal(before)
		}
		if hasAfter {
			item.Candidate, _ = json.Marshal(after)
		}
		result = append(result, item)
	}
	return result
}

func NewCacheKey(projectID, trustDomain, kind string, inputs ...string) (string, error) {
	if ValidateIdentity(projectID) != nil || ValidateIdentity(trustDomain) != nil || ValidateIdentity(kind) != nil || len(inputs) == 0 {
		return "", errors.New("cache identity and complete inputs are required")
	}
	hash := sha256.New()
	hash.Write([]byte("cache-v1\x00" + projectID + "\x00" + trustDomain + "\x00" + kind))
	for _, input := range inputs {
		if input == "" {
			return "", errors.New("cache keys cannot omit an input identity")
		}
		hash.Write([]byte("\x00" + input))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func NewTestImpact(projectID, revision string, changed []string, tests map[string][]string, mediumOrHighRisk bool) (TestImpact, error) {
	if ValidateIdentity(projectID) != nil || ValidateIdentity(revision) != nil || len(changed) == 0 {
		return TestImpact{}, errors.New("project, revision, and changed symbols are required")
	}
	changedSet := map[string]bool{}
	for _, symbol := range changed {
		changedSet[symbol] = true
	}
	ordered := make([]string, 0, len(tests))
	for testID := range tests {
		ordered = append(ordered, testID)
	}
	sort.Strings(ordered)
	impact := TestImpact{ProjectID: projectID, Revision: revision, ChangedSymbols: append([]string(nil), changed...), Selections: []ImpactSelection{}, FullSuiteRequired: mediumOrHighRisk, PolicyExplanation: "Targeted tests accelerate inner loops; publishable and medium/high-risk work still requires a fresh full suite."}
	for _, testID := range ordered {
		reasons := []string{}
		for _, target := range tests[testID] {
			if changedSet[target] {
				reasons = append(reasons, "covers changed symbol "+target)
			}
		}
		impact.Selections = append(impact.Selections, ImpactSelection{TestID: testID, Selected: len(reasons) > 0, Reasons: reasons, Confidence: map[bool]int{true: 90, false: 30}[len(reasons) > 0]})
	}
	return impact, nil
}

func (service *Service) CaptureBaseline(ctx context.Context, baseline Baseline) (Baseline, error) {
	if ValidateIdentity(baseline.ProjectID) != nil || ValidateIdentity(baseline.Revision) != nil || ValidateIdentity(baseline.ToolchainID) != nil || ValidateIdentity(baseline.ActorID) != nil || len(baseline.ConfigSHA256) != 64 || len(baseline.PackSetSHA256) != 64 || strings.TrimSpace(baseline.Reason) == "" || len(baseline.Reason) > 1000 || len(baseline.Observations) == 0 || len(baseline.Observations) > 10_000 {
		return Baseline{}, errors.New("baseline requires bounded identities, hashes, reason, and observations")
	}
	seen := map[string]bool{}
	for _, observation := range baseline.Observations {
		key := observation.Kind + "\x00" + observation.Key
		if observation.Kind == "" || observation.Key == "" || len(observation.Key) > 512 || seen[key] {
			return Baseline{}, errors.New("baseline observations require unique bounded kind and key")
		}
		seen[key] = true
	}
	return service.store.SaveBaseline(ctx, baseline)
}

func (service *Service) FindBaseline(ctx context.Context, projectID, revision, configSHA256, toolchainID, packSetSHA256 string) (Baseline, bool, error) {
	if ValidateIdentity(projectID) != nil || ValidateIdentity(revision) != nil || len(configSHA256) != 64 || ValidateIdentity(toolchainID) != nil || len(packSetSHA256) != 64 {
		return Baseline{}, false, errors.New("complete baseline identity is required")
	}
	return service.store.FindBaseline(ctx, projectID, revision, configSHA256, toolchainID, packSetSHA256)
}

func (service *Service) CompareAndSave(ctx context.Context, baseline Baseline, candidateSHA, purpose string, observations []Observation) (Differential, error) {
	if baseline.ID == "" || len(candidateSHA) != 64 || ValidateIdentity(purpose) != nil || len(observations) == 0 || len(observations) > 10_000 {
		return Differential{}, errors.New("stored baseline, candidate hash, purpose, and bounded observations are required")
	}
	differential := Differential{BaselineID: baseline.ID, CandidateSHA: candidateSHA, Purpose: purpose, Items: CompareObservations(baseline.Observations, observations)}
	return service.store.SaveDifferential(ctx, differential)
}

func (service *Service) RecordTestImpact(ctx context.Context, impact TestImpact) (TestImpact, error) {
	if ValidateIdentity(impact.ProjectID) != nil || ValidateIdentity(impact.Revision) != nil || len(impact.ChangedSymbols) == 0 || len(impact.Selections) > 10_000 || strings.TrimSpace(impact.PolicyExplanation) == "" {
		return TestImpact{}, errors.New("test impact requires bounded project, revision, symbols, selections, and policy explanation")
	}
	return service.store.SaveTestImpact(ctx, impact)
}

func (service *Service) FindTestImpact(ctx context.Context, projectID, revision string) (TestImpact, bool, error) {
	if ValidateIdentity(projectID) != nil || ValidateIdentity(revision) != nil {
		return TestImpact{}, false, errors.New("project and revision identities are required")
	}
	return service.store.FindTestImpact(ctx, projectID, revision)
}

func (service *Service) RegisterCacheEntry(ctx context.Context, entry CacheEntry) (CacheEntry, error) {
	if entry.QuotaBytes == 0 {
		entry.QuotaBytes = 512 << 20
	}
	if ValidateIdentity(entry.ProjectID) != nil || ValidateIdentity(entry.TrustDomain) != nil || ValidateIdentity(entry.Kind) != nil || len(entry.Key) != 64 || len(entry.InputSHA256) != 64 || len(entry.ObjectSHA256) != 64 || entry.Bytes < 0 || entry.QuotaBytes < 1<<20 || entry.QuotaBytes > 1<<40 || entry.Bytes > entry.QuotaBytes || !entry.Verified || entry.ExpiresAt.Before(time.Now().UTC()) {
		return CacheEntry{}, errors.New("cache entry requires project-isolated identities, complete hashes, verified content, and future retention")
	}
	return service.store.PutCacheEntry(ctx, entry)
}

func (service *Service) CacheEntries(ctx context.Context, projectID string, limit int) ([]CacheEntry, error) {
	if ValidateIdentity(projectID) != nil {
		return nil, errors.New("valid project identity is required")
	}
	return service.store.ListCacheEntries(ctx, projectID, limit)
}

func (service *Service) PurgeCache(ctx context.Context, projectID, kind, actorID, reason string, recentlyReauthenticated bool) (int, error) {
	if !recentlyReauthenticated {
		return 0, errors.New("recent reauthentication is required for cache purge")
	}
	if ValidateIdentity(projectID) != nil || (kind != "" && ValidateIdentity(kind) != nil) || ValidateIdentity(actorID) != nil || strings.TrimSpace(reason) == "" || len(reason) > 1000 {
		return 0, errors.New("valid project, cache kind, and actor identities are required")
	}
	return service.store.PurgeCacheEntries(ctx, projectID, kind, actorID, reason)
}
