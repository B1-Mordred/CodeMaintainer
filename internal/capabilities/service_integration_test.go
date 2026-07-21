package capabilities_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/local-code-maintainer/appliance/internal/capabilities"
	"github.com/local-code-maintainer/appliance/internal/projects"
	"github.com/local-code-maintainer/appliance/internal/storage"
	storesqlite "github.com/local-code-maintainer/appliance/internal/storage/sqlite"
)

func testService(t *testing.T) (*capabilities.Service, *storesqlite.Store) {
	t.Helper()
	store, err := storesqlite.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.UpsertProject(context.Background(), projects.UpsertRequest{ID: "fixture", Provider: "local", Repository: "owner/fixture", DefaultBranch: "main", LocalRemoteName: "fixture.git"}, "admin"); err != nil {
		t.Fatal(err)
	}
	service, err := capabilities.NewService(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}

func TestCatalogIsChecksummedAndAuthorityNeutral(t *testing.T) {
	service, _ := testService(t)
	for _, manifest := range service.Catalog() {
		_, report, err := service.Manifest(manifest.ID, manifest.Version)
		if err != nil {
			t.Fatal(err)
		}
		if !report.ChecksumValid || !report.AuthoritySafe || !report.Compatible {
			t.Fatalf("untrusted built-in manifest: %#v", report)
		}
		encoded, _ := json.Marshal(manifest)
		for _, forbidden := range []string{`"image"`, `"mount"`, `"network"`, `"executable"`, `"environment"`, `"host_path"`} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("manifest exposes forbidden authority field %s", forbidden)
			}
		}
	}
}

func TestRepoDoctorRequiresReviewAndExactInstalledPack(t *testing.T) {
	service, store := testService(t)
	scan, err := service.Scan(context.Background(), capabilities.ScanInput{ProjectID: "fixture", Repository: "owner/fixture", Revision: "abc123", Files: []capabilities.SourceFile{{Path: "composer.json", Content: []byte(`{"require":{"php":"^8.3"}}`)}, {Path: "phpunit.xml", Content: []byte(`<phpunit/>`)}, {Path: ".github/workflows/ci.yml", Content: []byte("name: ci\n")}, {Path: "migrations/001.php", Content: []byte("<?php\n")}}}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Proposals) == 0 {
		t.Fatal("expected evidence-backed proposals")
	}
	assignments, err := store.ListCapabilityAssignments(context.Background(), "fixture", 10)
	if err != nil || len(assignments) != 0 {
		t.Fatalf("scan silently applied configuration: %#v %v", assignments, err)
	}
	var pack capabilities.Proposal
	for _, proposal := range scan.Proposals {
		if proposal.Key == "php83-intranet" {
			pack = proposal
			break
		}
	}
	if pack.ID == "" || len(pack.Evidence) == 0 || pack.State != "pending" {
		t.Fatalf("missing disabled pack proposal: %#v", pack)
	}
	preview, err := service.DryRun(context.Background(), "fixture", scan.ID, pack.ID, json.RawMessage(`{"tests":{"coverage":"pcov"}}`))
	if err != nil || preview["will_modify_repository"] != false {
		t.Fatalf("unsafe dry run: %#v %v", preview, err)
	}
	_, _, err = service.Review(context.Background(), capabilities.ReviewRequest{ScanID: scan.ID, ProposalID: pack.ID, ExpectedVersion: 1, Accept: true, ActorID: "operator", Reason: "reviewed evidence", Config: json.RawMessage(`{}`)})
	if !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("assignment without installed pack error = %v", err)
	}
	manifest, _, _ := service.Manifest("php83-intranet", "1.0.0")
	if _, _, err := service.Transition(context.Background(), capabilities.TransitionRequest{PackID: manifest.ID, Action: "install", TargetVersion: manifest.Version, ActorID: "admin", Reason: "approved trusted pack"}, false); err == nil {
		t.Fatal("installation did not require reauthentication")
	}
	if _, _, err := service.Transition(context.Background(), capabilities.TransitionRequest{PackID: manifest.ID, Action: "install", TargetVersion: manifest.Version, ActorID: "admin", Reason: "approved trusted pack"}, true); err != nil {
		t.Fatal(err)
	}
	accepted, assignment, err := service.Review(context.Background(), capabilities.ReviewRequest{ScanID: scan.ID, ProposalID: pack.ID, ExpectedVersion: 1, Accept: true, ActorID: "operator", Reason: "reviewed evidence", Config: json.RawMessage(`{"tests":{"coverage":"pcov"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.State != "accepted" || assignment == nil || !assignment.Enabled || assignment.PackVersion != "1.0.0" {
		t.Fatalf("unexpected accepted proposal: %#v %#v", accepted, assignment)
	}
	if _, _, err := service.Review(context.Background(), capabilities.ReviewRequest{ScanID: scan.ID, ProposalID: pack.ID, ExpectedVersion: 1, Accept: false, ActorID: "operator", Reason: "stale review"}); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale review error = %v", err)
	}
}

func TestLifecyclePinUpgradeAndRollbackAreAuditedAndReversible(t *testing.T) {
	service, store := testService(t)
	base := service.Catalog()[0]
	base.ID = "lifecycle-fixture"
	base.Version = "1.0.0"
	base.ChecksumSHA256 = ""
	base.ChecksumSHA256, _ = capabilities.ComputeChecksum(base)
	next := base
	next.Version = "1.1.0"
	next.ChecksumSHA256 = ""
	next.ChecksumSHA256, _ = capabilities.ComputeChecksum(next)
	catalog, err := capabilities.NewCatalog([]capabilities.Manifest{base, next})
	if err != nil {
		t.Fatal(err)
	}
	service, err = capabilities.NewService(store, catalog)
	if err != nil {
		t.Fatal(err)
	}
	installed, _, err := service.Transition(context.Background(), capabilities.TransitionRequest{PackID: base.ID, Action: "install", TargetVersion: base.Version, ActorID: "admin", Reason: "fixture install"}, true)
	if err != nil {
		t.Fatal(err)
	}
	pinned, _, err := service.Transition(context.Background(), capabilities.TransitionRequest{PackID: base.ID, Action: "pin", ExpectedRevision: installed.Revision, ActorID: "admin", Reason: "freeze version"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Transition(context.Background(), capabilities.TransitionRequest{PackID: base.ID, Action: "upgrade", TargetVersion: next.Version, ExpectedRevision: pinned.Revision, ActorID: "admin", Reason: "blocked upgrade"}, true); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("pinned upgrade error=%v", err)
	}
	unpinned, _, err := service.Transition(context.Background(), capabilities.TransitionRequest{PackID: base.ID, Action: "unpin", ExpectedRevision: pinned.Revision, ActorID: "admin", Reason: "approve upgrade"}, true)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, _, err := service.Transition(context.Background(), capabilities.TransitionRequest{PackID: base.ID, Action: "upgrade", TargetVersion: next.Version, ExpectedRevision: unpinned.Revision, ActorID: "admin", Reason: "reviewed upgrade"}, true)
	if err != nil {
		t.Fatal(err)
	}
	rolled, _, err := service.Transition(context.Background(), capabilities.TransitionRequest{PackID: base.ID, Action: "rollback", TargetVersion: base.Version, ExpectedRevision: upgraded.Revision, ActorID: "admin", Reason: "regression rollback"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if rolled.PackVersion != base.Version || rolled.Previous != next.Version {
		t.Fatalf("unexpected rollback %#v", rolled)
	}
	events, err := service.Events(context.Background(), base.ID)
	if err != nil || len(events) != 5 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
}
