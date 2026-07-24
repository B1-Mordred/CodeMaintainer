package policy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	SchemaVersion = 1

	SourceStructuredTemplate = "structured_template"
	SourceAdvancedRego       = "advanced_rego"

	DecisionPointRiskRouting              = "risk_routing"
	DecisionPointRunnerProfile            = "runner_profile"
	DecisionPointNetworkEgress            = "network_egress"
	DecisionPointModelProvider            = "model_provider"
	DecisionPointBaselineRequirement      = "baseline_requirement"
	DecisionPointQCRequirement            = "qc_requirement"
	DecisionPointDocumentationRequirement = "documentation_requirement"
	DecisionPointMemoryPromotion          = "memory_promotion"
	DecisionPointForgePublication         = "forge_publication"
	DecisionPointWaiverApproval           = "waiver_approval"
	DecisionPointSigningInstaller         = "signing_installer"
	DecisionPointWindowsWorker            = "windows_worker"

	OutcomeAllow = "allow"
	OutcomeDeny  = "deny"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type StructuredRules struct {
	RequiredStages          map[string][]string `json:"required_stages"`
	ProtectedActions        []string            `json:"protected_actions"`
	AllowedRunnerProfiles   []string            `json:"allowed_runner_profiles"`
	AllowedNetworkProfiles  []string            `json:"allowed_network_profiles"`
	AllowedModelTrustTiers  []string            `json:"allowed_model_trust_tiers"`
	MemoryPromotionRequires []string            `json:"memory_promotion_requires"`
	PublicationRequires     []string            `json:"publication_requires"`
	WaiverRequires          []string            `json:"waiver_requires"`
}

type Bundle struct {
	ID                  string          `json:"id"`
	SchemaVersion       int             `json:"schema_version"`
	Version             string          `json:"version"`
	SourceKind          string          `json:"source_kind"`
	SourceSHA256        string          `json:"source_sha256"`
	CompiledSHA256      string          `json:"compiled_sha256"`
	StructuredRules     StructuredRules `json:"structured_rules"`
	StructuredRulesJSON json.RawMessage `json:"-"`
	RegoSource          string          `json:"rego_source,omitempty"`
	Status              string          `json:"status"`
	CreatedBy           string          `json:"created_by"`
	Reason              string          `json:"reason"`
	CreatedAt           time.Time       `json:"created_at"`
	ActivatedAt         *time.Time      `json:"activated_at,omitempty"`
}

type Activation struct {
	ID                   string    `json:"id"`
	BundleID             string    `json:"bundle_id"`
	BundleVersion        string    `json:"bundle_version"`
	Action               string    `json:"action"`
	PreviousBundleID     string    `json:"previous_bundle_id,omitempty"`
	ActorID              string    `json:"actor_id"`
	ActorRole            string    `json:"actor_role"`
	Reason               string    `json:"reason"`
	StagedRolloutPercent int       `json:"staged_rollout_percent"`
	Reauthenticated      bool      `json:"reauthenticated"`
	CreatedAt            time.Time `json:"created_at"`
}

type Decision struct {
	ID             string          `json:"id"`
	BundleID       string          `json:"bundle_id"`
	BundleVersion  string          `json:"bundle_version"`
	JobID          string          `json:"job_id,omitempty"`
	DecisionPoint  string          `json:"decision_point"`
	InputSHA256    string          `json:"input_sha256"`
	RedactedInput  json.RawMessage `json:"redacted_input"`
	Outcome        string          `json:"outcome"`
	Allowed        bool            `json:"allowed"`
	RequiredStages []string        `json:"required_stages"`
	Explanation    string          `json:"explanation"`
	FailClosed     bool            `json:"fail_closed"`
	CreatedAt      time.Time       `json:"created_at"`
}

type Simulation struct {
	ID            string          `json:"id"`
	BundleID      string          `json:"bundle_id"`
	BundleVersion string          `json:"bundle_version"`
	DecisionPoint string          `json:"decision_point"`
	InputSHA256   string          `json:"input_sha256"`
	RedactedInput json.RawMessage `json:"redacted_input"`
	Decision      Decision        `json:"decision"`
	Status        string          `json:"status"`
	Errors        []string        `json:"errors"`
	ActorID       string          `json:"actor_id"`
	CreatedAt     time.Time       `json:"created_at"`
}

type TestCase struct {
	ID              string          `json:"id"`
	DecisionPoint   string          `json:"decision_point"`
	Input           json.RawMessage `json:"input"`
	WantAllowed     bool            `json:"want_allowed"`
	WantStages      []string        `json:"want_required_stages,omitempty"`
	WantExplanation string          `json:"want_explanation_contains,omitempty"`
}

type TestResult struct {
	ID       string   `json:"id"`
	Passed   bool     `json:"passed"`
	Error    string   `json:"error,omitempty"`
	Decision Decision `json:"decision"`
}

type TestRun struct {
	ID                    string         `json:"id"`
	BundleID              string         `json:"bundle_id"`
	BundleVersion         string         `json:"bundle_version"`
	SourceSHA256          string         `json:"source_sha256"`
	FormattedSourceSHA256 string         `json:"formatted_source_sha256"`
	Status                string         `json:"status"`
	Tests                 []TestCase     `json:"tests"`
	Results               []TestResult   `json:"results"`
	Coverage              map[string]any `json:"coverage,omitempty"`
	Errors                []string       `json:"errors"`
	ActorID               string         `json:"actor_id"`
	CreatedAt             time.Time      `json:"created_at"`
}

type ActivationRequest struct {
	BundleID             string
	Action               string
	ActorID              string
	ActorRole            string
	Reason               string
	StagedRolloutPercent int
	Reauthenticated      bool
}

type CreateBundleRequest struct {
	Version    string
	SourceKind string
	RegoSource string
	CreatedBy  string
	Reason     string
	Tests      []TestCase
}

type SimulationRequest struct {
	BundleID      string          `json:"bundle_id,omitempty"`
	DecisionPoint string          `json:"decision_point"`
	Input         json.RawMessage `json:"input"`
	ActorID       string          `json:"-"`
}

type EvaluationRequest struct {
	Bundle        *Bundle
	JobID         string
	DecisionPoint string
	Input         any
}

type Store interface {
	CreatePolicyBundle(context.Context, Bundle) (Bundle, error)
	ListPolicyBundles(context.Context, int) ([]Bundle, error)
	GetPolicyBundle(context.Context, string) (Bundle, error)
	GetActivePolicyBundle(context.Context) (Bundle, error)
	ActivatePolicyBundle(context.Context, ActivationRequest) (Activation, error)
	ListPolicyActivations(context.Context, int) ([]Activation, error)
	SavePolicySimulation(context.Context, Simulation) (Simulation, error)
	ListPolicySimulations(context.Context, int) ([]Simulation, error)
	SavePolicyTestRun(context.Context, TestRun) (TestRun, error)
	ListPolicyTestRuns(context.Context, string, int) ([]TestRun, error)
	RecordPolicyDecision(context.Context, Decision) (Decision, error)
	ListPolicyDecisions(context.Context, string, int) ([]Decision, error)
}

func BuiltinBundle() Bundle {
	rules := StructuredRules{
		RequiredStages: map[string][]string{
			"low":    {"baseline", "targeted_tests", "full_tests", "documentation", "qc"},
			"medium": {"baseline", "targeted_tests", "full_tests", "test_designer", "golden_rehearsal", "documentation", "qc"},
			"high":   {"baseline", "targeted_tests", "full_tests", "test_designer", "golden_rehearsal", "documentation", "qc", "human_review"},
		},
		ProtectedActions:        []string{DecisionPointNetworkEgress, DecisionPointModelProvider, DecisionPointMemoryPromotion, DecisionPointForgePublication, DecisionPointWaiverApproval, DecisionPointSigningInstaller, DecisionPointWindowsWorker},
		AllowedRunnerProfiles:   []string{"verification", "implementation", "test_designer", "documentation", "qc"},
		AllowedNetworkProfiles:  []string{"none", "inference-only", "dependency-egress"},
		AllowedModelTrustTiers:  []string{"local", "approved-private"},
		MemoryPromotionRequires: []string{"project_namespace", "secret_scan_pass", "human_basis"},
		PublicationRequires:     []string{"full_verification_passed", "qc_passed", "approval_bound_to_result_sha"},
		WaiverRequires:          []string{"recent_reauthentication", "reason", "expires_at"},
	}
	raw, _ := json.Marshal(rules)
	sha := sha256.Sum256(raw)
	source := "builtin structured policy template for Increment 2 quality gates"
	sourceSHA := sha256.Sum256([]byte(source))
	return Bundle{
		ID: "builtin-increment2-policy-v1", SchemaVersion: SchemaVersion, Version: "2026.07.23.1",
		SourceKind: SourceStructuredTemplate, SourceSHA256: hex.EncodeToString(sourceSHA[:]),
		CompiledSHA256: hex.EncodeToString(sha[:]), StructuredRules: rules, StructuredRulesJSON: raw,
		Status: "active", CreatedBy: "system", Reason: "safe default policy bundle for Increment 2 deterministic gates",
	}
}

func (b Bundle) Validate() error {
	if (b.ID != "" && !safeID.MatchString(b.ID)) || b.SchemaVersion != SchemaVersion || strings.TrimSpace(b.Version) == "" ||
		(b.SourceKind != SourceStructuredTemplate && b.SourceKind != SourceAdvancedRego) ||
		!hex64(b.SourceSHA256) || !hex64(b.CompiledSHA256) || strings.TrimSpace(b.CreatedBy) == "" ||
		strings.TrimSpace(b.Reason) == "" {
		return errors.New("policy bundle violates identity or provenance bounds")
	}
	if b.SourceKind == SourceAdvancedRego && strings.TrimSpace(b.RegoSource) == "" {
		return errors.New("advanced policy bundles require source")
	}
	if len(b.RegoSource) > 256*1024 || len(b.StructuredRules.ProtectedActions) > 64 ||
		len(b.StructuredRules.AllowedRunnerProfiles) > 64 || len(b.StructuredRules.AllowedNetworkProfiles) > 64 ||
		len(b.StructuredRules.AllowedModelTrustTiers) > 64 {
		return errors.New("policy bundle exceeds structured bounds")
	}
	if len(b.StructuredRules.RequiredStages) == 0 {
		return errors.New("policy bundle must declare required stages")
	}
	for level, stages := range b.StructuredRules.RequiredStages {
		if level != "low" && level != "medium" && level != "high" {
			return errors.New("policy bundle has unknown risk level")
		}
		if len(stages) == 0 || len(stages) > 32 {
			return errors.New("policy bundle has invalid required-stage list")
		}
		for _, stage := range stages {
			if !safeID.MatchString(stage) {
				return errors.New("policy bundle has invalid required stage")
			}
		}
	}
	for _, point := range b.StructuredRules.ProtectedActions {
		if !KnownDecisionPoint(point) {
			return errors.New("policy bundle protects an unknown decision point")
		}
	}
	return nil
}

func (a Activation) Validate() error {
	if !safeID.MatchString(a.ID) || !safeID.MatchString(a.BundleID) || strings.TrimSpace(a.BundleVersion) == "" ||
		(a.Action != "activate" && a.Action != "rollback") || strings.TrimSpace(a.ActorID) == "" ||
		strings.TrimSpace(a.ActorRole) == "" || strings.TrimSpace(a.Reason) == "" ||
		a.StagedRolloutPercent < 0 || a.StagedRolloutPercent > 100 || !a.Reauthenticated {
		return errors.New("policy activation violates authorization or provenance bounds")
	}
	return nil
}

func (d Decision) Validate() error {
	if d.ID != "" && !safeID.MatchString(d.ID) {
		return errors.New("policy decision has invalid id")
	}
	if !safeID.MatchString(d.BundleID) || strings.TrimSpace(d.BundleVersion) == "" || !KnownDecisionPoint(d.DecisionPoint) ||
		!hex64(d.InputSHA256) || !json.Valid(d.RedactedInput) || (d.Outcome != OutcomeAllow && d.Outcome != OutcomeDeny) ||
		d.Allowed != (d.Outcome == OutcomeAllow) || strings.TrimSpace(d.Explanation) == "" || len(d.RequiredStages) > 64 {
		return errors.New("policy decision violates version-one bounds")
	}
	if d.JobID != "" && !safeID.MatchString(d.JobID) {
		return errors.New("policy decision has invalid job id")
	}
	for _, stage := range d.RequiredStages {
		if !safeID.MatchString(stage) {
			return errors.New("policy decision has invalid stage")
		}
	}
	return nil
}

func (s Simulation) Validate() error {
	if s.ID != "" && !safeID.MatchString(s.ID) {
		return errors.New("policy simulation has invalid id")
	}
	if !safeID.MatchString(s.BundleID) || strings.TrimSpace(s.BundleVersion) == "" || !KnownDecisionPoint(s.DecisionPoint) ||
		!hex64(s.InputSHA256) || !json.Valid(s.RedactedInput) || (s.Status != "passed" && s.Status != "failed") ||
		len(s.Errors) > 32 || strings.TrimSpace(s.ActorID) == "" {
		return errors.New("policy simulation violates version-one bounds")
	}
	if err := s.Decision.Validate(); err != nil {
		return err
	}
	return nil
}

func (t TestCase) Validate() error {
	if !safeID.MatchString(t.ID) || !KnownDecisionPoint(t.DecisionPoint) || !json.Valid(t.Input) ||
		len(t.Input) == 0 || len(t.Input) > 256*1024 || len(t.WantStages) > 64 ||
		len(t.WantExplanation) > 512 {
		return errors.New("policy test case violates version-one bounds")
	}
	for _, stage := range t.WantStages {
		if !safeID.MatchString(stage) {
			return errors.New("policy test case has invalid required stage")
		}
	}
	return nil
}

func (r TestResult) Validate() error {
	if !safeID.MatchString(r.ID) || len(r.Error) > 4000 {
		return errors.New("policy test result violates version-one bounds")
	}
	if r.Decision.BundleID == "" {
		if r.Passed {
			return errors.New("passing policy test result requires a decision")
		}
		return nil
	}
	copy := r.Decision
	copy.ID = ""
	copy.CreatedAt = time.Time{}
	if err := copy.Validate(); err != nil {
		return err
	}
	return nil
}

func (r TestRun) Validate() error {
	if r.ID != "" && !safeID.MatchString(r.ID) {
		return errors.New("policy test run has invalid id")
	}
	if !safeID.MatchString(r.BundleID) || strings.TrimSpace(r.BundleVersion) == "" ||
		!hex64(r.SourceSHA256) || !hex64(r.FormattedSourceSHA256) ||
		(r.Status != "passed" && r.Status != "failed") || len(r.Tests) == 0 ||
		len(r.Tests) > 64 || len(r.Results) != len(r.Tests) || len(r.Errors) > 64 ||
		strings.TrimSpace(r.ActorID) == "" {
		return errors.New("policy test run violates version-one bounds")
	}
	for _, test := range r.Tests {
		if err := test.Validate(); err != nil {
			return err
		}
	}
	for _, result := range r.Results {
		if err := result.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func KnownDecisionPoint(point string) bool {
	switch point {
	case DecisionPointRiskRouting, DecisionPointRunnerProfile, DecisionPointNetworkEgress, DecisionPointModelProvider,
		DecisionPointBaselineRequirement, DecisionPointQCRequirement, DecisionPointDocumentationRequirement,
		DecisionPointMemoryPromotion, DecisionPointForgePublication, DecisionPointWaiverApproval,
		DecisionPointSigningInstaller, DecisionPointWindowsWorker:
		return true
	default:
		return false
	}
}

func RedactedInputAndHash(input any) (json.RawMessage, string, error) {
	redacted := redact(input)
	raw, err := canonicalJSON(redacted)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

func DecodeSimulationInput(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > 256*1024 {
		return nil, errors.New("policy simulation input is empty or oversized")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("policy simulation input contains trailing data")
	}
	return value, nil
}

func hex64(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func redact(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if secretKey(key) {
				out[key] = "***REDACTED***"
				continue
			}
			out[key] = redact(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redact(item)
		}
		return out
	case json.RawMessage:
		var decoded any
		if json.Unmarshal(typed, &decoded) == nil {
			return redact(decoded)
		}
		return nil
	default:
		return typed
	}
}

func secretKey(key string) bool {
	lower := strings.ToLower(key)
	for _, marker := range []string{"secret", "token", "password", "credential", "private_key", "apikey", "api_key"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func canonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	if err := encodeCanonical(&buffer, value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func encodeCanonical(buffer *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case map[string]any:
		buffer.WriteByte('{')
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for i, key := range keys {
			if i > 0 {
				buffer.WriteByte(',')
			}
			keyRaw, _ := json.Marshal(key)
			buffer.Write(keyRaw)
			buffer.WriteByte(':')
			if err := encodeCanonical(buffer, typed[key]); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	case []any:
		buffer.WriteByte('[')
		for i, item := range typed {
			if i > 0 {
				buffer.WriteByte(',')
			}
			if err := encodeCanonical(buffer, item); err != nil {
				return err
			}
		}
		buffer.WriteByte(']')
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		buffer.Write(raw)
	}
	return nil
}
