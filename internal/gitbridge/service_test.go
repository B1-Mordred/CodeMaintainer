package gitbridge

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fixtureBackend struct {
	registration Registration
}

func (f *fixtureBackend) Register(_ context.Context, value Registration) error {
	f.registration = value
	return nil
}
func (f *fixtureBackend) Sync(_ context.Context, projectID string) (SyncResult, error) {
	return SyncResult{ProjectID: projectID, BaseSHA: "0123456789abcdef0123456789abcdef01234567"}, nil
}
func (f *fixtureBackend) CreateWorktree(_ context.Context, request WorktreeRequest) (WorktreeResult, error) {
	return WorktreeResult{ProjectID: request.ProjectID, JobID: request.JobID, BaseSHA: request.BaseSHA}, nil
}
func (f *fixtureBackend) Commit(_ context.Context, _ CommitRequest) (CommitResult, error) {
	return CommitResult{ResultSHA: "0123456789abcdef0123456789abcdef01234567"}, nil
}
func (f *fixtureBackend) Diff(_ context.Context, _ DiffRequest) (DiffResult, error) {
	return DiffResult{Patch: "fixture"}, nil
}
func (f *fixtureBackend) Publish(_ context.Context, request PublishRequest) (Publication, error) {
	return Publication{Provider: "local", Branch: "maintainer/" + request.JobID, Draft: true}, nil
}

func TestServiceAndClientAuthenticateAndKeepContractNarrow(t *testing.T) {
	token := []byte("0123456789abcdef0123456789abcdef")
	backend := &fixtureBackend{}
	handler, err := NewService(backend, token, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	response, err := http.Post(server.URL+"/v1/projects/register", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.StatusCode)
	}
	client, err := NewClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	registration := Registration{ProjectID: "p", Provider: "local", Repository: "o/r", DefaultBranch: "main", LocalRemoteName: "r.git"}
	if err := client.Register(context.Background(), registration); err != nil {
		t.Fatal(err)
	}
	if backend.registration != registration {
		t.Fatalf("registration = %#v", backend.registration)
	}
	synced, err := client.Sync(context.Background(), "p")
	if err != nil || synced.ProjectID != "p" {
		t.Fatalf("sync = %#v, %v", synced, err)
	}
}
