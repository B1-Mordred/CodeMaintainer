package runners

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnixClientImplementsBoundedRunnerContract(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "runnerd.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	token := []byte("0123456789abcdef0123456789abcdef")
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+string(token) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/runs":
			var request JobRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Kind != KindImplementation {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeClientFixture(w, http.StatusCreated, map[string]string{"run_id": "run_fixture"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run_fixture":
			writeClientFixture(w, http.StatusOK, Status{ID: "run_fixture", JobID: "job", Kind: KindImplementation, State: "running"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run_fixture/logs":
			if r.URL.Query().Get("limit") != "4" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeClientFixture(w, http.StatusOK, LogChunk{NextCursor: 4, Data: "test", Truncated: true})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run_fixture/artifacts":
			writeClientFixture(w, http.StatusOK, map[string]any{"items": []Artifact{}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/runs/run_fixture/stop":
			writeClientFixture(w, http.StatusOK, map[string]string{"status": "stopped"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() {
		server.Close()
		<-done
	}()

	client, err := NewUnixClient(socketPath, token)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	runID, err := client.Start(ctx, JobRequest{JobID: "job", ProjectID: "project", Kind: KindImplementation})
	if err != nil || runID != "run_fixture" {
		t.Fatalf("start returned %q, %v", runID, err)
	}
	status, err := client.Inspect(ctx, runID)
	if err != nil || status.State != "running" {
		t.Fatalf("inspect returned %#v, %v", status, err)
	}
	chunk, err := client.Logs(ctx, runID, 0, 4)
	if err != nil || chunk.Data != "test" || !chunk.Truncated {
		t.Fatalf("logs returned %#v, %v", chunk, err)
	}
	if artifacts, err := client.Artifacts(ctx, runID); err != nil || artifacts == nil || len(artifacts) != 0 {
		t.Fatalf("artifacts returned %#v, %v", artifacts, err)
	}
	if err := client.Stop(ctx, runID); err != nil {
		t.Fatal(err)
	}
}

func TestUnixClientBoundsResponsesAndDecodesErrors(t *testing.T) {
	client, closeServer := fixtureUnixClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/runs/missing" {
			writeClientFixture(w, http.StatusNotFound, map[string]any{"error": map[string]string{"code": "run_not_found", "message": "gone"}})
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", maxRunnerResponseBytes+1)))
	}))
	defer closeServer()
	_, err := client.Inspect(context.Background(), "missing")
	remote, ok := err.(*RemoteError)
	if !ok || remote.StatusCode != http.StatusNotFound || remote.Code != "run_not_found" {
		t.Fatalf("unexpected remote error %T %#v", err, err)
	}
	_, err = client.Inspect(context.Background(), "oversized")
	if err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("oversized response returned %v", err)
	}
}

func fixtureUnixClient(t *testing.T, handler http.Handler) (*UnixClient, func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runnerd.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	client, err := NewUnixClient(path, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return client, func() {
		server.Close()
		<-done
		_ = os.Remove(path)
	}
}

func writeClientFixture(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
