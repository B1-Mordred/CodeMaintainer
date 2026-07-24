package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/B1-Mordred/CodeMaintainer/internal/providers"
	"github.com/B1-Mordred/CodeMaintainer/internal/storage"
)

func (s *Store) ListProviderProfiles(ctx context.Context, limit int) ([]providers.ProviderProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,schema_version,interface_family,display_name,trust_tier,remote,enabled,
		approved_data_classes_json,credential_ref,credential_configured,operator_assertions_json,version,created_at,updated_at
		FROM provider_profiles ORDER BY updated_at DESC,id LIMIT ?`, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []providers.ProviderProfile{}
	for rows.Next() {
		item, err := scanProviderProfile(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpsertProviderProfile(ctx context.Context, profile providers.ProviderProfile, actor string) (providers.ProviderProfile, error) {
	now := s.now()
	if profile.SchemaVersion == 0 {
		profile.SchemaVersion = providers.SchemaVersion
	}
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = now
	}
	profile.UpdatedAt = now
	if profile.Version == 0 {
		profile.Version = 1
	}
	profile.CredentialConfigured = profile.CredentialRef != ""
	if err := profile.Validate(); err != nil {
		return providers.ProviderProfile{}, storage.ErrInvalid
	}
	classes, _ := json.Marshal(profile.ApprovedDataClasses)
	assertions, _ := json.Marshal(profile.OperatorAssertions)
	_, err := s.db.ExecContext(ctx, `INSERT INTO provider_profiles(
		id,schema_version,interface_family,display_name,trust_tier,remote,enabled,approved_data_classes_json,
		credential_ref,credential_configured,operator_assertions_json,version,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET interface_family=excluded.interface_family,display_name=excluded.display_name,
		trust_tier=excluded.trust_tier,remote=excluded.remote,enabled=excluded.enabled,
		approved_data_classes_json=excluded.approved_data_classes_json,credential_ref=excluded.credential_ref,
		credential_configured=excluded.credential_configured,operator_assertions_json=excluded.operator_assertions_json,
		version=provider_profiles.version+1,updated_at=excluded.updated_at`,
		profile.ID, profile.SchemaVersion, profile.InterfaceFamily, profile.DisplayName, profile.TrustTier, boolInt(profile.Remote),
		boolInt(profile.Enabled), string(classes), profile.CredentialRef, boolInt(profile.CredentialConfigured), string(assertions),
		profile.Version, profile.CreatedAt.Format(timestampFormat), profile.UpdatedAt.Format(timestampFormat))
	if err != nil {
		return providers.ProviderProfile{}, err
	}
	return s.getProviderProfile(ctx, profile.ID)
}

func (s *Store) getProviderProfile(ctx context.Context, id string) (providers.ProviderProfile, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,schema_version,interface_family,display_name,trust_tier,remote,enabled,
		approved_data_classes_json,credential_ref,credential_configured,operator_assertions_json,version,created_at,updated_at
		FROM provider_profiles WHERE id = ?`, id)
	return scanProviderProfile(row)
}

func scanProviderProfile(row scanner) (providers.ProviderProfile, error) {
	var item providers.ProviderProfile
	var classes, assertions, created, updated string
	var remote, enabled, credentialConfigured int
	err := row.Scan(&item.ID, &item.SchemaVersion, &item.InterfaceFamily, &item.DisplayName, &item.TrustTier,
		&remote, &enabled, &classes, &item.CredentialRef, &credentialConfigured, &assertions, &item.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return providers.ProviderProfile{}, storage.ErrNotFound
	}
	if err != nil {
		return providers.ProviderProfile{}, err
	}
	item.Remote = remote == 1
	item.Enabled = enabled == 1
	item.CredentialConfigured = credentialConfigured == 1
	if err := json.Unmarshal([]byte(classes), &item.ApprovedDataClasses); err != nil {
		return providers.ProviderProfile{}, err
	}
	if err := json.Unmarshal([]byte(assertions), &item.OperatorAssertions); err != nil {
		return providers.ProviderProfile{}, err
	}
	var parseErr error
	if item.CreatedAt, parseErr = time.Parse(timestampFormat, created); parseErr != nil {
		return providers.ProviderProfile{}, parseErr
	}
	if item.UpdatedAt, parseErr = time.Parse(timestampFormat, updated); parseErr != nil {
		return providers.ProviderProfile{}, parseErr
	}
	return item, nil
}

func (s *Store) ListEndpointProfiles(ctx context.Context, limit int) ([]providers.EndpointProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,provider_id,base_url,region,network_zone,allow_private_address,
		tls_mode,redirect_policy,dns_policy,timeout_millis,health_check_path,version,created_at,updated_at
		FROM endpoint_profiles ORDER BY updated_at DESC,id LIMIT ?`, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []providers.EndpointProfile{}
	for rows.Next() {
		item, err := scanEndpointProfile(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpsertEndpointProfile(ctx context.Context, endpoint providers.EndpointProfile, actor string) (providers.EndpointProfile, error) {
	provider, err := s.getProviderProfile(ctx, endpoint.ProviderID)
	if err != nil {
		return providers.EndpointProfile{}, err
	}
	now := s.now()
	if endpoint.CreatedAt.IsZero() {
		endpoint.CreatedAt = now
	}
	endpoint.UpdatedAt = now
	if endpoint.Version == 0 {
		endpoint.Version = 1
	}
	if err := endpoint.Validate(provider); err != nil {
		return providers.EndpointProfile{}, storage.ErrInvalid
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO endpoint_profiles(
		id,provider_id,base_url,region,network_zone,allow_private_address,tls_mode,redirect_policy,dns_policy,
		timeout_millis,health_check_path,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET base_url=excluded.base_url,region=excluded.region,network_zone=excluded.network_zone,
		allow_private_address=excluded.allow_private_address,tls_mode=excluded.tls_mode,redirect_policy=excluded.redirect_policy,
		dns_policy=excluded.dns_policy,timeout_millis=excluded.timeout_millis,health_check_path=excluded.health_check_path,
		version=endpoint_profiles.version+1,updated_at=excluded.updated_at`,
		endpoint.ID, endpoint.ProviderID, endpoint.BaseURL, endpoint.Region, endpoint.NetworkZone, boolInt(endpoint.AllowPrivateAddress),
		endpoint.TLSMode, endpoint.RedirectPolicy, endpoint.DNSPolicy, endpoint.TimeoutMillis, endpoint.HealthCheckPath,
		endpoint.Version, endpoint.CreatedAt.Format(timestampFormat), endpoint.UpdatedAt.Format(timestampFormat))
	if err != nil {
		return providers.EndpointProfile{}, err
	}
	return s.getEndpointProfile(ctx, endpoint.ID)
}

func (s *Store) getEndpointProfile(ctx context.Context, id string) (providers.EndpointProfile, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,provider_id,base_url,region,network_zone,allow_private_address,
		tls_mode,redirect_policy,dns_policy,timeout_millis,health_check_path,version,created_at,updated_at
		FROM endpoint_profiles WHERE id = ?`, id)
	return scanEndpointProfile(row)
}

func scanEndpointProfile(row scanner) (providers.EndpointProfile, error) {
	var item providers.EndpointProfile
	var allowPrivate int
	var created, updated string
	err := row.Scan(&item.ID, &item.ProviderID, &item.BaseURL, &item.Region, &item.NetworkZone, &allowPrivate,
		&item.TLSMode, &item.RedirectPolicy, &item.DNSPolicy, &item.TimeoutMillis, &item.HealthCheckPath,
		&item.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return providers.EndpointProfile{}, storage.ErrNotFound
	}
	if err != nil {
		return providers.EndpointProfile{}, err
	}
	item.AllowPrivateAddress = allowPrivate == 1
	var errParse error
	if item.CreatedAt, errParse = time.Parse(timestampFormat, created); errParse != nil {
		return providers.EndpointProfile{}, errParse
	}
	if item.UpdatedAt, errParse = time.Parse(timestampFormat, updated); errParse != nil {
		return providers.EndpointProfile{}, errParse
	}
	return item, nil
}

func (s *Store) ListModelProfiles(ctx context.Context, limit int) ([]providers.ModelProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,provider_id,endpoint_id,model_id,display_name,role_eligibility_json,
		capabilities_json,context_limit,output_limit,input_price_per_mtok,output_price_per_mtok,quality_status,
		capability_override,override_reason,override_expires_at,version,created_at,updated_at
		FROM model_profiles ORDER BY updated_at DESC,id LIMIT ?`, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []providers.ModelProfile{}
	for rows.Next() {
		item, err := scanModelProfile(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpsertModelProfile(ctx context.Context, model providers.ModelProfile, actor string) (providers.ModelProfile, error) {
	provider, err := s.getProviderProfile(ctx, model.ProviderID)
	if err != nil {
		return providers.ModelProfile{}, err
	}
	endpoint, err := s.getEndpointProfile(ctx, model.EndpointID)
	if err != nil {
		return providers.ModelProfile{}, err
	}
	now := s.now()
	if model.CreatedAt.IsZero() {
		model.CreatedAt = now
	}
	model.UpdatedAt = now
	if model.Version == 0 {
		model.Version = 1
	}
	if err := model.Validate(provider, endpoint); err != nil {
		return providers.ModelProfile{}, storage.ErrInvalid
	}
	roles, _ := json.Marshal(model.RoleEligibility)
	capabilities, _ := json.Marshal(model.Capabilities)
	overrideExpires := ""
	if !model.OverrideExpiresAt.IsZero() {
		overrideExpires = model.OverrideExpiresAt.Format(timestampFormat)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO model_profiles(
		id,provider_id,endpoint_id,model_id,display_name,role_eligibility_json,capabilities_json,
		context_limit,output_limit,input_price_per_mtok,output_price_per_mtok,quality_status,capability_override,
		override_reason,override_expires_at,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET model_id=excluded.model_id,display_name=excluded.display_name,
		role_eligibility_json=excluded.role_eligibility_json,capabilities_json=excluded.capabilities_json,
		context_limit=excluded.context_limit,output_limit=excluded.output_limit,input_price_per_mtok=excluded.input_price_per_mtok,
		output_price_per_mtok=excluded.output_price_per_mtok,quality_status=excluded.quality_status,
		capability_override=excluded.capability_override,override_reason=excluded.override_reason,
		override_expires_at=excluded.override_expires_at,version=model_profiles.version+1,updated_at=excluded.updated_at`,
		model.ID, model.ProviderID, model.EndpointID, model.ModelID, model.DisplayName, string(roles), string(capabilities),
		model.ContextLimit, model.OutputLimit, model.InputPricePerMTok, model.OutputPricePerMTok, model.QualityStatus,
		boolInt(model.CapabilityOverride), model.OverrideReason, overrideExpires, model.Version,
		model.CreatedAt.Format(timestampFormat), model.UpdatedAt.Format(timestampFormat))
	if err != nil {
		return providers.ModelProfile{}, err
	}
	return s.getModelProfile(ctx, model.ID)
}

func (s *Store) getModelProfile(ctx context.Context, id string) (providers.ModelProfile, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,provider_id,endpoint_id,model_id,display_name,role_eligibility_json,
		capabilities_json,context_limit,output_limit,input_price_per_mtok,output_price_per_mtok,quality_status,
		capability_override,override_reason,override_expires_at,version,created_at,updated_at
		FROM model_profiles WHERE id = ?`, id)
	return scanModelProfile(row)
}

func scanModelProfile(row scanner) (providers.ModelProfile, error) {
	var item providers.ModelProfile
	var roles, capabilities, overrideExpires, created, updated string
	var capabilityOverride int
	err := row.Scan(&item.ID, &item.ProviderID, &item.EndpointID, &item.ModelID, &item.DisplayName, &roles,
		&capabilities, &item.ContextLimit, &item.OutputLimit, &item.InputPricePerMTok, &item.OutputPricePerMTok,
		&item.QualityStatus, &capabilityOverride, &item.OverrideReason, &overrideExpires, &item.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return providers.ModelProfile{}, storage.ErrNotFound
	}
	if err != nil {
		return providers.ModelProfile{}, err
	}
	item.CapabilityOverride = capabilityOverride == 1
	if err := json.Unmarshal([]byte(roles), &item.RoleEligibility); err != nil {
		return providers.ModelProfile{}, err
	}
	if err := json.Unmarshal([]byte(capabilities), &item.Capabilities); err != nil {
		return providers.ModelProfile{}, err
	}
	if overrideExpires != "" {
		parsed, err := time.Parse(timestampFormat, overrideExpires)
		if err != nil {
			return providers.ModelProfile{}, err
		}
		item.OverrideExpiresAt = parsed
	}
	var errParse error
	if item.CreatedAt, errParse = time.Parse(timestampFormat, created); errParse != nil {
		return providers.ModelProfile{}, errParse
	}
	if item.UpdatedAt, errParse = time.Parse(timestampFormat, updated); errParse != nil {
		return providers.ModelProfile{}, errParse
	}
	return item, nil
}

func (s *Store) ListRouteProfiles(ctx context.Context, limit int) ([]providers.RouteProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,role,preference,ordered_model_ids_json,allowed_data_classes_json,
		max_tokens_per_request,max_cost_usd,retry_budget,fallback_policy,batch_policy,enabled,version,created_at,updated_at
		FROM route_profiles ORDER BY role,id LIMIT ?`, boundedLimit(limit, 100, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []providers.RouteProfile{}
	for rows.Next() {
		item, err := scanRouteProfile(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpsertRouteProfile(ctx context.Context, route providers.RouteProfile, actor string) (providers.RouteProfile, error) {
	now := s.now()
	if route.CreatedAt.IsZero() {
		route.CreatedAt = now
	}
	route.UpdatedAt = now
	if route.Version == 0 {
		route.Version = 1
	}
	if err := route.Validate(); err != nil {
		return providers.RouteProfile{}, storage.ErrInvalid
	}
	models, _ := json.Marshal(route.OrderedModelIDs)
	classes, _ := json.Marshal(route.AllowedDataClasses)
	_, err := s.db.ExecContext(ctx, `INSERT INTO route_profiles(
		id,role,preference,ordered_model_ids_json,allowed_data_classes_json,max_tokens_per_request,max_cost_usd,
		retry_budget,fallback_policy,batch_policy,enabled,version,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET role=excluded.role,preference=excluded.preference,
		ordered_model_ids_json=excluded.ordered_model_ids_json,allowed_data_classes_json=excluded.allowed_data_classes_json,
		max_tokens_per_request=excluded.max_tokens_per_request,max_cost_usd=excluded.max_cost_usd,
		retry_budget=excluded.retry_budget,fallback_policy=excluded.fallback_policy,batch_policy=excluded.batch_policy,
		enabled=excluded.enabled,version=route_profiles.version+1,updated_at=excluded.updated_at`,
		route.ID, route.Role, route.Preference, string(models), string(classes), route.MaxTokensPerRequest,
		route.MaxCostUSD, route.RetryBudget, route.FallbackPolicy, route.BatchPolicy, boolInt(route.Enabled),
		route.Version, route.CreatedAt.Format(timestampFormat), route.UpdatedAt.Format(timestampFormat))
	if err != nil {
		return providers.RouteProfile{}, err
	}
	return s.getRouteProfile(ctx, route.ID)
}

func (s *Store) getRouteProfile(ctx context.Context, id string) (providers.RouteProfile, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,role,preference,ordered_model_ids_json,allowed_data_classes_json,
		max_tokens_per_request,max_cost_usd,retry_budget,fallback_policy,batch_policy,enabled,version,created_at,updated_at
		FROM route_profiles WHERE id = ?`, id)
	return scanRouteProfile(row)
}

func scanRouteProfile(row scanner) (providers.RouteProfile, error) {
	var item providers.RouteProfile
	var models, classes, created, updated string
	var enabled int
	err := row.Scan(&item.ID, &item.Role, &item.Preference, &models, &classes, &item.MaxTokensPerRequest,
		&item.MaxCostUSD, &item.RetryBudget, &item.FallbackPolicy, &item.BatchPolicy, &enabled,
		&item.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return providers.RouteProfile{}, storage.ErrNotFound
	}
	if err != nil {
		return providers.RouteProfile{}, err
	}
	item.Enabled = enabled == 1
	if err := json.Unmarshal([]byte(models), &item.OrderedModelIDs); err != nil {
		return providers.RouteProfile{}, err
	}
	if err := json.Unmarshal([]byte(classes), &item.AllowedDataClasses); err != nil {
		return providers.RouteProfile{}, err
	}
	var errParse error
	if item.CreatedAt, errParse = time.Parse(timestampFormat, created); errParse != nil {
		return providers.RouteProfile{}, errParse
	}
	if item.UpdatedAt, errParse = time.Parse(timestampFormat, updated); errParse != nil {
		return providers.RouteProfile{}, errParse
	}
	return item, nil
}

func (s *Store) RecordEgressManifest(ctx context.Context, manifest providers.EgressManifest) (providers.EgressManifest, error) {
	if manifest.ID == "" {
		id, err := NewID("egress")
		if err != nil {
			return providers.EgressManifest{}, err
		}
		manifest.ID = id
	}
	if manifest.CreatedAt.IsZero() {
		manifest.CreatedAt = s.now()
	}
	if err := manifest.Validate(); err != nil {
		return providers.EgressManifest{}, storage.ErrInvalid
	}
	classes, _ := json.Marshal(manifest.DataClasses)
	artifacts, _ := json.Marshal(manifest.ArtifactIDs)
	redactions, _ := json.Marshal(manifest.Redactions)
	_, err := s.db.ExecContext(ctx, `INSERT INTO provider_egress_manifests(
		id,job_id,project_id,route_id,provider_id,endpoint_id,model_profile_id,purpose,data_classes_json,
		artifact_ids_json,redactions_json,estimated_bytes,estimated_tokens,retention,policy_decision,decision_reason,
		manifest_sha256,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		manifest.ID, manifest.JobID, manifest.ProjectID, manifest.RouteID, manifest.ProviderID, manifest.EndpointID,
		manifest.ModelProfileID, manifest.Purpose, string(classes), string(artifacts), string(redactions),
		manifest.EstimatedBytes, manifest.EstimatedTokens, manifest.Retention, manifest.PolicyDecision,
		manifest.DecisionReason, manifest.ManifestSHA256, manifest.CreatedAt.Format(timestampFormat))
	if err != nil {
		return providers.EgressManifest{}, err
	}
	return manifest, nil
}

func (s *Store) ListEgressManifests(ctx context.Context, projectID string, limit int) ([]providers.EgressManifest, error) {
	query := `SELECT id,job_id,project_id,route_id,provider_id,endpoint_id,model_profile_id,purpose,data_classes_json,
		artifact_ids_json,redactions_json,estimated_bytes,estimated_tokens,retention,policy_decision,decision_reason,
		manifest_sha256,created_at FROM provider_egress_manifests`
	args := []any{}
	if projectID != "" {
		query += ` WHERE project_id = ?`
		args = append(args, projectID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT ?`
	args = append(args, boundedLimit(limit, 100, 500))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []providers.EgressManifest{}
	for rows.Next() {
		item, err := scanEgressManifest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanEgressManifest(row scanner) (providers.EgressManifest, error) {
	var item providers.EgressManifest
	var classes, artifacts, redactions, created string
	err := row.Scan(&item.ID, &item.JobID, &item.ProjectID, &item.RouteID, &item.ProviderID, &item.EndpointID,
		&item.ModelProfileID, &item.Purpose, &classes, &artifacts, &redactions, &item.EstimatedBytes,
		&item.EstimatedTokens, &item.Retention, &item.PolicyDecision, &item.DecisionReason, &item.ManifestSHA256, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return providers.EgressManifest{}, storage.ErrNotFound
	}
	if err != nil {
		return providers.EgressManifest{}, err
	}
	if err := json.Unmarshal([]byte(classes), &item.DataClasses); err != nil {
		return providers.EgressManifest{}, err
	}
	if err := json.Unmarshal([]byte(artifacts), &item.ArtifactIDs); err != nil {
		return providers.EgressManifest{}, err
	}
	if err := json.Unmarshal([]byte(redactions), &item.Redactions); err != nil {
		return providers.EgressManifest{}, err
	}
	var errParse error
	if item.CreatedAt, errParse = time.Parse(timestampFormat, created); errParse != nil {
		return providers.EgressManifest{}, errParse
	}
	return item, nil
}

func (s *Store) RecordCapabilityProbe(ctx context.Context, probe providers.CapabilityProbe) (providers.CapabilityProbe, error) {
	if probe.ID == "" {
		id, err := NewID("probe")
		if err != nil {
			return providers.CapabilityProbe{}, err
		}
		probe.ID = id
	}
	if probe.CreatedAt.IsZero() {
		probe.CreatedAt = s.now()
	}
	if err := probe.Validate(); err != nil {
		return providers.CapabilityProbe{}, storage.ErrInvalid
	}
	capabilities, _ := json.Marshal(probe.Capabilities)
	errorsJSON, _ := json.Marshal(probe.Errors)
	_, err := s.db.ExecContext(ctx, `INSERT INTO provider_capability_probes(
		id,provider_id,endpoint_id,model_profile_id,interface_family,status,observed_model_id,native_api_shape,
		capabilities_json,request_schema_sha256,response_schema_sha256,latency_millis,errors_json,actor_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		probe.ID, probe.ProviderID, probe.EndpointID, probe.ModelProfileID, probe.InterfaceFamily, probe.Status,
		probe.ObservedModelID, probe.NativeAPIShape, string(capabilities), probe.RequestSchemaSHA256,
		probe.ResponseSchemaSHA256, probe.LatencyMillis, string(errorsJSON), probe.ActorID, probe.CreatedAt.Format(timestampFormat))
	if err != nil {
		return providers.CapabilityProbe{}, err
	}
	return probe, nil
}

func (s *Store) ListCapabilityProbes(ctx context.Context, modelProfileID string, limit int) ([]providers.CapabilityProbe, error) {
	query := `SELECT id,provider_id,endpoint_id,model_profile_id,interface_family,status,observed_model_id,native_api_shape,
		capabilities_json,request_schema_sha256,response_schema_sha256,latency_millis,errors_json,actor_id,created_at
		FROM provider_capability_probes`
	args := []any{}
	if strings.TrimSpace(modelProfileID) != "" {
		query += ` WHERE model_profile_id = ?`
		args = append(args, modelProfileID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT ?`
	args = append(args, boundedLimit(limit, 100, 500))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []providers.CapabilityProbe{}
	for rows.Next() {
		item, err := scanCapabilityProbe(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanCapabilityProbe(row scanner) (providers.CapabilityProbe, error) {
	var item providers.CapabilityProbe
	var capabilities, errorsJSON, created string
	err := row.Scan(&item.ID, &item.ProviderID, &item.EndpointID, &item.ModelProfileID, &item.InterfaceFamily,
		&item.Status, &item.ObservedModelID, &item.NativeAPIShape, &capabilities, &item.RequestSchemaSHA256,
		&item.ResponseSchemaSHA256, &item.LatencyMillis, &errorsJSON, &item.ActorID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return providers.CapabilityProbe{}, storage.ErrNotFound
	}
	if err != nil {
		return providers.CapabilityProbe{}, err
	}
	if err := json.Unmarshal([]byte(capabilities), &item.Capabilities); err != nil {
		return providers.CapabilityProbe{}, err
	}
	if err := json.Unmarshal([]byte(errorsJSON), &item.Errors); err != nil {
		return providers.CapabilityProbe{}, err
	}
	var parseErr error
	if item.CreatedAt, parseErr = time.Parse(timestampFormat, created); parseErr != nil {
		return providers.CapabilityProbe{}, parseErr
	}
	return item, nil
}
