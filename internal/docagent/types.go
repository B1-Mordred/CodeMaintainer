package docagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const SchemaVersion = 1

var (
	safeID    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	sha256Hex = regexp.MustCompile(`^[a-f0-9]{64}$`)
	gitSHA    = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)
)

type Requirement struct {
	ID            string   `json:"id"`
	Document      string   `json:"document"`
	Reason        string   `json:"reason"`
	Source        string   `json:"source"`
	RenderTargets []string `json:"render_targets"`
	Required      bool     `json:"required"`
	Status        string   `json:"status"`
}

type Change struct {
	Path              string   `json:"path"`
	Action            string   `json:"action"`
	PolicyRule        string   `json:"policy_rule"`
	SourceOfTruth     string   `json:"source_of_truth"`
	LinkedEvidenceIDs []string `json:"linked_evidence_ids"`
}

type Check struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Target  string `json:"target"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type UnsupportedClaim struct {
	Claim    string `json:"claim"`
	Location string `json:"location"`
	Reason   string `json:"reason"`
}

type Edit struct {
	Path           string `json:"path"`
	Content        string `json:"content"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
}

type Manifest struct {
	ID                string             `json:"id"`
	JobID             string             `json:"job_id"`
	SchemaVersion     int                `json:"schema_version"`
	ProjectID         string             `json:"project_id"`
	ContractSHA256    string             `json:"contract_sha256"`
	RiskLevel         string             `json:"risk_level"`
	ResultSHA         string             `json:"result_sha"`
	SourceContext     string             `json:"source_context"`
	PolicyVersion     string             `json:"policy_version"`
	Requirements      []Requirement      `json:"requirements"`
	Changes           []Change           `json:"changes"`
	Checks            []Check            `json:"checks"`
	UnsupportedClaims []UnsupportedClaim `json:"unsupported_claims"`
	Edits             []Edit             `json:"edits"`
	Status            string             `json:"status"`
	PolicySummary     string             `json:"policy_summary"`
	CreatedAt         time.Time          `json:"created_at"`
}

type PolicyProfile struct {
	ID          string       `json:"id"`
	Version     string       `json:"version"`
	Summary     string       `json:"summary"`
	Rules       []PolicyRule `json:"rules"`
	ToolProfile ToolProfile  `json:"tool_profile"`
}

type ToolProfile struct {
	ID            string   `json:"id"`
	RenderTargets []string `json:"render_targets"`
	Checks        []string `json:"checks"`
	PreviewModes  []string `json:"preview_modes"`
}

type PolicyRule struct {
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	Description       string      `json:"description"`
	Match             PolicyMatch `json:"match"`
	Documents         []string    `json:"documents"`
	RenderTargets     []string    `json:"render_targets"`
	Checks            []string    `json:"checks"`
	ReviewerRoles     []string    `json:"reviewer_roles"`
	PublicationGate   bool        `json:"publication_gate"`
	SourceOfTruth     string      `json:"source_of_truth"`
	PublicationTarget string      `json:"publication_target"`
}

type PolicyMatch struct {
	Paths           []string `json:"paths"`
	ChangeClasses   []string `json:"change_classes"`
	RiskLevels      []string `json:"risk_levels"`
	Languages       []string `json:"languages"`
	CapabilityPacks []string `json:"capability_packs"`
	Labels          []string `json:"labels"`
}

type PolicySimulationInput struct {
	Paths           []string `json:"paths"`
	ChangeClasses   []string `json:"change_classes"`
	RiskLevel       string   `json:"risk_level"`
	Languages       []string `json:"languages"`
	CapabilityPacks []string `json:"capability_packs"`
	Labels          []string `json:"labels"`
}

type PolicySimulationResult struct {
	ProfileID         string        `json:"profile_id"`
	ProfileVersion    string        `json:"profile_version"`
	MatchedRules      []PolicyRule  `json:"matched_rules"`
	Requirements      []Requirement `json:"requirements"`
	Checks            []Check       `json:"checks"`
	RenderTargets     []string      `json:"render_targets"`
	ReviewerRoles     []string      `json:"reviewer_roles"`
	PublicationGates  []string      `json:"publication_gates"`
	SourceMappings    []Change      `json:"source_mappings"`
	PolicySummary     string        `json:"policy_summary"`
	PublicationReady  bool          `json:"publication_ready"`
	NoDocumentationOK bool          `json:"no_documentation_required"`
}

type Store interface {
	SaveDocumentationManifest(context.Context, Manifest) (Manifest, error)
	ListDocumentationManifests(context.Context, string, int) ([]Manifest, error)
}

func BuiltinPolicyProfile() PolicyProfile {
	return PolicyProfile{
		ID:      "documentation-policy-default",
		Version: "documentation-policy-v1",
		Summary: "Controller-owned default documentation policy maps API, CLI, configuration, database, UI, security, risk, and capability-pack evidence to required documentation checks.",
		ToolProfile: ToolProfile{
			ID:            "documentation-tool-profile-ci-safe",
			RenderTargets: []string{"markdown", "html_preview", "openapi_reference"},
			Checks:        []string{"markdown_render", "links", "anchors", "openapi_schema", "source_truth_mapping", "unsupported_claims"},
			PreviewModes:  []string{"side_by_side_diff", "rendered_markdown", "artifact_download"},
		},
		Rules: []PolicyRule{
			{
				ID: "DOC-PUBLIC-API", Name: "Public API reference", Description: "OpenAPI, REST handler, or schema changes require API documentation and generated-reference checks.",
				Match:     PolicyMatch{Paths: []string{"internal/api/", "internal/api/openapi.yaml"}, ChangeClasses: []string{"public_api", "api_schema", "openapi"}},
				Documents: []string{"docs/api.md"}, RenderTargets: []string{"markdown", "openapi_reference"},
				Checks: []string{"openapi_schema", "links", "source_truth_mapping"}, ReviewerRoles: []string{"reviewer", "administrator"},
				PublicationGate: true, SourceOfTruth: "internal/api/openapi.yaml", PublicationTarget: "operator_documentation",
			},
			{
				ID: "DOC-CLI", Name: "CLI reference", Description: "CLI command, flag, or automation behavior changes require operator CLI documentation.",
				Match:     PolicyMatch{Paths: []string{"cmd/maintainctl/"}, ChangeClasses: []string{"cli", "automation"}},
				Documents: []string{"docs/cli.md"}, RenderTargets: []string{"markdown"},
				Checks: []string{"markdown_render", "links", "source_truth_mapping"}, ReviewerRoles: []string{"reviewer"},
				PublicationGate: true, SourceOfTruth: "cmd/maintainctl/main.go", PublicationTarget: "operator_documentation",
			},
			{
				ID: "DOC-CONFIG", Name: "Configuration reference", Description: "Configuration registry, schema, or workbench changes require configuration reference updates.",
				Match:     PolicyMatch{Paths: []string{"internal/config/", "web/src/ConfigurationPage"}, ChangeClasses: []string{"configuration", "config_schema"}},
				Documents: []string{"docs/configuration.md"}, RenderTargets: []string{"markdown"},
				Checks: []string{"markdown_render", "links", "source_truth_mapping"}, ReviewerRoles: []string{"reviewer", "administrator"},
				PublicationGate: true, SourceOfTruth: "internal/config/builtin_registry.go", PublicationTarget: "operator_documentation",
			},
			{
				ID: "DOC-DATABASE", Name: "Migration and recovery notes", Description: "Database migration or durable-state changes require migration and recovery documentation evidence.",
				Match:     PolicyMatch{Paths: []string{"internal/storage/sqlite/migrations/", "internal/storage/sqlite/"}, ChangeClasses: []string{"database", "migration", "backup_restore"}},
				Documents: []string{"docs/migrations.md"}, RenderTargets: []string{"markdown"},
				Checks: []string{"markdown_render", "links", "source_truth_mapping"}, ReviewerRoles: []string{"administrator"},
				PublicationGate: true, SourceOfTruth: "internal/storage/sqlite/migrations", PublicationTarget: "operator_documentation",
			},
			{
				ID: "DOC-UI", Name: "Operator console workflow", Description: "Operator-facing UI workflow changes require user/admin workflow documentation and accessibility notes.",
				Match:     PolicyMatch{Paths: []string{"web/src/", "internal/ui/"}, ChangeClasses: []string{"ui", "operator_workflow", "accessibility"}},
				Documents: []string{"docs/operator-console.md"}, RenderTargets: []string{"markdown", "html_preview"},
				Checks: []string{"markdown_render", "links", "accessibility_notes", "source_truth_mapping"}, ReviewerRoles: []string{"reviewer"},
				PublicationGate: true, SourceOfTruth: "web/src", PublicationTarget: "operator_documentation",
			},
			{
				ID: "DOC-SECURITY", Name: "Security boundary record", Description: "Security, runner, credential, protected-action, or network boundary changes require security and architecture documentation.",
				Match:     PolicyMatch{Paths: []string{"internal/runnerd/", "internal/auth/", "internal/policy/"}, ChangeClasses: []string{"security", "runner_boundary", "protected_action", "egress"}},
				Documents: []string{"docs/security.md", "docs/architecture.md"}, RenderTargets: []string{"markdown"},
				Checks: []string{"markdown_render", "links", "source_truth_mapping", "unsupported_claims"}, ReviewerRoles: []string{"administrator"},
				PublicationGate: true, SourceOfTruth: "project.md", PublicationTarget: "operator_documentation",
			},
			{
				ID: "DOC-RISK", Name: "Risk and quality workflow", Description: "Medium or high risk jobs require quality workflow and release/readiness evidence even when no narrower rule matches.",
				Match:     PolicyMatch{RiskLevels: []string{"medium", "high"}, ChangeClasses: []string{"risk", "quality_gate"}},
				Documents: []string{"docs/quality-workflow.md"}, RenderTargets: []string{"markdown"},
				Checks: []string{"markdown_render", "links", "source_truth_mapping"}, ReviewerRoles: []string{"reviewer"},
				PublicationGate: true, SourceOfTruth: "LOCAL_CODE_MAINTAINER_INCREMENT_2_GOAL.md", PublicationTarget: "operator_documentation",
			},
		},
	}
}

func SimulatePolicy(input PolicySimulationInput) (PolicySimulationResult, error) {
	profile := BuiltinPolicyProfile()
	if err := input.Validate(); err != nil {
		return PolicySimulationResult{}, err
	}
	result := PolicySimulationResult{
		ProfileID: profile.ID, ProfileVersion: profile.Version, MatchedRules: []PolicyRule{},
		Requirements: []Requirement{}, Checks: []Check{}, RenderTargets: []string{},
		ReviewerRoles: []string{}, PublicationGates: []string{}, SourceMappings: []Change{},
		PublicationReady: true, NoDocumentationOK: true,
	}
	seenDocuments, seenChecks, seenTargets, seenRoles, seenGates := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	for _, rule := range profile.Rules {
		if !rule.Matches(input) {
			continue
		}
		result.MatchedRules = append(result.MatchedRules, rule)
		result.NoDocumentationOK = false
		if rule.PublicationGate {
			result.PublicationReady = false
			seenGates[rule.ID] = struct{}{}
		}
		for _, role := range rule.ReviewerRoles {
			if _, ok := seenRoles[role]; !ok {
				seenRoles[role] = struct{}{}
				result.ReviewerRoles = append(result.ReviewerRoles, role)
			}
		}
		for _, target := range rule.RenderTargets {
			if _, ok := seenTargets[target]; !ok {
				seenTargets[target] = struct{}{}
				result.RenderTargets = append(result.RenderTargets, target)
			}
		}
		for _, check := range rule.Checks {
			if _, ok := seenChecks[check]; !ok {
				seenChecks[check] = struct{}{}
				result.Checks = append(result.Checks, Check{
					ID:   "DOC-CHECK-" + strings.ToUpper(strings.ReplaceAll(check, "_", "-")),
					Kind: check, Target: "documentation_policy", Status: "not_run",
					Summary: "policy simulation requires " + check + " before publication readiness",
				})
			}
		}
		for _, document := range rule.Documents {
			if _, ok := seenDocuments[document]; ok {
				continue
			}
			seenDocuments[document] = struct{}{}
			result.Requirements = append(result.Requirements, Requirement{
				ID: "DOC-REQ-" + strings.TrimPrefix(rule.ID, "DOC-"), Document: document,
				Reason: rule.Description, Source: rule.ID, RenderTargets: append([]string(nil), rule.RenderTargets...),
				Required: true, Status: "pending",
			})
			result.SourceMappings = append(result.SourceMappings, Change{
				Path: document, Action: "not_changed", PolicyRule: rule.ID, SourceOfTruth: rule.SourceOfTruth,
				LinkedEvidenceIDs: []string{"documentation_policy_simulation"},
			})
		}
	}
	for id := range seenGates {
		result.PublicationGates = append(result.PublicationGates, id)
	}
	if result.NoDocumentationOK {
		result.PolicySummary = "documentation policy simulation found no required documentation update for the supplied evidence"
	} else {
		result.PolicySummary = fmt.Sprintf("documentation policy simulation matched %d rule(s), requiring %d document(s), %d check(s), and %d render target(s)", len(result.MatchedRules), len(result.Requirements), len(result.Checks), len(result.RenderTargets))
	}
	return result, result.Validate()
}

func DecodeManifest(payload []byte, jobID, projectID, contractSHA256, riskLevel, resultSHA string) (Manifest, error) {
	if len(payload) == 0 || len(payload) > 4<<20 {
		return Manifest{}, errors.New("documentation manifest is empty or oversized")
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Manifest{}, errors.New("documentation manifest contains trailing data")
	}
	if manifest.JobID != jobID || manifest.ProjectID != projectID || manifest.ContractSHA256 != contractSHA256 ||
		manifest.RiskLevel != riskLevel || manifest.ResultSHA != resultSHA {
		return Manifest{}, errors.New("documentation manifest is not bound to the approved contract, risk, project, and result commit")
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func NoDocumentationRequired(jobID, projectID, contractSHA256, riskLevel, resultSHA string) (Manifest, error) {
	manifest := Manifest{
		JobID: jobID, ProjectID: projectID, SchemaVersion: SchemaVersion,
		ContractSHA256: contractSHA256, RiskLevel: riskLevel, ResultSHA: resultSHA,
		SourceContext: "documentation_policy_v1", PolicyVersion: "documentation-policy-v1",
		Requirements: []Requirement{}, Changes: []Change{}, Checks: []Check{{
			ID: "DOC-CHECK-NONE", Kind: "policy", Target: "change-impact", Status: "passed",
			Summary: "controller policy found no required documentation update for this exact candidate",
		}},
		UnsupportedClaims: []UnsupportedClaim{}, Edits: []Edit{}, Status: "no_documentation_required",
		PolicySummary: "no documentation update was required by the registered documentation policy for this exact candidate",
	}
	return manifest, manifest.Validate()
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion || !safeID.MatchString(m.JobID) || !safeID.MatchString(m.ProjectID) ||
		!sha256Hex.MatchString(m.ContractSHA256) || !gitSHA.MatchString(m.ResultSHA) ||
		(m.RiskLevel != "low" && m.RiskLevel != "medium" && m.RiskLevel != "high") ||
		strings.TrimSpace(m.SourceContext) == "" || strings.TrimSpace(m.PolicyVersion) == "" ||
		strings.TrimSpace(m.PolicySummary) == "" || len(m.Requirements) > 200 || len(m.Changes) > 200 ||
		len(m.Checks) > 200 || len(m.UnsupportedClaims) > 200 || len(m.Edits) > 64 {
		return errors.New("documentation manifest violates version-one bounds")
	}
	if m.Status != "passed" && m.Status != "changes_applied" && m.Status != "no_documentation_required" && m.Status != "blocked" {
		return errors.New("invalid documentation manifest status")
	}
	if len(m.UnsupportedClaims) != 0 && m.Status != "blocked" {
		return errors.New("unsupported documentation claims must block")
	}
	if len(m.Requirements) == 0 && m.Status != "no_documentation_required" {
		return errors.New("empty documentation policy result must be no_documentation_required")
	}
	if len(m.Requirements) != 0 && m.Status == "no_documentation_required" {
		return errors.New("required documentation cannot be marked unnecessary")
	}
	if len(m.Changes) != 0 && m.Status != "changes_applied" && m.Status != "passed" {
		return errors.New("documentation changes require a passing or applied status")
	}
	for _, requirement := range m.Requirements {
		if err := requirement.Validate(); err != nil {
			return err
		}
	}
	for _, change := range m.Changes {
		if err := change.Validate(); err != nil {
			return err
		}
	}
	for _, check := range m.Checks {
		if err := check.Validate(); err != nil {
			return err
		}
	}
	for _, claim := range m.UnsupportedClaims {
		if strings.TrimSpace(claim.Claim) == "" || strings.TrimSpace(claim.Location) == "" ||
			strings.TrimSpace(claim.Reason) == "" || len(claim.Claim)+len(claim.Location)+len(claim.Reason) > 16000 {
			return errors.New("unsupported documentation claim lacks bounded evidence")
		}
	}
	for _, edit := range m.Edits {
		if !safeRelativePath(edit.Path) || len(edit.Content) > 1<<20 ||
			(edit.ExpectedSHA256 != "" && !sha256Hex.MatchString(edit.ExpectedSHA256)) {
			return errors.New("documentation edit is invalid")
		}
	}
	return nil
}

func (r Requirement) Validate() error {
	if !safeID.MatchString(r.ID) || !safeRelativePath(r.Document) || strings.TrimSpace(r.Reason) == "" ||
		strings.TrimSpace(r.Source) == "" || len(r.RenderTargets) > 16 ||
		(r.Status != "satisfied" && r.Status != "pending" && r.Status != "not_required" && r.Status != "blocked") {
		return errors.New("documentation requirement lacks stable policy evidence")
	}
	if r.Required && r.Status == "not_required" {
		return errors.New("required documentation cannot be not_required")
	}
	for _, target := range r.RenderTargets {
		if !safeID.MatchString(target) {
			return errors.New("documentation render target is invalid")
		}
	}
	return nil
}

func (c Change) Validate() error {
	if !safeRelativePath(c.Path) || strings.TrimSpace(c.PolicyRule) == "" ||
		strings.TrimSpace(c.SourceOfTruth) == "" || len(c.LinkedEvidenceIDs) > 32 ||
		(c.Action != "created" && c.Action != "updated" && c.Action != "not_changed") {
		return errors.New("documentation change lacks path, action, policy rule, or source of truth")
	}
	for _, evidence := range c.LinkedEvidenceIDs {
		if !safeID.MatchString(evidence) {
			return errors.New("documentation evidence reference is invalid")
		}
	}
	return nil
}

func (c Check) Validate() error {
	if !safeID.MatchString(c.ID) || strings.TrimSpace(c.Kind) == "" || strings.TrimSpace(c.Target) == "" ||
		strings.TrimSpace(c.Summary) == "" || (c.Status != "passed" && c.Status != "failed" && c.Status != "not_run") {
		return errors.New("documentation check lacks bounded status evidence")
	}
	return nil
}

func (p PolicyProfile) Validate() error {
	if !safeID.MatchString(p.ID) || strings.TrimSpace(p.Version) == "" || strings.TrimSpace(p.Summary) == "" ||
		len(p.Rules) == 0 || len(p.Rules) > 100 || p.ToolProfile.ID == "" {
		return errors.New("documentation policy profile is invalid")
	}
	if err := p.ToolProfile.Validate(); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, rule := range p.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
		if _, ok := seen[rule.ID]; ok {
			return errors.New("documentation policy rule IDs must be unique")
		}
		seen[rule.ID] = struct{}{}
	}
	return nil
}

func (t ToolProfile) Validate() error {
	if !safeID.MatchString(t.ID) || len(t.RenderTargets) == 0 || len(t.RenderTargets) > 32 ||
		len(t.Checks) == 0 || len(t.Checks) > 64 || len(t.PreviewModes) > 16 {
		return errors.New("documentation tool profile is invalid")
	}
	for _, values := range [][]string{t.RenderTargets, t.Checks, t.PreviewModes} {
		for _, value := range values {
			if !safeID.MatchString(value) {
				return errors.New("documentation tool profile contains an invalid identifier")
			}
		}
	}
	return nil
}

func (r PolicyRule) Validate() error {
	if !safeID.MatchString(r.ID) || strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Description) == "" ||
		len(r.Documents) == 0 || len(r.Documents) > 16 || len(r.RenderTargets) == 0 || len(r.RenderTargets) > 16 ||
		len(r.Checks) == 0 || len(r.Checks) > 32 || len(r.ReviewerRoles) == 0 || len(r.ReviewerRoles) > 8 ||
		strings.TrimSpace(r.SourceOfTruth) == "" || strings.TrimSpace(r.PublicationTarget) == "" {
		return errors.New("documentation policy rule is invalid")
	}
	if err := r.Match.Validate(); err != nil {
		return err
	}
	for _, document := range r.Documents {
		if !safeRelativePath(document) {
			return errors.New("documentation policy rule document path is invalid")
		}
	}
	for _, values := range [][]string{r.RenderTargets, r.Checks, r.ReviewerRoles} {
		for _, value := range values {
			if !safeID.MatchString(value) {
				return errors.New("documentation policy rule contains an invalid identifier")
			}
		}
	}
	return nil
}

func (m PolicyMatch) Validate() error {
	if len(m.Paths)+len(m.ChangeClasses)+len(m.RiskLevels)+len(m.Languages)+len(m.CapabilityPacks)+len(m.Labels) == 0 ||
		len(m.Paths) > 32 || len(m.ChangeClasses) > 32 || len(m.RiskLevels) > 3 || len(m.Languages) > 32 ||
		len(m.CapabilityPacks) > 32 || len(m.Labels) > 32 {
		return errors.New("documentation policy match is invalid")
	}
	for _, path := range m.Paths {
		if strings.TrimSpace(path) == "" || strings.Contains(path, "\x00") || filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") {
			return errors.New("documentation policy match path is invalid")
		}
	}
	for _, level := range m.RiskLevels {
		if level != "low" && level != "medium" && level != "high" {
			return errors.New("documentation policy match risk level is invalid")
		}
	}
	for _, values := range [][]string{m.ChangeClasses, m.Languages, m.CapabilityPacks, m.Labels} {
		for _, value := range values {
			if !safeID.MatchString(value) {
				return errors.New("documentation policy match contains an invalid identifier")
			}
		}
	}
	return nil
}

func (r PolicyRule) Matches(input PolicySimulationInput) bool {
	return matchesPath(r.Match.Paths, input.Paths) ||
		intersects(r.Match.ChangeClasses, input.ChangeClasses) ||
		contains(r.Match.RiskLevels, input.RiskLevel) ||
		intersects(r.Match.Languages, input.Languages) ||
		intersects(r.Match.CapabilityPacks, input.CapabilityPacks) ||
		intersects(r.Match.Labels, input.Labels)
}

func (i PolicySimulationInput) Validate() error {
	if i.RiskLevel != "" && i.RiskLevel != "low" && i.RiskLevel != "medium" && i.RiskLevel != "high" {
		return errors.New("documentation policy simulation risk level is invalid")
	}
	if len(i.Paths)+len(i.ChangeClasses)+len(i.Languages)+len(i.CapabilityPacks)+len(i.Labels) == 0 && i.RiskLevel == "" {
		return errors.New("documentation policy simulation requires bounded change evidence")
	}
	if len(i.Paths) > 128 || len(i.ChangeClasses) > 64 || len(i.Languages) > 32 || len(i.CapabilityPacks) > 32 || len(i.Labels) > 64 {
		return errors.New("documentation policy simulation input is oversized")
	}
	for _, path := range i.Paths {
		if !safeRelativePath(path) {
			return errors.New("documentation policy simulation path is invalid")
		}
	}
	for _, values := range [][]string{i.ChangeClasses, i.Languages, i.CapabilityPacks, i.Labels} {
		for _, value := range values {
			if !safeID.MatchString(value) {
				return errors.New("documentation policy simulation contains an invalid identifier")
			}
		}
	}
	return nil
}

func (r PolicySimulationResult) Validate() error {
	if !safeID.MatchString(r.ProfileID) || strings.TrimSpace(r.ProfileVersion) == "" ||
		strings.TrimSpace(r.PolicySummary) == "" || len(r.MatchedRules) > 100 || len(r.Requirements) > 200 ||
		len(r.Checks) > 200 || len(r.RenderTargets) > 64 || len(r.ReviewerRoles) > 32 || len(r.PublicationGates) > 100 ||
		len(r.SourceMappings) > 200 {
		return errors.New("documentation policy simulation result is invalid")
	}
	for _, rule := range r.MatchedRules {
		if err := rule.Validate(); err != nil {
			return err
		}
	}
	for _, requirement := range r.Requirements {
		if err := requirement.Validate(); err != nil {
			return err
		}
	}
	for _, check := range r.Checks {
		if err := check.Validate(); err != nil {
			return err
		}
	}
	for _, mapping := range r.SourceMappings {
		if err := mapping.Validate(); err != nil {
			return err
		}
	}
	for _, values := range [][]string{r.RenderTargets, r.ReviewerRoles, r.PublicationGates} {
		for _, value := range values {
			if !safeID.MatchString(value) {
				return errors.New("documentation policy simulation result contains an invalid identifier")
			}
		}
	}
	return nil
}

func matchesPath(patterns, paths []string) bool {
	for _, pattern := range patterns {
		for _, path := range paths {
			if strings.HasPrefix(path, pattern) || path == strings.TrimSuffix(pattern, "/") {
				return true
			}
		}
	}
	return false
}

func intersects(left, right []string) bool {
	for _, item := range left {
		if contains(right, item) {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	if want == "" {
		return false
	}
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func safeRelativePath(path string) bool {
	clean := filepath.Clean(path)
	return path != "" && path == clean && clean != "." && clean != ".." && !filepath.IsAbs(clean) &&
		!strings.HasPrefix(clean, ".."+string(filepath.Separator)) && !strings.ContainsRune(clean, 0)
}
