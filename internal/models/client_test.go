package models

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
)

func TestControlClientUsesAuthenticatedBoundedServiceContract(t *testing.T) {
	token := "0123456789abcdef0123456789abcdef"
	manager := NewFake([]Profile{{ID: "implementation", Role: "implementation", ModelFamily: "qwen", Context: 32768}})
	handler, err := NewService(manager, token, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := NewControlClient(server.URL, []byte(token))
	if err != nil {
		t.Fatal(err)
	}
	status, err := client.Load(context.Background(), "implementation")
	if err != nil || status.ModelFamily != "qwen" {
		t.Fatalf("load = %#v, %v", status, err)
	}
	if err := client.Unload(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err = client.Status(context.Background())
	if err != nil || status.State != "unloaded" {
		t.Fatalf("status = %#v, %v", status, err)
	}
}
