package intelligence_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/local-code-maintainer/appliance/internal/intelligence"
	"github.com/local-code-maintainer/appliance/internal/projects"
	"github.com/local-code-maintainer/appliance/internal/storage"
	storesqlite "github.com/local-code-maintainer/appliance/internal/storage/sqlite"
)

func TestIncrementalIndexContextDifferentialImpactAndCacheLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.UpsertProject(ctx, projects.UpsertRequest{ID: "project-one", Provider: "local", Repository: "owner/repo", DefaultBranch: "main", LocalRemoteName: "fixture.git"}, "admin"); err != nil {
		t.Fatal(err)
	}
	service, err := intelligence.NewService(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	files := []intelligence.SourceFile{
		source("cmd/main.go", "package main\nimport \"fmt\"\ntype App struct{}\nfunc Run() { fmt.Println(\"ok\") }\n"),
		source("internal/broken.go", "package broken\nfunc {"),
		source("vendor/example/lib.ts", "export function ignored() {}"),
	}
	request := intelligence.IndexRequest{ProjectID: "project-one", Repository: "owner/repo", Revision: "abc123", ParserID: "bounded-parser-v1", Files: files}
	first, err := service.Index(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.State != "partial" || first.Parsed != 3 || first.Reused != 0 || first.Failures != 1 {
		t.Fatalf("first index = %#v", first)
	}
	second, err := service.Index(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if second.State != "partial" || second.Parsed != 0 || second.Reused != 3 || second.Failures != 1 {
		t.Fatalf("second index = %#v", second)
	}
	retention, err := store.PruneIntelligence(ctx, "project-one", time.Now().UTC().Add(time.Hour), time.Now().UTC())
	if err != nil || retention.RunsRemoved != 1 {
		t.Fatalf("retention result = %#v %v", retention, err)
	}
	query, err := service.Query(ctx, intelligence.Query{ProjectID: "project-one", Revision: "abc123", Term: "Run", Limit: 10})
	if err != nil || len(query.Symbols) != 1 || query.Symbols[0].Name != "Run" || !query.Partial {
		t.Fatalf("query = %#v %v", query, err)
	}
	status, err := service.Status(ctx, "project-one")
	if err != nil || status.Files != 3 || status.Failures != 1 || len(status.Languages) != 2 {
		t.Fatalf("status = %#v %v", status, err)
	}

	packet, err := service.CompileContext(ctx, "project-one", "", "implementation", 256, 64, []intelligence.ContextCandidate{
		{ID: "target", Source: "code_intelligence", Version: "abc123", Reason: "exact changed symbol", Trust: "untrusted", Content: []byte("func Run()"), Priority: 100},
		{ID: "duplicate", Source: "code_intelligence", Version: "abc123", Reason: "overlapping range", Trust: "untrusted", Content: []byte("func Run()"), Priority: 90},
		{ID: "large", Source: "task", Version: "1", Reason: "oversized context", Trust: "trusted", Content: make([]byte, 1000), Priority: 1},
	})
	if err != nil || len(packet.Content) != 1 || !packet.Manifest.Truncated || packet.Manifest.Selections[1].Exclusion != "duplicate content" || packet.Manifest.Selections[2].Exclusion != "budget exhausted" {
		t.Fatalf("context packet = %#v %v", packet.Manifest, err)
	}
	loadedManifest, err := store.GetContextManifest(ctx, "project-one", packet.Manifest.ID)
	if err != nil || loadedManifest.UsedTokens != packet.Manifest.UsedTokens {
		t.Fatalf("manifest = %#v %v", loadedManifest, err)
	}
	if listed, listErr := store.ListContextManifests(ctx, "project-one", 10); listErr != nil || len(listed) != 1 {
		t.Fatalf("context manifest list = %#v %v", listed, listErr)
	}

	baseline, err := service.CaptureBaseline(ctx, intelligence.Baseline{ProjectID: "project-one", Revision: "abc123", ConfigSHA256: hash("config"), ToolchainID: "go-v1", PackSetSHA256: hash("packs"), ActorID: "admin", Reason: "clean environment baseline", Observations: []intelligence.Observation{{Key: "unit", Kind: "test", Status: "failed", Value: json.RawMessage(`{"failures":1}`)}, {Key: "lint", Kind: "lint", Status: "passed", Value: json.RawMessage(`{"warnings":0}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	differential, err := service.CompareAndSave(ctx, baseline, hash("candidate"), "full", []intelligence.Observation{{Key: "unit", Kind: "test", Status: "passed", Value: json.RawMessage(`{"failures":0}`)}, {Key: "security", Kind: "scan", Status: "failed", Value: json.RawMessage(`{"findings":1}`)}})
	if err != nil || len(differential.Items) != 3 {
		t.Fatalf("differential = %#v %v", differential, err)
	}
	if listed, listErr := store.ListBaselines(ctx, "project-one", 10); listErr != nil || len(listed) != 1 {
		t.Fatalf("baseline list = %#v %v", listed, listErr)
	}
	foundBaseline, found, findErr := service.FindBaseline(ctx, baseline.ProjectID, baseline.Revision, baseline.ConfigSHA256, baseline.ToolchainID, baseline.PackSetSHA256)
	if findErr != nil || !found || foundBaseline.ID != baseline.ID {
		t.Fatalf("baseline identity lookup = %#v %t %v", foundBaseline, found, findErr)
	}
	if listed, listErr := store.ListDifferentials(ctx, "project-one", 10); listErr != nil || len(listed) != 1 {
		t.Fatalf("differential list = %#v %v", listed, listErr)
	}
	classes := map[string]string{}
	for _, item := range differential.Items {
		classes[item.Key] = item.Classification
	}
	if classes["lint"] != "resolved" || classes["security"] != "newly_introduced" || classes["unit"] != "changed" {
		t.Fatalf("classes = %#v", classes)
	}

	impact, err := intelligence.NewTestImpact("project-one", "abc123", []string{"Run"}, map[string][]string{"unit:run": {"Run"}, "unit:other": {"Other"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	impact, err = service.RecordTestImpact(ctx, impact)
	selected := map[string]bool{}
	for _, item := range impact.Selections {
		selected[item.TestID] = item.Selected
	}
	if err != nil || !impact.FullSuiteRequired || !selected["unit:run"] || selected["unit:other"] {
		t.Fatalf("impact = %#v %v", impact, err)
	}
	if listed, listErr := store.ListTestImpacts(ctx, "project-one", 10); listErr != nil || len(listed) != 1 {
		t.Fatalf("impact list = %#v %v", listed, listErr)
	}
	if foundImpact, found, findErr := service.FindTestImpact(ctx, impact.ProjectID, impact.Revision); findErr != nil || !found || foundImpact.ID != impact.ID {
		t.Fatalf("impact identity lookup = %#v %t %v", foundImpact, found, findErr)
	}

	cacheKey, _ := intelligence.NewCacheKey("project-one", "trusted", "parse", "owner/repo", "abc123", "bounded-parser-v1")
	entry, err := service.RegisterCacheEntry(ctx, intelligence.CacheEntry{Key: cacheKey, ProjectID: "project-one", TrustDomain: "trusted", Kind: "parse", InputSHA256: hash("inputs"), ObjectSHA256: hash("object"), Bytes: 123, Verified: true, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if err != nil || entry.Key != cacheKey {
		t.Fatalf("cache entry = %#v %v", entry, err)
	}
	entries, err := service.CacheEntries(ctx, "project-one", 10)
	if err != nil || len(entries) != 4 {
		t.Fatalf("cache list = %#v %v", entries, err)
	}
	hits := 0
	for _, cached := range entries {
		if cached.Kind == "source-parse" && cached.LastResult == "hit" && cached.QuotaBytes == 512<<20 {
			hits++
		}
	}
	if hits != 3 {
		t.Fatalf("parsed-blob cache results = %#v", entries)
	}
	overQuotaKey, _ := intelligence.NewCacheKey("project-one", "trusted", "quota-test", "complete-input")
	if _, err := service.RegisterCacheEntry(ctx, intelligence.CacheEntry{Key: overQuotaKey, ProjectID: "project-one", TrustDomain: "trusted", Kind: "quota-test", InputSHA256: hash("quota-input"), ObjectSHA256: hash("quota-object"), Bytes: 1 << 20, QuotaBytes: 1 << 20, Verified: true, ExpiresAt: time.Now().UTC().Add(time.Hour)}); !errors.Is(err, storage.ErrBudgetExceeded) {
		t.Fatalf("aggregate project cache quota returned %v", err)
	}
	if _, err := service.PurgeCache(ctx, "project-one", "parse", "admin", "test denial", false); err == nil {
		t.Fatal("cache purge bypassed reauthentication")
	}
	count, err := service.PurgeCache(ctx, "project-one", "parse", "admin", "remove expired derived parse", true)
	if err != nil || count != 1 {
		t.Fatalf("cache purge = %d %v", count, err)
	}
	if err := service.Rebuild(ctx, "project-one", "admin", "exercise audited index rebuild"); err != nil {
		t.Fatal(err)
	}
	status, err = service.Status(ctx, "project-one")
	if err != nil || status.State != "never_indexed" {
		t.Fatalf("rebuilt status = %#v %v", status, err)
	}
	cancelContext, cancel := context.WithCancel(ctx)
	analyzer := &cancellingAnalyzer{cancel: cancel}
	cancellable, _ := intelligence.NewService(store, analyzer)
	cancelled, err := cancellable.Index(cancelContext, intelligence.IndexRequest{ProjectID: "project-one", Repository: "owner/repo", Revision: "cancelled-revision", ParserID: "bounded-parser-v2", Files: []intelligence.SourceFile{source("one.go", "package one\n"), source("two.go", "package two\n")}})
	if err != nil || cancelled.State != "cancelled" || cancelled.Files != 1 || analyzer.calls != 1 {
		t.Fatalf("cancelled index = %#v calls=%d %v", cancelled, analyzer.calls, err)
	}
}

type cancellingAnalyzer struct {
	cancel context.CancelFunc
	calls  int
}

func (analyzer *cancellingAnalyzer) Analyze(ctx context.Context, path, parserID string, content []byte) intelligence.BlobAnalysis {
	analyzer.calls++
	result := (intelligence.LexicalAnalyzer{}).Analyze(ctx, path, parserID, content)
	analyzer.cancel()
	return result
}

func source(path, content string) intelligence.SourceFile {
	return intelligence.SourceFile{Path: path, BlobSHA256: hash(content), Content: []byte(content)}
}
func hash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
