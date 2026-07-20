package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/local-code-maintainer/appliance/internal/projects"
	"github.com/local-code-maintainer/appliance/internal/storage"
)

func TestProjectsAreValidatedDurableAndAudited(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	request := projects.UpsertRequest{
		ID: "fixture", Provider: "local", Repository: "fixture/arithmetic",
		DefaultBranch: "main", LocalRemoteName: "fixture.git",
	}
	created, err := store.UpsertProject(ctx, request, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !created.Enabled || created.LocalRemoteName != "fixture.git" {
		t.Fatalf("created project = %#v", created)
	}
	request.DefaultBranch = "trunk"
	updated, err := store.UpsertProject(ctx, request, "admin")
	if err != nil || updated.DefaultBranch != "trunk" || updated.CreatedAt != created.CreatedAt {
		t.Fatalf("updated project = %#v, %v", updated, err)
	}
	items, err := store.ListProjects(ctx, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("projects = %#v, %v", items, err)
	}
	if _, err := store.UpsertProject(ctx, projects.UpsertRequest{
		ID: "escape", Provider: "local", Repository: "fixture/escape",
		DefaultBranch: "main", LocalRemoteName: "../remote.git",
	}, "admin"); !errors.Is(err, storage.ErrInvalid) {
		t.Fatalf("unsafe local remote returned %v", err)
	}
	events, err := store.ListAudit(ctx, 0, 20)
	if err != nil || len(events) != 2 || events[0].Action != "project.upsert" {
		t.Fatalf("project audit = %#v, %v", events, err)
	}
}
