package models

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type runtimeFixture struct {
	mu      sync.Mutex
	started []string
	stops   int
}

func (r *runtimeFixture) Start(_ context.Context, manifest Manifest, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = append(r.started, manifest.ID)
	return nil
}
func (r *runtimeFixture) Stop(context.Context) error {
	r.mu.Lock()
	r.stops++
	r.mu.Unlock()
	return nil
}
func (r *runtimeFixture) Status(_ context.Context, manifest Manifest) (Status, error) {
	return statusForManifest(manifest), nil
}
func (r *runtimeFixture) Smoke(_ context.Context, manifest Manifest) (SmokeResult, Status, error) {
	return SmokeResult{ProfileID: manifest.ID, Healthy: true}, statusForManifest(manifest), nil
}

func TestSupervisorVerifiesAndSequentiallySwapsAllowListedModels(t *testing.T) {
	root := t.TempDir()
	implementationPayload := []byte("implementation")
	qcPayload := []byte("quality-control")
	implementation := testManifest(implementationPayload)
	qc := testManifest(qcPayload)
	qc.ID, qc.Role, qc.ModelFamily, qc.Filename = "qc", "qc", "mistral-small-4", "qc-q5_k_l.gguf"
	for manifest, payload := range map[Manifest][]byte{implementation: implementationPayload, qc: qcPayload} {
		if err := os.WriteFile(filepath.Join(root, manifest.Filename), payload, 0o444); err != nil {
			t.Fatal(err)
		}
	}
	catalog := &Catalog{profiles: map[string]Manifest{implementation.ID: implementation, qc.ID: qc}}
	runtime := &runtimeFixture{}
	supervisor, err := NewSupervisor(catalog, root, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Load(context.Background(), "arbitrary"); err == nil {
		t.Fatal("unknown model loaded")
	}
	if _, err := supervisor.Load(context.Background(), implementation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Load(context.Background(), qc.ID); err != nil {
		t.Fatal(err)
	}
	if runtime.stops != 1 || len(runtime.started) != 2 || runtime.started[0] != "implementation" || runtime.started[1] != "qc" {
		t.Fatalf("models were not swapped sequentially: starts=%#v stops=%d", runtime.started, runtime.stops)
	}
}

func TestSupervisorRefusesTamperedInstalledModel(t *testing.T) {
	root := t.TempDir()
	manifest := testManifest([]byte("expected"))
	if err := os.WriteFile(filepath.Join(root, manifest.Filename), []byte("tampered"), 0o444); err != nil {
		t.Fatal(err)
	}
	supervisor, _ := NewSupervisor(&Catalog{profiles: map[string]Manifest{manifest.ID: manifest}}, root, &runtimeFixture{})
	if _, err := supervisor.Load(context.Background(), manifest.ID); err == nil {
		t.Fatal("tampered model loaded")
	}
}
