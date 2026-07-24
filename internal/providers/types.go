package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = 1

const (
	FamilyLocalLlama             = "local_llamacpp"
	FamilyOpenAIResponses        = "openai_responses"
	FamilyOpenAIChat             = "openai_chat_completions"
	FamilyOpenAICompatible       = "openai_compatible"
	FamilyAzureOpenAI            = "azure_openai"
	FamilyAnthropicMessages      = "anthropic_messages"
	FamilyGemini                 = "gemini_generate_content"
	FamilyVertexGemini           = "vertex_gemini"
	FamilyBedrockConverse        = "bedrock_converse"
	FamilyBedrockResponsesCompat = "bedrock_responses_compatible"
)

const (
	TrustLocal      = "local"
	TrustPrivate    = "approved_private"
	TrustEnterprise = "approved_enterprise"
	TrustPublic     = "public_remote"
)

const (
	NetworkLocal  = "local"
	NetworkLAN    = "lan_private"
	NetworkVPC    = "enterprise_private"
	NetworkPublic = "public_internet"
)

const (
	DecisionAllowed = "allowed"
	DecisionDenied  = "denied"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type CapabilitySet struct {
	ResponsesAPI      bool `json:"responses_api"`
	ChatCompletions   bool `json:"chat_completions"`
	Streaming         bool `json:"streaming"`
	Cancellation      bool `json:"cancellation"`
	StructuredOutputs bool `json:"structured_outputs"`
	ToolCalls         bool `json:"tool_calls"`
	ParallelToolCalls bool `json:"parallel_tool_calls"`
	StableToolCallIDs bool `json:"stable_tool_call_ids"`
	SystemMessages    bool `json:"system_messages"`
	DeveloperMessages bool `json:"developer_messages"`
	UsageAccounting   bool `json:"usage_accounting"`
	ReasoningControls bool `json:"reasoning_controls"`
	PromptCaching     bool `json:"prompt_caching"`
	Batch             bool `json:"batch"`
	Asynchronous      bool `json:"asynchronous"`
	ModelListing      bool `json:"model_listing"`
	ImmutableModelIDs bool `json:"immutable_model_ids"`
}

type ProviderProfile struct {
	ID                   string    `json:"id"`
	SchemaVersion        int       `json:"schema_version"`
	InterfaceFamily      string    `json:"interface_family"`
	DisplayName          string    `json:"display_name"`
	TrustTier            string    `json:"trust_tier"`
	Remote               bool      `json:"remote"`
	Enabled              bool      `json:"enabled"`
	ApprovedDataClasses  []string  `json:"approved_data_classes"`
	CredentialRef        string    `json:"credential_ref,omitempty"`
	CredentialConfigured bool      `json:"credential_configured"`
	OperatorAssertions   []string  `json:"operator_assertions"`
	Version              int64     `json:"version"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type EndpointProfile struct {
	ID                  string    `json:"id"`
	ProviderID          string    `json:"provider_id"`
	BaseURL             string    `json:"base_url"`
	Region              string    `json:"region,omitempty"`
	NetworkZone         string    `json:"network_zone"`
	AllowPrivateAddress bool      `json:"allow_private_address"`
	TLSMode             string    `json:"tls_mode"`
	RedirectPolicy      string    `json:"redirect_policy"`
	DNSPolicy           string    `json:"dns_policy"`
	TimeoutMillis       int       `json:"timeout_millis"`
	HealthCheckPath     string    `json:"health_check_path"`
	Version             int64     `json:"version"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type ModelProfile struct {
	ID                 string        `json:"id"`
	ProviderID         string        `json:"provider_id"`
	EndpointID         string        `json:"endpoint_id"`
	ModelID            string        `json:"model_id"`
	DisplayName        string        `json:"display_name"`
	RoleEligibility    []string      `json:"role_eligibility"`
	Capabilities       CapabilitySet `json:"capabilities"`
	ContextLimit       int           `json:"context_limit"`
	OutputLimit        int           `json:"output_limit"`
	InputPricePerMTok  float64       `json:"input_price_per_mtok"`
	OutputPricePerMTok float64       `json:"output_price_per_mtok"`
	QualityStatus      string        `json:"quality_status"`
	CapabilityOverride bool          `json:"capability_override"`
	OverrideReason     string        `json:"override_reason,omitempty"`
	OverrideExpiresAt  time.Time     `json:"override_expires_at,omitempty"`
	Version            int64         `json:"version"`
	CreatedAt          time.Time     `json:"created_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
}

type RouteProfile struct {
	ID                  string    `json:"id"`
	Role                string    `json:"role"`
	Preference          string    `json:"preference"`
	OrderedModelIDs     []string  `json:"ordered_model_ids"`
	AllowedDataClasses  []string  `json:"allowed_data_classes"`
	MaxTokensPerRequest int       `json:"max_tokens_per_request"`
	MaxCostUSD          float64   `json:"max_cost_usd"`
	RetryBudget         int       `json:"retry_budget"`
	FallbackPolicy      string    `json:"fallback_policy"`
	BatchPolicy         string    `json:"batch_policy"`
	Enabled             bool      `json:"enabled"`
	Version             int64     `json:"version"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type EgressManifest struct {
	ID              string    `json:"id"`
	JobID           string    `json:"job_id,omitempty"`
	ProjectID       string    `json:"project_id"`
	RouteID         string    `json:"route_id"`
	ProviderID      string    `json:"provider_id"`
	EndpointID      string    `json:"endpoint_id"`
	ModelProfileID  string    `json:"model_profile_id"`
	Purpose         string    `json:"purpose"`
	DataClasses     []string  `json:"data_classes"`
	ArtifactIDs     []string  `json:"artifact_ids"`
	Redactions      []string  `json:"redactions"`
	EstimatedBytes  int64     `json:"estimated_bytes"`
	EstimatedTokens int       `json:"estimated_tokens"`
	Retention       string    `json:"retention"`
	PolicyDecision  string    `json:"policy_decision"`
	DecisionReason  string    `json:"decision_reason"`
	ManifestSHA256  string    `json:"manifest_sha256"`
	CreatedAt       time.Time `json:"created_at"`
}

type RouteRequest struct {
	JobID                    string   `json:"job_id,omitempty"`
	ProjectID                string   `json:"project_id"`
	Role                     string   `json:"role"`
	Purpose                  string   `json:"purpose"`
	DataClasses              []string `json:"data_classes"`
	ArtifactIDs              []string `json:"artifact_ids,omitempty"`
	EstimatedBytes           int64    `json:"estimated_bytes"`
	EstimatedTokens          int      `json:"estimated_tokens"`
	RequiresStructuredOutput bool     `json:"requires_structured_output"`
}

type RouteDecision struct {
	Status         string          `json:"status"`
	Reason         string          `json:"reason"`
	Route          RouteProfile    `json:"route,omitempty"`
	Provider       ProviderProfile `json:"provider,omitempty"`
	Endpoint       EndpointProfile `json:"endpoint,omitempty"`
	Model          ModelProfile    `json:"model,omitempty"`
	EgressManifest EgressManifest  `json:"egress_manifest"`
}

type Status struct {
	Families               []string          `json:"families"`
	Providers              []ProviderProfile `json:"providers"`
	Endpoints              []EndpointProfile `json:"endpoints"`
	Models                 []ModelProfile    `json:"models"`
	Routes                 []RouteProfile    `json:"routes"`
	RecentManifests        []EgressManifest  `json:"recent_egress_manifests"`
	RemoteEnabledByDefault bool              `json:"remote_enabled_by_default"`
}

type Store interface {
	ListProviderProfiles(context.Context, int) ([]ProviderProfile, error)
	UpsertProviderProfile(context.Context, ProviderProfile, string) (ProviderProfile, error)
	ListEndpointProfiles(context.Context, int) ([]EndpointProfile, error)
	UpsertEndpointProfile(context.Context, EndpointProfile, string) (EndpointProfile, error)
	ListModelProfiles(context.Context, int) ([]ModelProfile, error)
	UpsertModelProfile(context.Context, ModelProfile, string) (ModelProfile, error)
	ListRouteProfiles(context.Context, int) ([]RouteProfile, error)
	UpsertRouteProfile(context.Context, RouteProfile, string) (RouteProfile, error)
	RecordEgressManifest(context.Context, EgressManifest) (EgressManifest, error)
	ListEgressManifests(context.Context, string, int) ([]EgressManifest, error)
}

func RequiredFamilies() []string {
	return []string{
		FamilyLocalLlama, FamilyOpenAIResponses, FamilyOpenAIChat, FamilyOpenAICompatible,
		FamilyAzureOpenAI, FamilyAnthropicMessages, FamilyGemini, FamilyVertexGemini,
		FamilyBedrockConverse, FamilyBedrockResponsesCompat,
	}
}

func DefaultProfiles(now time.Time) ([]ProviderProfile, []EndpointProfile, []ModelProfile, []RouteProfile) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	providers := []ProviderProfile{
		{ID: "local-llamacpp", SchemaVersion: SchemaVersion, InterfaceFamily: FamilyLocalLlama, DisplayName: "Local llama.cpp supervisor", TrustTier: TrustLocal, Remote: false, Enabled: true, ApprovedDataClasses: allDataClasses(), OperatorAssertions: []string{"local-only supervisor profile"}, Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "fake-openai-responses", SchemaVersion: SchemaVersion, InterfaceFamily: FamilyOpenAIResponses, DisplayName: "CI fake OpenAI Responses", TrustTier: TrustPrivate, Remote: true, Enabled: false, ApprovedDataClasses: []string{"task_metadata", "documentation_public_source"}, OperatorAssertions: []string{"protocol fake for CI conformance only"}, Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "fake-anthropic-messages", SchemaVersion: SchemaVersion, InterfaceFamily: FamilyAnthropicMessages, DisplayName: "CI fake Anthropic Messages", TrustTier: TrustPrivate, Remote: true, Enabled: false, ApprovedDataClasses: []string{"task_metadata"}, OperatorAssertions: []string{"protocol fake for CI conformance only"}, Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "fake-gemini", SchemaVersion: SchemaVersion, InterfaceFamily: FamilyGemini, DisplayName: "CI fake Gemini generateContent", TrustTier: TrustPrivate, Remote: true, Enabled: false, ApprovedDataClasses: []string{"task_metadata"}, OperatorAssertions: []string{"protocol fake for CI conformance only"}, Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "fake-bedrock", SchemaVersion: SchemaVersion, InterfaceFamily: FamilyBedrockConverse, DisplayName: "CI fake Bedrock Converse", TrustTier: TrustPrivate, Remote: true, Enabled: false, ApprovedDataClasses: []string{"task_metadata"}, OperatorAssertions: []string{"protocol fake for CI conformance only"}, Version: 1, CreatedAt: now, UpdatedAt: now},
	}
	endpoints := []EndpointProfile{
		{ID: "local-llamacpp-endpoint", ProviderID: "local-llamacpp", BaseURL: "http://127.0.0.1:11438/v1", NetworkZone: NetworkLocal, AllowPrivateAddress: true, TLSMode: "local_http", RedirectPolicy: "reject", DNSPolicy: "loopback_only", TimeoutMillis: 1800000, HealthCheckPath: "/healthz", Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "fake-openai-responses-endpoint", ProviderID: "fake-openai-responses", BaseURL: "https://providers.invalid/openai-responses", NetworkZone: NetworkPublic, TLSMode: "verify", RedirectPolicy: "reject", DNSPolicy: "public_only", TimeoutMillis: 30000, HealthCheckPath: "/healthz", Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "fake-anthropic-endpoint", ProviderID: "fake-anthropic-messages", BaseURL: "https://providers.invalid/anthropic", NetworkZone: NetworkPublic, TLSMode: "verify", RedirectPolicy: "reject", DNSPolicy: "public_only", TimeoutMillis: 30000, HealthCheckPath: "/healthz", Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "fake-gemini-endpoint", ProviderID: "fake-gemini", BaseURL: "https://providers.invalid/gemini", NetworkZone: NetworkPublic, TLSMode: "verify", RedirectPolicy: "reject", DNSPolicy: "public_only", TimeoutMillis: 30000, HealthCheckPath: "/healthz", Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "fake-bedrock-endpoint", ProviderID: "fake-bedrock", BaseURL: "https://providers.invalid/bedrock", NetworkZone: NetworkPublic, TLSMode: "verify", RedirectPolicy: "reject", DNSPolicy: "public_only", TimeoutMillis: 30000, HealthCheckPath: "/healthz", Version: 1, CreatedAt: now, UpdatedAt: now},
	}
	models := []ModelProfile{
		{ID: "local-implementation", ProviderID: "local-llamacpp", EndpointID: "local-llamacpp-endpoint", ModelID: "active", DisplayName: "Local active implementation model", RoleEligibility: []string{"implementation", "repair", "qc", "test_designer", "documentation"}, Capabilities: CapabilitySet{ChatCompletions: true, Cancellation: true, StructuredOutputs: true, SystemMessages: true, UsageAccounting: true, ImmutableModelIDs: true}, ContextLimit: 32768, OutputLimit: 16384, QualityStatus: "accepted_local_default", Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "fake-remote-json", ProviderID: "fake-openai-responses", EndpointID: "fake-openai-responses-endpoint", ModelID: "fake-openai-responses-json-2026-07", DisplayName: "Fake remote structured JSON model", RoleEligibility: []string{"clarifier", "test_designer", "documentation", "qc", "summarization", "evaluation"}, Capabilities: CapabilitySet{ResponsesAPI: true, Streaming: true, Cancellation: true, StructuredOutputs: true, ToolCalls: true, StableToolCallIDs: true, SystemMessages: true, DeveloperMessages: true, UsageAccounting: true, PromptCaching: true, Batch: true, Asynchronous: true, ModelListing: true, ImmutableModelIDs: true}, ContextLimit: 128000, OutputLimit: 16384, InputPricePerMTok: 2.0, OutputPricePerMTok: 8.0, QualityStatus: "ci_fake_only", Version: 1, CreatedAt: now, UpdatedAt: now},
	}
	routes := []RouteProfile{
		{ID: "local-quality-default", Role: "implementation", Preference: "local_first", OrderedModelIDs: []string{"local-implementation"}, AllowedDataClasses: allDataClasses(), MaxTokensPerRequest: 32768, MaxCostUSD: 0, RetryBudget: 2, FallbackPolicy: "same_trust_or_stricter", BatchPolicy: "disabled", Enabled: true, Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "remote-documentation-ci-preview", Role: "documentation", Preference: "remote_when_policy_allows", OrderedModelIDs: []string{"fake-remote-json"}, AllowedDataClasses: []string{"task_metadata", "documentation_public_source"}, MaxTokensPerRequest: 16000, MaxCostUSD: 0.10, RetryBudget: 1, FallbackPolicy: "explicit_same_or_higher_trust_only", BatchPolicy: "project_isolated", Enabled: false, Version: 1, CreatedAt: now, UpdatedAt: now},
	}
	return providers, endpoints, models, routes
}

func allDataClasses() []string {
	return []string{"task_metadata", "documentation_public_source", "selected_symbols_tests", "candidate_diff", "context_packet", "memory_excerpt", "security_findings", "sbom_data", "logs_artifacts"}
}

func (p ProviderProfile) Validate() error {
	if p.SchemaVersion != SchemaVersion || !safeID.MatchString(p.ID) || !validFamily(p.InterfaceFamily) ||
		strings.TrimSpace(p.DisplayName) == "" || len(p.DisplayName) > 160 || !validTrustTier(p.TrustTier) ||
		len(p.ApprovedDataClasses) == 0 || len(p.ApprovedDataClasses) > 16 || p.Version < 0 {
		return errors.New("provider profile is invalid")
	}
	for _, dataClass := range p.ApprovedDataClasses {
		if !validDataClass(dataClass) {
			return errors.New("provider profile data class is invalid")
		}
	}
	if p.CredentialRef != "" && !safeID.MatchString(p.CredentialRef) {
		return errors.New("provider credential reference is invalid")
	}
	for _, assertion := range p.OperatorAssertions {
		if strings.TrimSpace(assertion) == "" || len(assertion) > 500 {
			return errors.New("provider operator assertion is invalid")
		}
	}
	return nil
}

func (e EndpointProfile) Validate(provider ProviderProfile) error {
	if !safeID.MatchString(e.ID) || e.ProviderID != provider.ID || !validNetworkZone(e.NetworkZone) ||
		(e.TLSMode != "verify" && e.TLSMode != "custom_ca" && e.TLSMode != "mtls" && e.TLSMode != "local_http") ||
		e.RedirectPolicy != "reject" || (e.DNSPolicy != "public_only" && e.DNSPolicy != "private_allowed" && e.DNSPolicy != "loopback_only") ||
		e.TimeoutMillis < 1000 || e.TimeoutMillis > 1800000 || e.Version < 0 {
		return errors.New("endpoint profile is invalid")
	}
	parsed, err := url.Parse(e.BaseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("endpoint base URL is invalid")
	}
	if provider.Remote {
		if parsed.Scheme != "https" || e.TLSMode == "local_http" {
			return errors.New("remote endpoints require HTTPS with certificate verification")
		}
	} else if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("local endpoint URL scheme is invalid")
	}
	if !e.AllowPrivateAddress && endpointHostIsPrivate(parsed.Hostname()) {
		return errors.New("endpoint profile rejects private, loopback, link-local, and metadata addresses")
	}
	if e.NetworkZone == NetworkPublic && endpointHostIsPrivate(parsed.Hostname()) {
		return errors.New("public endpoint profile cannot target private addresses")
	}
	return nil
}

func (m ModelProfile) Validate(provider ProviderProfile, endpoint EndpointProfile) error {
	if !safeID.MatchString(m.ID) || m.ProviderID != provider.ID || m.EndpointID != endpoint.ID ||
		strings.TrimSpace(m.ModelID) == "" || len(m.ModelID) > 200 || strings.TrimSpace(m.DisplayName) == "" ||
		len(m.RoleEligibility) == 0 || len(m.RoleEligibility) > 16 || m.ContextLimit < 1024 || m.ContextLimit > 4_000_000 ||
		m.OutputLimit < 1 || m.OutputLimit > m.ContextLimit || m.InputPricePerMTok < 0 || m.OutputPricePerMTok < 0 ||
		(m.QualityStatus != "accepted_local_default" && m.QualityStatus != "ci_fake_only" && m.QualityStatus != "experimental" && m.QualityStatus != "accepted") ||
		m.Version < 0 {
		return errors.New("model profile is invalid")
	}
	if m.CapabilityOverride && (strings.TrimSpace(m.OverrideReason) == "" || m.OverrideExpiresAt.IsZero()) {
		return errors.New("model capability override requires reason and expiry")
	}
	for _, role := range m.RoleEligibility {
		if !safeID.MatchString(role) {
			return errors.New("model role eligibility is invalid")
		}
	}
	return nil
}

func (r RouteProfile) Validate() error {
	if !safeID.MatchString(r.ID) || !safeID.MatchString(r.Role) ||
		(r.Preference != "local_first" && r.Preference != "remote_when_policy_allows" && r.Preference != "measured_hybrid") ||
		len(r.OrderedModelIDs) == 0 || len(r.OrderedModelIDs) > 32 || len(r.AllowedDataClasses) == 0 ||
		r.MaxTokensPerRequest < 1 || r.MaxTokensPerRequest > 4_000_000 || r.MaxCostUSD < 0 ||
		r.RetryBudget < 0 || r.RetryBudget > 10 ||
		(r.FallbackPolicy != "none" && r.FallbackPolicy != "same_trust_or_stricter" && r.FallbackPolicy != "explicit_same_or_higher_trust_only") ||
		(r.BatchPolicy != "disabled" && r.BatchPolicy != "project_isolated") || r.Version < 0 {
		return errors.New("route profile is invalid")
	}
	for _, id := range r.OrderedModelIDs {
		if !safeID.MatchString(id) {
			return errors.New("route references invalid model ID")
		}
	}
	for _, dataClass := range r.AllowedDataClasses {
		if !validDataClass(dataClass) {
			return errors.New("route data class is invalid")
		}
	}
	return nil
}

func (m EgressManifest) Validate() error {
	if (m.JobID != "" && !safeID.MatchString(m.JobID)) || !safeID.MatchString(m.ProjectID) ||
		!safeID.MatchString(m.RouteID) || !safeID.MatchString(m.ProviderID) || !safeID.MatchString(m.EndpointID) ||
		!safeID.MatchString(m.ModelProfileID) || strings.TrimSpace(m.Purpose) == "" || len(m.Purpose) > 500 ||
		len(m.DataClasses) == 0 || len(m.DataClasses) > 16 || m.EstimatedBytes < 0 || m.EstimatedBytes > 128<<20 ||
		m.EstimatedTokens < 0 || m.EstimatedTokens > 4_000_000 || strings.TrimSpace(m.Retention) == "" ||
		(m.PolicyDecision != DecisionAllowed && m.PolicyDecision != DecisionDenied) ||
		strings.TrimSpace(m.DecisionReason) == "" || len(m.DecisionReason) > 1000 ||
		!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(m.ManifestSHA256) {
		return errors.New("egress manifest is invalid")
	}
	for _, dataClass := range m.DataClasses {
		if !validDataClass(dataClass) {
			return errors.New("egress manifest data class is invalid")
		}
	}
	for _, artifactID := range m.ArtifactIDs {
		if !safeID.MatchString(artifactID) {
			return errors.New("egress manifest artifact reference is invalid")
		}
	}
	for _, redaction := range m.Redactions {
		if strings.TrimSpace(redaction) == "" || len(redaction) > 200 {
			return errors.New("egress manifest redaction is invalid")
		}
	}
	return nil
}

func (r RouteRequest) Validate() error {
	if (r.JobID != "" && !safeID.MatchString(r.JobID)) || !safeID.MatchString(r.ProjectID) || !safeID.MatchString(r.Role) ||
		strings.TrimSpace(r.Purpose) == "" || len(r.Purpose) > 500 || len(r.DataClasses) == 0 ||
		r.EstimatedBytes < 0 || r.EstimatedBytes > 128<<20 || r.EstimatedTokens < 0 || r.EstimatedTokens > 4_000_000 {
		return errors.New("provider route request is invalid")
	}
	for _, dataClass := range r.DataClasses {
		if !validDataClass(dataClass) {
			return errors.New("provider route request data class is invalid")
		}
	}
	for _, artifactID := range r.ArtifactIDs {
		if !safeID.MatchString(artifactID) {
			return errors.New("provider route request artifact reference is invalid")
		}
	}
	return nil
}

func HashManifest(manifest EgressManifest) (string, error) {
	copy := manifest
	copy.ID, copy.ManifestSHA256, copy.CreatedAt = "", "", time.Time{}
	payload, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func validFamily(value string) bool {
	for _, family := range RequiredFamilies() {
		if value == family {
			return true
		}
	}
	return false
}

func validTrustTier(value string) bool {
	switch value {
	case TrustLocal, TrustPrivate, TrustEnterprise, TrustPublic:
		return true
	default:
		return false
	}
}

func validNetworkZone(value string) bool {
	switch value {
	case NetworkLocal, NetworkLAN, NetworkVPC, NetworkPublic:
		return true
	default:
		return false
	}
}

func validDataClass(value string) bool {
	for _, candidate := range allDataClasses() {
		if value == candidate {
			return true
		}
	}
	return false
}

func endpointHostIsPrivate(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.EqualFold(host, "metadata.google.internal") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsPrivate() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 169 && ip4[1] == 254 && ip4[2] == 169 && ip4[3] == 254 {
		return true
	}
	return false
}

func DenyManifest(request RouteRequest, routeID, reason string) EgressManifest {
	return EgressManifest{
		JobID: request.JobID, ProjectID: request.ProjectID, RouteID: routeID, ProviderID: "unselected",
		EndpointID: "unselected", ModelProfileID: "unselected", Purpose: request.Purpose,
		DataClasses: sortedStrings(request.DataClasses), ArtifactIDs: sortedStrings(request.ArtifactIDs),
		Redactions:     []string{"secret_scan", "credential_patterns", "hidden_reasoning", "raw_environment"},
		EstimatedBytes: request.EstimatedBytes, EstimatedTokens: request.EstimatedTokens,
		Retention: "none", PolicyDecision: DecisionDenied, DecisionReason: reason,
	}
}

func CapabilitySummary(c CapabilitySet) string {
	items := []string{}
	if c.ResponsesAPI {
		items = append(items, "responses")
	}
	if c.ChatCompletions {
		items = append(items, "chat")
	}
	if c.StructuredOutputs {
		items = append(items, "structured-json")
	}
	if c.ToolCalls {
		items = append(items, "tools-proposed")
	}
	if c.Batch {
		items = append(items, "batch")
	}
	if len(items) == 0 {
		return "no probed capabilities"
	}
	return strings.Join(items, ", ")
}

func EnsureNoSecrets(dataClasses []string) error {
	for _, dataClass := range dataClasses {
		switch dataClass {
		case "secrets", "credentials", "signing_material", "hidden_reasoning", "raw_environment":
			return fmt.Errorf("forbidden remote data class %q", dataClass)
		}
	}
	return nil
}
