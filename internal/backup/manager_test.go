package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type fixtureSnapshotter struct{}

func (fixtureSnapshotter) Snapshot(_ context.Context, target string) error {
	return os.WriteFile(target, []byte("sqlite snapshot fixture"), 0o600)
}

func TestBackupCreatesChecksumBoundBundleAndValidatesDryRun(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "artifacts", "objects"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artifacts", "objects", "evidence"), []byte("verified evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := New(root, "mock", fixtureSnapshotter{}, "")
	if err != nil {
		t.Fatal(err)
	}
	record, err := manager.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if record.Encrypted || record.SHA256 == "" || record.Bytes == 0 {
		t.Fatalf("unexpected record: %#v", record)
	}
	report, err := manager.Validate(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !report.DryRun || !report.Compatible || !report.ChecksumsValid || report.Files != 2 {
		t.Fatalf("unexpected report: %#v", report)
	}
	items, err := manager.List(context.Background())
	if err != nil || len(items) != 1 || items[0].ID != record.ID {
		t.Fatalf("list = %#v, %v", items, err)
	}
	staged, err := manager.StageRestore(context.Background(), record.ID)
	if err != nil || !staged.Staged || !staged.RequiresRestart {
		t.Fatalf("stage = %#v, %v", staged, err)
	}
	applied, err := ApplyPending(root)
	if err != nil || !applied {
		t.Fatalf("apply pending = %v, %v", applied, err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "database", "controller.db"))
	if err != nil || string(payload) != "sqlite snapshot fixture" {
		t.Fatalf("restored database = %q, %v", payload, err)
	}
}
