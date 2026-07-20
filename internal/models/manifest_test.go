package models

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testManifest(payload []byte) Manifest {
	return Manifest{
		SchemaVersion: 1, ID: "implementation", Role: "implementation", ModelFamily: "qwen3.6",
		Filename: "implementation-q8.gguf", SHA256: fmt.Sprintf("%x", sha256.Sum256(payload)), Bytes: int64(len(payload)),
		SourceURI: "https://models.invalid/implementation-q8.gguf", License: "Apache-2.0", Quantization: "Q8_0",
		Context: 131072, Threads: 16, Batch: 1024, UBatch: 256, NUMA: "distribute", MinRAMBytes: 64 << 30,
		Sampling: Sampling{Temperature: 0.2, TopP: 0.95, TopK: 40, MinP: 0.05, Seed: 1},
	}
}

func TestCatalogIsStrictVersionedAndDeterministic(t *testing.T) {
	directory := t.TempDir()
	manifest := testManifest([]byte("model"))
	payload := fmt.Sprintf(`{"schema_version":1,"id":"%s","role":"%s","model_family":"%s","filename":"%s","sha256":"%s","bytes":%d,"source_uri":"%s","license":"%s","quantization":"%s","context":%d,"threads":%d,"batch":%d,"ubatch":%d,"numa":"%s","min_ram_bytes":%d,"sampling":{"temperature":0.2,"top_p":0.95,"top_k":40,"min_p":0.05,"seed":1}}`,
		manifest.ID, manifest.Role, manifest.ModelFamily, manifest.Filename, manifest.SHA256, manifest.Bytes,
		manifest.SourceURI, manifest.License, manifest.Quantization, manifest.Context, manifest.Threads,
		manifest.Batch, manifest.UBatch, manifest.NUMA, manifest.MinRAMBytes)
	path := filepath.Join(directory, "implementation.json")
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(directory)
	if err != nil {
		t.Fatal(err)
	}
	if profiles := catalog.Profiles(); len(profiles) != 1 || profiles[0].ID != "implementation" {
		t.Fatalf("unexpected profiles %#v", profiles)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSuffix(payload, "}")+`,"argument":"--unsafe"}`), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCatalog(directory); err == nil {
		t.Fatal("unknown manifest field was accepted")
	}
}

func TestImportAndDownloadVerifyBeforeNoOverwritePublication(t *testing.T) {
	model := []byte("tiny deterministic model fixture")
	manifest := testManifest(model)
	source := filepath.Join(t.TempDir(), "source.gguf")
	if err := os.WriteFile(source, model, 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	destination, err := ImportModel(source, root, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(destination); info.Mode().Perm() != 0o444 {
		t.Fatalf("installed model mode is %o", info.Mode().Perm())
	}
	if _, err := ImportModel(source, root, manifest); err == nil {
		t.Fatal("model import overwrote an installed file")
	}

	downloadRoot := t.TempDir()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(model) }))
	defer server.Close()
	manifest.SourceURI = server.URL + "/model.gguf"
	if _, err := DownloadModel(context.Background(), server.Client(), downloadRoot, manifest); err != nil {
		t.Fatal(err)
	}
	bad := manifest
	bad.Filename = "bad.gguf"
	bad.SHA256 = strings.Repeat("0", 64)
	if _, err := DownloadModel(context.Background(), server.Client(), downloadRoot, bad); err == nil {
		t.Fatal("bad model digest was accepted")
	}
	if _, err := os.Stat(filepath.Join(downloadRoot, bad.Filename)); !os.IsNotExist(err) {
		t.Fatal("failed download published a destination")
	}
}

func TestManifestRejectsPathsCredentialsAndUnsafeBounds(t *testing.T) {
	base := testManifest([]byte("x"))
	mutations := []func(*Manifest){
		func(m *Manifest) { m.Filename = "../model.gguf" },
		func(m *Manifest) { m.SourceURI = "https://token@models.example/model.gguf" },
		func(m *Manifest) { m.Context = 1 },
		func(m *Manifest) { m.UBatch = m.Batch + 1 },
		func(m *Manifest) { m.Role = "browser-selected" },
	}
	for _, mutate := range mutations {
		manifest := base
		mutate(&manifest)
		if err := manifest.Validate(); err == nil {
			t.Errorf("unsafe manifest accepted: %#v", manifest)
		}
	}
}
