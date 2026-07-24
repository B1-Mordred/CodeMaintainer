package providers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var ErrConflict = errors.New("provider profile changed; refresh and retry")

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
	now := s.now()
	providers, endpoints, models, routes := DefaultProfiles(now)
	existingProviders, err := s.store.ListProviderProfiles(ctx, 500)
	if err != nil {
		return err
	}
	providerIDs := make(map[string]bool, len(existingProviders))
	for _, profile := range existingProviders {
		providerIDs[profile.ID] = true
	}
	for _, profile := range providers {
		if providerIDs[profile.ID] {
			continue
		}
		if _, err := s.store.UpsertProviderProfile(ctx, profile, actor); err != nil {
			return err
		}
	}
	existingEndpoints, err := s.store.ListEndpointProfiles(ctx, 500)
	if err != nil {
		return err
	}
	endpointIDs := make(map[string]bool, len(existingEndpoints))
	for _, endpoint := range existingEndpoints {
		endpointIDs[endpoint.ID] = true
	}
	for _, endpoint := range endpoints {
		if endpointIDs[endpoint.ID] {
			continue
		}
		if _, err := s.store.UpsertEndpointProfile(ctx, endpoint, actor); err != nil {
			return err
		}
	}
	existingModels, err := s.store.ListModelProfiles(ctx, 500)
	if err != nil {
		return err
	}
	modelIDs := make(map[string]bool, len(existingModels))
	for _, model := range existingModels {
		modelIDs[model.ID] = true
	}
	for _, model := range models {
		if modelIDs[model.ID] {
			continue
		}
		if _, err := s.store.UpsertModelProfile(ctx, model, actor); err != nil {
			return err
		}
	}
	existingRoutes, err := s.store.ListRouteProfiles(ctx, 500)
	if err != nil {
		return err
	}
	routeIDs := make(map[string]bool, len(existingRoutes))
	for _, route := range existingRoutes {
		routeIDs[route.ID] = true
	}
	for _, route := range routes {
		if routeIDs[route.ID] {
			continue
		}
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
	denyReason := "no enabled model satisfied route, data, capability, endpoint, cost, and trust constraints"
	denyRouteID := candidates[0].ID
	for _, route := range candidates {
		if !dataClassesAllowed(request.DataClasses, route.AllowedDataClasses) {
			continue
		}
		if request.EstimatedTokens > route.MaxTokensPerRequest {
			continue
		}
		trustFloor := 0
		trustFloorTier := ""
		for _, modelID := range route.OrderedModelIDs {
			model, ok := models[modelID]
			if !ok || !roleAllowed(request.Role, model.RoleEligibility) {
				continue
			}
			provider, ok := providers[model.ProviderID]
			if !ok {
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
			candidateTrust := trustRank(provider.TrustTier)
			if fallbackPreservesTrust(route.FallbackPolicy) && trustFloor == 0 {
				trustFloor = candidateTrust
				trustFloorTier = provider.TrustTier
			}
			if fallbackPreservesTrust(route.FallbackPolicy) && trustFloor > 0 && candidateTrust < trustFloor {
				denyReason = fmt.Sprintf("route %s rejected lower-trust fallback from %s to %s", route.ID, trustFloorTier, provider.TrustTier)
				denyRouteID = route.ID
				continue
			}
			if provider.Remote {
				if err := s.remoteProbeAllows(ctx, provider, endpoint, model, request); err != nil {
					denyReason = err.Error()
					denyRouteID = route.ID
					continue
				}
			}
			if !provider.Enabled {
				continue
			}
			if request.RequiresStructuredOutput && !model.Capabilities.StructuredOutputs {
				continue
			}
			estimatedCost := estimateCostUSD(request.EstimatedTokens, model)
			if route.MaxCostUSD > 0 && estimatedCost > route.MaxCostUSD {
				denyReason = fmt.Sprintf("route %s rejected estimated cost %.6f above maximum %.6f", route.ID, estimatedCost, route.MaxCostUSD)
				denyRouteID = route.ID
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
	return s.recordDenied(ctx, request, denyRouteID, denyReason)
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

func (s *Service) UpdateProvider(ctx context.Context, id string, request UpdateProviderRequest, actor string) (ProviderProfile, error) {
	if s == nil || s.store == nil {
		return ProviderProfile{}, errors.New("provider store is required")
	}
	if err := s.EnsureDefaults(ctx, actor); err != nil {
		return ProviderProfile{}, err
	}
	if !safeID.MatchString(id) || request.Profile.ID != id || actor == "" || strings.TrimSpace(request.Reason) == "" || request.ExpectedVersion < 1 {
		return ProviderProfile{}, errors.New("provider update request is invalid")
	}
	current, err := s.findProvider(ctx, id)
	if err != nil {
		return ProviderProfile{}, err
	}
	if current.Version != request.ExpectedVersion {
		return ProviderProfile{}, ErrConflict
	}
	next := request.Profile
	next.SchemaVersion = SchemaVersion
	next.CreatedAt = current.CreatedAt
	next.UpdatedAt = current.UpdatedAt
	next.Version = current.Version
	if err := next.Validate(); err != nil {
		return ProviderProfile{}, err
	}
	return s.store.UpsertProviderProfile(ctx, next, actor)
}

func (s *Service) UpdateEndpoint(ctx context.Context, id string, request UpdateEndpointRequest, actor string) (EndpointProfile, error) {
	if s == nil || s.store == nil {
		return EndpointProfile{}, errors.New("provider store is required")
	}
	if err := s.EnsureDefaults(ctx, actor); err != nil {
		return EndpointProfile{}, err
	}
	if !safeID.MatchString(id) || request.Profile.ID != id || actor == "" || strings.TrimSpace(request.Reason) == "" || request.ExpectedVersion < 1 {
		return EndpointProfile{}, errors.New("endpoint update request is invalid")
	}
	providersByID, _, _, _, err := s.load(ctx)
	if err != nil {
		return EndpointProfile{}, err
	}
	current, err := s.findEndpoint(ctx, id)
	if err != nil {
		return EndpointProfile{}, err
	}
	if current.Version != request.ExpectedVersion {
		return EndpointProfile{}, ErrConflict
	}
	provider, ok := providersByID[request.Profile.ProviderID]
	if !ok {
		return EndpointProfile{}, errors.New("endpoint provider profile is not registered")
	}
	next := request.Profile
	next.CreatedAt = current.CreatedAt
	next.UpdatedAt = current.UpdatedAt
	next.Version = current.Version
	if err := next.Validate(provider); err != nil {
		return EndpointProfile{}, err
	}
	return s.store.UpsertEndpointProfile(ctx, next, actor)
}

func (s *Service) UpdateModel(ctx context.Context, id string, request UpdateModelRequest, actor string) (ModelProfile, error) {
	if s == nil || s.store == nil {
		return ModelProfile{}, errors.New("provider store is required")
	}
	if err := s.EnsureDefaults(ctx, actor); err != nil {
		return ModelProfile{}, err
	}
	if !safeID.MatchString(id) || request.Profile.ID != id || actor == "" || strings.TrimSpace(request.Reason) == "" || request.ExpectedVersion < 1 {
		return ModelProfile{}, errors.New("model update request is invalid")
	}
	providersByID, endpointsByID, _, _, err := s.load(ctx)
	if err != nil {
		return ModelProfile{}, err
	}
	current, err := s.findModel(ctx, id)
	if err != nil {
		return ModelProfile{}, err
	}
	if current.Version != request.ExpectedVersion {
		return ModelProfile{}, ErrConflict
	}
	provider, ok := providersByID[request.Profile.ProviderID]
	if !ok {
		return ModelProfile{}, errors.New("model provider profile is not registered")
	}
	endpoint, ok := endpointsByID[request.Profile.EndpointID]
	if !ok || endpoint.ProviderID != provider.ID {
		return ModelProfile{}, errors.New("model endpoint profile is not registered for provider")
	}
	next := request.Profile
	next.CreatedAt = current.CreatedAt
	next.UpdatedAt = current.UpdatedAt
	next.Version = current.Version
	if err := next.Validate(provider, endpoint); err != nil {
		return ModelProfile{}, err
	}
	return s.store.UpsertModelProfile(ctx, next, actor)
}

func (s *Service) UpdateRoute(ctx context.Context, id string, request UpdateRouteRequest, actor string) (RouteProfile, error) {
	if s == nil || s.store == nil {
		return RouteProfile{}, errors.New("provider store is required")
	}
	if err := s.EnsureDefaults(ctx, actor); err != nil {
		return RouteProfile{}, err
	}
	if !safeID.MatchString(id) || request.Profile.ID != id || actor == "" || strings.TrimSpace(request.Reason) == "" || request.ExpectedVersion < 1 {
		return RouteProfile{}, errors.New("route update request is invalid")
	}
	providersByID, _, modelsByID, _, err := s.load(ctx)
	if err != nil {
		return RouteProfile{}, err
	}
	current, err := s.findRoute(ctx, id)
	if err != nil {
		return RouteProfile{}, err
	}
	if current.Version != request.ExpectedVersion {
		return RouteProfile{}, ErrConflict
	}
	next := request.Profile
	next.CreatedAt = current.CreatedAt
	next.UpdatedAt = current.UpdatedAt
	next.Version = current.Version
	if err := next.Validate(); err != nil {
		return RouteProfile{}, err
	}
	usesRemote := false
	for _, modelID := range next.OrderedModelIDs {
		model, ok := modelsByID[modelID]
		if !ok {
			return RouteProfile{}, fmt.Errorf("route references unregistered model profile %q", modelID)
		}
		provider, ok := providersByID[model.ProviderID]
		if !ok {
			return RouteProfile{}, fmt.Errorf("route references model %q with unregistered provider", modelID)
		}
		if provider.Remote {
			usesRemote = true
		}
	}
	if next.Enabled && usesRemote && (!request.RemoteEgressApproved || strings.TrimSpace(request.RemoteEgressApprovalSummary) == "") {
		return RouteProfile{}, errors.New("enabling a remote-capable route requires explicit egress preview approval and rationale")
	}
	return s.store.UpsertRouteProfile(ctx, next, actor)
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

func (s *Service) findProvider(ctx context.Context, id string) (ProviderProfile, error) {
	items, err := s.store.ListProviderProfiles(ctx, 500)
	if err != nil {
		return ProviderProfile{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return ProviderProfile{}, errors.New("provider profile not found")
}

func (s *Service) findEndpoint(ctx context.Context, id string) (EndpointProfile, error) {
	items, err := s.store.ListEndpointProfiles(ctx, 500)
	if err != nil {
		return EndpointProfile{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return EndpointProfile{}, errors.New("endpoint profile not found")
}

func (s *Service) findModel(ctx context.Context, id string) (ModelProfile, error) {
	items, err := s.store.ListModelProfiles(ctx, 500)
	if err != nil {
		return ModelProfile{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return ModelProfile{}, errors.New("model profile not found")
}

func (s *Service) findRoute(ctx context.Context, id string) (RouteProfile, error) {
	items, err := s.store.ListRouteProfiles(ctx, 500)
	if err != nil {
		return RouteProfile{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return RouteProfile{}, errors.New("route profile not found")
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

func (s *Service) remoteProbeAllows(ctx context.Context, provider ProviderProfile, endpoint EndpointProfile, model ModelProfile, request RouteRequest) error {
	probes, err := s.store.ListCapabilityProbes(ctx, model.ID, 1)
	if err != nil {
		return err
	}
	if len(probes) == 0 {
		return fmt.Errorf("remote model %s has no retained successful capability probe", model.ID)
	}
	probe := probes[0]
	if probe.Status != "passed" {
		return fmt.Errorf("remote model %s latest capability probe failed", model.ID)
	}
	if probe.ProviderID != provider.ID || probe.EndpointID != endpoint.ID || probe.InterfaceFamily != provider.InterfaceFamily {
		return fmt.Errorf("remote model %s latest capability probe no longer matches provider endpoint", model.ID)
	}
	if probe.ObservedModelID != model.ModelID {
		return fmt.Errorf("remote model %s observed model drift: probe saw %s, profile pins %s", model.ID, probe.ObservedModelID, model.ModelID)
	}
	if request.RequiresStructuredOutput && !probe.Capabilities.StructuredOutputs {
		return fmt.Errorf("remote model %s latest capability probe lacks structured output support", model.ID)
	}
	if !capabilitySetCovers(model.Capabilities, probe.Capabilities) {
		return fmt.Errorf("remote model %s latest capability probe no longer covers pinned capability profile", model.ID)
	}
	return nil
}

func fallbackPreservesTrust(policy string) bool {
	return policy == "same_trust_or_stricter" || policy == "explicit_same_or_higher_trust_only"
}

func trustRank(trust string) int {
	switch trust {
	case TrustLocal:
		return 4
	case TrustEnterprise:
		return 3
	case TrustPrivate:
		return 2
	case TrustPublic:
		return 1
	default:
		return 0
	}
}

func capabilitySetCovers(required, observed CapabilitySet) bool {
	return (!required.ResponsesAPI || observed.ResponsesAPI) &&
		(!required.ChatCompletions || observed.ChatCompletions) &&
		(!required.Streaming || observed.Streaming) &&
		(!required.Cancellation || observed.Cancellation) &&
		(!required.StructuredOutputs || observed.StructuredOutputs) &&
		(!required.ToolCalls || observed.ToolCalls) &&
		(!required.ParallelToolCalls || observed.ParallelToolCalls) &&
		(!required.StableToolCallIDs || observed.StableToolCallIDs) &&
		(!required.SystemMessages || observed.SystemMessages) &&
		(!required.DeveloperMessages || observed.DeveloperMessages) &&
		(!required.UsageAccounting || observed.UsageAccounting) &&
		(!required.ReasoningControls || observed.ReasoningControls) &&
		(!required.PromptCaching || observed.PromptCaching) &&
		(!required.Batch || observed.Batch) &&
		(!required.Asynchronous || observed.Asynchronous) &&
		(!required.ModelListing || observed.ModelListing) &&
		(!required.ImmutableModelIDs || observed.ImmutableModelIDs)
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
