package backup

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fixtureSnapshotter struct{}

func TestProductRenamePreservesBackupEncryptionContext(t *testing.T) {
	if backupAssociatedData != "local-code-maintainer-backup-v1" {
		t.Fatalf("backup associated data changed to %q; existing encrypted backups would be unreadable", backupAssociatedData)
	}
}

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
	databasePath := filepath.Join(root, "database", "controller.db")
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(databasePath, []byte("previous database"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(databasePath+suffix, []byte("stale sidecar"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	applied, err := ApplyPending(root)
	if err != nil || !applied {
		t.Fatalf("apply pending = %v, %v", applied, err)
	}
	payload, err := os.ReadFile(databasePath)
	if err != nil || string(payload) != "sqlite snapshot fixture" {
		t.Fatalf("restored database = %q, %v", payload, err)
	}
	preserved, err := filepath.Glob(databasePath + ".pre-restore-*")
	if err != nil || len(preserved) != 1 {
		t.Fatalf("preserved previous database = %v, %v", preserved, err)
	}
	payload, err = os.ReadFile(preserved[0])
	if err != nil || string(payload) != "previous database" {
		t.Fatalf("preserved database = %q, %v", payload, err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(databasePath + suffix); !os.IsNotExist(err) {
			t.Fatalf("stale sidecar %s still exists or failed stat: %v", suffix, err)
		}
	}
}

func TestBackupRestoreRejectsUnsafePendingPath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "backups"), 0o700); err != nil {
		t.Fatal(err)
	}
	bundle := fixtureBundle("../outside")
	payload, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backups", "restore-pending.json"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	applied, err := ApplyPending(root)
	if err == nil || applied {
		t.Fatalf("unsafe restore path was accepted: applied=%v err=%v", applied, err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "..", "outside")); !os.IsNotExist(statErr) {
		t.Fatalf("unsafe restore path wrote outside root: %v", statErr)
	}
}

func fixtureBundle(path string) Bundle {
	data := []byte("unsafe restore fixture")
	sum := sha256.Sum256(data)
	file := File{Path: path, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data)), Data: base64.StdEncoding.EncodeToString(data)}
	bundle := Bundle{SchemaVersion: SchemaVersion, CreatedAt: time.Unix(1, 0).UTC(), Profile: "mock", Files: []File{file}}
	manifest := struct {
		SchemaVersion int       `json:"schema_version"`
		CreatedAt     time.Time `json:"created_at"`
		Profile       string    `json:"profile"`
		Files         []File    `json:"files"`
		Excluded      []string  `json:"excluded"`
	}{bundle.SchemaVersion, bundle.CreatedAt, bundle.Profile, append([]File(nil), bundle.Files...), bundle.Excluded}
	for index := range manifest.Files {
		manifest.Files[index].Data = ""
	}
	manifestBytes, _ := json.Marshal(manifest)
	manifestSum := sha256.Sum256(manifestBytes)
	bundle.ManifestSHA256 = hex.EncodeToString(manifestSum[:])
	return bundle
}

func FuzzBackupRestorePendingPathSafety(f *testing.F) {
	f.Add("database/controller.db")
	f.Add("../outside")
	f.Add("/absolute")
	f.Add("nested/../database/controller.db")
	f.Add("safe/subdir/file.txt")
	f.Fuzz(func(t *testing.T, path string) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "backups"), 0o700); err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(fixtureBundle(path))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "backups", "restore-pending.json"), payload, 0o600); err != nil {
			t.Fatal(err)
		}
		applied, err := ApplyPending(root)
		clean := filepath.Clean(filepath.FromSlash(path))
		unsafe := clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator))
		if unsafe && (err == nil || applied) {
			t.Fatalf("unsafe restore path %q was accepted: applied=%v err=%v", path, applied, err)
		}
	})
}
