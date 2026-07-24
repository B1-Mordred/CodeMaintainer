package providers

import (
	"context"
	"testing"
	"time"
)

type memoryStore struct {
	providers []ProviderProfile
	endpoints []EndpointProfile
	models    []ModelProfile
	routes    []RouteProfile
	manifests []EgressManifest
	probes    []CapabilityProbe
}

func (m *memoryStore) ListProviderProfiles(context.Context, int) ([]ProviderProfile, error) {
	return append([]ProviderProfile(nil), m.providers...), nil
}
func (m *memoryStore) UpsertProviderProfile(_ context.Context, profile ProviderProfile, _ string) (ProviderProfile, error) {
	for index, existing := range m.providers {
		if existing.ID == profile.ID {
			profile.Version = existing.Version + 1
			m.providers[index] = profile
			return profile, nil
		}
	}
	m.providers = append(m.providers, profile)
	return profile, nil
}
func (m *memoryStore) ListEndpointProfiles(context.Context, int) ([]EndpointProfile, error) {
	return append([]EndpointProfile(nil), m.endpoints...), nil
}
func (m *memoryStore) UpsertEndpointProfile(_ context.Context, endpoint EndpointProfile, _ string) (EndpointProfile, error) {
	for index, existing := range m.endpoints {
		if existing.ID == endpoint.ID {
			endpoint.Version = existing.Version + 1
			m.endpoints[index] = endpoint
			return endpoint, nil
		}
	}
	m.endpoints = append(m.endpoints, endpoint)
	return endpoint, nil
}
func (m *memoryStore) ListModelProfiles(context.Context, int) ([]ModelProfile, error) {
	return append([]ModelProfile(nil), m.models...), nil
}
func (m *memoryStore) UpsertModelProfile(_ context.Context, model ModelProfile, _ string) (ModelProfile, error) {
	for index, existing := range m.models {
		if existing.ID == model.ID {
			model.Version = existing.Version + 1
			m.models[index] = model
			return model, nil
		}
	}
	m.models = append(m.models, model)
	return model, nil
}
func (m *memoryStore) ListRouteProfiles(context.Context, int) ([]RouteProfile, error) {
	return append([]RouteProfile(nil), m.routes...), nil
}
func (m *memoryStore) UpsertRouteProfile(_ context.Context, route RouteProfile, _ string) (RouteProfile, error) {
	for index, existing := range m.routes {
		if existing.ID == route.ID {
			route.Version = existing.Version + 1
			m.routes[index] = route
			return route, nil
		}
	}
	m.routes = append(m.routes, route)
	return route, nil
}
func (m *memoryStore) RecordEgressManifest(_ context.Context, manifest EgressManifest) (EgressManifest, error) {
	if manifest.ID == "" {
		manifest.ID = "egress-memory"
	}
	m.manifests = append(m.manifests, manifest)
	return manifest, nil
}
func (m *memoryStore) ListEgressManifests(context.Context, string, int) ([]EgressManifest, error) {
	return append([]EgressManifest(nil), m.manifests...), nil
}
func (m *memoryStore) RecordCapabilityProbe(_ context.Context, probe CapabilityProbe) (CapabilityProbe, error) {
	if probe.ID == "" {
		probe.ID = "probe-memory"
	}
	m.probes = append(m.probes, probe)
	return probe, nil
}
func (m *memoryStore) ListCapabilityProbes(context.Context, string, int) ([]CapabilityProbe, error) {
	return append([]CapabilityProbe(nil), m.probes...), nil
}

func TestDefaultsKeepRemoteProvidersDisabledAndRouteLocal(t *testing.T) {
	store := &memoryStore{}
	service := NewService(store)
	service.now = func() time.Time { return time.Date(2026, 7, 24, 4, 10, 0, 0, time.UTC) }
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Families) != 10 || status.RemoteEnabledByDefault {
		t.Fatalf("status %#v", status)
	}
	if len(status.Providers) != len(status.Families) || len(status.Models) != len(status.Families) {
		t.Fatalf("default provider/model coverage providers=%d models=%d families=%d", len(status.Providers), len(status.Models), len(status.Families))
	}
	for _, provider := range status.Providers {
		if provider.Remote && provider.Enabled {
			t.Fatalf("remote provider %s should default disabled", provider.ID)
		}
	}
	decision, err := service.SimulateRoute(context.Background(), RouteRequest{
		ProjectID: "owner-repo", Role: "implementation", Purpose: "implementation structured JSON",
		DataClasses: []string{"task_metadata", "candidate_diff"}, EstimatedBytes: 4096, EstimatedTokens: 1024,
		RequiresStructuredOutput: true,
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != DecisionAllowed || decision.Provider.ID != "local-llamacpp" || decision.EgressManifest.PolicyDecision != DecisionAllowed {
		t.Fatalf("decision %#v", decision)
	}
	if decision.EgressManifest.ManifestSHA256 == "" || len(store.manifests) != 1 {
		t.Fatalf("manifest not retained: %#v", decision.EgressManifest)
	}
}

func TestEveryRequiredFamilyHasFakeAdapterAndRetainedProbe(t *testing.T) {
	store := &memoryStore{}
	service := NewService(store)
	service.now = func() time.Time { return time.Date(2026, 7, 24, 4, 30, 0, 0, time.UTC) }
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	modelByFamily := map[string]string{}
	for _, model := range status.Models {
		for _, provider := range status.Providers {
			if provider.ID == model.ProviderID {
				modelByFamily[provider.InterfaceFamily] = model.ID
			}
		}
	}
	for _, family := range RequiredFamilies() {
		if _, ok := AdapterForFamily(family); !ok {
			t.Fatalf("no adapter for %s", family)
		}
		modelID, ok := modelByFamily[family]
		if !ok {
			t.Fatalf("no seeded model profile for %s", family)
		}
		probe, err := service.ProbeModel(context.Background(), modelID, "tester")
		if err != nil {
			t.Fatalf("probe %s: %v", family, err)
		}
		if probe.Status != "passed" || probe.InterfaceFamily != family || probe.RequestSchemaSHA256 == "" || probe.ResponseSchemaSHA256 == "" {
			t.Fatalf("bad probe for %s: %#v", family, probe)
		}
		if family == FamilyOpenAICompatible && !probe.Capabilities.ChatCompletions {
			t.Fatalf("openai-compatible probe did not record chat-completion support: %#v", probe)
		}
	}
	if len(store.probes) != len(RequiredFamilies()) {
		t.Fatalf("retained probes = %d, want %d", len(store.probes), len(RequiredFamilies()))
	}
}

func TestRemoteRouteFailsClosedForForbiddenDataAndDisabledProfile(t *testing.T) {
	store := &memoryStore{}
	service := NewService(store)
	service.now = func() time.Time { return time.Date(2026, 7, 24, 4, 11, 0, 0, time.UTC) }
	decision, err := service.SimulateRoute(context.Background(), RouteRequest{
		ProjectID: "owner-repo", Role: "documentation", Purpose: "documentation preview",
		DataClasses: []string{"memory_excerpt"}, EstimatedBytes: 1024, EstimatedTokens: 512,
		RequiresStructuredOutput: true,
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != DecisionDenied || decision.EgressManifest.PolicyDecision != DecisionDenied {
		t.Fatalf("remote disabled/data decision %#v", decision)
	}
	secret, err := service.SimulateRoute(context.Background(), RouteRequest{
		ProjectID: "owner-repo", Role: "documentation", Purpose: "bad data",
		DataClasses: []string{"secrets"}, EstimatedBytes: 10, EstimatedTokens: 10,
	}, "tester")
	if err == nil || secret.Status == DecisionAllowed {
		t.Fatalf("forbidden data class should be rejected before manifest: %#v err=%v", secret, err)
	}
}

func TestEndpointRejectsPrivateAddressUnlessProfileAllowsIt(t *testing.T) {
	provider := ProviderProfile{ID: "public", SchemaVersion: SchemaVersion, InterfaceFamily: FamilyOpenAIResponses, DisplayName: "Public", TrustTier: TrustPublic, Remote: true, Enabled: true, ApprovedDataClasses: []string{"task_metadata"}, Version: 1}
	endpoint := EndpointProfile{ID: "endpoint", ProviderID: "public", BaseURL: "https://127.0.0.1/v1", NetworkZone: NetworkPublic, TLSMode: "verify", RedirectPolicy: "reject", DNSPolicy: "public_only", TimeoutMillis: 30000, HealthCheckPath: "/healthz", Version: 1}
	if err := endpoint.Validate(provider); err == nil {
		t.Fatal("public remote endpoint accepted a loopback target")
	}
	endpoint.BaseURL = "https://providers.invalid/v1"
	if err := endpoint.Validate(provider); err != nil {
		t.Fatalf("safe documentation-domain endpoint rejected: %v", err)
	}
}
