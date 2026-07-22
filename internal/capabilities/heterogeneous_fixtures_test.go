package capabilities_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/capabilities"
)

func TestHeterogeneousRepositoryFixturesProduceEvidenceBackedDisabledPacks(t *testing.T) {
	service, _ := testService(t)
	tests := []struct {
		name     string
		packIDs  []string
		findings []string
	}{
		{name: "php83", packIDs: []string{"php83-intranet", "sbom-fmea-security"}, findings: []string{"language:php", "framework:composer", "ci:gitlab-ci", "risk:migrations"}},
		{name: "dotnet-lab", packIDs: []string{"windows-dotnet-labautomation", "sbom-fmea-security"}, findings: []string{"language:dotnet"}},
		{name: "r-statistical", packIDs: []string{"r-statistical-validation", "sbom-fmea-security"}, findings: []string{"language:r", "package_manager:renv", "tool:testthat"}},
		{name: "mixed-malformed", packIDs: []string{"sbom-fmea-security"}, findings: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := loadRepositoryFixture(t, test.name)
			if test.name == "mixed-malformed" {
				files = append(files, capabilities.SourceFile{Path: "invalid.bin", Content: []byte{0xff, 0xfe, 0xfd}}, capabilities.SourceFile{Path: "windows.txt", Content: []byte("line one\r\nline two\r\n")})
			}
			scan, err := service.Scan(context.Background(), capabilities.ScanInput{ProjectID: "fixture", Repository: "owner/fixture", Revision: "fixture-" + test.name, Files: files}, "fixture-operator")
			if err != nil {
				t.Fatal(err)
			}
			packs := map[string]bool{}
			for _, proposal := range scan.Proposals {
				if proposal.Kind == "capability_pack" {
					if proposal.State != "pending" || len(proposal.Evidence) == 0 {
						t.Fatalf("unsafe proposal %#v", proposal)
					}
					packs[proposal.Key] = true
				}
			}
			for _, expected := range test.packIDs {
				if !packs[expected] {
					t.Errorf("missing pack proposal %s in %#v", expected, packs)
				}
			}
			observed := map[string]bool{}
			for _, finding := range scan.Findings {
				observed[finding.Category+":"+finding.Value] = true
			}
			for _, expected := range test.findings {
				if !observed[expected] {
					t.Errorf("missing finding %s in %#v", expected, observed)
				}
			}
			if test.name == "mixed-malformed" && (!observed["encoding:non-utf8"] || !observed["line_endings:crlf"]) {
				t.Fatalf("malformed evidence not retained: %#v", observed)
			}
		})
	}
}

func loadRepositoryFixture(t *testing.T, name string) []capabilities.SourceFile {
	t.Helper()
	root := filepath.Join("..", "..", "test", "fixtures", "repositories", name)
	items := []capabilities.SourceFile{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		items = append(items, capabilities.SourceFile{Path: filepath.ToSlash(relative), Content: content})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items
}
