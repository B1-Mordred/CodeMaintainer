package models

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelServiceIsAuthenticatedNarrowAndRejectsArguments(t *testing.T) {
	token := strings.Repeat("s", 48)
	manager := NewFake([]Profile{{ID: "implementation", Role: "implementation", ModelFamily: "qwen", Context: 131072}})
	service, err := NewService(manager, token, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(service)
	defer server.Close()
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/load", bytes.NewBufferString(`{"profile_id":"implementation"}`))
	response, _ := http.DefaultClient.Do(request)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated load returned %d", response.StatusCode)
	}
	response.Body.Close()
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/v1/load", bytes.NewBufferString(`{"profile_id":"implementation","model_path":"/host/model","arguments":["--unsafe"]}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response, _ = http.DefaultClient.Do(request)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("arbitrary model arguments returned %d", response.StatusCode)
	}
	response.Body.Close()
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/v1/load", bytes.NewBufferString(`{"profile_id":"implementation"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response, _ = http.DefaultClient.Do(request)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("allow-listed load returned %d", response.StatusCode)
	}
	response.Body.Close()
	status, _ := manager.Status(context.Background())
	if status.ProfileID != "implementation" {
		t.Fatalf("model was not loaded: %#v", status)
	}
}
