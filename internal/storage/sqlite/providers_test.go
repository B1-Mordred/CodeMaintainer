package sqlite

import (
	"context"
	"testing"

	"github.com/B1-Mordred/CodeMaintainer/internal/providers"
)

func TestProviderGatewayProfilesAndEgressManifestsPersist(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := providers.NewService(store)
	if err := service.EnsureDefaults(ctx, "tester"); err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Providers) < 5 || len(status.Routes) < 6 || len(status.Models) < 2 {
		t.Fatalf("seeded provider status %#v", status)
	}
	roles := map[string]bool{}
	for _, route := range status.Routes {
		roles[route.Role] = true
	}
	for _, role := range []string{"implementation", "repair", "test_designer", "documentation", "qc"} {
		if !roles[role] {
			t.Fatalf("seeded provider routes omitted %s: %#v", role, status.Routes)
		}
	}
	for _, provider := range status.Providers {
		if provider.Remote && provider.Enabled {
			t.Fatalf("remote provider %s should be disabled by default", provider.ID)
		}
	}
	decision, err := service.SimulateRoute(ctx, providers.RouteRequest{
		ProjectID: "owner-repo", Role: "implementation", Purpose: "local implementation",
		DataClasses: []string{"task_metadata", "candidate_diff"}, EstimatedBytes: 2048, EstimatedTokens: 1024,
		RequiresStructuredOutput: true,
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != providers.DecisionAllowed || decision.Provider.ID != "local-llamacpp" {
		t.Fatalf("route decision %#v", decision)
	}
	manifests, err := store.ListEgressManifests(ctx, "owner-repo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) != 1 || manifests[0].ManifestSHA256 == "" || manifests[0].PolicyDecision != providers.DecisionAllowed {
		t.Fatalf("manifest %#v", manifests)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE provider_egress_manifests SET purpose='mutated' WHERE id=?", manifests[0].ID); err == nil {
		t.Fatal("provider egress manifest update unexpectedly succeeded")
	}
	probe, err := service.ProbeModel(ctx, "fake-remote-json", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if probe.Status != "passed" || !probe.Capabilities.ResponsesAPI || probe.RequestSchemaSHA256 == "" {
		t.Fatalf("probe %#v", probe)
	}
	probes, err := store.ListCapabilityProbes(ctx, "fake-remote-json", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != 1 || probes[0].ID != probe.ID {
		t.Fatalf("probes %#v", probes)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE provider_capability_probes SET status='failed' WHERE id=?", probe.ID); err == nil {
		t.Fatal("provider capability probe update unexpectedly succeeded")
	}
}

func TestProviderEndpointRejectsUnsafeRemoteURL(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	provider := providers.ProviderProfile{
		ID: "public-openai", SchemaVersion: providers.SchemaVersion, InterfaceFamily: providers.FamilyOpenAIResponses,
		DisplayName: "Public OpenAI", TrustTier: providers.TrustPublic, Remote: true, Enabled: false,
		ApprovedDataClasses: []string{"task_metadata"}, Version: 1,
	}
	if _, err := store.UpsertProviderProfile(ctx, provider, "tester"); err != nil {
		t.Fatal(err)
	}
	endpoint := providers.EndpointProfile{
		ID: "unsafe", ProviderID: provider.ID, BaseURL: "https://169.254.169.254/latest/meta-data",
		NetworkZone: providers.NetworkPublic, TLSMode: "verify", RedirectPolicy: "reject", DNSPolicy: "public_only",
		TimeoutMillis: 30000, HealthCheckPath: "/healthz", Version: 1,
	}
	if _, err := store.UpsertEndpointProfile(ctx, endpoint, "tester"); err == nil {
		t.Fatal("unsafe metadata endpoint was accepted")
	}
}
