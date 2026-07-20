package models

import (
	"context"
	"testing"
)

func TestFakeLoadsOneAllowListedProfileAtATime(t *testing.T) {
	manager := NewFake([]Profile{
		{ID: "implementation", Role: "implementation", ModelFamily: "qwen", Context: 131072},
		{ID: "qc", Role: "qc", ModelFamily: "mistral", Context: 32768},
	})
	ctx := context.Background()
	if _, err := manager.Load(ctx, "arbitrary-path"); err == nil {
		t.Fatal("non-allow-listed profile loaded")
	}
	if _, err := manager.Load(ctx, "implementation"); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Load(ctx, "qc")
	if err != nil {
		t.Fatal(err)
	}
	if status.ProfileID != "qc" || status.ModelFamily != "mistral" {
		t.Fatalf("unexpected status: %#v", status)
	}
	if err := manager.Unload(ctx); err != nil {
		t.Fatal(err)
	}
	status, _ = manager.Status(ctx)
	if status.State != "unloaded" {
		t.Fatalf("model remained loaded: %#v", status)
	}
}
