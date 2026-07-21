package artifacts

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
	storesqlite "github.com/B1-Mordred/CodeMaintainer/internal/storage/sqlite"
)

func TestStorePublishesContentAddressedImmutableArtifacts(t *testing.T) {
	ctx := context.Background()
	metadata, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Close()
	job, err := metadata.CreateJob(ctx, storage.CreateJobParams{
		ID: "job_artifact", ProjectID: "project", Repository: "owner/repo", Task: "verify", ActorID: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(filepath.Join(t.TempDir(), "artifacts"), metadata)
	if err != nil {
		t.Fatal(err)
	}
	put := func() storage.ArtifactRecord {
		record, err := store.Put(ctx, PutRequest{
			JobID: job.ID, ProjectID: job.ProjectID, Kind: "command_result",
			MediaType: "application/json", Producer: "verifier", Metadata: []byte(`{"class":"full_tests"}`),
			Reader: strings.NewReader(`{"exit_code":0}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	first := put()
	second := put()
	if first.ID == second.ID || first.ObjectSHA256 != second.ObjectSHA256 || first.RelativePath != second.RelativePath {
		t.Fatalf("deduplication identities are wrong: %#v %#v", first, second)
	}
	path := filepath.Join(store.root, filepath.FromSlash(first.RelativePath))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o400 {
		t.Fatalf("artifact mode is %o", info.Mode().Perm())
	}
	record, reader, err := store.Open(ctx, job.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := io.ReadAll(reader)
	reader.Close()
	if record.ObjectSHA256 != first.ObjectSHA256 || string(payload) != `{"exit_code":0}` {
		t.Fatalf("opened artifact %#v %q", record, payload)
	}
}

func TestStoreBindsIdempotencyKeyToExactArtifact(t *testing.T) {
	ctx := context.Background()
	metadata, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Close()
	job, err := metadata.CreateJob(ctx, storage.CreateJobParams{
		ID: "job_idempotent", ProjectID: "project", Repository: "owner/repo", Task: "verify", ActorID: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(filepath.Join(t.TempDir(), "artifacts"), metadata)
	if err != nil {
		t.Fatal(err)
	}
	request := PutRequest{
		JobID: job.ID, ProjectID: job.ProjectID, Kind: "task_packet", MediaType: "application/json",
		Producer: "controller", IdempotencyKey: "phase_7_packet", Metadata: []byte(`{"phase":7}`),
		Reader: strings.NewReader(`{"task":"safe"}`),
	}
	first, err := store.Put(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Reader = strings.NewReader(`{"task":"safe"}`)
	repeated, err := store.Put(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.ID != first.ID {
		t.Fatalf("idempotent artifact changed identity: %s != %s", repeated.ID, first.ID)
	}
	request.Reader = strings.NewReader(`{"task":"different"}`)
	if _, err := store.Put(ctx, request); !errors.Is(err, storage.ErrIdempotencyKey) {
		t.Fatalf("changed idempotent artifact returned %v", err)
	}
}

func TestStoreRejectsOversizeAndDetectsTampering(t *testing.T) {
	ctx := context.Background()
	metadata, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Close()
	job, err := metadata.CreateJob(ctx, storage.CreateJobParams{
		ID: "job_artifact", ProjectID: "project", Repository: "owner/repo", Task: "verify", ActorID: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(filepath.Join(t.TempDir(), "artifacts"), metadata)
	if err != nil {
		t.Fatal(err)
	}
	request := PutRequest{
		JobID: job.ID, ProjectID: job.ProjectID, Kind: "log", MediaType: "text/plain",
		Producer: "verifier", Metadata: []byte(`{}`), Reader: strings.NewReader("too large"), MaxBytes: 3,
	}
	if _, err := store.Put(ctx, request); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized artifact returned %v", err)
	}
	request.Reader = strings.NewReader("safe")
	request.MaxBytes = 10
	record, err := store.Put(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.root, filepath.FromSlash(record.RelativePath))
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("evil"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, reader, err := store.Open(ctx, job.ID, record.ID); err == nil {
		reader.Close()
		t.Fatal("tampered artifact was opened")
	}
}

func TestStoreStagesOnlyJobAuthorizedInputsWithoutOverwrite(t *testing.T) {
	ctx := context.Background()
	metadata, err := storesqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Close()
	for _, id := range []string{"job_one", "job_two"} {
		if _, err := metadata.CreateJob(ctx, storage.CreateJobParams{
			ID: id, ProjectID: "project", Repository: "owner/repo", Task: "verify", ActorID: "operator",
		}); err != nil {
			t.Fatal(err)
		}
	}
	store, err := New(filepath.Join(t.TempDir(), "artifacts"), metadata)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Put(ctx, PutRequest{
		JobID: "job_one", ProjectID: "project", Kind: "task_packet", MediaType: "application/json",
		Producer: "controller", Metadata: []byte(`{}`), Reader: strings.NewReader(`{"task":"safe"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	directory, err := store.StageInputs(ctx, "job_one", []string{record.ID})
	if err != nil {
		t.Fatal(err)
	}
	staged, err := os.ReadFile(filepath.Join(directory, record.ID))
	if err != nil || string(staged) != `{"task":"safe"}` {
		t.Fatalf("staged input %q, %v", staged, err)
	}
	if _, err := store.StageInputs(ctx, "job_one", []string{record.ID}); err != nil {
		t.Fatalf("idempotent staging failed: %v", err)
	}
	if _, err := store.StageInputs(ctx, "job_two", []string{record.ID}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("cross-job staging returned %v", err)
	}
}
