package providers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{store: store, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	if s == nil || s.store == nil {
		return Status{}, errors.New("provider store is required")
	}
	if err := s.EnsureDefaults(ctx, "system"); err != nil {
		return Status{}, err
	}
	providers, err := s.store.ListProviderProfiles(ctx, 100)
	if err != nil {
		return Status{}, err
	}
	endpoints, err := s.store.ListEndpointProfiles(ctx, 100)
	if err != nil {
		return Status{}, err
	}
	models, err := s.store.ListModelProfiles(ctx, 200)
	if err != nil {
		return Status{}, err
	}
	routes, err := s.store.ListRouteProfiles(ctx, 100)
	if err != nil {
		return Status{}, err
	}
	manifests, err := s.store.ListEgressManifests(ctx, "", 25)
	if err != nil {
		return Status{}, err
	}
	probes, err := s.store.ListCapabilityProbes(ctx, "", 25)
	if err != nil {
		return Status{}, err
	}
	return Status{
		Families: RequiredFamilies(), Providers: providers, Endpoints: endpoints,
		Models: models, Routes: routes, RecentManifests: manifests, RecentProbes: probes,
		RemoteEnabledByDefault: false,
	}, nil
}

func (s *Service) EnsureDefaults(ctx context.Context, actor string) error {
	if s == nil || s.store == nil {
		return errors.New("provider store is required")
	}
	existing, err := s.store.ListProviderProfiles(ctx, 1)
	if err != nil {
		return err
	}
	if len(existing) != 0 {
		return nil
	}
	now := s.now()
	providers, endpoints, models, routes := DefaultProfiles(now)
	for _, profile := range providers {
		if _, err := s.store.UpsertProviderProfile(ctx, profile, actor); err != nil {
			return err
		}
	}
	for _, endpoint := range endpoints {
		if _, err := s.store.UpsertEndpointProfile(ctx, endpoint, actor); err != nil {
			return err
		}
	}
	for _, model := range models {
		if _, err := s.store.UpsertModelProfile(ctx, model, actor); err != nil {
			return err
		}
	}
	for _, route := range routes {
		if _, err := s.store.UpsertRouteProfile(ctx, route, actor); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) SimulateRoute(ctx context.Context, request RouteRequest, actor string) (RouteDecision, error) {
	if s == nil || s.store == nil {
		return RouteDecision{}, errors.New("provider store is required")
	}
	if err := s.EnsureDefaults(ctx, actor); err != nil {
		return RouteDecision{}, err
	}
	if err := request.Validate(); err != nil {
		return RouteDecision{}, err
	}
	if err := EnsureNoSecrets(request.DataClasses); err != nil {
		return s.recordDenied(ctx, request, "", err.Error())
	}
	providers, endpoints, models, routes, err := s.load(ctx)
	if err != nil {
		return RouteDecision{}, err
	}
	candidates := eligibleRoutes(routes, request.Role)
	if len(candidates) == 0 {
		return s.recordDenied(ctx, request, "", "no route profile is enabled for the requested role")
	}
	for _, route := range candidates {
		if !dataClassesAllowed(request.DataClasses, route.AllowedDataClasses) {
			continue
		}
		if request.EstimatedTokens > route.MaxTokensPerRequest {
			continue
		}
		for _, modelID := range route.OrderedModelIDs {
			model, ok := models[modelID]
			if !ok || !roleAllowed(request.Role, model.RoleEligibility) {
				continue
			}
			if request.RequiresStructuredOutput && !model.Capabilities.StructuredOutputs {
				continue
			}
			provider, ok := providers[model.ProviderID]
			if !ok || !provider.Enabled {
				continue
			}
			if !dataClassesAllowed(request.DataClasses, provider.ApprovedDataClasses) {
				continue
			}
			endpoint, ok := endpoints[model.EndpointID]
			if !ok {
				continue
			}
			if err := endpoint.Validate(provider); err != nil {
				continue
			}
			estimatedCost := estimateCostUSD(request.EstimatedTokens, model)
			if route.MaxCostUSD > 0 && estimatedCost > route.MaxCostUSD {
				continue
			}
			manifest := EgressManifest{
				JobID: request.JobID, ProjectID: request.ProjectID, RouteID: route.ID, ProviderID: provider.ID,
				EndpointID: endpoint.ID, ModelProfileID: model.ID, Purpose: request.Purpose,
				DataClasses: sortedStrings(request.DataClasses), ArtifactIDs: sortedStrings(request.ArtifactIDs),
				Redactions:     []string{"secret_scan", "credential_patterns", "hidden_reasoning", "raw_environment"},
				EstimatedBytes: request.EstimatedBytes, EstimatedTokens: request.EstimatedTokens,
				Retention: retentionFor(provider.TrustTier), PolicyDecision: DecisionAllowed,
				DecisionReason: fmt.Sprintf("selected %s via %s; capabilities: %s", model.ID, provider.InterfaceFamily, CapabilitySummary(model.Capabilities)),
			}
			return s.recordAllowed(ctx, route, provider, endpoint, model, manifest)
		}
	}
	return s.recordDenied(ctx, request, candidates[0].ID, "no enabled model satisfied route, data, capability, endpoint, cost, and trust constraints")
}

func (s *Service) ProbeModel(ctx context.Context, modelProfileID, actor string) (CapabilityProbe, error) {
	if s == nil || s.store == nil {
		return CapabilityProbe{}, errors.New("provider store is required")
	}
	if err := s.EnsureDefaults(ctx, actor); err != nil {
		return CapabilityProbe{}, err
	}
	if !safeID.MatchString(modelProfileID) || actor == "" {
		return CapabilityProbe{}, errors.New("model profile id and actor are required")
	}
	providerItems, endpointItems, modelItems, _, err := s.load(ctx)
	if err != nil {
		return CapabilityProbe{}, err
	}
	model, ok := modelItems[modelProfileID]
	if !ok {
		return CapabilityProbe{}, errors.New("model profile not found")
	}
	provider, ok := providerItems[model.ProviderID]
	if !ok {
		return CapabilityProbe{}, errors.New("provider profile not found")
	}
	endpoint, ok := endpointItems[model.EndpointID]
	if !ok {
		return CapabilityProbe{}, errors.New("endpoint profile not found")
	}
	adapter, ok := AdapterForFamily(provider.InterfaceFamily)
	if !ok {
		return CapabilityProbe{}, errors.New("provider adapter is not registered")
	}
	started := s.now()
	observation, err := adapter.Probe(ctx, provider, endpoint, model)
	probe := CapabilityProbe{
		ProviderID: provider.ID, EndpointID: endpoint.ID, ModelProfileID: model.ID,
		InterfaceFamily: provider.InterfaceFamily, Status: "passed", ObservedModelID: model.ModelID,
		NativeAPIShape: nativeShapeForFamily(provider.InterfaceFamily), Capabilities: CapabilitySet{},
		RequestSchemaSHA256:  sha256Text(requestShapeForFamily(provider.InterfaceFamily)),
		ResponseSchemaSHA256: sha256Text(responseShapeForFamily(provider.InterfaceFamily)),
		LatencyMillis:        s.now().Sub(started).Milliseconds(), ActorID: actor, CreatedAt: s.now(),
	}
	if err != nil {
		probe.Status = "failed"
		probe.Errors = []string{err.Error()}
	} else {
		probe.ObservedModelID = observation.ObservedModelID
		probe.NativeAPIShape = observation.NativeAPIShape
		probe.Capabilities = observation.Capabilities
		probe.RequestSchemaSHA256 = observation.RequestSchemaSHA256
		probe.ResponseSchemaSHA256 = observation.ResponseSchemaSHA256
		probe.LatencyMillis = observation.LatencyMillis
	}
	return s.store.RecordCapabilityProbe(ctx, probe)
}

func (s *Service) recordAllowed(ctx context.Context, route RouteProfile, provider ProviderProfile, endpoint EndpointProfile, model ModelProfile, manifest EgressManifest) (RouteDecision, error) {
	manifest.CreatedAt = s.now()
	hash, err := HashManifest(manifest)
	if err != nil {
		return RouteDecision{}, err
	}
	manifest.ManifestSHA256 = hash
	manifest, err = s.store.RecordEgressManifest(ctx, manifest)
	if err != nil {
		return RouteDecision{}, err
	}
	return RouteDecision{Status: DecisionAllowed, Reason: manifest.DecisionReason, Route: route, Provider: provider, Endpoint: endpoint, Model: model, EgressManifest: manifest}, nil
}

func (s *Service) recordDenied(ctx context.Context, request RouteRequest, routeID, reason string) (RouteDecision, error) {
	if routeID == "" {
		routeID = "unselected"
	}
	manifest := DenyManifest(request, routeID, reason)
	manifest.CreatedAt = s.now()
	hash, err := HashManifest(manifest)
	if err != nil {
		return RouteDecision{}, err
	}
	manifest.ManifestSHA256 = hash
	manifest, err = s.store.RecordEgressManifest(ctx, manifest)
	if err != nil {
		return RouteDecision{}, err
	}
	return RouteDecision{Status: DecisionDenied, Reason: reason, EgressManifest: manifest}, nil
}

func (s *Service) load(ctx context.Context) (map[string]ProviderProfile, map[string]EndpointProfile, map[string]ModelProfile, []RouteProfile, error) {
	providerItems, err := s.store.ListProviderProfiles(ctx, 100)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	endpointItems, err := s.store.ListEndpointProfiles(ctx, 100)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	modelItems, err := s.store.ListModelProfiles(ctx, 200)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	routeItems, err := s.store.ListRouteProfiles(ctx, 100)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	providers := make(map[string]ProviderProfile, len(providerItems))
	for _, item := range providerItems {
		providers[item.ID] = item
	}
	endpoints := make(map[string]EndpointProfile, len(endpointItems))
	for _, item := range endpointItems {
		endpoints[item.ID] = item
	}
	models := make(map[string]ModelProfile, len(modelItems))
	for _, item := range modelItems {
		models[item.ID] = item
	}
	return providers, endpoints, models, routeItems, nil
}

func eligibleRoutes(routes []RouteProfile, role string) []RouteProfile {
	result := []RouteProfile{}
	for _, route := range routes {
		if route.Enabled && route.Role == role {
			result = append(result, route)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Preference == result[j].Preference {
			return result[i].ID < result[j].ID
		}
		if result[i].Preference == "local_first" {
			return true
		}
		return result[j].Preference != "local_first"
	})
	return result
}

func dataClassesAllowed(requested, allowed []string) bool {
	allowedSet := map[string]bool{}
	for _, item := range allowed {
		allowedSet[item] = true
	}
	for _, item := range requested {
		if !allowedSet[item] {
			return false
		}
	}
	return true
}

func roleAllowed(role string, roles []string) bool {
	for _, candidate := range roles {
		if candidate == role {
			return true
		}
	}
	return false
}

func estimateCostUSD(tokens int, model ModelProfile) float64 {
	if tokens <= 0 {
		return 0
	}
	half := float64(tokens) / 2_000_000
	return half*model.InputPricePerMTok + half*model.OutputPricePerMTok
}

func retentionFor(trust string) string {
	switch trust {
	case TrustLocal:
		return "local-only; no remote retention"
	case TrustPrivate, TrustEnterprise:
		return "operator-asserted private retention profile"
	default:
		return "remote retention requires explicit provider assertion review"
	}
}
